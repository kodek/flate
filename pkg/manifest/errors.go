package manifest

import (
	"errors"
	"fmt"
)

// Sentinel errors. Callers branch on these with errors.Is. Every error
// produced by this module wraps ErrFlux, so a generic
// errors.Is(err, manifest.ErrFlux) classifies any flux-related failure.
var (
	ErrFlux                       = errors.New("flux error")
	ErrInput                      = fmt.Errorf("%w: input error", ErrFlux)
	ErrObjectNotFound             = fmt.Errorf("%w: object not found", ErrFlux)
	ErrInvalidValuesReference     = fmt.Errorf("%w: invalid values reference", ErrFlux)
	ErrInvalidSubstituteReference = fmt.Errorf("%w: invalid substitute reference", ErrFlux)
	ErrCommand                    = fmt.Errorf("%w: command error", ErrFlux)
)

// inputf formats an input error.
func inputf(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrInput}, args...)...)
}

// ResourceFailedError signals that a reconciliation entered a terminal
// failed state.
type ResourceFailedError struct {
	Resource string
	Reason   string
}

func (e *ResourceFailedError) Error() string {
	reason := e.Reason
	if reason == "" {
		reason = "unknown error"
	}
	return fmt.Sprintf("resource %s failed: %s", e.Resource, reason)
}

func (*ResourceFailedError) Unwrap() error { return ErrFlux }

// DependencyFailedError signals that a parent resource cannot proceed
// because one of its dependencies has failed.
type DependencyFailedError struct {
	Kustomization string
	Dependency    string
	Reason        string
}

func (e *DependencyFailedError) Error() string {
	reason := e.Reason
	if reason == "" {
		reason = "unknown error"
	}
	return fmt.Sprintf("kustomization %s dependency %s failed: %s",
		e.Kustomization, e.Dependency, reason)
}

func (*DependencyFailedError) Unwrap() error { return ErrInput }
