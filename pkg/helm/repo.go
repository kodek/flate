package helm

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	chart "helm.sh/helm/v4/pkg/chart/v2"
	"helm.sh/helm/v4/pkg/getter"
	repo "helm.sh/helm/v4/pkg/repo/v1"

	"github.com/home-operations/flate/pkg/manifest"
)

// chartCacheLocks serializes concurrent fetches of the same cached
// chart tarball so two reconcilers don't race on the same file.
var (
	chartCacheLocksMu sync.Mutex
	chartCacheLocks   = map[string]*sync.Mutex{}
)

func lockChartPath(p string) *sync.Mutex {
	chartCacheLocksMu.Lock()
	defer chartCacheLocksMu.Unlock()
	if l, ok := chartCacheLocks[p]; ok {
		return l
	}
	l := &sync.Mutex{}
	chartCacheLocks[p] = l
	return l
}

// writeAtomic writes data to path via a temp file + rename so partial
// writes never appear at the target path to concurrent readers.
func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op if rename succeeds
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// ChartLoadResult is the loaded chart plus the on-disk path it came from.
type ChartLoadResult struct {
	Path  string
	Chart *chart.Chart
}

// locateGitChart resolves a chart whose source is a GitRepository — the
// chart lives at <artifact.LocalPath>/<chart.Name>.
func (c *Client) locateGitChart(hr *manifest.HelmRelease) (string, error) {
	c.mu.RLock()
	g, ok := c.gitRepos[hr.RepoName()]
	c.mu.RUnlock()
	if !ok || g.Artifact == nil {
		return "", fmt.Errorf("%w: GitRepository %s not available for HelmRelease %s",
			manifest.ErrObjectNotFound, hr.RepoName(), hr.NamespacedName())
	}
	path := filepath.Join(g.Artifact.LocalPath, hr.Chart.Name)
	if _, err := os.Stat(filepath.Join(path, "Chart.yaml")); err != nil {
		return "", fmt.Errorf("chart not found at %s: %w", path, err)
	}
	return path, nil
}

// locateHelmRepoChart resolves a chart from a HelmRepository. For OCI
// HelmRepositories the URL is `oci://...` and we delegate to the OCI
// path. Otherwise we download the chart tarball via getter.
func (c *Client) locateHelmRepoChart(ctx context.Context, hr *manifest.HelmRelease) (string, error) {
	c.mu.RLock()
	r, ok := c.repos[hr.RepoName()]
	c.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("%w: HelmRepository %s not registered for HelmRelease %s",
			manifest.ErrObjectNotFound, hr.RepoName(), hr.NamespacedName())
	}

	if r.RepoType == manifest.RepoTypeOCI || strings.HasPrefix(r.URL, "oci://") {
		return c.fetchOCIChart(ctx, r.URL+"/"+hr.Chart.Name, hr.Chart.Version)
	}

	indexURL := strings.TrimSuffix(r.URL, "/") + "/index.yaml"
	idx, err := c.fetchIndex(indexURL)
	if err != nil {
		return "", err
	}
	cv, err := idx.Get(hr.Chart.Name, hr.Chart.Version)
	if err != nil {
		return "", fmt.Errorf("chart %s@%s not found in %s: %w", hr.Chart.Name, hr.Chart.Version, r.URL, err)
	}
	if len(cv.URLs) == 0 {
		return "", fmt.Errorf("chart %s@%s in %s has no URLs", hr.Chart.Name, hr.Chart.Version, r.URL)
	}
	chartURL, err := absChartURL(r.URL, cv.URLs[0])
	if err != nil {
		return "", err
	}

	cacheKey := safeName(hr.Chart.Name) + "-" + cv.Version + ".tgz"
	target := filepath.Join(c.cacheDir, cacheKey)

	lock := lockChartPath(target)
	lock.Lock()
	defer lock.Unlock()

	if _, err := os.Stat(target); err == nil {
		return target, nil
	}
	g, err := getter.NewHTTPGetter()
	if err != nil {
		return "", err
	}
	buf, err := g.Get(chartURL)
	if err != nil {
		return "", fmt.Errorf("download %s: %w", chartURL, err)
	}
	if err := writeAtomic(target, buf.Bytes()); err != nil {
		return "", err
	}
	return target, nil
}

// fetchIndex downloads and parses a HelmRepository index.yaml.
func (c *Client) fetchIndex(indexURL string) (*repo.IndexFile, error) {
	g, err := getter.NewHTTPGetter()
	if err != nil {
		return nil, err
	}
	buf, err := g.Get(indexURL)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", indexURL, err)
	}
	tmp, err := os.CreateTemp(c.tmpDir, "helm-index-*.yaml")
	if err != nil {
		return nil, err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(buf.Bytes()); err != nil {
		_ = tmp.Close()
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		return nil, err
	}
	defer func() { _ = os.Remove(tmpPath) }()
	idx, err := repo.LoadIndexFile(tmpPath)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", indexURL, err)
	}
	return idx, nil
}

// locateOCIChart resolves a chart whose source is an OCIRepository.
// The OCIRepository.url already points at the chart artifact (Flux's
// "chart-as-OCI-artifact" model) so we use it verbatim — the chart's
// short name from the HelmRelease is metadata, not part of the URL.
func (c *Client) locateOCIChart(ctx context.Context, hr *manifest.HelmRelease) (string, error) {
	c.mu.RLock()
	r, ok := c.ociRepos[hr.RepoName()]
	c.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("%w: OCIRepository %s not registered", manifest.ErrObjectNotFound, hr.RepoName())
	}
	ver, err := r.Version()
	if err != nil {
		return "", err
	}
	return c.fetchOCIChart(ctx, r.URL, ver)
}

// fetchOCIChart pulls an OCI chart via the helm registry client.
func (c *Client) fetchOCIChart(ctx context.Context, ref, version string) (string, error) {
	if c.registry == nil {
		return "", errors.New("helm registry client not initialized")
	}
	target := filepath.Join(c.cacheDir, safeName(filepath.Base(ref))+"-"+version+".tgz")

	lock := lockChartPath(target)
	lock.Lock()
	defer lock.Unlock()

	if _, err := os.Stat(target); err == nil {
		return target, nil
	}

	pullRef := ref
	if version != "" {
		pullRef = ref + ":" + version
	}
	_ = ctx // reserved for future per-pull cancellation when helm supports it
	result, err := c.registry.Pull(pullRef)
	if err != nil {
		return "", fmt.Errorf("oci pull %s: %w", pullRef, err)
	}
	if result == nil || result.Chart == nil {
		return "", fmt.Errorf("oci pull %s: empty result", pullRef)
	}
	if err := writeAtomic(target, result.Chart.Data); err != nil {
		return "", err
	}
	return target, nil
}

func absChartURL(base, urlStr string) (string, error) {
	u, err := url.Parse(urlStr)
	if err != nil {
		return "", err
	}
	if u.IsAbs() {
		return urlStr, nil
	}
	baseURL, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	return baseURL.ResolveReference(u).String(), nil
}

func safeName(s string) string {
	out := strings.Builder{}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-' || r == '_' || r == '.':
			out.WriteRune(r)
		default:
			out.WriteRune('-')
		}
	}
	return out.String()
}
