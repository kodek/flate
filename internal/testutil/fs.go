// Package testutil is shared fixture scaffolding for tests across the
// repo — kept minimal so each test file stays self-describing.
package testutil

import (
	"os"
	"path/filepath"
	"testing"
)

// WriteFile writes body to root/rel, creating any missing parent
// directories. Fails the test on any I/O error.
func WriteFile(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(p), err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
}
