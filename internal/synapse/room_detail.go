package synapse

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

// One room, its members, and the one moderation action that can be taken back
// (etappe 41).
//
// Deleting a room is deliberately absent. It evicts every member and purges the
// history, and nothing undoes it — it gets its own etappe, the way user
// deactivation did in E28 rather than riding along with the user list.

// RoomDetail is a single room, from GET /_synapse/admin/v1/rooms/<id>.
//
// The v2 path is *not* the newer version of this. `GET /_synapse/admin/v2/rooms/<id>`
// answers 405: that route exists only for DELETE. Reaching for it to read a room
// would be reaching for the one thing this etappe does not do.
type RoomDetail struct {
	Room
	// Blocked is not part of the room object Synapse returns here; GetRoom fills it
	// in from the block endpoint, because the two questions have two endpoints and
	// pretending otherwise would mean guessing one of them.
	Blocked bool `json:"blocked"`
}

// MemberPage is the members endpoint's answer.
//
// Synapse returns the whole list — this endpoint has no paging of its own — so the
// page boundaries here are ours. A room with 5 000 members would otherwise render
// 5 000 rows to answer a question that the first twenty usually settle.
type MemberPage struct {
	Members []string `json:"members"`
	Total   int      `json:"total"`
	// Offset and Returned describe the slice actually handed back, so the UI can
	// say "1–50 of 5000" truthfully rather than implying it has everything.
	Offset   int `json:"offset"`
	Returned int `json:"returned"`
}

// roomPath escapes a room ID for use in a URL path.
//
// Room IDs look like `!AbCdEf:example.org` — the `!` and `:` are part of the ID, not
// punctuation. url.PathEscape leaves `:` alone, which is legal inside a path segment
// and which Synapse accepts; the `!` is escaped. Building this by concatenation is
// the most likely way for this feature to break quietly on a real ID while working
// on every example anyone types by hand.
func roomPath(roomID string) string {
	return url.PathEscape(roomID)
}

// GetRoom returns one room's details together with whether it is blocked.
//
// Two calls, because Synapse keeps the answers in two places. The block state is
// read rather than inferred: after a write, showing what was *requested* instead of
// what the server now reports is how a UI ends up confidently displaying a change
// that silently failed.
func (c *Client) GetRoom(ctx context.Context, roomID string) (*RoomDetail, error) {
	raw, err := c.get(ctx, c.baseURL+"/_synapse/admin/v1/rooms/"+roomPath(roomID))
	if err != nil {
		return nil, err
	}
	var detail RoomDetail
	if err := json.Unmarshal(raw, &detail); err != nil {
		return nil, fmt.Errorf("could not read the room: %w", err)
	}

	blocked, err := c.IsBlocked(ctx, roomID)
	if err != nil {
		// A room that loads but whose block state does not is still worth showing;
		// the failure belongs to one field, not to the page. It surfaces as "not
		// blocked" only because that is the false-by-default of a bool — which is why
		// the block control reads its own state again before it is used, rather than
		// trusting what the detail call managed to fill in.
		return &detail, nil
	}
	detail.Blocked = blocked
	return &detail, nil
}

// ListMembers returns one page of a room's joined members.
//
// Synapse's endpoint returns all of them at once, so from/limit are applied here.
// Doing the slicing on this side is honest about where the boundary is: the network
// cost is the full list either way, and pretending the server paged it would set the
// wrong expectation for anyone reading this later.
func (c *Client) ListMembers(ctx context.Context, roomID string, from, limit int) (*MemberPage, error) {
	raw, err := c.get(ctx, c.baseURL+"/_synapse/admin/v1/rooms/"+roomPath(roomID)+"/members")
	if err != nil {
		return nil, err
	}
	var all struct {
		Members []string `json:"members"`
		Total   int      `json:"total"`
	}
	if err := json.Unmarshal(raw, &all); err != nil {
		return nil, fmt.Errorf("could not read the member list: %w", err)
	}

	if from < 0 {
		from = 0
	}
	page := &MemberPage{
		Members: sliceMembers(all.Members, from, limit),
		Total:   all.Total,
		Offset:  from,
	}
	page.Returned = len(page.Members)
	// Synapse reports `total` itself, but a server that omits it would make the UI
	// claim "1–50 of 0". The length is the fallback that cannot be wrong.
	if page.Total == 0 {
		page.Total = len(all.Members)
	}
	return page, nil
}

// IsBlocked reports whether new joins to the room are refused.
func (c *Client) IsBlocked(ctx context.Context, roomID string) (bool, error) {
	raw, err := c.get(ctx, c.baseURL+"/_synapse/admin/v1/rooms/"+roomPath(roomID)+"/block")
	if err != nil {
		return false, err
	}
	var out struct {
		Block bool `json:"block"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return false, fmt.Errorf("could not read the block state: %w", err)
	}
	return out.Block, nil
}

// SetBlocked turns the block flag on or off and returns the state the server
// reports afterwards.
//
// **Blocking prevents new joins and nothing else.** It does not remove anyone
// already in the room, delete any message, or stop existing members from talking.
// The word suggests something far more final than it is, which is the mirror image
// of the E28 finding about `deactivate` — there a verb did more than it sounded
// like, here it does considerably less. Either way the operator has to be told
// before they act, not after.
func (c *Client) SetBlocked(ctx context.Context, roomID string, block bool) (bool, error) {
	_, err := c.do(ctx, http.MethodPut,
		c.baseURL+"/_synapse/admin/v1/rooms/"+roomPath(roomID)+"/block",
		map[string]bool{"block": block})
	if err != nil {
		return false, err
	}
	// Read back rather than returning the requested value. The write answering 200
	// is not the same claim as the flag now being set, and this is the field the UI
	// renders as a fact.
	return c.IsBlocked(ctx, roomID)
}

// sliceMembers takes one page out of the full member list.
//
// Separate from ListMembers so the boundary arithmetic is testable without a
// server. Every case that goes wrong here is an off-by-one at an edge — a page
// starting past the end, a page running off it — and those are exactly the ones
// that never come up while clicking through a small room.
func sliceMembers(all []string, from, limit int) []string {
	if from < 0 {
		from = 0
	}
	if limit <= 0 {
		limit = defaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	// Never nil: the frontend checks `.length`, and nil marshals to JSON null.
	// The room list hit exactly this in E36.
	if from >= len(all) {
		return []string{}
	}
	end := from + limit
	if end > len(all) {
		end = len(all)
	}
	out := make([]string, end-from)
	copy(out, all[from:end])
	return out
}
