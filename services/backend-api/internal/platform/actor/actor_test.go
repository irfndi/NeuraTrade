package actor

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

type testMessage struct {
	value int
}

func (m testMessage) MessageType() string {
	return "test"
}

type testActor struct {
	id string
	fn func(ctx context.Context, env Envelope) error
}

func (a *testActor) Receive(ctx context.Context, env Envelope) error {
	if a.fn != nil {
		return a.fn(ctx, env)
	}
	return nil
}

func (a *testActor) ID() string {
	return a.id
}

func TestEnvelopeIsExpired(t *testing.T) {
	tests := []struct {
		name     string
		deadline time.Time
		want     bool
	}{
		{"no deadline", time.Time{}, false},
		{"future deadline", time.Now().Add(time.Hour), false},
		{"past deadline", time.Now().Add(-time.Hour), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := Envelope{Deadline: tt.deadline}
			if got := env.IsExpired(); got != tt.want {
				t.Errorf("IsExpired() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMailboxSendReceive(t *testing.T) {
	m := NewMailbox(DefaultConfig())

	msg := testMessage{value: 42}
	env := Envelope{Message: msg}

	ctx := context.Background()
	if err := m.Send(ctx, env); err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	select {
	case received := <-m.Receive():
		if received.Message.(testMessage).value != 42 {
			t.Errorf("wrong message: %v", received.Message)
		}
	case <-time.After(time.Second):
		t.Error("timeout waiting for message")
	}
}

func TestMailboxOverflowDropOldest(t *testing.T) {
	config := Config{
		MailboxSize:      2,
		OverflowStrategy: OverflowDropOldest,
	}
	m := NewMailbox(config)
	ctx := context.Background()

	// Fill mailbox
	for i := 0; i < 5; i++ {
		m.Send(ctx, Envelope{Message: testMessage{value: i}})
	}

	// Should have dropped oldest messages
	received := make([]int, 0)
	timeout := time.After(time.Second)
	for {
		select {
		case env := <-m.Receive():
			received = append(received, env.Message.(testMessage).value)
		case <-timeout:
			goto done
		}
	}
done:
	if len(received) != 2 {
		t.Errorf("expected 2 messages, got %d", len(received))
	}
	// Should have the newest messages
	if len(received) == 2 && (received[0] != 3 || received[1] != 4) {
		t.Errorf("expected [3, 4], got %v", received)
	}
}

func TestMailboxOverflowReject(t *testing.T) {
	config := Config{
		MailboxSize:      1,
		OverflowStrategy: OverflowReject,
	}
	m := NewMailbox(config)
	ctx := context.Background()

	// Fill mailbox
	m.Send(ctx, Envelope{Message: testMessage{value: 1}})

	// Should reject
	err := m.Send(ctx, Envelope{Message: testMessage{value: 2}})
	if err != ErrMailboxFull {
		t.Errorf("expected ErrMailboxFull, got %v", err)
	}
}

func TestMailboxExpiredMessage(t *testing.T) {
	m := NewMailbox(DefaultConfig())
	ctx := context.Background()

	env := Envelope{
		Message:  testMessage{value: 42},
		Deadline: time.Now().Add(-time.Hour), // Expired
	}

	if err := m.Send(ctx, env); err != nil {
		t.Errorf("expired message should be silently dropped, got %v", err)
	}

	// Mailbox should be empty
	select {
	case <-m.Receive():
		t.Error("should not have received expired message")
	default:
		// Good
	}
}

func TestMailboxStop(t *testing.T) {
	m := NewMailbox(DefaultConfig())
	m.Stop()

	if !m.stopped.Load() {
		t.Error("mailbox should be stopped")
	}

	err := m.Send(context.Background(), Envelope{Message: testMessage{}})
	if err != ErrActorStopped {
		t.Errorf("expected ErrActorStopped, got %v", err)
	}
}

func TestRefSend(t *testing.T) {
	var received atomic.Int32
	actor := ActorFunc(func(ctx context.Context, env Envelope) error {
		if msg, ok := env.Message.(testMessage); ok {
			received.Store(int32(msg.value))
		}
		return nil
	})

	ref := NewRef(actor, DefaultConfig())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go ref.Run(ctx)
	time.Sleep(10 * time.Millisecond) // Let actor start

	if err := ref.Send(ctx, testMessage{value: 123}); err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	if received.Load() != 123 {
		t.Errorf("expected 123, got %d", received.Load())
	}
}

func TestRefAsk(t *testing.T) {
	actor := ActorFunc(func(ctx context.Context, env Envelope) error {
		if env.Reply != nil {
			env.Reply <- "response"
		}
		return nil
	})

	ref := NewRef(actor, DefaultConfig())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go ref.Run(ctx)
	time.Sleep(10 * time.Millisecond)

	resp, err := ref.Ask(ctx, testMessage{value: 42})
	if err != nil {
		t.Fatalf("Ask failed: %v", err)
	}

	if resp != "response" {
		t.Errorf("expected 'response', got %v", resp)
	}
}

func TestRefStop(t *testing.T) {
	actor := ActorFunc(func(ctx context.Context, env Envelope) error {
		return nil
	})

	ref := NewRef(actor, DefaultConfig())
	ctx := context.Background()

	go ref.Run(ctx)
	time.Sleep(10 * time.Millisecond)

	ref.Stop()

	if ref.IsRunning() {
		t.Error("actor should not be running after Stop")
	}

	err := ref.Send(ctx, testMessage{})
	if err != ErrActorStopped {
		t.Errorf("expected ErrActorStopped, got %v", err)
	}
}

func TestSystemSpawn(t *testing.T) {
	sys := NewSystem(DefaultConfig())

	actor := &testActor{id: "test-actor"}

	ref, err := sys.Spawn(actor, DefaultConfig())
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	if ref.ID() != "test-actor" {
		t.Errorf("expected 'test-actor', got %s", ref.ID())
	}

	// Duplicate should fail
	_, err = sys.Spawn(actor, DefaultConfig())
	if err == nil {
		t.Error("expected error for duplicate actor")
	}
}

func TestSystemGet(t *testing.T) {
	sys := NewSystem(DefaultConfig())

	actor := &testActor{id: "test-actor"}

	ref, _ := sys.Spawn(actor, DefaultConfig())

	got, ok := sys.Get("test-actor")
	if !ok {
		t.Error("actor not found")
	}
	if got != ref {
		t.Error("wrong actor returned")
	}

	_, ok = sys.Get("nonexistent")
	if ok {
		t.Error("should not find nonexistent actor")
	}
}

func TestSystemStop(t *testing.T) {
	sys := NewSystem(DefaultConfig())

	actor := &testActor{id: "test-actor"}

	sys.Spawn(actor, DefaultConfig())

	if err := sys.Stop("test-actor"); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	if _, ok := sys.Get("test-actor"); ok {
		t.Error("actor should be removed after stop")
	}
}

func TestSystemStopAll(t *testing.T) {
	sys := NewSystem(DefaultConfig())

	for i := 0; i < 3; i++ {
		actor := &testActor{id: fmt.Sprintf("actor-%d", i)}
		sys.Spawn(actor, DefaultConfig())
	}

	sys.StopAll()

	if len(sys.List()) != 0 {
		t.Error("all actors should be stopped")
	}
}
