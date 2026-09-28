package preview

import (
	"strings"
	"testing"
)

const synapse = `---
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: ess-synapse-main
spec:
  template:
    metadata:
      annotations:
        checksum/config: %s
    spec:
      containers:
        - name: synapse
          image: synapse:1
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: ess-synapse
data:
  a: "%s"
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: ess-element-web
spec:
  template:
    spec:
      containers:
        - name: web
          image: element:%s
`

func render(checksum, cm, element string) string {
	r := strings.NewReplacer("%s", "\x00")
	parts := strings.Split(r.Replace(synapse), "\x00")
	return parts[0] + checksum + parts[1] + cm + parts[2] + element + parts[3]
}

func TestNothingChangedMeansNothingRestarts(t *testing.T) {
	m := render("aaa", "x", "1")
	if got := Restarts(m, m); len(got) != 0 {
		t.Errorf("identical manifests must restart nothing, got %v", got)
	}
}

// ESS restarts Synapse after a config change through a checksum annotation on the pod
// template. Comparing only the containers would miss exactly that.
func TestAChecksumAnnotationIsARestart(t *testing.T) {
	got := Restarts(render("aaa", "x", "1"), render("bbb", "y", "1"))
	if len(got) != 1 || got[0].Name != "ess-synapse-main" || got[0].Kind != "StatefulSet" {
		t.Fatalf("Synapse should restart, and only Synapse: %v", got)
	}
}

// A ConfigMap is not a workload: changing one alone restarts nothing.
func TestAConfigMapAloneRestartsNothing(t *testing.T) {
	if got := Restarts(render("aaa", "x", "1"), render("aaa", "y", "1")); len(got) != 0 {
		t.Errorf("got %v", got)
	}
}

func TestANewWorkloadIsReportedAsNew(t *testing.T) {
	current := render("aaa", "x", "1")
	rendered := current + `---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: ess-hookshot
spec:
  template:
    spec:
      containers:
        - name: hookshot
          image: hookshot:1
`
	got := Restarts(current, rendered)
	if len(got) != 1 || !got[0].New || got[0].Name != "ess-hookshot" {
		t.Errorf("a workload that does not exist yet starts rather than restarts: %v", got)
	}
}

// Restarts of running services come first — those are the interruptions an operator
// is being warned about.
func TestRunningServicesAreListedBeforeNewOnes(t *testing.T) {
	current := render("aaa", "x", "1")
	rendered := render("bbb", "x", "2") + `---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: aaa-new
spec:
  template:
    spec:
      containers: [{name: n, image: n}]
`
	got := Restarts(current, rendered)
	if len(got) != 3 || got[len(got)-1].Name != "aaa-new" {
		t.Errorf("new workloads belong at the end: %v", got)
	}
}
