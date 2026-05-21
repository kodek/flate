package change

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"golang.org/x/sync/errgroup"
)

// Set is the immutable result of Detect — the set of file paths
// (relative to the scan roots) whose contents differ.
type Set struct {
	paths map[string]struct{}
}

// NewSet constructs a Set from an iterable of relative paths.
func NewSet(paths []string) *Set {
	out := &Set{paths: make(map[string]struct{}, len(paths))}
	for _, p := range paths {
		out.paths[filepath.ToSlash(p)] = struct{}{}
	}
	return out
}

// Len reports how many files differ.
func (s *Set) Len() int {
	if s == nil {
		return 0
	}
	return len(s.paths)
}

// Paths returns the changed files as a sorted slice.
func (s *Set) Paths() []string {
	if s == nil {
		return nil
	}
	out := make([]string, 0, len(s.paths))
	for p := range s.paths {
		out = append(out, p)
	}
	// Stable order makes logs and CI output deterministic.
	slices.Sort(out)
	return out
}

// Contains reports whether rel is in the change set. rel is expected
// to be filepath.ToSlash-normalized.
func (s *Set) Contains(rel string) bool {
	if s == nil {
		return false
	}
	_, ok := s.paths[filepath.ToSlash(rel)]
	return ok
}

// Reroot returns a copy of s with prefix prepended to every entry —
// used to lift a change set produced from a subdir-relative diff up
// into the repo-relative coordinate system that SourceFiles uses.
func (s *Set) Reroot(prefix string) *Set {
	if s == nil {
		return nil
	}
	prefix = strings.TrimSuffix(filepath.ToSlash(prefix), "/")
	if prefix == "" || prefix == "." {
		return s
	}
	out := &Set{paths: make(map[string]struct{}, len(s.paths))}
	for p := range s.paths {
		out.paths[prefix+"/"+p] = struct{}{}
	}
	return out
}

// Detect walks before and after concurrently, hashes every regular
// file, and returns the relative paths whose contents differ. Files
// present on only one side are also included.
//
// Directories whose name begins with "." (e.g. .git, .fluxrr-cache)
// and well-known noise dirs (node_modules) are skipped.
func Detect(before, after string) (*Set, error) {
	if before == "" || after == "" {
		return nil, errors.New("change.Detect: both paths required")
	}

	var (
		eg      errgroup.Group
		beforeH map[string]string
		afterH  map[string]string
	)
	eg.Go(func() error {
		h, err := hashTree(before)
		beforeH = h
		return err
	})
	eg.Go(func() error {
		h, err := hashTree(after)
		afterH = h
		return err
	})
	if err := eg.Wait(); err != nil {
		return nil, err
	}

	paths := make(map[string]struct{}, len(afterH)/8)
	for rel, h := range afterH {
		if beforeH[rel] != h {
			paths[rel] = struct{}{}
		}
	}
	for rel := range beforeH {
		if _, ok := afterH[rel]; !ok {
			paths[rel] = struct{}{}
		}
	}
	return &Set{paths: paths}, nil
}

// hashTree returns a map of relative-slash path → SHA-256 hex for
// every regular file under root.
func hashTree(root string) (map[string]string, error) {
	type job struct{ rel, abs string }
	jobs := make(chan job, 64)
	var (
		mu  sync.Mutex
		out = map[string]string{}
	)

	const workers = 8
	eg, _ := errgroup.WithContext(context.Background())
	for range workers {
		eg.Go(func() error {
			for j := range jobs {
				h, err := hashFile(j.abs)
				if err != nil {
					return err
				}
				mu.Lock()
				out[j.rel] = h
				mu.Unlock()
			}
			return nil
		})
	}

	walkErr := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		base := d.Name()
		if d.IsDir() {
			if base != root && shouldSkipDir(base) {
				return fs.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		jobs <- job{rel: filepath.ToSlash(rel), abs: p}
		return nil
	})
	close(jobs)
	if walkErr != nil {
		return nil, walkErr
	}
	if err := eg.Wait(); err != nil {
		return nil, err
	}
	return out, nil
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func shouldSkipDir(name string) bool {
	if strings.HasPrefix(name, ".") {
		return true
	}
	switch name {
	case "node_modules", "vendor":
		return true
	}
	return false
}
