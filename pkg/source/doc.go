// Package source implements SourceController — reconciles GitRepository
// and OCIRepository resources by fetching the underlying artifact into a
// content-addressed on-disk cache and publishing a *store.GitArtifact or
// *store.OCIArtifact for downstream controllers.
//
// Native libraries are used throughout:
//
//   - go-git for git clone / checkout. No `git` subprocess invocation.
//   - oras-go for OCI artifact pulls. No `crane` or `oras` subprocess.
//
// The cache is keyed by SHA256(url + ref) so multiple revisions of the
// same upstream can co-exist on disk. A LocalRepository — a synthetic
// GitRepository pointing at the user's working directory — is also
// supported by the orchestrator.
package source
