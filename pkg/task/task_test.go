package task

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestService_BlockTillDone(t *testing.T) {
	s := New()
	var n atomic.Int64
	for range 50 {
		s.Go(context.Background(), "w", func(ctx context.Context) {
			time.Sleep(time.Millisecond)
			n.Add(1)
		})
	}
	s.BlockTillDone()
	if n.Load() != 50 {
		t.Errorf("expected 50, got %d", n.Load())
	}
	if s.ActiveCount() != 0 {
		t.Errorf("ActiveCount: %d", s.ActiveCount())
	}
}

func TestService_BackgroundDoesNotBlockActive(t *testing.T) {
	s := New()
	bgDone := make(chan struct{})
	defer close(bgDone)
	s.GoBackground(context.Background(), "bg", func(ctx context.Context) {
		<-bgDone
	})

	var done atomic.Bool
	s.Go(context.Background(), "active", func(ctx context.Context) { done.Store(true) })
	s.BlockTillDone()
	if !done.Load() {
		t.Errorf("active task didn't run")
	}
	if s.BackgroundCount() != 1 {
		t.Errorf("background should still be running, got %d", s.BackgroundCount())
	}
}

func TestService_PanicCountedAndRecovered(t *testing.T) {
	s := New()
	s.Go(context.Background(), "boom", func(ctx context.Context) {
		panic("oops")
	})
	s.BlockTillDone()
	if s.Failures() != 1 {
		t.Errorf("expected 1 failure, got %d", s.Failures())
	}
}

func TestService_ActiveNames(t *testing.T) {
	s := New()
	gate := make(chan struct{})
	started := make(chan struct{})
	s.Go(context.Background(), "alpha", func(ctx context.Context) {
		close(started)
		<-gate
	})
	<-started
	if names := s.ActiveNames(); len(names) != 1 || names[0] != "alpha" {
		t.Errorf("ActiveNames: %v", names)
	}
	close(gate)
	s.BlockTillDone()
}
