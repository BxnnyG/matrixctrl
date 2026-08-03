// Package drift answers one question per hook action: is this patch still applied?
//
// It exists because of 2026-08-02. A Helm upgrade was run outside MatrixCtrl, so
// the post-upgrade hooks never fired. The chart re-rendered the SFU deployment
// without `hostNetwork: true` and three Services fell back to
// `externalTrafficPolicy: Cluster`. The SFU stopped binding its host ports, media
// stopped arriving, and every screen in the product stayed green — pods healthy,
// release deployed, hooks listed as enabled. "Enabled" says a hook would run. It
// says nothing about whether its effect is currently in the cluster.
//
// docs/DESIGN.md S11 already required "the SFU patches survive a Helm upgrade"
// before every ship. It was a sentence in a document, and sentences do not run.
//
// The method needs no new specification, because the hooks already are one. Each
// kubectl_patch action carries the resource, the name and the patch body. So:
//
//	fetch the live object → apply the patch in memory → did anything change?
//
// A patch that changes nothing has already taken effect. That test is exact, and it
// keeps working for hooks nobody has written yet, because nothing here is
// hardcoded to the SFU or to any particular field.
package drift

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	jsonpatch "github.com/evanphx/json-patch"
)

type Status string

const (
	// Satisfied — applying the patch would change nothing.
	Satisfied Status = "satisfied"
	// Drifted — the patch would change the live object, so it is not in effect.
	Drifted Status = "drifted"
	// Unknown — we could not tell. Never collapse this into Satisfied: reporting
	// "fine" when the answer was unavailable is the exact failure this package
	// exists to remove, one layer up.
	Unknown Status = "unknown"
)

// Finding is one answer about one action.
type Finding struct {
	Hook      string   `json:"hook"`
	Action    string   `json:"action"`
	Resource  string   `json:"resource"`
	Namespace string   `json:"namespace"`
	Name      string   `json:"name"`
	Status    Status   `json:"status"`
	Detail    string   `json:"detail"`
	Paths     []string `json:"paths,omitempty"` // fields the patch would change
}

// Action is the subset of a hook action this package needs. Declared here rather
// than importing the hooks package so the check has no opinion about storage.
type Action struct {
	Hook        string
	Description string
	Resource    string
	Namespace   string
	Name        string
	PatchType   string // "json" | "merge" | "strategic"
	Patch       string
}

// Fetcher returns the live object as JSON.
type Fetcher interface {
	GetObjectJSON(ctx context.Context, resourceType, namespace, name string) ([]byte, error)
}

// Check evaluates every action and returns one finding each, in input order so the
// report is stable across polls.
func Check(ctx context.Context, actions []Action, f Fetcher) []Finding {
	findings := make([]Finding, 0, len(actions))
	for _, a := range actions {
		findings = append(findings, checkOne(ctx, a, f))
	}
	return findings
}

func checkOne(ctx context.Context, a Action, f Fetcher) Finding {
	out := Finding{
		Hook:      a.Hook,
		Action:    a.Description,
		Resource:  a.Resource,
		Namespace: namespaceOr(a.Namespace),
		Name:      a.Name,
	}

	if f == nil {
		out.Status, out.Detail = Unknown, "no cluster connection"
		return out
	}

	live, err := f.GetObjectJSON(ctx, a.Resource, out.Namespace, a.Name)
	if err != nil {
		// A resource that does not exist is not drift. On a greenfield cluster the
		// SFU deployment is simply absent, and calling that "drifted" would train
		// the operator to ignore the report.
		out.Status, out.Detail = Unknown, err.Error()
		return out
	}

	patched, err := apply(live, a.PatchType, a.Patch)
	if err != nil {
		out.Status, out.Detail = Unknown, err.Error()
		return out
	}

	paths := changedPaths(live, patched)
	if len(paths) == 0 {
		out.Status, out.Detail = Satisfied, "applying this patch would change nothing"
		return out
	}

	out.Status = Drifted
	out.Paths = paths
	out.Detail = fmt.Sprintf("the patch is not in effect; it would still change %s", strings.Join(paths, ", "))
	return out
}

// apply runs the patch in memory using the same semantics as the apply path
// (internal/hooks.runPatch). Strategic merge is deliberately not guessed at: it
// needs the typed Go schema of the target resource to handle lists correctly, and
// approximating it with plain merge semantics would report drift on list fields
// that are actually fine. An honest Unknown beats a confident wrong answer.
func apply(live []byte, patchType, patch string) ([]byte, error) {
	switch patchType {
	case "json":
		p, err := jsonpatch.DecodePatch([]byte(patch))
		if err != nil {
			return nil, fmt.Errorf("undecodable json patch: %w", err)
		}
		return p.Apply(live)
	case "strategic":
		return nil, fmt.Errorf("strategic merge patches cannot be checked without the resource schema")
	default: // "merge" and empty, matching runPatch's default
		return jsonpatch.MergePatch(live, []byte(patch))
	}
}

// changedPaths reports which fields differ, as dotted paths. The list is what makes
// a finding actionable: "drifted" alone sends someone diffing YAML by hand, while
// "spec.template.spec.hostNetwork" is the answer.
func changedPaths(before, after []byte) []string {
	var a, b any
	if json.Unmarshal(before, &a) != nil || json.Unmarshal(after, &b) != nil {
		return nil
	}
	var paths []string
	diff("", a, b, &paths)
	sort.Strings(paths)
	return paths
}

func diff(prefix string, a, b any, out *[]string) {
	am, aok := a.(map[string]any)
	bm, bok := b.(map[string]any)
	if aok && bok {
		// Only walk keys present after the patch. A patch cannot remove a field
		// without naming it, and walking the union would report every field the
		// live object has and the patch does not — which is all of them.
		for k, bv := range bm {
			diff(join(prefix, k), am[k], bv, out)
		}
		// A JSON patch *can* remove, so catch keys that disappeared.
		for k := range am {
			if _, still := bm[k]; !still {
				*out = append(*out, join(prefix, k))
			}
		}
		return
	}
	if !equalJSON(a, b) {
		*out = append(*out, prefix)
	}
}

func join(prefix, key string) string {
	if prefix == "" {
		return key
	}
	return prefix + "." + key
}

func equalJSON(a, b any) bool {
	ab, err1 := json.Marshal(a)
	bb, err2 := json.Marshal(b)
	if err1 != nil || err2 != nil {
		return false
	}
	return string(ab) == string(bb)
}

// namespaceOr mirrors runPatch: an empty namespace means ess. Duplicating the
// default would be a bug the day someone changes it, so it is stated once here and
// referenced from the caller that builds Actions.
func namespaceOr(ns string) string {
	if ns == "" {
		return "ess"
	}
	return ns
}

// Summary counts findings by status — the dashboard needs the number, not the list.
func Summary(findings []Finding) map[Status]int {
	counts := map[Status]int{Satisfied: 0, Drifted: 0, Unknown: 0}
	for _, f := range findings {
		counts[f.Status]++
	}
	return counts
}
