package manifest

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	yaml "go.yaml.in/yaml/v4"
	sigsyaml "sigs.k8s.io/yaml"
)

// DecodeDocs reads zero or more YAML documents from r and returns each as
// a generic map. Empty documents are skipped.
func DecodeDocs(r io.Reader) ([]map[string]any, error) {
	dec := yaml.NewDecoder(r)
	var out []map[string]any
	for {
		var node yaml.Node
		if err := dec.Decode(&node); err != nil {
			if errors.Is(err, io.EOF) {
				return out, nil
			}
			return nil, fmt.Errorf("%w: %v", ErrInput, err)
		}
		m, err := nodeToMap(&node)
		if err != nil {
			return nil, err
		}
		if m == nil {
			continue
		}
		out = append(out, m)
	}
}

// SplitDocs is the byte-slice convenience wrapper for DecodeDocs.
func SplitDocs(data []byte) ([]map[string]any, error) {
	return DecodeDocs(bytes.NewReader(data))
}

// nodeToMap converts a yaml.Node to map[string]any by routing through
// sigs.k8s.io/yaml so map keys come out as strings (go.yaml.in/yaml/v4's
// native decode produces interface{} keys for some shapes).
func nodeToMap(node *yaml.Node) (map[string]any, error) {
	if node == nil || node.Kind == 0 {
		return nil, nil
	}
	// Unwrap DocumentNode (yaml.v3 emits one per "---" boundary), and
	// treat empty or null-scalar roots as a no-op so a bare "---" or
	// "null" document doesn't error.
	target := node
	if node.Kind == yaml.DocumentNode {
		if len(node.Content) == 0 {
			return nil, nil
		}
		target = node.Content[0]
		if target.Tag == "!!null" {
			return nil, nil
		}
	}
	if target.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%w: expected mapping at document root, got %d", ErrInput, target.Kind)
	}
	raw, err := yaml.Marshal(target)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInput, err)
	}
	var m map[string]any
	if err := sigsyaml.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInput, err)
	}
	if len(m) == 0 {
		return nil, nil
	}
	return m, nil
}
