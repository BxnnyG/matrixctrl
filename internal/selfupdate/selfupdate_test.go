package selfupdate

import (
	"reflect"
	"testing"
)

// The same cases as scripts/test-install.sh for carry_values — the panel and the
// script must carry the same values, or an update means something different depending
// on where it was started.
func TestCarryValuesMatchesInstallSh(t *testing.T) {
	cases := []struct {
		name     string
		in, want map[string]interface{}
	}{
		{"the pin is removed, everything else survives",
			map[string]interface{}{"ess": map[string]interface{}{"namespace": "ess"},
				"image":   map[string]interface{}{"pullPolicy": "IfNotPresent", "tag": "0.1.70"},
				"ingress": map[string]interface{}{"host": "example.org"}},
			map[string]interface{}{"ess": map[string]interface{}{"namespace": "ess"},
				"image":   map[string]interface{}{"pullPolicy": "IfNotPresent"},
				"ingress": map[string]interface{}{"host": "example.org"}}},
		{"an image block left empty is dropped entirely",
			map[string]interface{}{"ess": map[string]interface{}{"namespace": "ess"},
				"image": map[string]interface{}{"tag": "0.1.70"}},
			map[string]interface{}{"ess": map[string]interface{}{"namespace": "ess"}}},
		{"a private mirror stays",
			map[string]interface{}{"image": map[string]interface{}{"repository": "registry.local/matrixctrl", "tag": "0.1.70"}},
			map[string]interface{}{"image": map[string]interface{}{"repository": "registry.local/matrixctrl"}}},
		{"no image block is left alone",
			map[string]interface{}{"ess": map[string]interface{}{"namespace": "ess"}},
			map[string]interface{}{"ess": map[string]interface{}{"namespace": "ess"}}},
	}
	for _, c := range cases {
		if got := CarryValues(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s:\n got %v\nwant %v", c.name, got, c.want)
		}
	}
}

// The caller's map is not changed: the API reads the same values for display.
func TestCarryValuesDoesNotMutate(t *testing.T) {
	in := map[string]interface{}{"image": map[string]interface{}{"tag": "0.1.70"}}
	CarryValues(in)
	if in["image"].(map[string]interface{})["tag"] != "0.1.70" {
		t.Error("the input was changed")
	}
}
