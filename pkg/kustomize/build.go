package kustomize

import (
	"bytes"
	"fmt"
	"path/filepath"
	"sync"

	"sigs.k8s.io/kustomize/api/krusty"
	"sigs.k8s.io/kustomize/api/resmap"
	"sigs.k8s.io/kustomize/api/types"
	"sigs.k8s.io/kustomize/kyaml/filesys"

	"github.com/home-operations/flate/pkg/manifest"
)

// Options tunes Build behavior. Build is retained for callers that
// invoke a plain kustomize directory build; Flux Kustomizations should
// use RenderFlux instead, which honors every spec feature.
type Options struct {
	// LoadRestrictions controls relative-path safety. Defaults to
	// types.LoadRestrictionsRootOnly, matching `kustomize build`.
	// Set to "none" to disable.
	LoadRestrictions string
}

// pathLocks serialize concurrent builds against the same path —
// kustomize.yaml mutation during postBuild is not safe for parallel
// execution.
var (
	pathLocksMu sync.Mutex
	pathLocks   = map[string]*sync.Mutex{}
)

// lockPath returns a process-wide mutex keyed on absolute path.
func lockPath(path string) *sync.Mutex {
	pathLocksMu.Lock()
	defer pathLocksMu.Unlock()
	if l, ok := pathLocks[path]; ok {
		return l
	}
	l := &sync.Mutex{}
	pathLocks[path] = l
	return l
}

// Build renders the kustomization at path using krusty and returns the
// resulting documents as YAML bytes. The directory MUST contain a
// kustomization.yaml — use RenderFlux for Flux Kustomizations whose
// path may omit one.
func Build(path string, opts Options) ([]byte, error) {
	if path == "" {
		return nil, fmt.Errorf("%w: empty path", manifest.ErrInput)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		path = resolved
	}
	if err := validatePath(path); err != nil {
		return nil, err
	}

	lock := lockPath(path)
	lock.Lock()
	defer lock.Unlock()

	kopts := krusty.MakeDefaultOptions()
	switch opts.LoadRestrictions {
	case "none", "LoadRestrictionsNone":
		kopts.LoadRestrictions = types.LoadRestrictionsNone
	}

	k := krusty.MakeKustomizer(kopts)
	rm, err := k.Run(filesys.MakeFsOnDisk(), path)
	if err != nil {
		return nil, fmt.Errorf("kustomize build %s: %w", path, err)
	}
	return rm.AsYaml()
}

// ResourceMap is exposed for advanced callers that want the kustomize
// ResMap directly.
type ResourceMap = resmap.ResMap

// String is a debug helper that joins build output into a single
// printable string.
func String(data []byte) string {
	return string(bytes.TrimSpace(data))
}
