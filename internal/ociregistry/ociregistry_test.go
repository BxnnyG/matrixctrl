package ociregistry

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// A registry that pages the way GHCR does: at most `page` tags per answer, in push
// order, with a relative rel="next" link — and it ignores a larger n, as a registry
// that caps the page size may.
func pagedRegistry(t *testing.T, tags []string, page int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/token"):
			fmt.Fprint(w, `{"token":"anonymous"}`)
		case strings.HasSuffix(r.URL.Path, "/tags/list"):
			if r.Header.Get("Authorization") != "Bearer anonymous" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			start := 0
			if last := r.URL.Query().Get("last"); last != "" {
				for i, tag := range tags {
					if tag == last {
						start = i + 1
					}
				}
			}
			end := start + page
			if end > len(tags) {
				end = len(tags)
			}
			if end < len(tags) {
				w.Header().Set("Link", fmt.Sprintf(`<%s?last=%s&n=0>; rel="next"`, r.URL.Path, tags[end-1]))
			}
			fmt.Fprintf(w, `{"tags":["%s"]}`, strings.Join(tags[start:end], `","`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// 101 tags and a page of 100: the 101st — the newest release — is exactly the one a
// first-page reader loses. That is the state GHCR was in on 2026-09-30.
func TestTheTagPastTheFirstPageIsSeen(t *testing.T) {
	var tags []string
	for i := 0; i <= 100; i++ {
		tags = append(tags, "0.1."+strconv.Itoa(i+18))
	}
	srv := pagedRegistry(t, tags, 100)
	got, err := Tags(context.Background(), srv.Client(), srv.URL, "x/charts/y", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 101 || got[100] != "0.1.118" {
		t.Fatalf("got %d tags, last %q; want 101 ending in 0.1.118", len(got), got[len(got)-1])
	}
}

func TestNextPage(t *testing.T) {
	if got := NextPage("https://ghcr.io", `</v2/a/b/tags/list?last=0.1.117&n=0>; rel="next"`); got != "https://ghcr.io/v2/a/b/tags/list?last=0.1.117&n=0" {
		t.Errorf("relative link: %q", got)
	}
	if got := NextPage("https://ghcr.io", `<https://other/v2/x>; rel="next"`); got != "https://other/v2/x" {
		t.Errorf("absolute link: %q", got)
	}
	if got := NextPage("https://ghcr.io", ""); got != "" {
		t.Errorf("no link: %q", got)
	}
}
