// Package actor provides an actor-based concurrency model with bounded mailboxes.
// Each actor processes messages sequentially, owns its state, and is supervised.
//
// Key features:
//   - Bounded mailboxes with backpressure (drop oldest, block, or reject)
//   - Dead letter queue for overflow
//   - Message envelopes with trace IDs, deadlines, and optional reply channels
//   - Request/response pattern support
package actor

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// Errors
var (
	ErrMailboxFull    = errors.New("mailbox full")
	ErrActorStopped   = errors.New("actor stopped")
	ErrTimeout        = errors.New("timeout")
	ErrInvalidMessage = errors.New("invalid message")
	ErrNoReplyChannel = errors.New("no reply channel")
)

// OverflowStrategy determines how to handle mailbox overflow.
type OverflowStrategy int

const (
	// OverflowBlock blocks the sender until space is available.
	OverflowBlock OverflowStrategy = iota
	// OverflowDropOldest drops the oldest message in the mailbox.
	OverflowDropOldest
	// OverflowReject returns ErrMailboxFull immediately.
	OverflowReject
)

// Config holds actor configuration.
type Config struct {
	// MailboxSize is the maximum number of messages in the mailbox.
	// Default: 256
	MailboxSize int

	// OverflowStrategy determines how to handle mailbox overflow.
	// Default: OverflowDropOldest
	OverflowStrategy OverflowStrategy

	// Name is used for debugging and logging.
	Name string

	// DeadLetterHandler is called when a message cannot be delivered.
	DeadLetterHandler func(msg Message)
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		MailboxSize:      256,
		OverflowStrategy: OverflowDropOldest,
		Name:             "unnamed",
		DeadLetterHandler: func(msg Message) {
			// Default: no-op
		},
	}
}

// Message is the base interface for all actor messages.
type Message interface {
	// MessageType returns the type identifier for routing.
	MessageType() string
}

// Envelope wraps a Message with metadata.
type Envelope struct {
	// Message is the actual payload.
	Message Message

	// TraceID for distributed tracing.
	TraceID string

	// Deadline for message processing.
	Deadline time.Time

	// Reply channel for request/response pattern (optional).
	Reply chan<- any

	// CorrelationID for request/response matching.
	CorrelationID string
}

// IsExpired returns true if the envelope deadline has passed.
func (e *Envelope) IsExpired() bool {
	if e.Deadline.IsZero() {
		return false
	}
	return time.Now().After(e.Deadline)
}

// Actor processes messages sequentially and owns its state.
type Actor interface {
	// Receive processes a single message. It should not block indefinitely.
	// Context is scoped to the message processing.
	Receive(ctx context.Context, env Envelope) error

	// ID returns the unique identifier for this actor.
	ID() string
}

// ActorFunc is an adapter to allow using functions as Actors.
type ActorFunc func(ctx context.Context, env Envelope) error

// Receive implements Actor.
func (f ActorFunc) Receive(ctx context.Context, env Envelope) error {
	return f(ctx, env)
}

// ID implements Actor with a default ID.
func (f ActorFunc) ID() string {
	return "actor-func"
}

// Mailbox is a bounded message queue with overflow handling.
type Mailbox struct {
	config     Config
	messages   chan Envelope
	deadLetter func(Message)
	stopped    atomic.Bool
}

// NewMailbox creates a new bounded mailbox.
func NewMailbox(config Config) *Mailbox {
	return &Mailbox{
		config:     config,
		messages:   make(chan Envelope, config.MailboxSize),
		deadLetter: config.DeadLetterHandler,
	}
}

