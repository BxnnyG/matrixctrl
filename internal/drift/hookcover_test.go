package drift

import (
	"errors"
	"strings"
	"testing"

	"github.com/bxnnyg/matrixctrl/internal/hooks"
	"github.com/bxnnyg/matrixctrl/internal/hooks/builtin"
)

// The SFU as matrix-stack 26.9.3 renders it with `matrixRTC.sfu.hostNetwork: true` and
// externalTrafficPolicy Local on the three exposed services — the production values of
// 2026-10-10, cut to what the hooks touch.
const coveredManifest = `---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: ess-matrix-rtc-sfu
spec:
  template:
    spec:
      dnsPolicy: ClusterFirstWithHostNet
      hostNetwork: true
---
apiVersion: v1
kind: Service
metadata:
  name: ess-matrix-rtc-sfu-turn
spec:
  externalTrafficPolicy: Local
---
apiVersion: v1
kind: Service
metadata:
  name: ess-matrix-rtc-sfu-muxed-udp
spec:
  externalTrafficPolicy: Local
---
apiVersion: v1
kind: Service
metadata:
  name: ess-matrix-rtc-sfu-tcp
spec:
  externalTrafficPolicy: Local
`

func hookNamed(t *testing.T, name string) hooks.Hook {
	for _, h := range builtin.ESSRTCHooks {
		if h.Name == name {
			return h
		}
	}
	t.Fatalf("no built-in hook %q", name)
	return hooks.Hook{}
}

func TestBothBuiltinHooksAreCoveredByTheChartOfToday(t *testing.T) {
	cov := HookCoverage(func() (string, error) { return coveredManifest, nil })
	for _, h := range builtin.ESSRTCHooks {
		if reason := cov(h); !strings.Contains(reason, "Nicht mehr nötig") {
			t.Errorf("%s: not covered by a manifest that sets everything it patches", h.Name)
		}
	}
}

// hostNetwork without dnsPolicy is *not* what the hook does: with the host network and
// the default policy the SFU loses the cluster's DNS and cannot find Valkey. A chart that
// set only half must leave the hook running.
func TestHalfCoveredStillRuns(t *testing.T) {
	half := strings.Replace(coveredManifest, "      dnsPolicy: ClusterFirstWithHostNet\n", "", 1)
	cov := HookCoverage(func() (string, error) { return half, nil })
	if reason := cov(hookNamed(t, "ESS RTC: SFU Host Network")); reason != "" {
		t.Fatalf("skipped a hook whose dnsPolicy the chart does not set: %s", reason)
	}
	// The other hook is untouched by that difference.
	if reason := cov(hookNamed(t, "ESS RTC: Service ExternalTrafficPolicy")); reason == "" {
		t.Error("the service hook should still be covered")
	}
}

func TestAChartThatSaysClusterKeepsTheServiceHook(t *testing.T) {
	m := strings.Replace(coveredManifest, "name: ess-matrix-rtc-sfu-tcp\nspec:\n  externalTrafficPolicy: Local", "name: ess-matrix-rtc-sfu-tcp\nspec:\n  externalTrafficPolicy: Cluster", 1)
	cov := HookCoverage(func() (string, error) { return m, nil })
	if reason := cov(hookNamed(t, "ESS RTC: Service ExternalTrafficPolicy")); reason != "" {
		t.Fatalf("skipped although one service is still Cluster: %s", reason)
	}
}

func TestWhatCannotBeComparedAlwaysRuns(t *testing.T) {
	cov := HookCoverage(func() (string, error) { return coveredManifest, nil })
	http := hooks.Hook{Name: "notify", Actions: []hooks.HookAction{{Type: hooks.ActionHTTPRequest, URL: "https://example.com"}}}
	if cov(http) != "" {
		t.Error("an HTTP hook was skipped")
	}
	remove := hooks.Hook{Name: "rm", Actions: []hooks.HookAction{{Type: hooks.ActionKubectlPatch, Resource: "deployment", Name: "ess-matrix-rtc-sfu",
		PatchType: "json", Patch: `[{"op":"remove","path":"/spec/template/spec/hostNetwork"}]`}}}
	if cov(remove) != "" {
		t.Error("a removing patch was skipped")
	}
	// The dangerous mix: a patch the chart covers, plus a call that still has to go out.
	// Skipping it would silently drop the call.
	mixed := hookNamed(t, "ESS RTC: SFU Host Network")
	mixed.Actions = append(append([]hooks.HookAction(nil), mixed.Actions...), hooks.HookAction{Type: hooks.ActionHTTPRequest, URL: "https://example.com/notify"})
	if cov(mixed) != "" {
		t.Error("a covered patch hid an HTTP call that still has to run")
	}
	broken := HookCoverage(func() (string, error) { return "", errors.New("helm unreachable") })
	if broken(hookNamed(t, "ESS RTC: SFU Host Network")) != "" {
		t.Error("skipped without a manifest to compare against")
	}
}
