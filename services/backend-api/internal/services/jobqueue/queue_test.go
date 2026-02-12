package jobqueue

import (
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()

	client := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer func() { _ = client.Close() }()

	queue := New(client, Config{Namespace: "test"})
	require.NotNil(t, queue)
	assert.Equal(t, "test", queue.namespace)
	assert.NotNil(t, queue.queues)
}

func TestQueue_EnqueueDequeue(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()

	client := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer func() { _ = client.Close() }()

	queue := New(client, Config{Namespace: "test"})
	ctx := t.Context()

	payload := map[string]interface{}{"key": "value"}
	job, err := queue.Enqueue(ctx, "test-job", payload, NORMAL)
	require.NoError(t, err)
	require.NotNil(t, job)
	assert.Equal(t, "test-job", job.Type)
	assert.Equal(t, NORMAL, job.Priority)
	assert.Equal(t, payload, job.Payload)
	assert.NotEmpty(t, job.ID)

	dequeued, err := queue.DequeueWithTimeout(ctx, 2*time.Second)
	require.NoError(t, err)
	require.NotNil(t, dequeued)
	assert.Equal(t, job.ID, dequeued.ID)
	assert.Equal(t, 1, dequeued.Attempts)
}

func TestQueue_PriorityOrdering(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()

	client := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer func() { _ = client.Close() }()

	queue := New(client, Config{Namespace: "test"})
	ctx := t.Context()

	_, err := queue.Enqueue(ctx, "low-job", nil, LOW)
	require.NoError(t, err)

	_, err = queue.Enqueue(ctx, "high-job", nil, HIGH)
	require.NoError(t, err)

	_, err = queue.Enqueue(ctx, "critical-job", nil, CRITICAL)
	require.NoError(t, err)

	job1, err := queue.DequeueWithTimeout(ctx, 2*time.Second)
	require.NoError(t, err)
	assert.Equal(t, "critical-job", job1.Type)

	job2, err := queue.DequeueWithTimeout(ctx, 2*time.Second)
	require.NoError(t, err)
	assert.Equal(t, "high-job", job2.Type)

	job3, err := queue.DequeueWithTimeout(ctx, 2*time.Second)
	require.NoError(t, err)
	assert.Equal(t, "low-job", job3.Type)
}

func TestQueue_FailAndRetry(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()

	client := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer func() { _ = client.Close() }()

	queue := New(client, Config{Namespace: "test"})
	ctx := t.Context()

	job, err := queue.EnqueueWithOptions(ctx, "retry-job", nil, NORMAL, EnqueueOptions{MaxAttempts: 2})
	require.NoError(t, err)
	require.NotNil(t, job)

	dequeued, err := queue.DequeueWithTimeout(ctx, 2*time.Second)
	require.NoError(t, err)

	err = queue.Fail(ctx, dequeued, assert.AnError)
	require.NoError(t, err)

	retried, err := queue.DequeueWithTimeout(ctx, 2*time.Second)
	require.NoError(t, err)
	require.NotNil(t, retried)
	assert.Equal(t, job.ID, retried.ID)
	assert.Equal(t, 2, retried.Attempts)

	err = queue.Fail(ctx, retried, assert.AnError)
	require.NoError(t, err)

	deadLetterDepth, err := queue.GetDeadLetterDepth(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(1), deadLetterDepth)
}

func TestQueue_DeadLetter(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()

	client := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer func() { _ = client.Close() }()

	queue := New(client, Config{Namespace: "test"})
	ctx := t.Context()

	job, err := queue.EnqueueWithOptions(ctx, "dead-job", map[string]interface{}{"data": "test"}, HIGH, EnqueueOptions{MaxAttempts: 1})
	require.NoError(t, err)

	dequeued, err := queue.DequeueWithTimeout(ctx, 2*time.Second)
	require.NoError(t, err)

	err = queue.Fail(ctx, dequeued, assert.AnError)
	require.NoError(t, err)

	items, err := queue.PeekDeadLetter(ctx, 10)
	require.NoError(t, err)
	assert.Len(t, items, 1)

	err = queue.RetryDeadLetter(ctx, 0)
	require.NoError(t, err)

	retried, err := queue.DequeueWithTimeout(ctx, 2*time.Second)
	require.NoError(t, err)
	require.NotNil(t, retried)
	assert.Equal(t, job.ID, retried.ID)

	deadLetterDepth, err := queue.GetDeadLetterDepth(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(0), deadLetterDepth)
}

func TestQueue_ClearDeadLetter(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()

	client := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer func() { _ = client.Close() }()

	queue := New(client, Config{Namespace: "test"})
	ctx := t.Context()

	_, err := queue.EnqueueWithOptions(ctx, "dead-job", nil, NORMAL, EnqueueOptions{MaxAttempts: 1})
	require.NoError(t, err)

	dequeued, err := queue.DequeueWithTimeout(ctx, 2*time.Second)
	require.NoError(t, err)

	err = queue.Fail(ctx, dequeued, assert.AnError)
	require.NoError(t, err)

	err = queue.ClearDeadLetter(ctx)
	require.NoError(t, err)

	depth, err := queue.GetDeadLetterDepth(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(0), depth)
}

func TestQueue_GetQueueDepth(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()

	client := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer func() { _ = client.Close() }()

	queue := New(client, Config{Namespace: "test"})
	ctx := t.Context()

	_, err := queue.Enqueue(ctx, "job1", nil, HIGH)
	require.NoError(t, err)

	_, err = queue.Enqueue(ctx, "job2", nil, NORMAL)
	require.NoError(t, err)

	_, err = queue.Enqueue(ctx, "job3", nil, LOW)
	require.NoError(t, err)

	depths, err := queue.GetQueueDepth(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(1), depths[HIGH])
	assert.Equal(t, int64(1), depths[NORMAL])
	assert.Equal(t, int64(1), depths[LOW])
}

func TestQueue_Scheduled(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()

	client := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer func() { _ = client.Close() }()

	queue := New(client, Config{Namespace: "test"})
	ctx := t.Context()

	future := time.Now().Add(100 * time.Millisecond)
	_, err := queue.EnqueueWithOptions(ctx, "scheduled-job", nil, NORMAL, EnqueueOptions{
		ScheduleFor: &future,
	})
	require.NoError(t, err)

	job, err := queue.DequeueWithTimeout(ctx, 50*time.Millisecond)
	assert.NoError(t, err)
	assert.Nil(t, job)

	job, err = queue.DequeueWithTimeout(ctx, 300*time.Millisecond)
	require.NoError(t, err)
	require.NotNil(t, job)
	assert.Equal(t, "scheduled-job", job.Type)
}

func TestPriority_String(t *testing.T) {
	assert.Equal(t, "critical", CRITICAL.String())
	assert.Equal(t, "high", HIGH.String())
	assert.Equal(t, "normal", NORMAL.String())
	assert.Equal(t, "low", LOW.String())
	assert.Equal(t, "unknown", Priority(99).String())
}

func TestPriorityFromString(t *testing.T) {
	assert.Equal(t, CRITICAL, PriorityFromString("critical"))
	assert.Equal(t, HIGH, PriorityFromString("high"))
	assert.Equal(t, NORMAL, PriorityFromString("normal"))
	assert.Equal(t, LOW, PriorityFromString("low"))
	assert.Equal(t, NORMAL, PriorityFromString("unknown"))
}
