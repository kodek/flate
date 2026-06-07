package kustomize

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

// gitCloneTimeout caps each preflight git clone (both ref attempts
// combined). Wider than remoteFetchTimeout since a clone moves a full
// tree, but a hung git server still fails in a minute, not never.
const gitCloneTimeout = 60 * time.Second

// gitRemoteSpec is a parsed kustomize git remote base URL.
//
//	https://github.com/org/repo?ref=v1.0         → {repoURL: "https://github.com/org/repo", subPath: ".", ref: "v1.0"}
//	https://github.com/org/repo//config?ref=main → {repoURL: "https://github.com/org/repo", subPath: "config", ref: "main"}
type gitRemoteSpec struct {
	repoURL string
	subPath string // "." when no // subpath present
	ref     string // "" means the remote's default branch
}

func (s gitRemoteSpec) cloneKey() string {
	return urlHash(s.repoURL + "\n" + s.ref)
}

// parseGitRemoteSpec returns (spec, true) if s is a kustomize git remote base
// URL that should be resolved via git clone, (zero, false) for plain HTTP files.
// Git remote bases carry one of kustomize's markers (?ref=, // subpath, .git
// suffix) or are a bare org/repo path on a well-known git host; marker-less
// deeper paths (releases/download/…, blob/…) are direct file downloads.
func parseGitRemoteSpec(s string) (gitRemoteSpec, bool) {
	if !strings.HasPrefix(s, "http://") && !strings.HasPrefix(s, "https://") {
		return gitRemoteSpec{}, false
	}
	u, err := url.Parse(s)
	if err != nil {
		return gitRemoteSpec{}, false
	}

	ref := u.Query().Get("ref")
	subPath := "."

	// Kustomize uses // in the URL path to separate the repo from a subdirectory.
	if idx := strings.Index(u.Path, "//"); idx >= 0 {
		if sub := strings.TrimLeft(u.Path[idx+2:], "/"); sub != "" {
			subPath = sub
		}
		u.Path = u.Path[:idx]
	}

	if ref == "" && subPath == "." && !strings.HasSuffix(u.Path, ".git") {
		// No marker; only a bare org/repo path on a known host qualifies.
		switch u.Hostname() {
		case "github.com", "gitlab.com", "bitbucket.org":
			if strings.Count(strings.Trim(u.Path, "/"), "/") != 1 {
				return gitRemoteSpec{}, false
			}
		default:
			return gitRemoteSpec{}, false
		}
	}
	u.RawQuery = ""
	return gitRemoteSpec{repoURL: u.String(), subPath: subPath, ref: ref}, true
}

// gitClone carries the result of one clone, deduped via the same
// sync.Once + done-channel discipline as remoteFetch (remotefetch.go).
type gitClone struct {
	start sync.Once
	done  chan struct{}
	dir   string
	err   error
}

