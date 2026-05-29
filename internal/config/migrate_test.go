package config

import "testing"

func TestBuildSectionFiles(t *testing.T) {
	values := `## The server name
serverName: bxnny.de
## Synapse config
synapse:
  ## worker count
  workers: 1
certManager: {}
matrixRTC:
  sfu:
    ## stun toggle
    useStunToDiscoverPublicIP: true
`
	hostnames := `synapse:
  ingress:
    host: matrix.bxnny.de
elementWeb:
  ingress:
    host: element.bxnny.de
serverName: bxnny.de
`
	rtc := `matrixRTC:
  sfu:
    hostNetwork: true
`
	tls := `certManager:
  clusterIssuer: letsencrypt-prod
`
	files, err := BuildSectionFiles([]NamedSlice{
		{"values", values}, {"hostnames", hostnames}, {"rtc", rtc}, {"tls", tls},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Expect per-section files + general.yaml; no values/hostnames/rtc/tls.
	for _, want := range []string{"synapse.yaml", "elementWeb.yaml", "matrixRTC.yaml", "general.yaml"} {
		if _, ok := files[want]; !ok {
			t.Errorf("missing %s; got files: %v", want, keysOf(files))
		}
	}

	// Comment from the base must survive in the section file.
	if !contains(files["synapse.yaml"], "worker count") {
		t.Errorf("synapse comment lost:\n%s", files["synapse.yaml"])
	}
	// Override value from hostnames merged into synapse.
	if !contains(files["synapse.yaml"], "matrix.bxnny.de") {
		t.Errorf("synapse ingress host not merged:\n%s", files["synapse.yaml"])
	}
	// rtc override merged into matrixRTC, base comment preserved.
	if !contains(files["matrixRTC.yaml"], "hostNetwork: true") {
		t.Errorf("rtc override not merged:\n%s", files["matrixRTC.yaml"])
	}
	if !contains(files["matrixRTC.yaml"], "stun toggle") {
		t.Errorf("matrixRTC base comment lost:\n%s", files["matrixRTC.yaml"])
	}
	// general.yaml carries serverName + certManager (with tls override).
	if !contains(files["general.yaml"], "serverName") || !contains(files["general.yaml"], "letsencrypt-prod") {
		t.Errorf("general.yaml wrong:\n%s", files["general.yaml"])
	}
	// Round-trip: merged sections must reproduce the effective config.
	var merged []string
	for _, c := range files {
		merged = append(merged, c)
	}
	m, err := MergeToMap(merged)
	if err != nil {
		t.Fatal(err)
	}
	syn, _ := m["synapse"].(map[string]interface{})
	if syn == nil {
		t.Fatal("merged synapse missing")
	}
}

func keysOf(m map[string]string) []string {
	var k []string
	for x := range m {
		k = append(k, x)
	}
	return k
}
