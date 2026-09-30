package synapse

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

// The sweep has to keep asking until Synapse's total is reached — a server that talks
// to more destinations than one page holds must not look like it talks to fewer.
func TestAllDestinationsFollowsThePages(t *testing.T) {
	const total = 2500
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/_synapse/admin/v1/federation/destinations" {
			http.NotFound(w, r)
			return
		}
		from, _ := strconv.Atoi(r.URL.Query().Get("from"))
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		fmt.Fprint(w, `{"destinations":[`)
		for i := from; i < from+limit && i < total; i++ {
			if i > from {
				fmt.Fprint(w, ",")
			}
			fmt.Fprintf(w, `{"destination":"s%d.example","retry_interval":0,"failure_ts":null}`, i)
		}
		fmt.Fprintf(w, `],"total":%d}`, total)
	}))
	defer srv.Close()

	c := New(srv.URL, func(context.Context) (string, error) { return "t", nil })
	all, cut, err := c.AllDestinations(context.Background())
	if err != nil || cut || len(all) != total {
		t.Fatalf("got %d destinations, cut=%v, err=%v; want %d", len(all), cut, err, total)
	}
	if all[total-1].Destination != "s2499.example" {
		t.Errorf("last = %q", all[total-1].Destination)
	}
}

// A destination is a server name, which may carry a port. It goes into the path as one
// segment, and "/" in it must not open a different endpoint.
func TestDestinationIsOnePathSegment(t *testing.T) {
	var seen string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.URL.EscapedPath()
		fmt.Fprint(w, `{}`)
	}))
	defer srv.Close()
	c := New(srv.URL, func(context.Context) (string, error) { return "t", nil })

	if err := c.ResetConnection(context.Background(), "matrix.example.com:8448"); err != nil {
		t.Fatal(err)
	}
	if seen != "/_synapse/admin/v1/federation/destinations/matrix.example.com:8448/reset_connection" {
		t.Errorf("path = %q", seen)
	}
	_ = c.ResetConnection(context.Background(), "evil/../../v2/users")
	if seen != "/_synapse/admin/v1/federation/destinations/evil%2F..%2F..%2Fv2%2Fusers/reset_connection" {
		t.Errorf("a slash left its segment: %q", seen)
	}
}
