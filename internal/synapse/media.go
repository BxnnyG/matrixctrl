package synapse

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// Media quarantine (etappe 47).
//
// The endpoint that does the work reports nothing about what it did:
//
//	await self.store.quarantine_media_by_id(server_name, media_id, requester...)
//	return HTTPStatus.OK, {}
//
// and the store skips media that has been protected —
//
//	if quarantined_by is not None:
//	    hash_sql += " AND safe_from_quarantine = FALSE"
//
// so a quarantine request against protected media returns `200 {}`, identical to
// success, having changed nothing. Note the condition: the filter applies on
// quarantine and *not* on unquarantine, so the two directions are not symmetric.
//
// Everything in this file therefore reads the state back. What the panel reports is
// what Synapse says afterwards, never what was asked for.

// MediaRef is one media item referenced by an event.
type MediaRef struct {
	Server string `json:"server"`
	ID     string `json:"id"`
	// Kind distinguishes the reference site: the media itself, its thumbnail, or the
	// encrypted-room variant. An admin quarantining a thumbnail while the full image
	// stays available has not done what they meant to.
	Kind string `json:"kind"`
}

// MXC renders the reference back to its canonical form.
func (m MediaRef) MXC() string { return "mxc://" + m.Server + "/" + m.ID }

// MediaInfo is what Synapse knows about one item.
type MediaInfo struct {
	MediaID string `json:"media_id"`
	Type    string `json:"media_type,omitempty"`
	Length  int64  `json:"media_length,omitempty"`
	Name    string `json:"upload_name,omitempty"`
	// QuarantinedBy is the admin who quarantined it, empty when it is not
	// quarantined. It is the only reliable answer to "did that work?".
	QuarantinedBy string `json:"quarantined_by,omitempty"`
	// SafeFromQuarantine means a quarantine request will be accepted and ignored.
	SafeFromQuarantine bool `json:"safe_from_quarantine"`
}

func (m MediaInfo) Quarantined() bool { return m.QuarantinedBy != "" }

// mxcRe-free parsing: an mxc URI is `mxc://server/mediaid` and nothing else, so
// splitting is clearer than a regex and fails in the same places.
func parseMXC(s string) (MediaRef, bool) {
	const prefix = "mxc://"
	if !strings.HasPrefix(s, prefix) {
		return MediaRef{}, false
	}
	rest := strings.TrimPrefix(s, prefix)
	slash := strings.IndexByte(rest, '/')
	if slash <= 0 || slash == len(rest)-1 {
		return MediaRef{}, false
	}
	server, id := rest[:slash], rest[slash+1:]
	// A media id containing a slash would address something else entirely once it
	// reaches a URL path. Refusing is simpler to be sure of than escaping.
	if strings.ContainsAny(id, "/?#") || strings.ContainsAny(server, "/?#") {
		return MediaRef{}, false
	}
	return MediaRef{Server: server, ID: id}, true
}

// MediaInEvent extracts every media reference an event carries.
//
// Three sites, because Matrix has three: `content.url` for the item,
// `content.info.thumbnail_url` for its preview, and `content.file.url` in encrypted
// rooms. Nothing else is guessed at — an unrecognised shape yields no references,
// which reads as "no media" and is the honest answer for an event this code does not
// understand.
func MediaInEvent(eventJSON json.RawMessage) []MediaRef {
	if len(eventJSON) == 0 {
		return nil
	}
	var ev struct {
		Content struct {
			URL  string `json:"url"`
			File struct {
				URL string `json:"url"`
			} `json:"file"`
			Info struct {
				ThumbnailURL string `json:"thumbnail_url"`
			} `json:"info"`
		} `json:"content"`
	}
	if err := json.Unmarshal(eventJSON, &ev); err != nil {
		return nil
	}

	var out []MediaRef
	seen := map[string]bool{}
	add := func(raw, kind string) {
		ref, ok := parseMXC(raw)
		if !ok || seen[ref.MXC()] {
			return
		}
		seen[ref.MXC()] = true
		ref.Kind = kind
		out = append(out, ref)
	}
	add(ev.Content.URL, "media")
	add(ev.Content.File.URL, "encrypted")
	add(ev.Content.Info.ThumbnailURL, "thumbnail")
	return out
}

// GetMedia reads what Synapse knows about one item, including whether it is
// quarantined. This is the read-back the write path depends on.
func (c *Client) GetMedia(ctx context.Context, server, mediaID string) (*MediaInfo, error) {
	endpoint := c.baseURL + "/_synapse/admin/v1/media/" +
		url.PathEscape(server) + "/" + url.PathEscape(mediaID)

	raw, err := c.get(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	var wrap struct {
		MediaInfo struct {
			MediaID            string `json:"media_id"`
			MediaType          string `json:"media_type"`
			MediaLength        int64  `json:"media_length"`
			UploadName         string `json:"upload_name"`
			QuarantinedBy      string `json:"quarantined_by"`
			SafeFromQuarantine bool   `json:"safe_from_quarantine"`
		} `json:"media_info"`
	}
	if err := json.Unmarshal(raw, &wrap); err != nil {
		return nil, fmt.Errorf("could not read the media info: %w", err)
	}
	m := wrap.MediaInfo
	return &MediaInfo{
		MediaID: m.MediaID, Type: m.MediaType, Length: m.MediaLength,
		Name: m.UploadName, QuarantinedBy: m.QuarantinedBy,
		SafeFromQuarantine: m.SafeFromQuarantine,
	}, nil
}

// QuarantineResult is what actually happened, as opposed to what was requested.
type QuarantineResult struct {
	Requested bool `json:"requested"`
	// Quarantined is the state read back afterwards.
	Quarantined bool   `json:"quarantined"`
	By          string `json:"by,omitempty"`
	// Changed is false when the read-back disagrees with the request. The only case
	// that produces it in practice is protected media, which Synapse skips while
	// answering 200 — but it is computed from the read-back rather than from
	// SafeFromQuarantine, so a future reason to silently no-op is caught too.
	Changed   bool `json:"changed"`
	Protected bool `json:"protected"`
}

// SetQuarantined quarantines or releases one media item and reports the state
// Synapse holds afterwards.
//
// The POST's response body is `{}` in every case, so it is not read. Correctness
// here comes entirely from the GET that follows.
func (c *Client) SetQuarantined(ctx context.Context, server, mediaID string, quarantine bool) (*QuarantineResult, error) {
	verb := "unquarantine"
	if quarantine {
		verb = "quarantine"
	}
	endpoint := c.baseURL + "/_synapse/admin/v1/media/" + verb + "/" +
		url.PathEscape(server) + "/" + url.PathEscape(mediaID)

	if _, err := c.do(ctx, "POST", endpoint, struct{}{}); err != nil {
		return nil, err
	}

	info, err := c.GetMedia(ctx, server, mediaID)
	if err != nil {
		// The write may well have succeeded; this says only that it could not be
		// confirmed. Reporting success here would be exactly the assumption this
		// whole file exists to avoid.
		return nil, fmt.Errorf("the change could not be confirmed: %w", err)
	}

	return &QuarantineResult{
		Requested:   quarantine,
		Quarantined: info.Quarantined(),
		By:          info.QuarantinedBy,
		Changed:     info.Quarantined() == quarantine,
		Protected:   info.SafeFromQuarantine,
	}, nil
}
