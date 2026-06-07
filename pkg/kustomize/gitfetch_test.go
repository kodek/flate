package kustomize

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

func makeTestRepo(t *testing.T, files map[string]string, tag string) string {
	t.Helper()
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("PlainInit: %v", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		fpath := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(fpath), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fpath, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := wt.Add(name); err != nil {
			t.Fatalf("git add %s: %v", name, err)
		}
	}
	hash, err := wt.Commit("init", &git.CommitOptions{
		Author: &object.Signature{Name: "test", Email: "test@test", When: time.Unix(0, 0)},
	})
	if err != nil {
		t.Fatalf("git commit: %v", err)
	}
	if tag != "" {
		if _, err := repo.CreateTag(tag, hash, nil); err != nil {
			t.Fatalf("git tag %s: %v", tag, err)
		}
	}
	return dir
}

func addSymlink(t *testing.T, repoDir, name, target, tag string) {
	t.Helper()
	repo, err := git.PlainOpen(repoDir)
	if err != nil {
		t.Fatalf("PlainOpen: %v", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(repoDir, name)); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Add(name); err != nil {
		t.Fatalf("git add %s: %v", name, err)
	}
	hash, err := wt.Commit("symlink", &git.CommitOptions{
		Author: &object.Signature{Name: "test", Email: "test@test", When: time.Unix(0, 0)},
	})
	if err != nil {
		t.Fatalf("git commit: %v", err)
	}
	if _, err := repo.CreateTag(tag, hash, nil); err != nil {
		t.Fatalf("git tag %s: %v", tag, err)
	}
}

func TestParseGitRemoteSpec(t *testing.T) {
	cases := []struct {
		url     string
		wantGit bool
		repo    string
		subPath string
		ref     string
	}{
		{
			url:     "https://github.com/kubernetes-sigs/repo?ref=v0.2.2",
			wantGit: true, repo: "https://github.com/kubernetes-sigs/repo",
			subPath: ".", ref: "v0.2.2",
		},
		{
			url:     "https://gitlab.com/org/repo?ref=main",
			wantGit: true, repo: "https://gitlab.com/org/repo",
			subPath: ".", ref: "main",
		},
		{
			url:     "https://bitbucket.org/org/repo?ref=v1.0",
			wantGit: true, repo: "https://bitbucket.org/org/repo",
			subPath: ".", ref: "v1.0",
		},
		{
			url:     "https://github.com/org/repo//config/default?ref=main",
			wantGit: true, repo: "https://github.com/org/repo",
			subPath: "config/default", ref: "main",
		},
		{
			url:     "https://gitea.internal/org/repo?ref=v1.0",
			wantGit: true, repo: "https://gitea.internal/org/repo",
			subPath: ".", ref: "v1.0",
		},
		{
			url:     "https://gitea.internal/org/repo//subdir",
			wantGit: true, repo: "https://gitea.internal/org/repo",
			subPath: "subdir", ref: "",
		},
		{
			url:     "https://gitea.internal/org/repo.git",
			wantGit: true, repo: "https://gitea.internal/org/repo.git",
			subPath: ".", ref: "",
		},
		{
			url:     "https://github.com/org/repo",
			wantGit: true, repo: "https://github.com/org/repo",
			subPath: ".", ref: "",
		},
		// Marker-less deeper paths are file downloads, even on git hosts.
		{url: "https://github.com/org/repo/releases/download/v1.0/manifest.yaml", wantGit: false},
		{url: "https://github.com/org", wantGit: false},
		{url: "https://example.com/org/repo", wantGit: false},
		{url: "https://raw.githubusercontent.com/org/repo/main/file.yaml", wantGit: false},
		{url: "https://example.com/manifests.yaml", wantGit: false},
		{url: "./local.yaml", wantGit: false},
		{url: "../shared/cm.yaml", wantGit: false},
	}

	for _, tc := range cases {
		t.Run(tc.url, func(t *testing.T) {
			spec, ok := parseGitRemoteSpec(tc.url)
			if ok != tc.wantGit {
				t.Fatalf("parseGitRemoteSpec(%q) git=%v, want %v", tc.url, ok, tc.wantGit)
			}
			if !ok {
				return
			}
			if spec.repoURL != tc.repo {
				t.Errorf("repoURL: got %q, want %q", spec.repoURL, tc.repo)
			}
			if spec.subPath != tc.subPath {
				t.Errorf("subPath: got %q, want %q", spec.subPath, tc.subPath)
			}
			if spec.ref != tc.ref {
				t.Errorf("ref: got %q, want %q", spec.ref, tc.ref)
			}
		})
	}
}

func TestCloneAtRef_Tag(t *testing.T) {
	src := makeTestRepo(t, map[string]string{
		"app/cm.yaml": "apiVersion: v1\nkind: ConfigMap\n",
	}, "v1.0")

	dst := t.TempDir()
	if err := cloneAtRef(context.Background(), src, "v1.0", dst); err != nil {
		t.Fatalf("cloneAtRef: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "app/cm.yaml")); err != nil {
		t.Errorf("expected app/cm.yaml in clone: %v", err)
	}
}

func TestCloneAtRef_Branch(t *testing.T) {
	src := makeTestRepo(t, map[string]string{
		"deploy.yaml": "kind: Deployment\n",
	}, "")

	dst := t.TempDir()
	// go-git PlainInit defaults to "master"; clone by that branch name.
	if err := cloneAtRef(context.Background(), src, "master", dst); err != nil {
		t.Fatalf("cloneAtRef: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "deploy.yaml")); err != nil {
		t.Errorf("expected deploy.yaml in clone: %v", err)
	}
}

func TestCloneAtRef_RefNotFound(t *testing.T) {
	src := makeTestRepo(t, map[string]string{
		"cm.yaml": "kind: ConfigMap\n",
	}, "v1.0")

	dst := filepath.Join(t.TempDir(), "clone")
	err := cloneAtRef(context.Background(), src, "does-not-exist", dst)
	if err == nil {
		t.Fatal("expected error for missing ref, got nil")
	}
	if !isGitRefNotFound(err) {
		t.Errorf("expected gitRefNotFoundError, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "not found as tag or branch") {
		t.Errorf("error should name the failure mode; got %q", err)
	}
	if _, statErr := os.Stat(dst); statErr == nil {
		t.Error("partial clone dir left behind after failure")
	}
}

func TestFetchGitResource_Root(t *testing.T) {
	src := makeTestRepo(t, map[string]string{
		"cm.yaml":  "apiVersion: v1\nkind: ConfigMap\n",
		"svc.yaml": "apiVersion: v1\nkind: Service\n",
	}, "v2.0")

	cache := newPreflightCache(t)
	dir := t.TempDir()

	spec := gitRemoteSpec{repoURL: src, subPath: ".", ref: "v2.0"}
	name, err := fetchGitResource(context.Background(), cache, dir, src+"?ref=v2.0", spec)
	if err != nil {
		t.Fatalf("fetchGitResource: %v", err)
	}

	for _, f := range []string{"cm.yaml", "svc.yaml"} {
		if _, err := os.Stat(filepath.Join(dir, name, f)); err != nil {
			t.Errorf("expected %s in fetched dir: %v", f, err)
		}
	}
	// SkipStageDir excludes dot-dirs, so .git must not be copied.
	if _, err := os.Stat(filepath.Join(dir, name, ".git")); err == nil {
		t.Error(".git directory should not be present in fetched output")
	}
}

func TestFetchGitResource_SubPath(t *testing.T) {
	src := makeTestRepo(t, map[string]string{
		"app/cm.yaml":    "apiVersion: v1\nkind: ConfigMap\n",
		"other/svc.yaml": "apiVersion: v1\nkind: Service\n",
	}, "v1.0")

	cache := newPreflightCache(t)
	dir := t.TempDir()

	spec := gitRemoteSpec{repoURL: src, subPath: "app", ref: "v1.0"}
	name, err := fetchGitResource(context.Background(), cache, dir, src+"//app?ref=v1.0", spec)
	if err != nil {
		t.Fatalf("fetchGitResource: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, name, "cm.yaml")); err != nil {
		t.Errorf("expected cm.yaml in app subpath: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, name, "other")); err == nil {
		t.Error("other/ should not be present when subPath=app")
	}
}

func TestFetchGitResource_RejectsTraversal(t *testing.T) {
	src := makeTestRepo(t, map[string]string{
		"cm.yaml": "kind: ConfigMap\n",
	}, "v1.0")

	cache := newPreflightCache(t)
	dir := t.TempDir()

	for _, sub := range []string{"..", "../..", "../../../etc", "app/../.."} {
		spec := gitRemoteSpec{repoURL: src, subPath: sub, ref: "v1.0"}
		_, err := fetchGitResource(context.Background(), cache, dir, src+"//"+sub+"?ref=v1.0", spec)
		if err == nil {
			t.Errorf("subPath %q: expected escape error, got nil", sub)
			continue
		}
		if !strings.Contains(err.Error(), "escapes the cloned repository") {
			t.Errorf("subPath %q: expected escape error, got %q", sub, err)
		}
	}
}

// Destroying the source repo after the first fetch proves the second
// fetch is served from the cached clone.
func TestFetchGitResource_ClonesOnce(t *testing.T) {
	src := makeTestRepo(t, map[string]string{
		"cm.yaml": "kind: ConfigMap\n",
	}, "v1.0")

	cache := newPreflightCache(t)
	spec := gitRemoteSpec{repoURL: src, subPath: ".", ref: "v1.0"}
	urlStr := src + "?ref=v1.0"

	dir1, dir2 := t.TempDir(), t.TempDir()
	if _, err := fetchGitResource(context.Background(), cache, dir1, urlStr, spec); err != nil {
		t.Fatalf("first fetch: %v", err)
	}

	if err := os.RemoveAll(src); err != nil {
		t.Fatal(err)
	}
	name, err := fetchGitResource(context.Background(), cache, dir2, urlStr, spec)
	if err != nil {
		t.Fatalf("second fetch should reuse the cached clone: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir2, name, "cm.yaml")); err != nil {
		t.Errorf("second copy missing cm.yaml: %v", err)
	}
}

// Ref-not-found is definitive and stays cached: after destroying the
// source repo, a retry must return the cached error rather than a
// repository-missing transport error.
func TestFetchGitBase_NegativeCachesRefNotFound(t *testing.T) {
	src := makeTestRepo(t, map[string]string{
		"cm.yaml": "kind: ConfigMap\n",
	}, "v1.0")

	cache := newPreflightCache(t)
	spec := gitRemoteSpec{repoURL: src, subPath: ".", ref: "nope"}

	_, err := cache.fetchGitBase(context.Background(), spec)
	if !isGitRefNotFound(err) {
		t.Fatalf("expected gitRefNotFoundError, got %v", err)
	}

	if err := os.RemoveAll(src); err != nil {
		t.Fatal(err)
	}
	_, err2 := cache.fetchGitBase(context.Background(), spec)
	if !isGitRefNotFound(err2) {
		t.Errorf("negative cache miss: second call re-cloned (got %v)", err2)
	}
}

func TestFetchGitBase_EvictsTransientFailure(t *testing.T) {
	cache := newPreflightCache(t)
	spec := gitRemoteSpec{repoURL: filepath.Join(t.TempDir(), "no-such-repo"), subPath: ".", ref: "v1.0"}

	_, err := cache.fetchGitBase(context.Background(), spec)
	if err == nil {
		t.Fatal("expected error cloning a nonexistent repo")
	}
	if isGitRefNotFound(err) {
		t.Fatalf("transport failure misclassified as ref-not-found: %v", err)
	}
	if _, loaded := cache.gitClones.Load(spec.cloneKey()); loaded {
		t.Error("transient failure left a cached entry; next caller would inherit it")
	}
}

// A relative ../ symlink escaping the clone must fail the fetch.
// Absolute targets aren't tested: go-git re-roots them at checkout,
// so they arrive dangling.
func TestFetchGitBase_RejectsEscapingSymlink(t *testing.T) {
	secret := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(secret, []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	src := makeTestRepo(t, map[string]string{
		"cm.yaml": "kind: ConfigMap\n",
	}, "")

	cacheRoot := t.TempDir()
	cache, err := NewStagingCache(cacheRoot, 0)
	if err != nil {
		t.Fatalf("NewStagingCache: %v", err)
	}
	t.Cleanup(func() { _ = cache.Close() })

	// The clone lands at <cacheRoot>/flate-stage-git-*, one level deep;
	// "clone-level" stands in for that dir to get the ../ count right.
	relTarget, err := filepath.Rel(filepath.Join(cacheRoot, "clone-level"), secret)
	if err != nil {
		t.Fatal(err)
	}
	addSymlink(t, src, "esc", relTarget, "v1.0")

	_, err = cache.fetchGitBase(context.Background(), gitRemoteSpec{repoURL: src, subPath: ".", ref: "v1.0"})
	if err == nil {
		t.Fatal("expected escaping-symlink error, got nil")
	}
	if !strings.Contains(err.Error(), "escapes the cloned repository") {
		t.Errorf("expected escape error, got %q", err)
	}
}

func TestFetchGitBase_AllowsInternalSymlink(t *testing.T) {
	src := makeTestRepo(t, map[string]string{
		"cm.yaml": "kind: ConfigMap\n",
	}, "")
	addSymlink(t, src, "alias.yaml", "cm.yaml", "v1.0")

	cache := newPreflightCache(t)
	dir, err := cache.fetchGitBase(context.Background(), gitRemoteSpec{repoURL: src, subPath: ".", ref: "v1.0"})
	if err != nil {
		t.Fatalf("internal symlink should be allowed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "alias.yaml")); err != nil {
		t.Errorf("alias.yaml missing from clone: %v", err)
	}
}

func TestStagingCacheClose_RemovesGitClones(t *testing.T) {
	src := makeTestRepo(t, map[string]string{
		"cm.yaml": "kind: ConfigMap\n",
	}, "v1.0")

	root := t.TempDir()
	cache, err := NewStagingCache(root, 0)
	if err != nil {
		t.Fatalf("NewStagingCache: %v", err)
	}

	spec := gitRemoteSpec{repoURL: src, subPath: ".", ref: "v1.0"}
	cloneDir, err := cache.fetchGitBase(context.Background(), spec)
	if err != nil {
		t.Fatalf("fetchGitBase: %v", err)
	}
	if _, err := os.Stat(cloneDir); err != nil {
		t.Fatalf("clone dir missing before Close: %v", err)
	}

	if err := cache.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := os.Stat(cloneDir); err == nil {
		t.Error("clone dir survived Close")
	}
	matches, _ := filepath.Glob(filepath.Join(root, "flate-stage-git-*"))
	if len(matches) != 0 {
		t.Errorf("clone dirs left under cache root after Close: %v", matches)
	}
}
