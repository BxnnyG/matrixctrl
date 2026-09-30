package k8s

import (
	"strings"
	"testing"
)

// The update Job: the panel's own identity and image, the subcommand, never retried,
// and at least the panel's own hardening.
func TestTheUpdateJobRunsAsThePanelAndIsHardened(t *testing.T) {
	j := selfUpdateJob("matrixctrl-update-1", "matrixctrl", "ghcr.io/bxnnyg/matrixctrl:0.1.113", "matrixctrl", "matrixctrl", "0.1.114")
	spec := j.Spec.Template.Spec
	if spec.ServiceAccountName != "matrixctrl" || j.Namespace != "matrixctrl" {
		t.Errorf("identity: sa=%q ns=%q", spec.ServiceAccountName, j.Namespace)
	}
	if *j.Spec.BackoffLimit != 0 {
		t.Error("an upgrade must not be retried blindly")
	}
	c := spec.Containers[0]
	if c.Image != "ghcr.io/bxnnyg/matrixctrl:0.1.113" {
		t.Errorf("the running image runs the update, not the target: %s", c.Image)
	}
	cmd := strings.Join(c.Command, " ")
	if !strings.HasPrefix(cmd, "/usr/local/bin/matrixctrl self-update") || !strings.Contains(cmd, "--version 0.1.114") {
		t.Errorf("command: %s", cmd)
	}
	sc := c.SecurityContext
	if sc == nil || !*sc.RunAsNonRoot || !*sc.ReadOnlyRootFilesystem || *sc.AllowPrivilegeEscalation || len(sc.Capabilities.Drop) == 0 {
		t.Errorf("hardening: %+v", sc)
	}
	if j.Annotations[selfUpdateVersionAnno] != "0.1.114" || j.Labels[selfUpdateLabel] != "true" {
		t.Errorf("the panel finds its jobs by label and reads the target from an annotation: %v %v", j.Labels, j.Annotations)
	}
	var home bool
	for _, e := range c.Env {
		if e.Name == "HOME" && e.Value == "/tmp" {
			home = true
		}
	}
	if !home || len(c.VolumeMounts) == 0 || c.VolumeMounts[0].MountPath != "/tmp" {
		t.Error("helm needs a writable HOME on a read-only root file system")
	}
}
