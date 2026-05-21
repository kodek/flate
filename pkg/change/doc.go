// Package change computes file-level differences between two
// filesystem trees and maps them onto the Flux resources they affect.
//
// It is the core of flate's "changed-only" mode (--path-orig on any
// command): walk both trees in parallel, SHA-256 every regular file,
// emit the relative paths that differ. Callers then ask Filter whether
// a given resource — identified by the file it was loaded from — has
// changed.
//
// The filter cascades through references: when a HelmRelease's file
// changed, its chart source (OCIRepository / HelmRepository /
// GitRepository) is also marked needed so the actual chart still gets
// downloaded. Likewise, a Kustomization's sourceRef and dependsOn
// ancestors are kept ready so downstream waits succeed.
package change
