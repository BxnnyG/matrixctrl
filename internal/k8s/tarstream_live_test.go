package k8s

import (
	"archive/tar"
	"context"
	"io"
	"os"
	"strconv"
	"testing"
	"time"
)

// TestLiveTarFromPodSlowReader streams a directory out of a pod through TarFromPod and
// reads it as slowly as an archive pipeline might, to see whether the stream arrives
// whole. System files only — never the media volume.
//
//	RUN_LIVE=1 TAR_POD=ess-synapse-main-0 TAR_RATE_KB=2048 [TAR_HELD=1] go test ./internal/k8s/ -run TarFromPodSlowReader -v
func TestLiveTarFromPodSlowReader(t *testing.T) {
	if os.Getenv("RUN_LIVE") == "" {
		t.Skip("RUN_LIVE not set")
	}
	c, err := New()
	if err != nil {
		t.Fatal(err)
	}
	rate, _ := strconv.Atoi(os.Getenv("TAR_RATE_KB"))
	// TAR_HELD=1 holds the process open until the end marker is read (the fix);
	// without it this is the counter-probe and a slow reader must lose the tail.
	var release chan struct{}
	if os.Getenv("TAR_HELD") != "" {
		release = make(chan struct{})
	}
	pr, pw := io.Pipe()
	done := make(chan error, 1)
	go func() {
		err := c.TarFromPod(context.Background(), "ess", os.Getenv("TAR_POD"), "synapse", "/usr/lib", pw, release)
		_ = pw.CloseWithError(err)
		done <- err
	}()
	start := time.Now()
	tr := tar.NewReader(pr)
	var files, total int64
	buf := make([]byte, 32<<10)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			// tar pads past the end marker; whoever stops reading here leaves the exec
			// blocked on a full pipe forever.
			if release != nil {
				close(release)
			}
			_, _ = io.Copy(io.Discard, pr)
			break
		}
		if err != nil {
			t.Fatalf("after %d files, %d bytes, %s: header: %v", files, total, time.Since(start).Round(time.Second), err)
		}
		for {
			n, rerr := tr.Read(buf)
			total += int64(n)
			if rate > 0 && n > 0 {
				time.Sleep(time.Duration(n) * time.Second / time.Duration(rate<<10))
			}
			if rerr == io.EOF {
				break
			}
			if rerr != nil {
				t.Fatalf("after %d files, %d bytes, %s: %s: %v (exec: %v)", files, total, time.Since(start).Round(time.Second), h.Name, rerr, <-done)
			}
		}
		files++
	}
	t.Logf("whole: %d files, %d bytes in %s, exec: %v", files, total, time.Since(start).Round(time.Second), <-done)
}
