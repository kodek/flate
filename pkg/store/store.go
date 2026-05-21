package store

import (
	"maps"
	"reflect"
	"slices"
	"sync"

	"github.com/buroa/fluxrr/pkg/manifest"
)

// Store is the central in-memory state container.
type Store struct {
	mu        sync.RWMutex
	objects   map[manifest.NamedResource]manifest.BaseManifest
	status    map[manifest.NamedResource]StatusInfo
	artifacts map[manifest.NamedResource]Artifact

	// listeners is keyed by EventKind. Each entry is a slice of
	// (id, listener) pairs. We use a slice + linear scan because:
	//   - listener counts are tiny (a handful per event)
	//   - removal preserves order
	//   - Unsubscribe identity is the slice index encoded in a closure
	listeners map[EventKind]*listenerSet
}

// New constructs an empty Store.
func New() *Store {
	return &Store{
		objects:   make(map[manifest.NamedResource]manifest.BaseManifest),
		status:    make(map[manifest.NamedResource]StatusInfo),
		artifacts: make(map[manifest.NamedResource]Artifact),
		listeners: map[EventKind]*listenerSet{
			EventObjectAdded:     newListenerSet(),
			EventStatusUpdated:   newListenerSet(),
			EventArtifactUpdated: newListenerSet(),
		},
	}
}

// AddObject inserts a manifest. Re-adding an equal object is a no-op.
// Re-adding a different object overwrites the existing entry AND still
// dispatches an ObjectAdded event (so newer values propagate).
func (s *Store) AddObject(obj manifest.BaseManifest) {
	id := obj.Named()
	s.mu.Lock()
	prev, exists := s.objects[id]
	if exists && reflect.DeepEqual(prev, obj) {
		s.mu.Unlock()
		return
	}
	s.objects[id] = obj
	s.mu.Unlock()
	s.fire(EventObjectAdded, id, obj)
}

// GetObject returns the manifest for id, or nil if not present.
func (s *Store) GetObject(id manifest.NamedResource) manifest.BaseManifest {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.objects[id]
}

// DeleteObject removes the object stored under id. Returns whether
// anything was removed. Status and artifact entries (if any) are also
// dropped so a re-add under a different id starts clean.
func (s *Store) DeleteObject(id manifest.NamedResource) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.objects[id]; !ok {
		return false
	}
	delete(s.objects, id)
	delete(s.status, id)
	delete(s.artifacts, id)
	return true
}

// ListObjects returns every stored manifest, optionally filtered by kind.
// An empty kind matches all objects.
func (s *Store) ListObjects(kind string) []manifest.BaseManifest {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]manifest.BaseManifest, 0, len(s.objects))
	for id, obj := range s.objects {
		if kind != "" && id.Kind != kind {
			continue
		}
		out = append(out, obj)
	}
	return out
}

// UpdateStatus records a status transition and dispatches a
// StatusUpdated event when the status info changes.
func (s *Store) UpdateStatus(id manifest.NamedResource, status Status, message string) {
	info := StatusInfo{Status: status, Message: message}
	s.mu.Lock()
	prev, exists := s.status[id]
	if exists && prev == info {
		s.mu.Unlock()
		return
	}
	s.status[id] = info
	s.mu.Unlock()
	s.fire(EventStatusUpdated, id, info)
}

// GetStatus returns the status for id and whether it was present.
func (s *Store) GetStatus(id manifest.NamedResource) (StatusInfo, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	info, ok := s.status[id]
	return info, ok
}

// SetArtifact stores an artifact for id and dispatches an
// ArtifactUpdated event. Re-setting with a deep-equal value is a no-op.
func (s *Store) SetArtifact(id manifest.NamedResource, artifact Artifact) {
	s.mu.Lock()
	prev, exists := s.artifacts[id]
	if exists && reflect.DeepEqual(prev, artifact) {
		s.mu.Unlock()
		return
	}
	s.artifacts[id] = artifact
	s.mu.Unlock()
	s.fire(EventArtifactUpdated, id, artifact)
}

