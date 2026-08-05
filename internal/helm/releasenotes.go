package helm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sync"
	"time"
)

// Release notes for an ESS chart version.
//
// Less cosmetic than it sounds. The notes for 26.8.0 say, in their own body,
// "Upgrade Element Web to v1.12.25" and "Upgrade Synapse to v1.158.0" — exactly the
// two upgrades the operator's pinned image tags were silently preventing (E31). One
// screen now carries both halves: what the version brings, and what a pin will stop
// it bringing.
//
// ListVersions already fetches from ghcr.io, so reaching the internet is established
// behaviour here. Unlike the reachability check in internal/reach, this discloses
// nothing about the deployment: it is a public GET for a public version's notes.

const (
	releaseNotesURL = "https://api.github.com/repos/element-hq/ess-helm/releases/tags/%s"
	notesTimeout    = 10 * time.Second
	// notesBodyLimit bounds the read. Release notes are a couple of kilobytes; an
	// unbounded read from a third party is how a status page becomes the outage.
	notesBodyLimit = 512 << 10
	// notesCacheMax bounds the cache. A published version's notes do not change, so
	// entries never expire — but the number of versions does grow, and unbounded is
	// unbounded.
	notesCacheMax = 64
)

// ReleaseNotes is one version's published notes.
type ReleaseNotes struct {
	Version     string `json:"version"`
	Available   bool   `json:"available"`
	Title       string `json:"title,omitempty"`
	PublishedAt string `json:"published_at,omitempty"`
	Body        string `json:"body,omitempty"`
	URL         string `json:"url,omitempty"`
	// Reason explains an unavailable result, so an air-gapped install reads
	// "could not be fetched" rather than "this version has no notes".
	Reason string `json:"reason,omitempty"`
}

// safeVersion is deliberately strict. The version becomes a URL path segment, and a
// value with a slash or a dot-dot in it would address a different GitHub resource
// entirely. Escaping would be enough; refusing is simpler to be sure of.
var safeVersion = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z.\-_+]{0,63}$`)

var (
	notesMu    sync.Mutex
	notesCache = map[string]ReleaseNotes{}
)

// FetchReleaseNotes returns the notes for a chart version.
//
// Never returns an error for a missing or unreachable release: the caller is a page
// that should still render. Only a malformed version is an error, because that is a
// bug in the caller rather than a condition of the world.
func FetchReleaseNotes(ctx context.Context, version string) (ReleaseNotes, error) {
	if !safeVersion.MatchString(version) {
		return ReleaseNotes{}, fmt.Errorf("invalid version")
	}

	notesMu.Lock()
	cached, ok := notesCache[version]
	notesMu.Unlock()
	if ok {
		return cached, nil
	}

	out := fetchNotes(ctx, version)

	notesMu.Lock()
	// A failed fetch is cached too, briefly in effect: without it, a page that
	// polls would hammer a rate-limited API and stay empty either way. It is
	// evicted with the rest when the cache is trimmed.
	if len(notesCache) >= notesCacheMax {
		notesCache = map[string]ReleaseNotes{}
	}
	notesCache[version] = out
	notesMu.Unlock()

	return out, nil
}

func fetchNotes(ctx context.Context, version string) ReleaseNotes {
	out := ReleaseNotes{Version: version}

	reqCtx, cancel := context.WithTimeout(ctx, notesTimeout)
	defer cancel()

	endpoint := fmt.Sprintf(releaseNotesURL, url.PathEscape(version))
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		out.Reason = "Anfrage konnte nicht gestellt werden."
		return out
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		out.Reason = "Die Release Notes konnten nicht abgerufen werden (keine Verbindung)."
		return out
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		out.Reason = "Für diese Version sind keine Release Notes veröffentlicht."
		return out
	case http.StatusForbidden, http.StatusTooManyRequests:
		out.Reason = "Das Limit der GitHub-API ist erreicht — später erneut versuchen."
		return out
	default:
		out.Reason = fmt.Sprintf("GitHub antwortete mit %d.", resp.StatusCode)
		return out
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, notesBodyLimit))
	if err != nil {
		out.Reason = "Die Antwort konnte nicht gelesen werden."
		return out
	}

	var doc struct {
		Name        string `json:"name"`
		Body        string `json:"body"`
		PublishedAt string `json:"published_at"`
		HTMLURL     string `json:"html_url"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		out.Reason = "Die Antwort war nicht lesbar."
		return out
	}
	if doc.Body == "" {
		out.Reason = "Für diese Version sind keine Release Notes hinterlegt."
		return out
	}

	out.Available = true
	out.Title = doc.Name
	out.Body = doc.Body
	out.PublishedAt = doc.PublishedAt
	out.URL = doc.HTMLURL
	return out
}
