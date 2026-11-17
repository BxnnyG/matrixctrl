package handlers

import "testing"

func TestSelfUpdateVersionShape(t *testing.T) {
	for v, want := range map[string]bool{
		"0.1.128": true, "0.1.128-rc.1": true, "0.1.128-rc.12": true,
		"0.1.128-beta": false, "0.1.128-rc": false, "0.1.128-rc.1.2": false, "0.1.128-": false, "x": false,
	} {
		if semverRE.MatchString(v) != want {
			t.Errorf("%q: want %v", v, want)
		}
	}
}
