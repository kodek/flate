package store

import "github.com/home-operations/flate/pkg/manifest"

// EventKind enumerates the three observable changes the Store dispatches.
type EventKind int

const (
	// EventObjectAdded fires when a new manifest is added (or when a
	// listener is registered with Flush=true, to replay existing state).
	EventObjectAdded EventKind = iota + 1
	// EventStatusUpdated fires when status transitions.
	EventStatusUpdated
	// EventArtifactUpdated fires when an artifact is stored.
	EventArtifactUpdated
)

// Listener receives store events. The payload type depends on EventKind:
//   - EventObjectAdded     → manifest.BaseManifest
//   - EventStatusUpdated   → StatusInfo
//   - EventArtifactUpdated → Artifact
//
// Listeners run synchronously on the goroutine that triggered the event,
// so they MUST NOT call back into the same Store with a blocking call.
type Listener func(id manifest.NamedResource, payload any)

// Unsubscribe removes a listener. It is safe to call from inside the
// listener.
type Unsubscribe func()
