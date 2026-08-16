package helm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type VersionInfo struct {
	Version     string    `json:"version"`
	PublishedAt time.Time `json:"published_at,omitempty"`
	Prerelease  bool      `json:"prerelease,omitempty"`
}

const (
	tagsListURL = "https://ghcr.io/v2/element-hq/ess-helm/matrix-stack/tags/list"
	// GHCR paginates tags. ESS publishes a per-commit "<version>-sha<40 hex>" tag
	// for every build, so the release tags we actually want sit far beyond the
	// first page — without following pagination the UI only ever showed ancient
	// 0.2.x dev builds.
	tagsPageSize = 1000
	maxTagPages  = 25
)

// releaseTagRe matches semver-shaped tags ("26.5.1", "v26.5.1", "26.5.1-rc.1").
var releaseTagRe = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)(?:-([0-9A-Za-z.-]+))?$`)

// buildTagRe matches the per-commit build tags ESS publishes for every merge
// ("26.7.3-sha<40 hex>"). They outnumber real releases ~150:1 and are never a
// valid upgrade target, so they never reach the UI.
var buildTagRe = regexp.MustCompile(`^sha[0-9a-f]{7,}$`)

// devTagRe matches the older build-tag convention: `0.7.2-dev`.
//
// Dropping these is not the same judgement as hiding pre-releases, so it is made
// here rather than left to the UI. A full walk of the registry — 3 pages, 2740 raw
// tags — yields 67 releases and exactly 12 suffixed tags, and all twelve are
// `0.x.y-dev` from the chart's first months. None has a GitHub release: the release
// index carries plain `0.1.0`, `0.2.0`, `0.3.0`. They are development builds under
// the naming scheme that preceded `-sha…`, not versions anyone can upgrade to.
//
// The UI used to offer a "show pre-releases" toggle for them, which revealed twelve
// 2024 dev builds of a chart now on 26.8.0 — and, because the list renders 25 rows
// and they sort at index 56 and beyond, usually revealed nothing at all (E42, E43).
//
// Prerelease itself stays: `26.9.0-rc.1` would be a real pre-release worth showing.
// ESS has never published one, which is a fact about today rather than a rule.
var devTagRe = regexp.MustCompile(`^dev$`)

// ListVersions queries the GHCR OCI registry for available ESS chart versions,
// newest first.
func ListVersions(ctx context.Context) ([]VersionInfo, error) {
	token, err := getGHCRToken(ctx)
	if err != nil {
		return nil, err
	}

	var versions []VersionInfo
	seen := map[string]bool{}
	next := fmt.Sprintf("%s?n=%d", tagsListURL, tagsPageSize)

	for page := 0; page < maxTagPages && next != ""; page++ {
		tags, link, err := fetchTagPage(ctx, next, token)
		if err != nil {
			return nil, err
		}
		for _, tag := range tags {
			v, ok := parseReleaseTag(tag)
			if !ok || seen[v.Version] {
				continue
			}
			seen[v.Version] = true
			versions = append(versions, v)
		}
		next = link
	}

	sort.Slice(versions, func(i, j int) bool {
		return compareVersions(versions[i].Version, versions[j].Version) > 0
	})
	// Dates come from a second source and are allowed to be missing — see
	// releaseindex.go. The list is correct and complete without them.
	return withDates(ctx, versions), nil
}

func fetchTagPage(ctx context.Context, url, token string) ([]string, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("list tags: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("list tags: status %d", resp.StatusCode)
	}

	var result struct {
		Tags []string `json:"tags"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, "", err
	}
	return result.Tags, nextPageURL(resp.Header.Get("Link")), nil
}

// nextPageURL extracts the rel="next" target from a registry Link header.
func nextPageURL(link string) string {
	if link == "" {
		return ""
	}
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
		return "https://ghcr.io" + path
	}
	return ""
}

func parseReleaseTag(tag string) (VersionInfo, bool) {
	m := releaseTagRe.FindStringSubmatch(tag)
	if m == nil || buildTagRe.MatchString(m[4]) || devTagRe.MatchString(m[4]) {
		return VersionInfo{}, false
	}
	return VersionInfo{Version: tag, Prerelease: m[4] != ""}, true
}

// compareVersions orders release tags numerically. A plain string sort put
// "26.5.1" above "26.10.0", which is wrong — 26.10.0 is the newer release.
// Returns >0 if a is newer than b.
func compareVersions(a, b string) int {
	ma := releaseTagRe.FindStringSubmatch(a)
	mb := releaseTagRe.FindStringSubmatch(b)
	if ma == nil || mb == nil {
		return strings.Compare(a, b)
	}
	for i := 1; i <= 3; i++ {
		na, _ := strconv.Atoi(ma[i])
		nb, _ := strconv.Atoi(mb[i])
		if na != nb {
			return na - nb
		}
	}
	// Same x.y.z: a release outranks its prereleases (1.0.0 > 1.0.0-rc.1).
	switch {
	case ma[4] == "" && mb[4] != "":
		return 1
	case ma[4] != "" && mb[4] == "":
		return -1
	default:
		return strings.Compare(ma[4], mb[4])
	}
}

// CompareVersions is exported for callers that need to know whether an upgrade
// target is actually newer than what is deployed.
func CompareVersions(a, b string) int { return compareVersions(a, b) }

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
