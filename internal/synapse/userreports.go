package synapse

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
)

// The user report queue (etappe 48) — reports about a *person*, as opposed to the
// event reports in reports.go.
//
// Synapse serves this from `rest/admin/user_reports.py` and it is a genuinely
// different object, not a variant of the other one: no room, no event, no score. Two
// user ids and a reason.
//
// The same naming rule as reports.go applies and matters more here, because both ids
// are plain user ids with nothing in their shape to tell them apart. Synapse's
// `user_id` is whoever *filed* the report and `target_user_id` is whoever was
// reported. Reporter and Target here.
//
// There is deliberately no GetUserReport. Synapse has the endpoint, but
// `get_user_report` returns exactly the five fields `get_user_reports_paginate`
// already put in the list row, so fetching it would be a second round trip for an
// identical answer. The event queue needs its detail call for `event_json`; this one
// has nothing to add.

// UserReport is one entry in the user-report queue.
type UserReport struct {
	ID int64 `json:"id"`
	// Reporter filed the report. Synapse calls this `user_id`.
	Reporter string `json:"reporter"`
	// Target is the reported user. Synapse calls this `target_user_id`.
	Target     string `json:"target"`
	Reason     string `json:"reason,omitempty"`
	ReceivedTS int64  `json:"received_ts"`
}

// userReportWire is Synapse's shape, kept separate so the rename happens once.
type userReportWire struct {
	ID           int64  `json:"id"`
	ReceivedTS   int64  `json:"received_ts"`
	TargetUserID string `json:"target_user_id"` // the reported user
	UserID       string `json:"user_id"`        // the reporter
	Reason       string `json:"reason"`
}

func (w userReportWire) toUserReport() UserReport {
	return UserReport{
		ID: w.ID, Reporter: w.UserID, Target: w.TargetUserID,
		Reason: w.Reason, ReceivedTS: w.ReceivedTS,
	}
}

// UserReportPage is one page of the queue.
type UserReportPage struct {
	Reports []UserReport `json:"reports"`
	Total   int          `json:"total"`
	// NextToken is Synapse's offset for the next page, absent on the last one.
	NextToken *int64 `json:"next_token,omitempty"`
}

// UserReportOptions selects a page.
//
// Reporter and Target are *searches*, not filters: Synapse matches them with
// `LIKE '%…%'`, so a query for one user also matches every user id containing it.
// Named for what they do so a narrowed count is not read as exact.
type UserReportOptions struct {
	From  int
	Limit int
	// Dir is "f" (oldest first) or "b" (newest first); newest first by default,
	// because a moderation queue is read from the top.
	Dir            string
	ReporterSearch string
	TargetSearch   string
}

func (o UserReportOptions) query() string {
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
	if o.ReporterSearch != "" {
		v.Set("user_id", o.ReporterSearch)
	}
	if o.TargetSearch != "" {
		v.Set("target_user_id", o.TargetSearch)
	}
	return v.Encode()
}

// ListUserReports returns one page of the user-report queue.
func (c *Client) ListUserReports(ctx context.Context, opts UserReportOptions) (*UserReportPage, error) {
	raw, err := c.get(ctx, c.baseURL+"/_synapse/admin/v1/user_reports?"+opts.query())
	if err != nil {
		return nil, err
	}

	var wire struct {
		UserReports []userReportWire `json:"user_reports"`
		Total       int              `json:"total"`
		NextToken   *int64           `json:"next_token"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, fmt.Errorf("could not read the user report queue: %w", err)
	}

	// Never nil, for the same reason as the event queue: a nil slice reaches the
	// frontend as `null` and every `.map` on it is a crash (etappe 41).
	page := &UserReportPage{Reports: make([]UserReport, 0, len(wire.UserReports)),
		Total: wire.Total, NextToken: wire.NextToken}
	for _, w := range wire.UserReports {
		page.Reports = append(page.Reports, w.toUserReport())
	}
	return page, nil
}
