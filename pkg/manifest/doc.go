// Package manifest defines the data model for Flux GitOps resources as
// observed locally in a Git repository.
//
// The types here mirror the Flux CRDs (GitRepository, OCIRepository,
// HelmRepository, HelmRelease, Kustomization, ...) together with the
// supporting Kubernetes core kinds (ConfigMap, Secret). Each resource type
// can be decoded from raw YAML (the form Flux stores in a repo) via its
// ParseDoc-equivalent constructor, and re-encoded to a canonical YAML
// representation for export.
//
// Secrets are stripped of their values by default during parsing: the
// data/stringData fields are rewritten with placeholder tokens of the form
// "..PLACEHOLDER_<key>..". This matches flux-local's behavior — flate never
// needs the cleartext values to verify cluster shape.
package manifest
