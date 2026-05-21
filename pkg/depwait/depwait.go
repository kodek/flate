// Package depwait blocks a controller until a set of NamedResource
// dependencies become Ready (or, for kinds without a status pipeline,
// merely exist) — with per-dep timeouts and fail-fast for missing refs.
package depwait

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/home-operations/flate/pkg/manifest"
	"github.com/home-operations/flate/pkg/store"
)

// DefaultTimeout is the per-dep timeout when not specified. The
// upstream Flux controllers default to several minutes since they
// wait for in-cluster reconciliation; flate is purely offline, so
// waits past a few seconds almost always indicate a misconfigured
// reference. Keep this short so typos in dependsOn / sourceRef
// surface immediately instead of stalling a render.
const DefaultTimeout = 30 * time.Second

// MissingGrace is the brief window we tolerate a missing dep before
// failing — it covers the legitimate case where a KS render produces
// the dep slightly later in the same run.
const MissingGrace = 2 * time.Second

// DepStatus enumerates the per-dependency resolution result.
type DepStatus int

// Possible DepStatus values.
const (
	DepPending DepStatus = iota
	DepReady
	DepFailed
	DepTimeout
	DepCancelled
)

// Event is yielded for each dependency as it resolves.
type Event struct {
	Dep    manifest.NamedResource
	Status DepStatus
	Reason string
}

// Success reports whether the dependency reached DepReady.
func (e Event) Success() bool { return e.Status == DepReady }

// Failure reports whether the dependency reached a terminal non-success state.
func (e Event) Failure() bool {
	return e.Status == DepFailed || e.Status == DepTimeout || e.Status == DepCancelled
}

// Summary tallies the final state of a Waiter run.
type Summary struct {
	Ready    []manifest.NamedResource
	Failed   []manifest.NamedResource
	Pending  []manifest.NamedResource
	Messages map[manifest.NamedResource]string
}

// AllReady reports whether every dependency reached DepReady.
func (s Summary) AllReady() bool { return len(s.Failed) == 0 && len(s.Pending) == 0 }

// AnyFailed reports whether at least one dependency ended in failure.
func (s Summary) AnyFailed() bool { return len(s.Failed) > 0 }

// Waiter holds the parameters for one dependency-wait operation.
type Waiter struct {
	Store   *store.Store
	Parent  manifest.NamedResource
	Timeout time.Duration
}

// Watch concurrently watches each dep and returns a channel of Events.
// The channel closes when every dep has reached a terminal state or ctx
// expires. Callers should drain the channel.
//
// Watch picks WatchReady vs WatchExists per-dep based on
// store.SupportsStatus.
func (w *Waiter) Watch(ctx context.Context, deps []manifest.NamedResource) <-chan Event {
	out := make(chan Event, len(deps))
	if len(deps) == 0 {
		close(out)
		return out
	}
	timeout := w.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}

	var wg sync.WaitGroup
	for _, dep := range deps {
		wg.Add(1)
		go func(dep manifest.NamedResource) {
			defer wg.Done()
			ev := w.watchOne(ctx, dep, timeout)
			select {
			case out <- ev:
			case <-ctx.Done():
			}
		}(dep)
	}

	go func() {
		wg.Wait()
		close(out)
	}()
	return out
}

func (w *Waiter) watchOne(ctx context.Context, dep manifest.NamedResource, timeout time.Duration) Event {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Fail fast on deps that never made it into the store: wait a
	// short grace window for a late-arriving render output, then
	// surface a clear error instead of timing out at the full
	// per-dep budget. We treat "object exists" OR "status known" as
	// proof of presence — the latter covers controllers that update
	// status before AddObject (and unit tests that set status only).
	if !w.depExists(dep) {
		graceCtx, graceCancel := context.WithTimeout(ctx, MissingGrace)
		_, err := w.Store.WatchExists(graceCtx, dep)
		graceCancel()
		if err != nil && !w.depExists(dep) {
			return Event{Dep: dep, Status: DepFailed, Reason: "dependency not found"}
		}
	}

	if !store.SupportsStatus(dep.Kind) {
		_, err := w.Store.WatchExists(ctx, dep)
		return classify(dep, err, "")
	}

	info, err := w.Store.WatchReady(ctx, dep)
	if err == nil {
		return Event{Dep: dep, Status: DepReady, Reason: info.Message}
	}
	return classify(dep, err, info.Message)
}

// depExists reports whether a dep is known to the store via either an
// added object or a recorded status entry.
func (w *Waiter) depExists(dep manifest.NamedResource) bool {
	if w.Store.GetObject(dep) != nil {
		return true
	}
	_, ok := w.Store.GetStatus(dep)
	return ok
}

func classify(dep manifest.NamedResource, err error, fallback string) Event {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return Event{Dep: dep, Status: DepTimeout, Reason: "timeout"}
	case errors.Is(err, context.Canceled):
		return Event{Dep: dep, Status: DepCancelled, Reason: "cancelled"}
	}
	var rfe *manifest.ResourceFailedError
	if errors.As(err, &rfe) {
		reason := rfe.Reason
		if reason == "" {
			reason = fallback
		}
		return Event{Dep: dep, Status: DepFailed, Reason: reason}
	}
	if err != nil {
		return Event{Dep: dep, Status: DepFailed, Reason: err.Error()}
	}
	return Event{Dep: dep, Status: DepReady, Reason: fallback}
}

// WaitAll consumes the channel returned by Watch and produces a Summary.
// First-failure cancellation is not performed automatically — callers
// that want it should drive Watch directly.
func WaitAll(ch <-chan Event) Summary {
	s := Summary{Messages: make(map[manifest.NamedResource]string)}
	for ev := range ch {
		s.Messages[ev.Dep] = ev.Reason
		switch ev.Status {
		case DepReady:
			s.Ready = append(s.Ready, ev.Dep)
		case DepFailed, DepTimeout, DepCancelled:
			s.Failed = append(s.Failed, ev.Dep)
		default:
			s.Pending = append(s.Pending, ev.Dep)
		}
	}
	return s
}
