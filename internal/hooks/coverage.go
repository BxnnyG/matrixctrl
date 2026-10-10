package hooks

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Sets is what a kubectl_patch action writes, as dotted paths — the notation
// internal/drift reads manifests with. Other action types write nothing that a chart
// could cover and return nil.
//
// JSON patches contribute their add and replace operations; a remove or a move cannot
// be "already done by the chart" in this sense and makes the action uncoverable
// (error). Merge patches are flattened to their leaves; a list is one value.
func (a HookAction) Sets() (map[string]any, error) {
	if a.Type != ActionKubectlPatch {
		return nil, nil
	}
	out := map[string]any{}
	switch a.PatchType {
	case "json":
		var ops []struct {
			Op    string `json:"op"`
			Path  string `json:"path"`
			Value any    `json:"value"`
		}
		if err := json.Unmarshal([]byte(a.Patch), &ops); err != nil {
			return nil, err
		}
		for _, op := range ops {
			if op.Op != "add" && op.Op != "replace" {
				return nil, fmt.Errorf("op %q cannot be covered by a chart", op.Op)
			}
			out[jsonPointerToDotted(op.Path)] = op.Value
		}
	default: // merge, strategic
		var patch map[string]any
		if err := json.Unmarshal([]byte(a.Patch), &patch); err != nil {
			return nil, err
		}
		flatten("", patch, out)
	}
	return out, nil
}

func jsonPointerToDotted(p string) string {
	parts := strings.Split(strings.TrimPrefix(p, "/"), "/")
	for i, s := range parts {
		parts[i] = strings.ReplaceAll(strings.ReplaceAll(s, "~1", "/"), "~0", "~")
	}
	return strings.Join(parts, ".")
}

func flatten(prefix string, m map[string]any, out map[string]any) {
	for k, v := range m {
		key := k
		if prefix != "" {
			key = prefix + "." + k
		}
		if sub, ok := v.(map[string]any); ok && len(sub) > 0 {
			flatten(key, sub, out)
			continue
		}
		out[key] = v
	}
}

// Coverage decides whether a hook still has anything to do. It returns a reason when
// the hook can be skipped, "" when it must run.
type Coverage func(h Hook) (reason string)
