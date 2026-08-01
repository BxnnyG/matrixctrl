package helm

import (
	"context"
	"strconv"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/metadata/fake"
)

// releaseSecret builds the metadata of a Helm release secret the way Helm writes
// it — the name carries the revision, the labels carry everything the probe reads.
func releaseSecret(ns, release string, rev int, status, modifiedAt string) *metav1.PartialObjectMetadata {
	return &metav1.PartialObjectMetadata{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ns,
			Name:      "sh.helm.release.v1." + release + ".v" + strconv.Itoa(rev),
			Labels: map[string]string{
				"owner":      "helm",
				"name":       release,
				"status":     status,
				"version":    strconv.Itoa(rev),
				"modifiedAt": modifiedAt,
			},
		},
	}
}

func probeClient(ns string, objs ...*metav1.PartialObjectMetadata) *Client {
	scheme := fake.NewTestScheme()
	_ = metav1.AddMetaToScheme(scheme)
	runtimeObjs := make([]runtime.Object, 0, len(objs))
	for _, o := range objs {
		runtimeObjs = append(runtimeObjs, o)
	}
	return &Client{namespace: ns, meta: fake.NewSimpleMetadataClient(scheme, runtimeObjs...)}
}

// The list revisions arrive in is whatever the API server feels like. Deliberately
// unsorted here, and deliberately not ascending — a probe that took the last item
// would pass on sorted input and be wrong in production.
func TestProbeTakesTheHighestRevision(t *testing.T) {
	c := probeClient("ess",
		releaseSecret("ess", "ess", 21, "superseded", "1769000000"),
		releaseSecret("ess", "ess", 22, "deployed", "1769459689"),
		releaseSecret("ess", "ess", 9, "superseded", "1768000000"),
	)

	id, err := c.probeNewestRelease(context.Background(), "ess")
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if id.Revision != 22 {
		t.Fatalf("expected revision 22, got %d", id.Revision)
	}
	if id.Status != "deployed" {
		t.Fatalf("expected status deployed, got %q", id.Status)
	}
	if id.ModifiedAt != "1769459689" {
		t.Fatalf("modifiedAt not carried through: %q", id.ModifiedAt)
	}
}

// Revision 10 must beat revision 9. Sorting the `version` label as a string puts
// "9" after "10", which would silently pin the dashboard to an old revision from
// the tenth upgrade onwards — and look perfectly fine for the first nine.
func TestProbeComparesRevisionsNumerically(t *testing.T) {
	c := probeClient("ess",
		releaseSecret("ess", "ess", 9, "superseded", "1768000000"),
		releaseSecret("ess", "ess", 10, "deployed", "1769000000"),
	)

	id, err := c.probeNewestRelease(context.Background(), "ess")
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if id.Revision != 10 {
		t.Fatalf("expected revision 10, got %d", id.Revision)
	}
}

// The reason storage.Deployed() was rejected. It would have returned revision 22
// here — the last one that worked — and the operator would see a green dashboard
// while the upgrade they just ran sat in `failed`.
func TestProbeReportsAFailedNewestRevisionRatherThanTheLastGoodOne(t *testing.T) {
	c := probeClient("ess",
		releaseSecret("ess", "ess", 22, "deployed", "1769459689"),
		releaseSecret("ess", "ess", 23, "failed", "1769999999"),
	)

	id, err := c.probeNewestRelease(context.Background(), "ess")
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if id.Revision != 23 || id.Status != "failed" {
		t.Fatalf("a failed upgrade must be what is reported, got revision %d status %q", id.Revision, id.Status)
	}
}

// Greenfield. The probe must fail so the caller drops to the plain Helm read,
// which produces the "release: not found" the discovery path in main.go keys on.
func TestProbeFailsWhenThereAreNoReleaseSecrets(t *testing.T) {
	c := probeClient("ess")

	if _, err := c.probeNewestRelease(context.Background(), "ess"); err == nil {
		t.Fatal("a namespace with no release secrets must be an error, not revision 0")
	}
}

// Two releases in one namespace is normal once a second ESS is adopted. The label
// selector has to keep them apart, or the dashboard reads the wrong one's revision.
func TestProbeIgnoresOtherReleases(t *testing.T) {
	c := probeClient("ess",
		releaseSecret("ess", "ess", 22, "deployed", "1769459689"),
		releaseSecret("ess", "other", 99, "deployed", "1769999999"),
	)

	id, err := c.probeNewestRelease(context.Background(), "ess")
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if id.Revision != 22 {
		t.Fatalf("picked up another release's secret: revision %d", id.Revision)
	}
}

// A nil metadata client is a supported state (see client.go). It must produce an
// error so the caller falls back, never a zero-valued identity that would look
// like a real answer.
func TestProbeWithoutMetadataClientFailsCleanly(t *testing.T) {
	c := &Client{namespace: "ess"}
	if _, err := c.probeNewestRelease(context.Background(), "ess"); err == nil {
		t.Fatal("no metadata client must be an error")
	}
}

func TestRevisionOf(t *testing.T) {
	cases := []struct {
		name       string
		secret     string
		label      string
		wantRev    int
		wantOK     bool
		wantReason string
	}{
		{"label wins", "sh.helm.release.v1.ess.v22", "22", 22, true, "the documented source"},
		{"falls back to the name", "sh.helm.release.v1.ess.v22", "", 22, true, "Helm cannot change the name without breaking its own lookups"},
		{"garbage label, good name", "sh.helm.release.v1.ess.v7", "not-a-number", 7, true, "one bad label must not hide a readable revision"},
		{"neither readable", "some-unrelated-secret", "", 0, false, "guessing a revision is worse than skipping the secret"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := revisionOf(tc.secret, tc.label)
			if ok != tc.wantOK || got != tc.wantRev {
				t.Fatalf("revisionOf(%q, %q) = %d, %v; want %d, %v — %s",
					tc.secret, tc.label, got, ok, tc.wantRev, tc.wantOK, tc.wantReason)
			}
		})
	}
}
