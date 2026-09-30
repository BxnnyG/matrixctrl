// Package ociregistry reads tag lists from an OCI registry — anonymously, the way any
// `helm pull` does, and across every page.
//
// Every page, because a registry hands tags out a page at a time and a caller that
// reads only the first sees a registry that stopped publishing. That happened twice:
// ESS's release tags sat beyond thousands of per-commit build tags (E42), and on
// 2026-09-30 MatrixCtrl's own chart passed 100 tags — GHCR's default page — so
// 0.1.118 was published and invisible to every installed update check (etappe 117).
// One implementation, so the lesson does not have to be learned a third time.
package ociregistry

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// pageSize is asked for explicitly; registries may cap it, which paging then covers.
const pageSize = 1000

// Token fetches an anonymous pull token for repo.
func Token(ctx context.Context, hc *http.Client, registry, repo string) (string, error) {
	u, err := url.Parse(registry)
	if err != nil {
		return "", err
	}
	tokenURL := fmt.Sprintf("%s/token?scope=repository:%s:pull&service=%s", strings.TrimRight(registry, "/"), repo, u.Host)
	var tok struct {
		Token string `json:"token"`
	}
	if _, err := getJSON(ctx, hc, tokenURL, "", &tok); err != nil {
		return "", fmt.Errorf("registry token: %w", err)
	}
	if tok.Token == "" {
		return "", fmt.Errorf("registry returned an empty token")
	}
	return tok.Token, nil
}

// Tags lists every tag of repo, following rel="next" for at most maxPages pages.
func Tags(ctx context.Context, hc *http.Client, registry, repo string, maxPages int) ([]string, error) {
	token, err := Token(ctx, hc, registry, repo)
	if err != nil {
		return nil, err
	}
	registry = strings.TrimRight(registry, "/")
	next := fmt.Sprintf("%s/v2/%s/tags/list?n=%d", registry, repo, pageSize)
	var all []string
	for page := 0; page < maxPages && next != ""; page++ {
		var list struct {
			Tags []string `json:"tags"`
		}
		link, err := getJSON(ctx, hc, next, token, &list)
		if err != nil {
			return nil, fmt.Errorf("tag list: %w", err)
		}
		all = append(all, list.Tags...)
		next = NextPage(registry, link)
	}
	return all, nil
}

// NextPage extracts the rel="next" target from a registry Link header, made absolute
// against registry.
func NextPage(registry, link string) string {
	for _, part := range strings.Split(link, ",") {
		part = strings.TrimSpace(part)
		if !strings.Contains(part, `rel="next"`) {
			continue
		}
		start := strings.Index(part, "<")
		end := strings.Index(part, ">")
		if start < 0 || end <= start {
			continue
		}
		path := part[start+1 : end]
		if strings.HasPrefix(path, "http") {
			return path
		}
		return strings.TrimRight(registry, "/") + path
	}
	return ""
}

// getJSON decodes url into into and returns the response's Link header.
func getJSON(ctx context.Context, hc *http.Client, url, bearer string, into any) (string, error) {
	if hc == nil {
		hc = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s answered %s", url, resp.Status)
	}
	return resp.Header.Get("Link"), json.NewDecoder(resp.Body).Decode(into)
}
