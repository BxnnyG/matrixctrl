package synapse

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

// Destination is one remote server Synapse federates with, as the admin API reports it
// (etappe 117). Times are milliseconds since the epoch.
//
// FailureTS set means "failing since"; nil means the last attempt worked. RetryLastTS
// plus RetryInterval is when Synapse will try again on its own — which after a long
// outage can be days away, the reason ResetConnection exists.
type Destination struct {
	Destination                  string `json:"destination"`
	RetryLastTS                  *int64 `json:"retry_last_ts"`
	RetryInterval                int64  `json:"retry_interval"`
	FailureTS                    *int64 `json:"failure_ts"`
	LastSuccessfulStreamOrdering *int64 `json:"last_successful_stream_ordering"`
}

// destinationPageSize is Synapse's page size for the sweep; destinationCap bounds it.
// A small server talks to tens of servers, a busy one to thousands — past the cap the
// caller is told the list is cut, never handed a silently partial one as complete.
const (
	destinationPageSize = 1000
	destinationCap      = 10000
)

// AllDestinations returns every destination up to the cap, and whether it stopped there.
//
// All at once rather than a page at a time: the question is "which ones are failing",
// and Synapse cannot filter by that — ordering by failure_ts puts the never-failed
// (NULL) rows first under Postgres' DESC. The caller sorts and filters instead.
func (c *Client) AllDestinations(ctx context.Context) ([]Destination, bool, error) {
	var out []Destination
	from := 0
	for {
		q := url.Values{}
		q.Set("from", strconv.Itoa(from))
		q.Set("limit", strconv.Itoa(destinationPageSize))
		raw, err := c.get(ctx, c.baseURL+"/_synapse/admin/v1/federation/destinations?"+q.Encode())
		if err != nil {
			return nil, false, err
		}
		var page struct {
			Destinations []Destination `json:"destinations"`
			Total        int           `json:"total"`
		}
		if err := json.Unmarshal(raw, &page); err != nil {
			return nil, false, fmt.Errorf("could not read the destination list: %w", err)
		}
		out = append(out, page.Destinations...)
		from += len(page.Destinations)
		if len(page.Destinations) == 0 || from >= page.Total {
			return out, false, nil
		}
		if len(out) >= destinationCap {
			return out, true, nil
		}
	}
}

// DestinationRoom is a room this server shares with a destination.
type DestinationRoom struct {
	RoomID         string `json:"room_id"`
	StreamOrdering int64  `json:"stream_ordering"`
}

// DestinationRooms lists the rooms shared with one destination, up to limit.
func (c *Client) DestinationRooms(ctx context.Context, destination string, limit int) ([]DestinationRoom, int, error) {
	if limit <= 0 || limit > maxLimit {
		limit = defaultLimit
	}
	endpoint := c.baseURL + "/_synapse/admin/v1/federation/destinations/" + url.PathEscape(destination) +
		"/rooms?limit=" + strconv.Itoa(limit)
	raw, err := c.get(ctx, endpoint)
	if err != nil {
		return nil, 0, err
	}
	var page struct {
		Rooms []DestinationRoom `json:"rooms"`
		Total int               `json:"total"`
	}
	if err := json.Unmarshal(raw, &page); err != nil {
		return nil, 0, fmt.Errorf("could not read the rooms for %s: %w", destination, err)
	}
	return page.Rooms, page.Total, nil
}

// ResetConnection makes Synapse try a failing destination now instead of waiting out
// its backoff.
func (c *Client) ResetConnection(ctx context.Context, destination string) error {
	endpoint := c.baseURL + "/_synapse/admin/v1/federation/destinations/" + url.PathEscape(destination) + "/reset_connection"
	_, err := c.do(ctx, http.MethodPost, endpoint, map[string]any{})
	return err
}
