package drift

import (
	"fmt"
	"reflect"
	"strings"

	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/yaml"
)

// Whether a hand-set field still differs from what the chart wants (etappe 111).
//
// Ownership says who wrote a field last, not whether it matters. The resources of
// Postgres and Synapse were set with `kubectl set resources` in an emergency and then
// written into the settings with the same values; Helm's three-way merge saw nothing to
// change, so kubectl stayed the owner, and the dashboard kept warning that the fields
// "survive every upgrade unnoticed" — about values the chart itself now sets. A field
// whose live value equals the release manifest's is not an exception any more.

// ManifestObject finds one object in a rendered release manifest. The resource type is
// matched against the kind without regard to case ("statefulset" ~ "StatefulSet").
func ManifestObject(manifest, resourceType, name string) map[string]any {
	for _, doc := range strings.Split(manifest, "\n---") {
		var obj map[string]any
		if yaml.Unmarshal([]byte(doc), &obj) != nil || obj == nil {
			continue
		}
		kind, _ := obj["kind"].(string)
		meta, _ := obj["metadata"].(map[string]any)
		n, _ := meta["name"].(string)
		if strings.EqualFold(kind, resourceType) && n == name {
			return obj
		}
	}
	return nil
}

// MatchesChart reports whether every path has the same value live as in the chart's
// object. A path missing on either side is not a match: "the chart does not set it"
// means the hand-set value is the only one there is.
func MatchesChart(live, chart map[string]any, paths []string) bool {
	if live == nil || chart == nil || len(paths) == 0 {
		return false
	}
	for _, p := range paths {
		a, okA := ValueAt(live, p)
		b, okB := ValueAt(chart, p)
		if !okA || !okB || !SameValue(a, b) {
			return false
		}
	}
	return true
}

// ValueAt resolves a path in the report's notation: dotted keys, `{name=x}` for list
// items selected by key. Keys may themselves contain dots (annotation names), so at each
// map the longest run of segments that names an existing key wins.
func ValueAt(obj any, path string) (any, bool) {
	return valueAt(obj, strings.Split(path, "."))
}

func valueAt(cur any, segs []string) (any, bool) {
	if len(segs) == 0 {
		return cur, true
	}
	switch node := cur.(type) {
	case map[string]any:
		for n := len(segs); n >= 1; n-- {
			key := strings.Join(segs[:n], ".")
			if v, ok := node[key]; ok {
				return valueAt(v, segs[n:])
			}
		}
		return nil, false
	case []any:
		seg := segs[0]
		if !strings.HasPrefix(seg, "{") || !strings.HasSuffix(seg, "}") {
			return nil, false // positional and set items carry no stable identity
		}
		want := map[string]string{}
		for _, kv := range strings.Split(seg[1:len(seg)-1], ",") {
			k, v, ok := strings.Cut(kv, "=")
			if !ok {
				return nil, false
			}
			want[k] = v
		}
		for _, item := range node {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			match := true
			for k, v := range want {
				if fmt.Sprint(m[k]) != v {
					match = false
					break
				}
			}
			if match {
				return valueAt(m, segs[1:])
			}
		}
	}
	return nil, false
}

// SameValue compares two field values; Kubernetes quantities by amount, so "1" equals
// "1000m" and "1536Mi" equals "1.5Gi" — the API server and a values file spell the same
// request differently.
func SameValue(a, b any) bool {
	if reflect.DeepEqual(a, b) {
		return true
	}
	qa, errA := resource.ParseQuantity(fmt.Sprint(a))
	qb, errB := resource.ParseQuantity(fmt.Sprint(b))
	if errA == nil && errB == nil {
		return qa.Cmp(qb) == 0
	}
	return fmt.Sprint(a) == fmt.Sprint(b)
}