// fetchGitBase returns the path of a shallow clone of spec.repoURL at
// spec.ref, cloning at most once per (repoURL, ref) within the
// StagingCache lifetime. Same cache discipline as FetchRemote:
// ref-not-found is definitive within a run and stays cached; transient
// errors drop the entry so the next caller retries.
//
// Clones land in flate-stage-git-* tempdirs under c.root, inheriting
// the per-process scratch lifecycle: removed by Close, swept by
// sweepStaleStageDirs after a crash, skipped by the LRU and GC sweeps.
// MkdirTemp keeps concurrent flate processes off each other's clones.
func (c *StagingCache) fetchGitBase(ctx context.Context, spec gitRemoteSpec) (string, error) {
	key := spec.cloneKey()
	loaded, _ := c.gitClones.LoadOrStore(key, &gitClone{done: make(chan struct{})})
	gc := loaded.(*gitClone)
	gc.start.Do(func() {
		go func() {
			var dir string
			dir, gc.err = os.MkdirTemp(c.root, "flate-stage-git-*")
			if gc.err == nil {
				gc.err = cloneAtRef(context.Background(), spec.repoURL, spec.ref, dir)
				if gc.err == nil {
					gc.err = rejectEscapingSymlinks(dir)
				}
				if gc.err == nil {
					gc.dir = dir
				} else {
					_ = os.RemoveAll(dir)
				}
			}
			// Evict transient failures before signaling done so no
			// caller can observe the error and still load the dead entry.
			if gc.err != nil && !isGitRefNotFound(gc.err) {
				c.gitClones.CompareAndDelete(key, gc)
			}
			close(gc.done)
		}()
	})
	select {
	case <-gc.done:
		return gc.dir, gc.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// fetchGitResource clones the remote git base, copies spec.subPath
// into dir, and returns the directory name for use as a kustomization
// resource entry. The copied tree is not re-walked, so nested remote
// bases don't resolve — same depth-1 limit as the HTTP fetch path.
func fetchGitResource(ctx context.Context, cache *StagingCache, dir, urlStr string, spec gitRemoteSpec) (string, error) {
	cloneDir, err := cache.fetchGitBase(ctx, spec)
	if err != nil {
		return "", err
	}
	src := filepath.Join(cloneDir, filepath.FromSlash(spec.subPath))
	// filepath.Join cleans ".." segments through the base — reject any
	// subpath that escapes the clone, or a crafted //../../ URL would
	// copy arbitrary host files into the render output.
	if rel, relErr := filepath.Rel(cloneDir, src); relErr != nil ||
		rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("subpath %q escapes the cloned repository", spec.subPath)
	}
	name := ".flate-remote-" + urlHash(urlStr)
	dst := filepath.Join(dir, name)
	if err := os.MkdirAll(dst, 0o750); err != nil {
		return "", err
	}
	if err := cache.copyTreeInto(src, dst); err != nil {
		return "", err
	}
	return name, nil
}

// gitRefNotFoundError is the typed sentinel for a definitive ref-
// resolution failure — the git-path counterpart of httpStatusError,
// classified via errors.As so it survives wrapping.
type gitRefNotFoundError struct {
	Ref  string
	Repo string
}

func (e *gitRefNotFoundError) Error() string {
	return fmt.Sprintf("ref %q not found as tag or branch in %s", e.Ref, e.Repo)
}

func isGitRefNotFound(err error) bool {
	var rnf *gitRefNotFoundError
	return errors.As(err, &rnf)
}

// refMissing reports whether a clone attempt failed because the named
// ref doesn't exist, rather than a transport failure.
func refMissing(err error) bool {
	return errors.Is(err, plumbing.ErrReferenceNotFound) || errors.Is(err, git.NoMatchingRefSpecError{})
}

// cloneAtRef does a depth-1 clone of repoURL at ref, applying
// gitCloneTimeout itself (like httpGetURL). Tries tag before branch
// since semver refs are almost always tags; commit SHAs are not
// resolvable this way and surface as gitRefNotFoundError. Removes the
// dir on failure so a retry starts clean.
func cloneAtRef(ctx context.Context, repoURL, ref, dir string) error {
	cloneCtx, cancel := context.WithTimeout(ctx, gitCloneTimeout)
	defer cancel()
	if ref == "" {
		_, err := git.PlainCloneContext(cloneCtx, dir, false, &git.CloneOptions{
			URL:   repoURL,
			Depth: 1,
			Tags:  git.NoTags,
		})
		return err
	}
	tagErr := tryClone(cloneCtx, dir, repoURL, plumbing.NewTagReferenceName(ref))
	if tagErr == nil {
		return nil
	}
	_ = os.RemoveAll(dir)
	if !refMissing(tagErr) {
		// Transport failure or timeout — a branch attempt against the
		// same server would fail the same way.
		return fmt.Errorf("clone %s at ref %q: %w", repoURL, ref, tagErr)
	}
	branchErr := tryClone(cloneCtx, dir, repoURL, plumbing.NewBranchReferenceName(ref))
	if branchErr == nil {
		return nil
	}
	_ = os.RemoveAll(dir)
	if refMissing(branchErr) {
		return &gitRefNotFoundError{Ref: ref, Repo: repoURL}
	}
	return fmt.Errorf("clone %s at ref %q: %w", repoURL, ref, branchErr)
}

// rejectEscapingSymlinks errors on any symlink in a fresh clone that
// resolves outside it. copyTreeInto dereferences symlinks, so a repo
// carrying `link -> ../../home/user/.ssh` would otherwise copy host
// files into the render output. Absolute targets arrive dangling
// (go-git re-roots them at checkout) and are skipped, as are in-clone
// symlinks, which shared-base layouts legitimately use.
func rejectEscapingSymlinks(root string) error {
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() && path != root && SkipStageDir(d.Name()) {
			return fs.SkipDir
		}
		if d.Type()&fs.ModeSymlink == 0 {
			return nil
		}
		target, err := filepath.EvalSymlinks(path)
		if err != nil {
			return nil // dangling
		}
		rel, err := filepath.Rel(resolved, target)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			name, _ := filepath.Rel(root, path)
			return fmt.Errorf("symlink %s escapes the cloned repository", name)
		}
		return nil
	})
}

func tryClone(ctx context.Context, dir, repoURL string, ref plumbing.ReferenceName) error {
	_, err := git.PlainCloneContext(ctx, dir, false, &git.CloneOptions{
		URL:           repoURL,
		Depth:         1,
		SingleBranch:  true,
		ReferenceName: ref,
		Tags:          git.NoTags,
	})
	return err
}
