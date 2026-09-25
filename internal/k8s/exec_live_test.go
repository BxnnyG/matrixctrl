package k8s

import (
	"archive/tar"
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

// Reaching a volume no other pod mounts, for real — as the account that will reach it.
//
// Every archive this product has produced said the uploaded files were *not included*,
// because nothing outside the Synapse pod can read them. The exec subresource is the
// way in, and whether it works is a question about SPDY, the container runtime and
// whether `tar` exists in that image — none of which a fixture can answer.
//
// The numbers this test printed ("290 files, 37.2 MB") are quoted in §4.102 as the
// proof the media export works. They came from a root shell, while the feature ran as
// a service account that was forbidden to exec at all (§4.104). So it goes through
// the same impersonation the permission checks use — and impersonation has to reach
// further here than there: a SubjectAccessReview is an ordinary request, an exec is a
// SPDY upgrade with its own round tripper. Whether the headers survive that is again
// not something a fixture can answer, which is what the counter-check below is for.
func TestLiveTarFromPod(t *testing.T) {
	if os.Getenv("RUN_LIVE") == "" {
		t.Skip("set RUN_LIVE=1")
	}
	admin, err := New()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Finding the pod is not the thing under test; reaching into it is.
	pods, err := admin.PodsByLabel(ctx, "ess", "app.kubernetes.io/name=synapse-main")
	if err != nil || len(pods) == 0 {
		t.Skipf("no synapse pod here: %v", err)
	}
	pod := pods[0]

	c := asServiceAccount(ctx, t, admin)
	assertExecIsRefusedForOthers(ctx, t, admin, pod)

	size, err := c.DirSizeInPod(ctx, "ess", pod, "synapse", "/media")
	if err != nil {
		t.Fatalf("size of the media directory: %v", err)
	}
	if size <= 0 {
		t.Fatalf("media reported as %d bytes — the number the operator decides against", size)
	}
	t.Logf("media directory: %.1f MB", float64(size)/1024/1024)

	var buf bytes.Buffer
	if err := c.TarFromPod(ctx, "ess", pod, "synapse", "/media", &buf); err != nil {
		t.Fatalf("streaming the media out: %v", err)
	}

	// A stream that ends early looks like success to anything that only checks err.
	tr := tar.NewReader(&buf)
	files, bytesSeen := 0, int64(0)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("the tar is not readable after %d entries: %v", files, err)
		}
		if h.Typeflag == tar.TypeReg {
			files++
			bytesSeen += h.Size
		}
	}
	if files == 0 {
		t.Fatal("the tar came back with no files in it")
	}
	t.Logf("%d Dateien, %.1f MB im tar", files, float64(bytesSeen)/1024/1024)
}

// assertExecIsRefusedForOthers gives the success above its meaning.
//
// Impersonation is configured on the rest.Config; the exec path builds its own SPDY
// round tripper from that config. If the headers were dropped somewhere along it, the
// stream would simply run as whoever holds the kubeconfig — a maintainer with
// cluster-admin — and the test would pass exactly as it does now, proving nothing.
// That is the failure of etappe 104 verbatim, and a green run cannot distinguish it
// from the real thing.
//
// So the same call is made as an account that is definitely not allowed to exec:
// matrixctrl's own `default` service account, which holds no role at all. It has to
// come back refused. If it succeeds, impersonation is not reaching exec, and the
// numbers above are about the wrong identity (§4.105).
func assertExecIsRefusedForOthers(ctx context.Context, t *testing.T, admin *Client, pod string) {
	t.Helper()
	powerless := "system:serviceaccount:" + ownNamespace() + ":default"

	other, err := admin.As(powerless)
	if err != nil {
		t.Fatalf("cannot build a client for %q: %v", powerless, err)
	}
	err = other.TarFromPod(ctx, "ess", pod, "synapse", "/media", io.Discard)
	if err == nil {
		t.Fatalf("%s streamed the media out of the pod.\n"+
			"It holds no role, so this cannot be true: the impersonation is not reaching\n"+
			"the exec path and every result in this test is about the kubeconfig instead.",
			powerless)
	}
	if !apierrors.IsForbidden(err) {
		t.Logf("refused for %s, but not with a 403: %v", powerless, err)
		return
	}
	t.Logf("counter-check: exec as %s refused, so the run above is about the right account", powerless)
}

