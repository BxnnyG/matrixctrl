package preview

import (
	"strings"
	"testing"
)

// The case from the move: the alias still names the old cluster's Traefik.
const aliasManifest = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: ess-matrix-rtc-authorisation-service
spec:
  template:
    spec:
      hostAliases:
      - ip: 10.43.222.87
        hostnames: [rtc.example.org]
      containers: [{name: c, image: x}]
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: lan-client
spec:
  template:
    spec:
      hostAliases:
      - ip: 192.168.1.20
        hostnames: [nas.lan]
      containers: [{name: c, image: x}]
`

var cluster = []ServiceIP{
	{"kube-system", "traefik", "10.43.227.208"},
	{"kube-system", "kube-dns", "10.43.0.10"},
	{"ess", "ess-synapse", "10.43.18.202"},
	{"ess", "ess-postgres", "None"},
}

func TestAnAliasToAClusterIPNobodyHoldsIsReported(t *testing.T) {
	got := StaleHostAliases(aliasManifest, cluster)
	if len(got) != 1 {
		t.Fatalf("want exactly the RTC alias, got %+v", got)
	}
	a := got[0]
	if a.IP != "10.43.222.87" || a.Suggest != "10.43.227.208" {
		t.Errorf("got %+v", a)
	}
	if !strings.Contains(a.Message, "kube-system/traefik") {
		t.Errorf("the message should name the likely target: %q", a.Message)
	}
}

// Counter-probe: the same alias with the right address is silent, and so is an alias
// outside the service network — a LAN host is not a stale ClusterIP.
func TestAnAliasToAnExistingServiceIsSilent(t *testing.T) {
	fixed := strings.Replace(aliasManifest, "10.43.222.87", "10.43.227.208", 1)
	if got := StaleHostAliases(fixed, cluster); len(got) != 0 {
		t.Errorf("nothing is stale: %+v", got)
	}
}

func TestNoServicesMeansNoVerdict(t *testing.T) {
	if got := StaleHostAliases(aliasManifest, nil); got != nil {
		t.Errorf("without a service list there is nothing to compare with: %+v", got)
	}
}
