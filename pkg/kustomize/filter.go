package kustomize

import (
	"slices"

	"github.com/home-operations/flate/pkg/manifest"
)

// FilterKinds returns a new slice with only the documents whose `kind`
// is in keep.
func FilterKinds(docs []map[string]any, keep []string) []map[string]any {
	if len(keep) == 0 {
		return docs
	}
	return filter(docs, func(doc map[string]any) bool {
		kind, _ := doc["kind"].(string)
		return slices.Contains(keep, kind)
	})
}

// ExcludeKinds returns a new slice with documents whose `kind` is NOT
// in skip.
func ExcludeKinds(docs []map[string]any, skip []string) []map[string]any {
	if len(skip) == 0 {
		return docs
	}
	return filter(docs, func(doc map[string]any) bool {
		kind, _ := doc["kind"].(string)
		return !slices.Contains(skip, kind)
	})
}

// GrepHelmRelease keeps only documents matching the given HelmRelease.
// Returns docs unchanged when release is nil.
func GrepHelmRelease(docs []map[string]any, release *manifest.HelmRelease) []map[string]any {
	if release == nil {
		return docs
	}
	return filter(docs, func(doc map[string]any) bool {
		if kind, _ := doc["kind"].(string); kind != manifest.KindHelmRelease {
			return false
		}
		md, _ := doc["metadata"].(map[string]any)
		if md == nil {
			return false
		}
		return md["name"] == release.Name && md["namespace"] == release.Namespace
	})
}

// filter returns the elements of docs for which pred is true, without
// mutating the input slice.
func filter(docs []map[string]any, pred func(map[string]any) bool) []map[string]any {
	out := make([]map[string]any, 0, len(docs))
	for _, doc := range docs {
		if pred(doc) {
			out = append(out, doc)
		}
	}
	return out
}
