package manifest

// StripResourceAttributes removes the listed annotation/label keys from
// the metadata of a raw Kubernetes resource and (when relevant) from its
// pod-template metadata and the items of a List. Useful for cutting
// kustomize-injected noise out of diffs.
func StripResourceAttributes(resource map[string]any, attrs []string) {
	if metadata, ok := resource["metadata"].(map[string]any); ok {
		stripAttrs(metadata, attrs)
	}
	if spec, ok := resource["spec"].(map[string]any); ok {
		if tmpl, ok := spec["template"].(map[string]any); ok {
			if meta, ok := tmpl["metadata"].(map[string]any); ok {
				stripAttrs(meta, attrs)
			}
		}
	}
	if kind, _ := resource["kind"].(string); kind == "List" {
		if items, ok := resource["items"].([]any); ok {
			for _, it := range items {
				m, ok := it.(map[string]any)
				if !ok {
					continue
				}
				if meta, ok := m["metadata"].(map[string]any); ok {
					stripAttrs(meta, attrs)
				}
			}
		}
	}
}

func stripAttrs(metadata map[string]any, attrs []string) {
	for _, key := range []string{"annotations", "labels"} {
		val, ok := metadata[key].(map[string]any)
		if !ok || val == nil {
			continue
		}
		for _, a := range attrs {
			delete(val, a)
		}
		if len(val) == 0 {
			delete(metadata, key)
		}
	}
}
