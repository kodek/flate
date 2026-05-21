// Package values implements HelmRelease values resolution and
// Kustomization postBuild substitution. It is the bridge between
// authored manifests and rendered manifests: it consults the central
// Store for ConfigMap/Secret content and merges referenced data into a
// HelmRelease's values map or a Kustomization's postBuild substitution
// table.
//
// Two key behaviors mirror flux-local:
//
//   - Deep merge follows Helm semantics: nested maps are merged, but
//     lists are REPLACED entirely (not concatenated).
//   - When a valuesFrom reference is optional and the target ConfigMap /
//     Secret is missing, a placeholder string is substituted so the
//     downstream YAML still parses and diffs are meaningful.
package values
