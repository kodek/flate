package source

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"oras.land/oras-go/v2"
	orasfile "oras.land/oras-go/v2/content/file"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/credentials"

	"github.com/buroa/fluxrr/pkg/manifest"
	"github.com/buroa/fluxrr/pkg/store"
)

// FetchOCI pulls the OCIRepository artifact into cache. Credentials are
// read from a docker-style config.json honored by oras-go's
// credentials.NewFileStore.
func FetchOCI(ctx context.Context, cache *Cache, repo *manifest.OCIRepository, registryConfig string) (*store.OCIArtifact, error) {
	if repo == nil {
		return nil, errors.New("oci repository is nil")
	}
	if repo.URL == "" {
		return nil, fmt.Errorf("%w: OCIRepository %s missing url", manifest.ErrInput, repo.RepoName())
	}

	versioned := repo.VersionedURL()
	slot, exists, err := cache.Slot(versioned, "")
	if err != nil {
		return nil, fmt.Errorf("cache slot for %s: %w", versioned, err)
	}
	if exists {
		return &store.OCIArtifact{URL: repo.URL, LocalPath: slot, Ref: repo.Ref, Digest: repo.Ref.Digest}, nil
	}

	reference, err := parseOCIRef(versioned)
	if err != nil {
		_ = cache.Reset(slot)
		return nil, err
	}

	repoClient, err := remote.NewRepository(reference)
	if err != nil {
		_ = cache.Reset(slot)
		return nil, fmt.Errorf("oras: %w", err)
	}

	credStore, err := loadCredentials(registryConfig)
	if err != nil {
		_ = cache.Reset(slot)
		return nil, err
	}
	if credStore != nil {
		client := &auth.Client{Credential: credentials.Credential(credStore)}
		repoClient.Client = client
	}

	tag := versionTag(repo.Ref)
	if tag == "" {
		tag = "latest"
	}

	dest, err := orasfile.New(slot)
	if err != nil {
		return nil, fmt.Errorf("oras file store: %w", err)
	}
	defer dest.Close()

	desc, err := oras.Copy(ctx, repoClient, tag, dest, tag, oras.DefaultCopyOptions)
	if err != nil {
		_ = os.RemoveAll(slot)
		return nil, fmt.Errorf("oras copy %s: %w", versioned, err)
	}

	return &store.OCIArtifact{URL: repo.URL, LocalPath: slot, Ref: repo.Ref, Digest: desc.Digest.String()}, nil
}

// loadCredentials returns a credentials.Store backed by the given config
// path. An empty configPath uses the docker default lookup.
func loadCredentials(configPath string) (credentials.Store, error) {
	opts := credentials.StoreOptions{AllowPlaintextPut: false}
	if configPath != "" {
		s, err := credentials.NewFileStore(configPath)
		if err != nil {
			return nil, fmt.Errorf("load credentials %s: %w", configPath, err)
		}
		return s, nil
	}
	s, err := credentials.NewStoreFromDocker(opts)
	if err != nil {
		// Missing docker config is not fatal — anonymous pulls work.
		return nil, nil
	}
	return s, nil
}

// parseOCIRef converts a Flux versioned URL into the form oras-go expects:
//
//	oci://ghcr.io/owner/chart:tag  → ghcr.io/owner/chart
//	oci://ghcr.io/owner/chart@sha  → ghcr.io/owner/chart
//
// The tag/digest is dropped here and re-supplied to oras.Copy below.
func parseOCIRef(versioned string) (string, error) {
	versioned = strings.TrimPrefix(versioned, "oci://")
	// Strip ":<tag>" or "@<digest>" portion for the reference; oras
	// takes them separately.
	if i := strings.LastIndex(versioned, "@"); i > 0 {
		versioned = versioned[:i]
	}
	if i := strings.LastIndex(versioned, ":"); i > 0 {
		// Don't confuse port numbers with tags ("registry:5000/x").
		if !strings.Contains(versioned[i+1:], "/") {
			versioned = versioned[:i]
		}
	}
	if _, err := url.Parse("oci://" + versioned); err != nil {
		return "", fmt.Errorf("parse OCI ref %q: %w", versioned, err)
	}
	return versioned, nil
}

func versionTag(ref manifest.OCIRepositoryRef) string {
	switch {
	case ref.Digest != "":
		return ref.Digest
	case ref.Tag != "":
		return ref.Tag
	case ref.Semver != "":
		return ref.Semver
	}
	return ""
}
