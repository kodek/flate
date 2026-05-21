// Package diff produces structured and unified diffs between two sets
// of rendered Kubernetes manifests. Four output flavors are supported:
//
//   - Unified — line-oriented `diff -u` output (the default).
//   - Object  — a per-resource heading followed by the unified diff.
//   - YAML    — structured YAML where each entry is a resource and its
//     unified diff body.
//   - JSON    — same shape as YAML, marshaled as JSON.
//
// fluxrr intentionally does NOT shell out to an external diff tool.
// Callers wanting `dyff`-style structured rendering should pipe
// `fluxrr build` output through dyff themselves, or consume the YAML
// output format here.
package diff
