package k8s

import (
	"context"
	"os"
	"testing"
	"time"
)

// The update Job, for real (etappe 116): created the way the panel creates it, run
// under MatrixCtrl's own ServiceAccount with the running image, to the version given in
// SELFUPDATE_TO. Proves the rights in the chart are enough for a complete Helm upgrade
// and that the Job reports its outcome. Destructive only in the sense an update is:
// run it on a test installation.
func TestLiveSelfUpdateJob(t *testing.T) {
	target := os.Getenv("SELFUPDATE_TO")
	if os.Getenv("RUN_LIVE") == "" || target == "" {
		t.Skip("set RUN_LIVE=1 and SELFUPDATE_TO=<version>")
	}
	c, err := New()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	image, sa, err := c.SelfImage(ctx, "matrixctrl", "matrixctrl")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("läuft mit %s als %s", image, sa)
	job, err := c.StartSelfUpdate(ctx, "matrixctrl", image, sa, "matrixctrl", target)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Job %s gestartet", job.Name)

	deadline := time.Now().Add(8 * time.Minute)
	for time.Now().Before(deadline) {
		time.Sleep(10 * time.Second)
		cur, err := c.LatestSelfUpdate(ctx, "matrixctrl")
		if err != nil || cur == nil {
			continue
		}
		if cur.State != "running" {
			t.Logf("Zustand: %s\n%s", cur.State, cur.Log)
			if cur.State != "succeeded" {
				t.Fatal("the update Job did not succeed")
			}
			return
		}
	}
	t.Fatal("the update Job did not finish in time")
}
