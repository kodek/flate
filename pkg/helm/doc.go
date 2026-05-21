// Package helm wraps helm.sh/helm/v3 to render HelmReleases without
// shelling out to the `helm` binary.
//
// The exported surface mirrors the operations flux-local performs:
//
//   - Client.Template renders a HelmRelease to YAML documents (the
//     equivalent of `helm template --dry-run --client-only`).
//   - Client.AddRepo registers a HelmRepository / OCIRepository /
//     LocalGitRepository so subsequent Template calls can resolve their
//     charts.
//   - Options exposes the helm CLI flags fluxrr understands
//     (--kube-version, --api-versions, --no-hooks, etc.).
//
// The client is safe for concurrent use; chart downloads are cached on
// disk keyed by chart name + version.
package helm
