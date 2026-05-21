package store

import "github.com/home-operations/flate/pkg/manifest"

// Status is the processing state of a resource.
type Status string

// Possible Status values.
const (
	StatusPending Status = "Pending"
	StatusReady   Status = "Ready"
	StatusFailed  Status = "Failed"
)

// StatusInfo bundles a status with an optional descriptive message.
type StatusInfo struct {
	Status  Status
	Message string
}

// SupportsStatus reports whether the given kind participates in the
// status pipeline. Kinds outside this set (ConfigMap, Secret, ...) are
// considered "ready" simply by existing.
func SupportsStatus(kind string) bool {
	switch kind {
	case manifest.KindKustomization,
		manifest.KindGitRepository,
		manifest.KindHelmRelease,
		manifest.KindHelmRepository,
		manifest.KindOCIRepository:
		return true
	}
	return false
}
