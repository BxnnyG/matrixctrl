package helm

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// A release secret is small to *look at* and expensive to *open*. The labels Helm
// puts on it answer most of what the dashboard asks:
//
//	{"modifiedAt":"1769459689","name":"ess","owner":"helm","status":"deployed","version":"22"}
//
// A metadata-only list (PartialObjectMetadataList) returns those labels for every
// revision without transferring any release payload — ~15 ms against the live
// cluster, versus ~4.3 s for the equivalent question asked through action.NewGet.
const (
	helmSecretOwner = "helm"

	// The probe is the fast path, so its timeout is short. Exceeding it does not
	// produce a wrong answer, only the old slow one.
	probeTimeout = 5 * time.Second
)

var secretGVR = schema.GroupVersionResource{Version: "v1", Resource: "secrets"}

// probeNewestRelease finds the highest revision of a release and reads its status
// straight from the secret labels, without decoding anything.
//
// It deliberately mirrors storage.Last() — highest revision wins — and not
// storage.Deployed(). Deployed() would be a cheaper one-line change but it returns
// the last *successful* revision, so a failed upgrade would leave the dashboard
// happily displaying the release before it. Hiding a failure to save 15 ms is the
// wrong trade in a tool whose job is to show what is actually going on.
func (c *Client) probeNewestRelease(ctx context.Context, name string) (releaseIdentity, error) {
	if c.meta == nil {
		return releaseIdentity{}, fmt.Errorf("no metadata client")
	}

	list, err := c.meta.Resource(secretGVR).Namespace(c.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "owner=" + helmSecretOwner + ",name=" + name,
	})
	if err != nil {
		return releaseIdentity{}, fmt.Errorf("probe release secrets: %w", err)
	}

	best := releaseIdentity{Revision: -1}
	for _, item := range list.Items {
		rev, ok := revisionOf(item.Name, item.Labels["version"])
		if !ok || rev <= best.Revision {
			continue
		}
		best = releaseIdentity{
			Revision:   rev,
			Status:     item.Labels["status"],
			ModifiedAt: item.Labels["modifiedAt"],
			SecretName: item.Name,
		}
	}

	if best.Revision < 0 {
		// No release secrets at all. This is greenfield, or the release lives
		// somewhere else — the same thing the slow path reports, so say it the
		// same way rather than inventing a second vocabulary for it.
		return releaseIdentity{}, fmt.Errorf("release: not found")
	}
	return best, nil
}

// revisionOf prefers the `version` label and falls back to the revision suffix of
// the secret name (sh.helm.release.v1.<release>.v<revision>). Two sources because
// the label is the documented one and the name is the one Helm cannot change
// without breaking its own lookups.
func revisionOf(secretName, versionLabel string) (int, bool) {
	if n, err := strconv.Atoi(versionLabel); err == nil {
		return n, true
	}
	if i := strings.LastIndex(secretName, ".v"); i >= 0 {
		if n, err := strconv.Atoi(secretName[i+2:]); err == nil {
			return n, true
		}
	}
	return 0, false
}

// readRelease returns the release info for a name, doing as little work as the
// cluster state allows:
//
//	probe (~15 ms) → identity unchanged? → return the memoised value
//	              → identity changed?   → decode that one revision (~500 ms)
//	              → probe failed?       → fall back to the old full read (~4.3 s)
//
// The fallback is what makes this safe to ship: every way the fast path can fail
// ends in the code that was running before, so the worst case is the old latency
// rather than a wrong answer.
func (c *Client) readRelease(name string) (*ReleaseInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()

	id, err := c.probeNewestRelease(ctx, name)
	if err != nil {
		return c.getReleaseUncached(name)
	}

	if info, ok := c.memoisedReleaseInfo(name, id); ok {
		return info, nil
	}

	// Helm's own storage layer does the fetching and decoding — we only choose
	// which revision to ask for. The secret format (base64 → gzip → JSON) stays
	// Helm's business, so a future change to it cannot silently break us.
	rel, err := c.cfg.Releases.Get(name, id.Revision)
	if err != nil {
		return c.getReleaseUncached(name)
	}

	info := toReleaseInfo(rel)
	c.storeReleaseInfo(name, id, info)
	return info, nil
}
