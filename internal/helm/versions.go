package helm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

type VersionInfo struct {
	Version     string    `json:"version"`
	PublishedAt time.Time `json:"published_at,omitempty"`
}

// ListVersions queries the GHCR OCI registry for available ESS chart versions.
func ListVersions(ctx context.Context) ([]VersionInfo, error) {
	// Get anonymous token for ghcr.io
	token, err := getGHCRToken(ctx)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://ghcr.io/v2/element-hq/ess-helm/matrix-stack/tags/list", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list tags: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list tags: status %d", resp.StatusCode)
	}

	var result struct {
		Tags []string `json:"tags"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	// Filter to semver-looking tags only
	var versions []VersionInfo
	for _, tag := range result.Tags {
		if isVersionTag(tag) {
			versions = append(versions, VersionInfo{Version: tag})
		}
	}

	sort.Slice(versions, func(i, j int) bool {
		return versions[i].Version > versions[j].Version
	})

	return versions, nil
}

func getGHCRToken(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://ghcr.io/token?scope=repository:element-hq/ess-helm/matrix-stack:pull&service=ghcr.io", nil)
	if err != nil {
		return "", err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("get token: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	return result.Token, nil
}

func isVersionTag(tag string) bool {
	// Accept tags like "26.5.1", "v26.5.1", "26.5.1-beta.1"
	if strings.HasPrefix(tag, "v") {
		tag = tag[1:]
	}
	parts := strings.SplitN(tag, ".", 3)
	return len(parts) >= 2
}
