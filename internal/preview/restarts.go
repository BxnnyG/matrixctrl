// Package preview answers "what would applying this do?" without doing it (etappe 108).
//
// The settings page had one button that committed, rendered, applied and waited, and
// only while it ran did the log say which services were restarting and whether they
// still fit on the node. By then the question was academic. This package works on two
// manifests — the one running and the one that would run — so the answer exists before
// anything is pressed.
package preview

import (
	"encoding/json"
	"sort"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/yaml"
)

// Restart is one workload whose pods would be replaced.
type Restart struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
	// New is true when the workload does not exist yet — "starts" rather than "restarts",
	// which is a different thing to tell an operator.
	New bool `json:"new,omitempty"`
}

// Restarts lists the workloads whose pod template differs between the running manifest
// and the rendered one.
//
// The whole template, not only the spec: ESS rolls a Synapse after a config change
// through a checksum annotation on the pod template, so comparing containers alone
// would call a restart "no change". Anything that changes the template makes the
// controller replace the pods; anything that does not, does not.
func Restarts(current, rendered string) []Restart {
	before := templates(current)
	after := templates(rendered)

	var out []Restart
	for key, t := range after {
		old, existed := before[key]
		if !existed {
			out = append(out, Restart{Kind: t.kind, Name: t.name, New: true})
			continue
		}
		if old.template != t.template {
			out = append(out, Restart{Kind: t.kind, Name: t.name})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].New != out[j].New {
			return !out[i].New // restarts of running services first: those are the interruptions
		}
		return out[i].Name < out[j].Name
	})
	return out
}

type podTemplate struct {
	kind, name string
	// template is the pod template re-encoded as JSON, so two renders of the same chart
	// compare equal regardless of key order or YAML formatting.
	template string
}

func templates(manifest string) map[string]podTemplate {
	out := map[string]podTemplate{}
	for _, doc := range strings.Split(manifest, "\n---") {
		var head struct {
			Kind     string `json:"kind"`
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
		}
		if yaml.Unmarshal([]byte(doc), &head) != nil || head.Metadata.Name == "" {
			continue
		}
		var tmpl *corev1.PodTemplateSpec
		switch head.Kind {
		case "Deployment":
			var d appsv1.Deployment
			if yaml.Unmarshal([]byte(doc), &d) == nil {
				tmpl = &d.Spec.Template
			}
		case "StatefulSet":
			var s appsv1.StatefulSet
			if yaml.Unmarshal([]byte(doc), &s) == nil {
				tmpl = &s.Spec.Template
			}
		case "DaemonSet":
			var s appsv1.DaemonSet
			if yaml.Unmarshal([]byte(doc), &s) == nil {
				tmpl = &s.Spec.Template
			}
		default:
			continue
		}
		if tmpl == nil {
			continue
		}
		blob, err := json.Marshal(tmpl)
		if err != nil {
			continue
		}
		out[head.Kind+"/"+head.Metadata.Name] = podTemplate{kind: head.Kind, name: head.Metadata.Name, template: string(blob)}
	}
	return out
}