// Send delivers a message to the mailbox.
// Returns ErrMailboxFull if the mailbox is full and strategy is OverflowReject.
// Returns ErrActorStopped if the mailbox is stopped.
func (m *Mailbox) Send(ctx context.Context, env Envelope) error {
	if m.stopped.Load() {
		return ErrActorStopped
	}

	if env.IsExpired() {
		if m.deadLetter != nil && env.Message != nil {
			m.deadLetter(env.Message)
		}
		return nil // Expired messages are silently dropped
	}

	select {
	case m.messages <- env:
		return nil
	default:
		// Mailbox full - apply strategy
		switch m.config.OverflowStrategy {
		case OverflowBlock:
			select {
			case m.messages <- env:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		case OverflowDropOldest:
			select {
			case <-m.messages: // Drop oldest
			default:
			}
			select {
			case m.messages <- env:
				return nil
			default:
				return ErrMailboxFull
			}
		case OverflowReject:
			return ErrMailboxFull
		default:
			return ErrMailboxFull
		}
	}
}

// Receive returns the channel for receiving messages.
func (m *Mailbox) Receive() <-chan Envelope {
	return m.messages
}

// Stop closes the mailbox and prevents further sends.
func (m *Mailbox) Stop() {
	m.stopped.Store(true)
	close(m.messages)
}

// Ref represents a reference to a running actor.
type Ref struct {
	id      string
	mailbox *Mailbox
	actor   Actor
	running atomic.Bool
	wg      sync.WaitGroup
	mu      sync.Mutex
	started bool
}

// NewRef creates a new actor reference.
func NewRef(actor Actor, config Config) *Ref {
	if config.MailboxSize <= 0 {
		config.MailboxSize = DefaultConfig().MailboxSize
	}
	ref := &Ref{
		id:      actor.ID(),
		mailbox: NewMailbox(config),
		actor:   actor,
	}
	// Pre-add to wait group - Run() will call Done()
	ref.wg.Add(1)
	return ref
}

// ID returns the actor's unique identifier.
func (r *Ref) ID() string {
	return r.id
}

// Send delivers a message to the actor.
func (r *Ref) Send(ctx context.Context, msg Message) error {
	return r.SendEnvelope(ctx, Envelope{Message: msg})
}

// SendEnvelope delivers an envelope to the actor.
func (r *Ref) SendEnvelope(ctx context.Context, env Envelope) error {
	if !r.running.Load() {
		return ErrActorStopped
	}
	return r.mailbox.Send(ctx, env)
}

// Ask sends a message and waits for a reply (request/response pattern).
// The context deadline is used as the message deadline.
func (r *Ref) Ask(ctx context.Context, msg Message) (any, error) {
	if !r.running.Load() {
		return nil, ErrActorStopped
	}

	reply := make(chan any, 1)
	env := Envelope{
		Message:  msg,
		Reply:    reply,
		Deadline: time.Now().Add(30 * time.Second),
	}

	if deadline, ok := ctx.Deadline(); ok {
		env.Deadline = deadline
	}

	if err := r.mailbox.Send(ctx, env); err != nil {
		return nil, err
	}

	select {
	case resp := <-reply:
		if err, ok := resp.(error); ok {
			return nil, err
		}
		return resp, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Run starts the actor's message processing loop.
// It blocks until the context is cancelled.
func (r *Ref) Run(ctx context.Context) error {
	r.mu.Lock()
	if r.started {
		r.mu.Unlock()
		return errors.New("actor already running")
	}
	r.started = true
	r.mu.Unlock()

	if !r.running.CompareAndSwap(false, true) {
		return errors.New("actor already running")
	}

	defer r.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case env, ok := <-r.mailbox.Receive():
			if !ok {
				return nil // Mailbox closed
			}

			// Process message with timeout from envelope
			msgCtx := ctx
			cancelFunc := func() {}
			if !env.Deadline.IsZero() {
				var cancel context.CancelFunc
				msgCtx, cancel = context.WithDeadline(ctx, env.Deadline)
				cancelFunc = cancel
			}

			err := r.actor.Receive(msgCtx, env)
			cancelFunc() // Call cancel immediately after processing
			if env.Reply != nil {
				if err != nil {
					env.Reply <- err
				} else {
					env.Reply <- struct{}{}
				}
			}

			// On fatal error, stop the actor
			if err != nil && errors.Is(err, context.Canceled) {
				return err
			}
		}
	}
}

// Stop gracefully stops the actor.
func (r *Ref) Stop() {
	r.running.Store(false)
	r.mailbox.Stop()
	// Only wait if the actor was actually started
	r.mu.Lock()
	started := r.started
	r.mu.Unlock()
	if started {
		r.wg.Wait()
	}
}

// IsRunning returns whether the actor is currently running.
func (r *Ref) IsRunning() bool {
	return r.running.Load()
}

// System manages multiple actors.
type System struct {
	mu     sync.RWMutex
	actors map[string]*Ref
	config Config
}

// NewSystem creates a new actor system.
func NewSystem(config Config) *System {
	return &System{
		actors: make(map[string]*Ref),
		config: config,
	}
}

// Spawn creates and registers a new actor.
func (s *System) Spawn(actor Actor, config Config) (*Ref, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	id := actor.ID()
	if _, exists := s.actors[id]; exists {
		return nil, fmt.Errorf("actor %s already exists", id)
	}

	ref := NewRef(actor, config)
	s.actors[id] = ref
	return ref, nil
}

// Get returns an actor by ID.
func (s *System) Get(id string) (*Ref, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ref, ok := s.actors[id]
	return ref, ok
}

// Stop stops an actor by ID.
func (s *System) Stop(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	ref, ok := s.actors[id]
	if !ok {
		return fmt.Errorf("actor %s not found", id)
	}

	ref.Stop()
	delete(s.actors, id)
	return nil
}

// StopAll stops all actors.
func (s *System) StopAll() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, ref := range s.actors {
		ref.Stop()
	}
	s.actors = make(map[string]*Ref)
}

// List returns all actor IDs.
func (s *System) List() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ids := make([]string, 0, len(s.actors))
	for id := range s.actors {
		ids = append(ids, id)
	}
	return ids
}
