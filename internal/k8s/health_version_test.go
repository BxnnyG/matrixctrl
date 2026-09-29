package k8s

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestTheMainContainerIsNotTheSidecar(t *testing.T) {
	cs := []corev1.Container{
		{Name: "postgres-exporter", Image: "quay.io/prometheuscommunity/postgres-exporter:v0.17.1"},
		{Name: "postgres", Image: "docker.io/library/postgres:17.6-alpine"},
		{Name: "postgres-ess-updater", Image: "ghcr.io/element-hq/ess-helm/matrix-tools:0.10.3"},
	}
	if got := mainImage("ess-postgres", cs); got != "docker.io/library/postgres:17.6-alpine" {
		t.Errorf("got %s", got)
	}
	if got := mainImage("ess-matrix-rtc-sfu", []corev1.Container{{Name: "sfu", Image: "docker.io/livekit/livekit-server:v1.10.1"}}); got != "docker.io/livekit/livekit-server:v1.10.1" {
		t.Errorf("got %s", got)
	}
}

func TestImageVersion(t *testing.T) {
	for in, want := range map[string]string{
		"oci.element.io/synapse:v1.161.0":        "v1.161.0",
		"registry.local:5000/team/app:2.1":       "2.1",
		"registry.local:5000/team/app":           "",
		"ghcr.io/x/y:1.0@sha256:abcdef":          "1.0",
		"ghcr.io/x/y@sha256:abcdef":              "",
		"docker.io/library/postgres:17.6-alpine": "17.6-alpine",
	} {
		if got := imageVersion(in); got != want {
			t.Errorf("%s: got %q, want %q", in, got, want)
		}
	}
}
