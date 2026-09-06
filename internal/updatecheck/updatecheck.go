// Package updatecheck answers one question the product could not answer about
// itself: is there a newer MatrixCtrl than the one running?
//
// An operator asked three things in a row — which version is this, is there a newer
// one, and how do I get it — and the product had no answer to any of them. The
// version existed only in a log line at startup and in backup manifests.
//
// It asks GHCR, the same registry `helm install` resolves against, using the
// anonymous pull token any client gets. The cluster already pulls its image from
// there, so this opens no new relationship with anyone.
package updatecheck

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/Masterminds/semver/v3"
)

// Result is what the UI needs to say something useful, including when the check
// itself failed.
//
// A failed check reports as a Result with Error set, never as an error return. The
// version endpoint answers a question that is always answerable locally; letting a
// network call to a third party decide whether it responds at all is how a dashboard
// ends up blank because something optional was unreachable (§4.78).
type Result struct {
	Current   string    `json:"current"`
	Latest    string    `json:"latest,omitempty"`
	Available bool      `json:"available"`
	CheckedAt time.Time `json:"checked_at,omitempty"`
	// Why there is no answer, in words. Empty when the check succeeded.
	Error string `json:"error,omitempty"`
}

// Checker caches, because the question changes at most once per release and the UI
// asks it on every page load.
type Checker struct {
	registry string // https://ghcr.io, overridable for tests
	repo     string // bxnnyg/charts/matrixctrl
	current  string
	client   *http.Client
	ttl      time.Duration
	now      func() time.Time

	mu       sync.Mutex
	cached   Result
	fetched  time.Time
	hasValue bool
}

func New(current string) *Checker {
	return &Checker{
		registry: "https://ghcr.io",
		repo:     "bxnnyg/charts/matrixctrl",
		current:  current,
		client:   &http.Client{Timeout: 8 * time.Second},
		ttl:      6 * time.Hour,
		now:      time.Now,
	}
}

// Check returns the newest published version, from cache when it is fresh.
func (c *Checker) Check(ctx context.Context) Result {
	c.mu.Lock()
	if c.hasValue && c.now().Sub(c.fetched) < c.ttl {
		r := c.cached
		c.mu.Unlock()
		return r
	}
	c.mu.Unlock()

	r := c.fetch(ctx)

	c.mu.Lock()
	defer c.mu.Unlock()
	// A failed check does not evict a good answer: the newest version we ever saw is
	// still better information than "could not reach the registry", and the operator
	// gets told when it was last confirmed.
	if r.Error != "" && c.hasValue {
		stale := c.cached
		stale.Error = r.Error
		return stale
	}
	c.cached, c.fetched, c.hasValue = r, c.now(), true
	return r
}

func (c *Checker) fetch(ctx context.Context) Result {
	out := Result{Current: c.current, CheckedAt: c.now().UTC()}

	// A development build has no version to compare against, and inventing one would
	// mean telling a developer their working copy is out of date.
	cur, err := semver.NewVersion(c.current)
	if err != nil {
		out.Error = fmt.Sprintf("running version %q is not a release version", c.current)
		return out
	}

	tags, err := c.tags(ctx)
	if err != nil {
		out.Error = err.Error()
		return out
	}

	var newest *semver.Version
	for _, t := range tags {
		v, err := semver.NewVersion(t)
		if err != nil {
			continue // "latest" and anything else that is not a release
		}
		if newest == nil || v.GreaterThan(newest) {
			newest = v
		}
	}
	if newest == nil {
		out.Error = "the registry published no version-shaped tags"
		return out
	}
	out.Latest = newest.String()
	out.Available = newest.GreaterThan(cur)
	return out
}

// tags reads the repository's tag list with an anonymous pull token.
func (c *Checker) tags(ctx context.Context) ([]string, error) {
	tokenURL := fmt.Sprintf("%s/token?scope=repository:%s:pull&service=ghcr.io", c.registry, c.repo)
	var tok struct {
		Token string `json:"token"`
	}
	if err := c.getJSON(ctx, tokenURL, "", &tok); err != nil {
		return nil, fmt.Errorf("registry token: %w", err)
	}
	if tok.Token == "" {
		return nil, fmt.Errorf("registry returned an empty token")
	}

	var list struct {
		Tags []string `json:"tags"`
	}
	listURL := fmt.Sprintf("%s/v2/%s/tags/list", c.registry, c.repo)
	if err := c.getJSON(ctx, listURL, tok.Token, &list); err != nil {
		return nil, fmt.Errorf("tag list: %w", err)
	}
	sort.Strings(list.Tags)
	return list.Tags, nil
}

func (c *Checker) getJSON(ctx context.Context, url, bearer string, into interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s answered %s", url, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(into)
}
