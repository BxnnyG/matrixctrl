package k8s

import "testing"

// "Deployment" is how a manifest spells it; the MAS restart of etappe 110 failed on it.
func TestResourceTypesResolveInAnyCase(t *testing.T) {
	for _, k := range []string{"deployment", "Deployment", "StatefulSet", "statefulset"} {
		if _, ok := gvrFor(k); !ok {
			t.Errorf("%s did not resolve", k)
		}
	}
	if _, ok := gvrFor("Secret"); ok {
		t.Error("an unknown type must still be refused")
	}
}
