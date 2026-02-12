// Package jobqueue provides a Redis-backed job queue with priority levels and scheduling.
package jobqueue

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// Queue manages job queuing using Redis.
type Queue struct {
	client     *redis.Client
	namespace  string
	queues     map[Priority]string
	deadLetter string
}

// Priority defines job priority levels.
type Priority int

const (
	LOW Priority = iota
	NORMAL
	HIGH
	CRITICAL
)

// Job represents a unit of work to be processed.
type Job struct {
	ID           string                 `json:"id"`
	Type         string                 `json:"type"`
	Payload      map[string]interface{} `json:"payload"`
	Priority     Priority               `json:"priority"`
	CreatedAt    time.Time              `json:"created_at"`
	ScheduledFor *time.Time             `json:"scheduled_for,omitempty"`
	Attempts     int                    `json:"attempts"`
	MaxAttempts  int                    `json:"max_attempts"`
}

// JobResult represents the outcome of job processing.
type JobResult struct {
	JobID     string
	Success   bool
	Error     error
	Processed time.Time
}

// Handler processes jobs of a specific type.
type Handler func(ctx context.Context, job Job) error

// Config defines queue configuration.
type Config struct {
	Namespace string
}

// New creates a new job queue.
func New(client *redis.Client, cfg Config) *Queue {
	ns := cfg.Namespace
	if ns == "" {
		ns = "jobs"
	}

	return &Queue{
		client:    client,
		namespace: ns,
		queues: map[Priority]string{
			LOW:      fmt.Sprintf("%s:queue:low", ns),
			NORMAL:   fmt.Sprintf("%s:queue:normal", ns),
			HIGH:     fmt.Sprintf("%s:queue:high", ns),
			CRITICAL: fmt.Sprintf("%s:queue:critical", ns),
		},
		deadLetter: fmt.Sprintf("%s:deadletter", ns),
	}
}

// Enqueue adds a job to the queue.
func (q *Queue) Enqueue(ctx context.Context, jobType string, payload map[string]interface{}, priority Priority) (*Job, error) {
	return q.EnqueueWithOptions(ctx, jobType, payload, priority, EnqueueOptions{})
}

// EnqueueOptions configures enqueue behavior.
type EnqueueOptions struct {
	MaxAttempts int
	ScheduleFor *time.Time
}

// EnqueueWithOptions adds a job with custom options.
func (q *Queue) EnqueueWithOptions(ctx context.Context, jobType string, payload map[string]interface{}, priority Priority, opts EnqueueOptions) (*Job, error) {
	if q.client == nil {
		return nil, fmt.Errorf("redis client is nil")
	}

	maxAttempts := opts.MaxAttempts
	if maxAttempts == 0 {
		maxAttempts = 3
	}

	job := Job{
		ID:          uuid.NewString(),
		Type:        jobType,
		Payload:     payload,
		Priority:    priority,
		CreatedAt:   time.Now(),
		Attempts:    0,
		MaxAttempts: maxAttempts,
	}

	if opts.ScheduleFor != nil {
		job.ScheduledFor = opts.ScheduleFor
	}

	data, err := json.Marshal(job)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal job: %w", err)
	}

	queueName := q.queues[priority]

	if job.ScheduledFor != nil {
		scheduledQueue := fmt.Sprintf("%s:scheduled", q.namespace)
		score := job.ScheduledFor.Unix()
		if err := q.client.ZAdd(ctx, scheduledQueue, redis.Z{Score: float64(score), Member: data}).Err(); err != nil {
			return nil, fmt.Errorf("failed to schedule job: %w", err)
		}
	} else {
		if err := q.client.LPush(ctx, queueName, data).Err(); err != nil {
			return nil, fmt.Errorf("failed to enqueue job: %w", err)
		}
	}

	return &job, nil
}

// Dequeue retrieves the next job from the queue.
func (q *Queue) Dequeue(ctx context.Context) (*Job, error) {
	if q.client == nil {
		return nil, fmt.Errorf("redis client is nil")
	}

	if err := q.processScheduled(ctx); err != nil {
		return nil, err
	}

	priorities := []Priority{CRITICAL, HIGH, NORMAL, LOW}
	for _, priority := range priorities {
		queueName := q.queues[priority]
		result, err := q.client.RPop(ctx, queueName).Result()
		if err == redis.Nil {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("failed to dequeue: %w", err)
		}

		var job Job
		if err := json.Unmarshal([]byte(result), &job); err != nil {
			return nil, fmt.Errorf("failed to unmarshal job: %w", err)
		}

		job.Attempts++
		return &job, nil
	}

	return nil, nil
}

// DequeueWithTimeout retrieves a job with a timeout.
func (q *Queue) DequeueWithTimeout(ctx context.Context, timeout time.Duration) (*Job, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return q.Dequeue(ctx)
}

// Complete marks a job as completed.
func (q *Queue) Complete(ctx context.Context, job *Job) error {
	return nil
}

