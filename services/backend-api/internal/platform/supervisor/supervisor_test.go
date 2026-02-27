package supervisor

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

func TestGroupEmpty(t *testing.T) {
	g := NewGroup()
	err := g.Run(context.Background())
	if err != nil {
		t.Errorf("empty group should return nil, got %v", err)
	}
}

func TestGroupSingleRunnable(t *testing.T) {
	g := NewGroup()
	called := false
	g.AddFunc("test", func(ctx context.Context) error {
		called = true
		return nil
	})

	err := g.Run(context.Background())
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !called {
		t.Error("runnable was not called")
	}
}

func TestGroupMultipleRunnables(t *testing.T) {
	g := NewGroup()
	var count atomic.Int32

	for i := 0; i < 5; i++ {
		g.AddFunc("worker", func(ctx context.Context) error {
			count.Add(1)
			return nil
		})
	}

	err := g.Run(context.Background())
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if count.Load() != 5 {
		t.Errorf("expected 5 calls, got %d", count.Load())
	}
}

func TestGroupError(t *testing.T) {
	g := NewGroup()
	expectedErr := errors.New("test error")

	g.AddFunc("failer", func(ctx context.Context) error {
		return expectedErr
	})
	g.AddFunc("blocker", func(ctx context.Context) error {
		<-ctx.Done()
		return nil
	})

	err := g.Run(context.Background())
	if !errors.Is(err, expectedErr) {
		t.Errorf("expected %v, got %v", expectedErr, err)
	}
}

func TestGroupCancellation(t *testing.T) {
	g := NewGroup()
	started := make(chan struct{})
	stopped := make(chan struct{})

	g.AddFunc("worker", func(ctx context.Context) error {
		close(started)
		<-ctx.Done()
		close(stopped)
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-started
		cancel()
	}()

	err := g.Run(ctx)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	select {
	case <-stopped:
		// Good
	case <-time.After(time.Second):
		t.Error("worker did not stop on cancellation")
	}
}

func TestGroupShutdown(t *testing.T) {
	g := NewGroup()
	done := make(chan struct{})

	g.AddFunc("worker", func(ctx context.Context) error {
		<-ctx.Done()
		close(done)
		return nil
	})

	go g.Run(context.Background())

	// Give worker time to start
	time.Sleep(10 * time.Millisecond)

	err := g.Shutdown(time.Second)
	if err != nil {
		t.Errorf("shutdown error: %v", err)
	}

	select {
	case <-done:
		// Good
	case <-time.After(time.Second):
		t.Error("worker did not stop")
	}
}

func TestSupervisor(t *testing.T) {
	s := New()
	var count atomic.Int32

	s.AddFunc("worker1", func(ctx context.Context) error {
		count.Add(1)
		return nil
	})
	s.AddFunc("worker2", func(ctx context.Context) error {
		count.Add(1)
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	err := s.Run(ctx)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if count.Load() != 2 {
		t.Errorf("expected 2 workers, got %d", count.Load())
	}
}

func TestSupervisorIsRunning(t *testing.T) {
	s := New()

	if s.IsRunning() {
		t.Error("supervisor should not be running initially")
	}

	started := make(chan struct{})
	s.AddFunc("worker", func(ctx context.Context) error {
		close(started)
		<-ctx.Done()
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	go s.Run(ctx)

	<-started

	if !s.IsRunning() {
		t.Error("supervisor should be running")
	}

	cancel()
	time.Sleep(50 * time.Millisecond)

	if s.IsRunning() {
		t.Error("supervisor should not be running after cancellation")
	}
}