// GetArtifact returns the artifact for id, or nil if none was set.
func (s *Store) GetArtifact(id manifest.NamedResource) Artifact {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.artifacts[id]
}

// HasFailedResources reports whether any tracked status is Failed.
func (s *Store) HasFailedResources() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, info := range s.status {
		if info.Status == StatusFailed {
			return true
		}
	}
	return false
}

// FailedResources returns every (id, info) currently in Failed state.
func (s *Store) FailedResources() map[manifest.NamedResource]StatusInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[manifest.NamedResource]StatusInfo)
	for id, info := range s.status {
		if info.Status == StatusFailed {
			out[id] = info
		}
	}
	return out
}

// AddListener registers a callback for the given event kind. When
// flush==true, the listener is immediately invoked with every matching
// object already in the store before the call returns, in deterministic
// order. The returned Unsubscribe removes the listener.
func (s *Store) AddListener(event EventKind, fn Listener, flush bool) Unsubscribe {
	set, ok := s.listeners[event]
	if !ok {
		panic("store: unknown event kind")
	}
	handle := set.add(fn)

	if flush {
		s.replayInto(event, fn)
	}
	return func() { set.remove(handle) }
}

// replayInto delivers existing state to a fresh listener so it can catch
// up. Called under no lock; takes its own RLock.
func (s *Store) replayInto(event EventKind, fn Listener) {
	switch event {
	case EventObjectAdded:
		s.mu.RLock()
		snap := make(map[manifest.NamedResource]manifest.BaseManifest, len(s.objects))
		maps.Copy(snap, s.objects)
		s.mu.RUnlock()
		for id, obj := range snap {
			fn(id, obj)
		}
	case EventStatusUpdated:
		s.mu.RLock()
		snap := make(map[manifest.NamedResource]StatusInfo, len(s.status))
		maps.Copy(snap, s.status)
		s.mu.RUnlock()
		for id, info := range snap {
			fn(id, info)
		}
	case EventArtifactUpdated:
		s.mu.RLock()
		snap := make(map[manifest.NamedResource]Artifact, len(s.artifacts))
		maps.Copy(snap, s.artifacts)
		s.mu.RUnlock()
		for id, art := range snap {
			fn(id, art)
		}
	}
}

// fire dispatches an event to every registered listener. Listeners run
// synchronously on the calling goroutine. A panic in any listener is
// recovered to prevent one bad listener from breaking the dispatch.
func (s *Store) fire(event EventKind, id manifest.NamedResource, payload any) {
	set := s.listeners[event]
	if set == nil {
		return
	}
	for _, fn := range set.snapshot() {
		safeInvoke(fn, id, payload)
	}
}

func safeInvoke(fn Listener, id manifest.NamedResource, payload any) {
	defer func() { _ = recover() }()
	fn(id, payload)
}

// listenerSet is a copy-on-snapshot slice of listeners. add returns a
// handle (a stable id) used by remove to find the entry. We deliberately
// do not reuse handles after removal to avoid ABA bugs in long sessions.
type listenerSet struct {
	mu      sync.Mutex
	entries []listenerEntry
	nextID  int64
}

type listenerEntry struct {
	id int64
	fn Listener
}

func newListenerSet() *listenerSet { return &listenerSet{} }

func (l *listenerSet) add(fn Listener) int64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.nextID++
	id := l.nextID
	l.entries = append(l.entries, listenerEntry{id: id, fn: fn})
	return id
}

func (l *listenerSet) remove(id int64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = slices.DeleteFunc(l.entries, func(e listenerEntry) bool {
		return e.id == id
	})
}

// snapshot returns a copy of the current listener funcs so dispatch can
// iterate without holding the lock (and so listeners can mutate the set
// during dispatch without affecting the current pass).
func (l *listenerSet) snapshot() []Listener {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]Listener, len(l.entries))
	for i := range l.entries {
		out[i] = l.entries[i].fn
	}
	return out
}