// Fail marks a job as failed and potentially moves it to dead letter queue.
func (q *Queue) Fail(ctx context.Context, job *Job, err error) error {
	if job.Attempts < job.MaxAttempts {
		if job.Payload == nil {
			job.Payload = make(map[string]interface{})
		}
		job.Payload["_error"] = err.Error()
		data, marshalErr := json.Marshal(job)
		if marshalErr != nil {
			return fmt.Errorf("failed to marshal job for retry: %w", marshalErr)
		}

		queueName := q.queues[job.Priority]
		if pushErr := q.client.RPush(ctx, queueName, data).Err(); pushErr != nil {
			return fmt.Errorf("failed to requeue job: %w", pushErr)
		}

		return nil
	}

	data, marshalErr := json.Marshal(map[string]interface{}{
		"job":       job,
		"error":     err.Error(),
		"failed_at": time.Now(),
	})
	if marshalErr != nil {
		return fmt.Errorf("failed to marshal dead letter: %w", marshalErr)
	}

	if pushErr := q.client.LPush(ctx, q.deadLetter, data).Err(); pushErr != nil {
		return fmt.Errorf("failed to add to dead letter: %w", pushErr)
	}

	return nil
}

// GetQueueDepth returns the number of jobs in each queue.
func (q *Queue) GetQueueDepth(ctx context.Context) (map[Priority]int64, error) {
	depths := make(map[Priority]int64)

	for priority, queueName := range q.queues {
		length, err := q.client.LLen(ctx, queueName).Result()
		if err != nil {
			return nil, fmt.Errorf("failed to get depth for %s: %w", queueName, err)
		}
		depths[priority] = length
	}

	return depths, nil
}

// GetDeadLetterDepth returns the number of jobs in the dead letter queue.
func (q *Queue) GetDeadLetterDepth(ctx context.Context) (int64, error) {
	return q.client.LLen(ctx, q.deadLetter).Result()
}

// PeekDeadLetter returns jobs from the dead letter queue without removing them.
func (q *Queue) PeekDeadLetter(ctx context.Context, count int64) ([]map[string]interface{}, error) {
	items, err := q.client.LRange(ctx, q.deadLetter, 0, count-1).Result()
	if err != nil {
		return nil, err
	}

	var jobs []map[string]interface{}
	for _, item := range items {
		var job map[string]interface{}
		if err := json.Unmarshal([]byte(item), &job); err != nil {
			continue
		}
		jobs = append(jobs, job)
	}

	return jobs, nil
}

// ClearDeadLetter removes all jobs from the dead letter queue.
func (q *Queue) ClearDeadLetter(ctx context.Context) error {
	return q.client.Del(ctx, q.deadLetter).Err()
}

// RetryDeadLetter moves a job from dead letter back to its original queue.
func (q *Queue) RetryDeadLetter(ctx context.Context, index int64) error {
	item, err := q.client.LIndex(ctx, q.deadLetter, index).Result()
	if err != nil {
		return fmt.Errorf("failed to get dead letter item: %w", err)
	}

	var deadLetter struct {
		Job Job `json:"job"`
	}
	if err := json.Unmarshal([]byte(item), &deadLetter); err != nil {
		return fmt.Errorf("failed to unmarshal dead letter: %w", err)
	}

	job := deadLetter.Job
	job.Attempts = 0

	data, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("failed to marshal job: %w", err)
	}

	queueName := q.queues[job.Priority]
	if err := q.client.LPush(ctx, queueName, data).Err(); err != nil {
		return fmt.Errorf("failed to requeue job: %w", err)
	}

	if err := q.client.LRem(ctx, q.deadLetter, 1, item).Err(); err != nil {
		return fmt.Errorf("failed to remove from dead letter: %w", err)
	}

	return nil
}

func (q *Queue) processScheduled(ctx context.Context) error {
	scheduledQueue := fmt.Sprintf("%s:scheduled", q.namespace)
	now := float64(time.Now().Unix())

	items, err := q.client.ZRangeByScore(ctx, scheduledQueue, &redis.ZRangeBy{
		Min: "0",
		Max: fmt.Sprintf("%f", now),
	}).Result()
	if err != nil {
		return err
	}

	for _, item := range items {
		var job Job
		if err := json.Unmarshal([]byte(item), &job); err != nil {
			continue
		}

		queueName := q.queues[job.Priority]
		if err := q.client.LPush(ctx, queueName, item).Err(); err != nil {
			continue
		}

		if err := q.client.ZRem(ctx, scheduledQueue, item).Err(); err != nil {
			continue
		}
	}

	return nil
}

// PriorityFromString converts a string to Priority.
func PriorityFromString(s string) Priority {
	switch s {
	case "critical":
		return CRITICAL
	case "high":
		return HIGH
	case "normal":
		return NORMAL
	case "low":
		return LOW
	default:
		return NORMAL
	}
}

// String returns the string representation of Priority.
func (p Priority) String() string {
	switch p {
	case CRITICAL:
		return "critical"
	case HIGH:
		return "high"
	case NORMAL:
		return "normal"
	case LOW:
		return "low"
	default:
		return "unknown"
	}
}
