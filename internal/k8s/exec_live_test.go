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
)

// Reaching a volume no other pod mounts, for real.
//
// Every archive this product has produced said the uploaded files were *not included*,
// because nothing outside the Synapse pod can read them. The exec subresource is the
// way in, and whether it works is a question about SPDY, the container runtime and
// whether `tar` exists in that image — none of which a fixture can answer.
func TestLiveTarFromPod(t *testing.T) {
	if os.Getenv("RUN_LIVE") == "" {
		t.Skip("set RUN_LIVE=1")
	}
	c, err := New()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	pods, err := c.PodsByLabel(ctx, "ess", "app.kubernetes.io/name=synapse-main")
	if err != nil || len(pods) == 0 {
		t.Skipf("no synapse pod here: %v", err)
	}
	pod := pods[0]

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
