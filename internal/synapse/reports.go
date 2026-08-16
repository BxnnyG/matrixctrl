package synapse

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"

	"context"
)

// The event report queue: what users have reported, from Synapse's admin API
// (etappe 46).
//
// The whole of this file exists to serve one screen, so the naming is deliberately
// *not* Synapse's. Its report object carries two user IDs — `user_id` is whoever
// filed the report and `sender` is whoever sent the reported event — and a field
// called UserID sitting next to one called Sender is an invitation to render the
// wrong person as the offender. They are Reporter and Sender here, and the JSON tags
// carry the translation.

// Report is one entry in the queue.
type Report struct {
	ID      int64  `json:"id"`
	EventID string `json:"event_id"`
	RoomID  string `json:"room_id"`
	// Name and CanonicalAlias describe the room, and are frequently empty — a room
	// with no name is normal, not an error.
	Name           string `json:"name,omitempty"`
	CanonicalAlias string `json:"canonical_alias,omitempty"`

	// Reporter filed the report. Synapse calls this `user_id`.
	Reporter string `json:"reporter"`
	// Sender wrote the reported event. Synapse calls this `sender`.
	Sender string `json:"sender"`

	Reason string `json:"reason,omitempty"`
	// Score is the reporter's severity rating, -100 to 0 in the Matrix spec. Zero is
	// both "not severe" and "not supplied", and no client this project has seen sets
	// it, so it is carried but never presented as a measurement.
	Score      int   `json:"score,omitempty"`
	ReceivedTS int64 `json:"received_ts"`
}

// reportWire is Synapse's shape. Kept separate so the rename above is performed in
// exactly one place rather than by every reader remembering which is which.
type reportWire struct {
	ID             int64  `json:"id"`
	EventID        string `json:"event_id"`
	RoomID         string `json:"room_id"`
	Name           string `json:"name"`
	CanonicalAlias string `json:"canonical_alias"`
	UserID         string `json:"user_id"` // the reporter
	Sender         string `json:"sender"`  // the reported event's author
	Reason         string `json:"reason"`
	Score          int    `json:"score"`
	ReceivedTS     int64  `json:"received_ts"`
}

func (w reportWire) toReport() Report {
	return Report{
		ID: w.ID, EventID: w.EventID, RoomID: w.RoomID,
		Name: w.Name, CanonicalAlias: w.CanonicalAlias,
		Reporter: w.UserID, Sender: w.Sender,
		Reason: w.Reason, Score: w.Score, ReceivedTS: w.ReceivedTS,
	}
}

// ReportPage is one page of the queue.
type ReportPage struct {
	Reports []Report `json:"reports"`
	Total   int      `json:"total"`
	// NextToken is Synapse's offset for the next page, absent on the last one.
	NextToken *int64 `json:"next_token,omitempty"`
}

// ReportDetail adds the reported event itself.
type ReportDetail struct {
	Report
	// EventJSON is the reported event, passed through rather than modelled. The
	// screen shows the body and the sender; modelling every event type Matrix has
	// in order to display a quoted message would be a large surface for one panel.
	EventJSON json.RawMessage `json:"event_json,omitempty"`
}

// ReportOptions selects a page of the queue.
type ReportOptions struct {
	From  int
	Limit int
	// Dir is "f" (oldest first) or "b" (newest first). Newest first is the default
	// here, unlike Synapse's, because a moderation queue is read from the top.
	Dir string
	// UserID and RoomID filter, both optional.
	UserID string
	RoomID string
}

func (o ReportOptions) query() string {
	v := url.Values{}
	if o.From > 0 {
		v.Set("from", strconv.Itoa(o.From))
	}
	limit := o.Limit
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	v.Set("limit", strconv.Itoa(limit))
	dir := o.Dir
	if dir != "f" {
		dir = "b"
	}
	v.Set("dir", dir)
	if o.UserID != "" {
		v.Set("user_id", o.UserID)
	}
	if o.RoomID != "" {
		v.Set("room_id", o.RoomID)
	}
	return v.Encode()
}

// ListReports returns one page of the report queue.
func (c *Client) ListReports(ctx context.Context, opts ReportOptions) (*ReportPage, error) {
	raw, err := c.get(ctx, c.baseURL+"/_synapse/admin/v1/event_reports?"+opts.query())
	if err != nil {
		return nil, err
	}

	var wire struct {
		EventReports []reportWire `json:"event_reports"`
		Total        int          `json:"total"`
		NextToken    *int64       `json:"next_token"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, fmt.Errorf("could not read the report queue: %w", err)
	}

	// Never nil: an empty queue must render as "no reports", and a nil slice reaches
	// the frontend as `null`, where every `.map` on it is a crash (etappe 41).
	page := &ReportPage{Reports: make([]Report, 0, len(wire.EventReports)),
		Total: wire.Total, NextToken: wire.NextToken}
	for _, w := range wire.EventReports {
		page.Reports = append(page.Reports, w.toReport())
	}
	return page, nil
}

// GetReport returns one report with the reported event attached.
func (c *Client) GetReport(ctx context.Context, id int64) (*ReportDetail, error) {
	raw, err := c.get(ctx, c.baseURL+"/_synapse/admin/v1/event_reports/"+strconv.FormatInt(id, 10))
	if err != nil {
		return nil, err
	}

	var wire struct {
		reportWire
		EventJSON json.RawMessage `json:"event_json"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, fmt.Errorf("could not read the report: %w", err)
	}
	return &ReportDetail{Report: wire.reportWire.toReport(), EventJSON: wire.EventJSON}, nil
}

// ReportedBody digs the message text out of a reported event.
//
// Returns "" whenever it is not a plain `content.body` string, which is the honest
// answer for a reported image, reaction, state change or encrypted event. A screen
// that invented a description for those would be describing something it cannot
// read — and an encrypted event is exactly the case where the admin must know the
// server cannot see it either.
func ReportedBody(eventJSON json.RawMessage) string {
	if len(eventJSON) == 0 {
		return ""
	}
	var ev struct {
		Content struct {
			Body string `json:"body"`
		} `json:"content"`
	}
	if err := json.Unmarshal(eventJSON, &ev); err != nil {
		return ""
	}
	return ev.Content.Body
}

// ReportedEventType returns the event's type, or "" if absent. `m.room.encrypted`
// is why this is shown: it is the difference between "there is nothing to display"
// and "the server cannot read this".
func ReportedEventType(eventJSON json.RawMessage) string {
	if len(eventJSON) == 0 {
		return ""
	}
	var ev struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(eventJSON, &ev); err != nil {
		return ""
	}
	return ev.Type
}