// The keys, as something that can be applied to another cluster.
func TestLiveSecretYAMLIsApplyable(t *testing.T) {
	if os.Getenv("RUN_LIVE") == "" {
		t.Skip("set RUN_LIVE=1")
	}
	c, err := New()
	if err != nil {
		t.Fatal(err)
	}
	blob, err := c.SecretYAML(context.Background(), "ess", "ess-generated")
	if err != nil {
		t.Skipf("no ess-generated here: %v", err)
	}
	out := string(blob)

	for _, want := range []string{"SYNAPSE_SIGNING_KEY", "SYNAPSE_MACAROON", "MAS_ENCRYPTION_SECRET", "kind: Secret"} {
		if !strings.Contains(out, want) {
			t.Errorf("%s missing — without it a restore is a different server", want)
		}
	}
	// Bookkeeping from this cluster would make the apply fail on another one.
	for _, unwanted := range []string{"resourceVersion", "uid:", "managedFields"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("%s is still in the YAML; an apply elsewhere is refused because of it", unwanted)
		}
	}
}

// Putting files back, in the pod, as the account that will do it.
//
// TarFromPod has been proven since etappe 102; the way back had not, and "the same
// mechanism in reverse" is exactly the sort of assumption this repository keeps finding
// in its own comments. It writes into a directory of its own under /media, reads it back
// and removes it — so it exercises the real path on the real volume without touching a
// single uploaded file.
func TestLiveTarIntoPod(t *testing.T) {
	if os.Getenv("RUN_LIVE") == "" {
		t.Skip("set RUN_LIVE=1")
	}
	admin, err := New()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	pods, err := admin.PodsByLabel(ctx, "ess", "app.kubernetes.io/name=synapse-main")
	if err != nil || len(pods) == 0 {
		t.Skipf("no synapse pod here: %v", err)
	}
	pod := pods[0]
	c := asServiceAccount(ctx, t, admin)

	free, err := c.FreeSpaceInPod(ctx, "ess", pod, "synapse", "/media")
	if err != nil {
		t.Fatalf("free space: %v", err)
	}
	t.Logf("frei auf /media: %.1f GB", float64(free)/1024/1024/1024)
	if free <= 0 {
		t.Fatal("a restore has to be able to ask how much room is left")
	}

	staging := "/media/.matrixctrl-restore-test"
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := c.RunInPod(ctx, "ess", pod, "synapse", "rm", "-rf", staging); err != nil {
			t.Errorf("the staging directory was left in the pod: %v", err)
		}
	})
	if _, err := c.RunInPod(ctx, "ess", pod, "synapse", "mkdir", "-p", staging); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	body := []byte("restored by the live test\n")
	if err := tw.WriteHeader(&tar.Header{
		Name: "proof.txt", Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}

	if err := c.TarIntoPod(ctx, "ess", pod, "synapse", staging, &buf); err != nil {
		t.Fatalf("streaming the files in: %v", err)
	}

	// Read back rather than trusting the absence of an error: tar exits 0 on an empty
	// stream, which would look exactly like a successful restore of nothing.
	got, err := c.RunInPod(ctx, "ess", pod, "synapse", "cat", staging+"/proof.txt")
	if err != nil {
		t.Fatalf("reading it back: %v", err)
	}
	if got != strings.TrimSpace(string(body)) {
		t.Errorf("what arrived is not what was sent: %q", got)
	}
	t.Logf("in den Pod geschrieben und zurückgelesen: %q", got)
}
