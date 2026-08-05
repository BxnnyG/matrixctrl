// Package imagepin finds image tags the config holds behind the chart.
//
// On 2026-08-05 an upgrade to ESS 26.8.0 failed because the config pinned
// `matrixAuthenticationService.image.tag: "1.15.0"` while the chart expected 1.22.0.
// The chart's new MAS config used `database.password_file`, a field 1.15 does not
// know, so MAS ignored it, connected with no password, and Postgres refused it.
//
// The pin was not a one-off. The config migration froze *every* image tag at the
// moment it ran, so each chart upgrade since had been upgrading templates while
// keeping old images — partially inert, and nobody was told. 26.8.0 was simply the
// first version where the mismatch turned fatal instead of merely stale.
package imagepin

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Finding is one component whose configured tag is behind the chart's.
type Finding struct {
	Component string `json:"component"`
	Config    string `json:"config"`
	Chart     string `json:"chart"`
}

func (f Finding) String() string {
	return fmt.Sprintf("%s: Config %s, Chart %s", f.Component, f.Config, f.Chart)
}

// Compare reports components pinned to something older than the chart ships.
//
// Only *older*. A tag equal to the chart's is the normal state, and one ahead of it
// is an operator running a newer image on purpose — reporting that would be telling
// them their own decision is a mistake. Anything not comparable is left alone for
// the same reason: a digest, a branch name or a date-based tag is a deliberate
// choice, and guessing at its ordering would produce warnings nobody can act on.
func Compare(configTags, chartTags map[string]string) []Finding {
	var out []Finding

	for component, cfg := range configTags {
		chart, ok := chartTags[component]
		if !ok || cfg == "" || chart == "" || cfg == chart {
			continue
		}
		if older, comparable := isOlder(cfg, chart); comparable && older {
			out = append(out, Finding{Component: component, Config: cfg, Chart: chart})
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Component < out[j].Component })
	return out
}

// isOlder compares two version-ish tags.
//
// Returns comparable=false whenever the two cannot be ordered with confidence.
// That is the important half: a wrong "you are behind" costs the operator an
// upgrade they did not need, and the whole point of this check is to be believed.
func isOlder(a, b string) (older, comparable bool) {
	an, aok := numbers(a)
	bn, bok := numbers(b)
	if !aok || !bok || len(an) == 0 || len(bn) == 0 {
		return false, false
	}
	// Different shapes — 1.15.0 against 26.8.0-ess.1 — are not the same versioning
	// scheme, and ordering across schemes is guesswork.
	if prefixOf(a) != prefixOf(b) {
		return false, false
	}

	for i := 0; i < len(an) && i < len(bn); i++ {
		if an[i] != bn[i] {
			return an[i] < bn[i], true
		}
	}
	return len(an) < len(bn), true
}

// prefixOf returns the non-numeric lead-in, so `v1.12.14` and `1.12.25` are not
// silently compared as if they were the same scheme.
func prefixOf(s string) string {
	for i, r := range s {
		if r >= '0' && r <= '9' {
			return s[:i]
		}
	}
	return s
}

// numbers extracts the dotted numeric components, stopping at the first part that
// is not purely numeric. `v1.151.0-ess.1` yields [1 151 0]: the suffix is a
// downstream build marker, not a version ordering this should try to reason about.
func numbers(s string) ([]int, bool) {
	s = strings.TrimPrefix(prefixTrim(s), ".")
	var out []int
	for _, part := range strings.Split(s, ".") {
		// Cut a trailing suffix like `0-ess` down to its numeric head.
		if i := strings.IndexAny(part, "-+_"); i >= 0 {
			part = part[:i]
			if part == "" {
				break
			}
			n, err := strconv.Atoi(part)
			if err != nil {
				break
			}
			out = append(out, n)
			break
		}
		n, err := strconv.Atoi(part)
		if err != nil {
			break
		}
		out = append(out, n)
	}
	return out, len(out) > 0
}

func prefixTrim(s string) string {
	return strings.TrimPrefix(s, prefixOf(s))
}

// Describe renders the findings for a log line during an upgrade.
func Describe(findings []Finding) string {
	if len(findings) == 0 {
		return ""
	}
	parts := make([]string, 0, len(findings))
	for _, f := range findings {
		parts = append(parts, f.String())
	}
	n := len(findings)
	return fmt.Sprintf("%d Image-Tag%s in der Config %s älter als im Chart: %s. "+
		"Diese Komponente%s wird vom Upgrade nicht mit aktualisiert.",
		n, plural(n, "", "s"), plural(n, "ist", "sind"),
		strings.Join(parts, " · "), plural(n, "", "n werden"))
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// ExtractTags pulls `<component>.image.tag` out of a merged values map.
//
// Only the top-level component sections are considered. Nested images — the
// Postgres exporter, init containers — belong to a component rather than being one,
// and reporting them beside the components an operator recognises would turn a
// short, actionable list into a long one.
func ExtractTags(values map[string]interface{}) map[string]string {
	out := map[string]string{}
	for component, raw := range values {
		section, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		image, ok := section["image"].(map[string]interface{})
		if !ok {
			continue
		}
		if tag, ok := image["tag"].(string); ok && tag != "" {
			out[component] = tag
		}
	}
	return out
}
