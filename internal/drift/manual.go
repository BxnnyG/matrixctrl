package drift

import (
	"encoding/json"
	"sort"
	"strings"
)

// Manual edits are the other half of drift, and the half MatrixCtrl was built for.
//
// E21 checks every patch a hook *declares*. It cannot see an edit no hook knows
// about — which is the case P1-11 was opened for: an Ingress carried
// `ingressClassName: disabled` and `ingress.class: ignore`, neither rendered by the
// chart, both applied by hand 69 days earlier. Helm's three-way merge preserves
// fields it has never owned, so the exception outlived every upgrade in silence.
//
// The obvious approach — render the release manifests and diff them against live —
// needs a curated list of fields to watch, because a live object carries hundreds of
// fields no manifest mentions. A curated list only ever finds what someone already
// thought of.
//
// The API server already tracks this exactly. Every object carries
// `metadata.managedFields`: one entry per manager, each naming the fields it set. So
// the question is not "what differs from the chart" but "who owns this field", and
// the cluster answers it without being asked to guess.

// ManagerKind says what kind of thing set a field.
type ManagerKind string

const (
	// Human — a `kubectl-*` manager. The one name that unambiguously means someone
	// ran a command.
	Human ManagerKind = "human"
	// Automation — a manager known to be a controller, the chart, or this product.
	Automation ManagerKind = "automation"
	// Foreign — anything else. Possibly an operator this product has never heard of
	// and possibly entirely correct, so it is reported a level quieter rather than
	// called a fault.
	Foreign ManagerKind = "foreign"
)

// knownAutomation is matched exactly. Prefix matching was rejected: a manager
// called `helm-my-own-tool` is not Helm, and quietly folding it in would hide the
// exact class of thing this file exists to surface.
var knownAutomation = map[string]bool{
	"helm":                    true,
	"matrixctrl":              true,
	"k3s":                     true,
	"kube-controller-manager": true,
	"kube-scheduler":          true,
	"kubelet":                 true,
	"traefik":                 true,
	"cert-manager":            true,
	// ESS's own helper, which writes the deployment-marker ConfigMap. It is part of
	// the managed stack, so reporting it would be reporting the chart back at the
	// operator under a different name.
	"matrix-tools": true,
}

// ClassifyManager decides how loudly a manager's fields are reported.
func ClassifyManager(manager string) ManagerKind {
	switch {
	case manager == "":
		return Foreign
	case manager == "kubectl" || strings.HasPrefix(manager, "kubectl-"):
		return Human
	case knownAutomation[manager]:
		return Automation
	case strings.HasSuffix(manager, "-controller"), strings.HasSuffix(manager, "-operator"):
		// Controllers name themselves consistently enough that this is a rule and
		// not a guess, and being wrong here costs one quiet line, not a false alarm.
		return Automation
	default:
		return Foreign
	}
}

// ManualEdit is one manager's ownership of fields on one object.
type ManualEdit struct {
	Resource  string      `json:"resource"`
	Namespace string      `json:"namespace"`
	Name      string      `json:"name"`
	Manager   string      `json:"manager"`
	Kind      ManagerKind `json:"kind"`
	Paths     []string    `json:"paths"`
	// Covered reports that a hook maintains at least one of these fields. A
	// hand-edit on a maintained field means someone went around the product; a
	// hand-edit on an unmaintained one means nothing will ever restore it. Same
	// evidence, different sentence.
	Covered bool `json:"covered"`
	// UpdatedAt is when that manager last wrote, in the object's own words.
	UpdatedAt string `json:"updated_at,omitempty"`
}

// ManagedFieldsEntry mirrors the parts of metav1.ManagedFieldsEntry this needs,
// declared locally so the reasoning stays testable with plain JSON and no k8s types.
type ManagedFieldsEntry struct {
	Manager     string          `json:"manager"`
	Operation   string          `json:"operation"`
	Time        string          `json:"time"`
	Subresource string          `json:"subresource"`
	FieldsV1    json.RawMessage `json:"fieldsV1"`
}

// ObjectFields is one live object's ownership record.
type ObjectFields struct {
	Resource  string
	Namespace string
	Name      string
	Entries   []ManagedFieldsEntry
}

// FieldPaths turns a fieldsV1 document into dotted paths.
//
// The encoding is k8s's own: `f:<name>` is a field, `k:{...}` selects a list entry
// by key, `v:<value>` by value, `i:<n>` by index, and a `.` key marks the node
// itself as owned. Only leaves are emitted — an owned parent with owned children
// would otherwise report the same ownership twice at different depths.
func FieldPaths(fieldsV1 json.RawMessage) []string {
	var doc map[string]any
	if len(fieldsV1) == 0 || json.Unmarshal(fieldsV1, &doc) != nil {
		return nil
	}
	var out []string
	walkFields("", doc, &out)
	sort.Strings(out)
	return out
}

func walkFields(prefix string, node map[string]any, out *[]string) {
	for key, raw := range node {
		if key == "." {
			continue // marks this node as owned; the parent already named it
		}
		name := decodeFieldKey(key)
		path := name
		if prefix != "" {
			path = prefix + "." + name
		}

		child, isMap := raw.(map[string]any)
		if !isMap || !hasNamedChildren(child) {
			*out = append(*out, path)
			continue
		}
		walkFields(path, child, out)
	}
}

func hasNamedChildren(m map[string]any) bool {
	for k := range m {
		if k != "." {
			return true
		}
	}
	return false
}

// decodeFieldKey renders a managedFields key readably. `k:{"name":"sfu"}` becomes
// `{name=sfu}`, because a path an operator cannot read is a path they cannot act on.
func decodeFieldKey(key string) string {
	switch {
	case strings.HasPrefix(key, "f:"):
		return key[2:]
	case strings.HasPrefix(key, "k:"):
		var sel map[string]any
		if json.Unmarshal([]byte(key[2:]), &sel) == nil && len(sel) > 0 {
			parts := make([]string, 0, len(sel))
			for k, v := range sel {
				parts = append(parts, k+"="+scalar(v))
			}
			sort.Strings(parts)
			return "{" + strings.Join(parts, ",") + "}"
		}
		return key[2:]
	case strings.HasPrefix(key, "v:"), strings.HasPrefix(key, "i:"):
		return "[" + key[2:] + "]"
	default:
		return key
	}
}

func scalar(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

// ignoredPrefixes are paths whose ownership says nothing about hand-editing.
// `status` is written by controllers by definition. The last-applied annotation is
// kubectl's own bookkeeping and would double-report every field it describes.
var ignoredPrefixes = []string{
	"status",
	"metadata.managedFields",
	"metadata.resourceVersion",
	"metadata.generation",
	"metadata.annotations.kubectl.kubernetes.io/last-applied-configuration",
	"metadata.annotations.deployment.kubernetes.io/revision",
	// `kubectl rollout restart` stamps this. It records that a restart happened; it
	// is not a configuration exception, nothing is lost when the chart overwrites
	// it, and on this cluster it was three of eight findings — enough noise to teach
	// an operator to skim past the two that mattered.
	"spec.template.metadata.annotations.kubectl.kubernetes.io/restartedAt",
}

func interesting(path string) bool {
	for _, p := range ignoredPrefixes {
		if path == p || strings.HasPrefix(path, p+".") {
			return false
		}
	}
	return true
}

// FindManualEdits reports every non-Helm ownership worth mentioning.
//
// Automation is dropped entirely — including this product's own writes. MatrixCtrl
// applying its own hooks is not drift, and reporting it would make the report a list
// of everything that has ever worked correctly.
func FindManualEdits(objects []ObjectFields, hookPaths map[string][]string) []ManualEdit {
	var out []ManualEdit

	for _, obj := range objects {
		for _, e := range obj.Entries {
			kind := ClassifyManager(e.Manager)
			if kind == Automation {
				continue
			}
			// A subresource write (status, scale) is a controller doing its job
			// regardless of which manager name it arrived under.
			if e.Subresource != "" {
				continue
			}

			var paths []string
			for _, p := range FieldPaths(e.FieldsV1) {
				if interesting(p) {
					paths = append(paths, p)
				}
			}
			if len(paths) == 0 {
				continue
			}

			out = append(out, ManualEdit{
				Resource:  obj.Resource,
				Namespace: obj.Namespace,
				Name:      obj.Name,
				Manager:   e.Manager,
				Kind:      kind,
				Paths:     paths,
				Covered:   coveredByHook(hookPaths[hookKey(obj.Resource, obj.Namespace, obj.Name)], paths),
				UpdatedAt: e.Time,
			})
		}
	}

	// Loudest first, then stable by object so the list does not reshuffle between
	// polls — a report that reorders itself is one nobody can scan.
	sort.SliceStable(out, func(i, j int) bool {
		if rank(out[i]) != rank(out[j]) {
			return rank(out[i]) < rank(out[j])
		}
		if out[i].Resource != out[j].Resource {
			return out[i].Resource < out[j].Resource
		}
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Manager < out[j].Manager
	})
	return out
}

// rank orders the report: an unmaintained hand-edit first, because it is the only
// one of the three that nothing in the system will ever put back.
func rank(e ManualEdit) int {
	switch {
	case e.Kind == Human && !e.Covered:
		return 0
	case e.Kind == Human:
		return 1
	default:
		return 2
	}
}

// HookKey builds the map key used for the hook cross-reference. Exported because the
// handler builds the map and this package consumes it; two spellings of the same key
// would silently disable the cross-reference rather than fail.
func HookKey(resource, namespace, name string) string {
	return hookKey(resource, namespace, name)
}

func hookKey(resource, namespace, name string) string {
	return strings.ToLower(resource) + "/" + namespaceOr(namespace) + "/" + name
}

func coveredByHook(hookPaths, edited []string) bool {
	for _, hp := range hookPaths {
		for _, ep := range edited {
			// Prefix either way: a hook setting `spec.template.spec` covers a hand
			// edit of `spec.template.spec.hostNetwork`, and a hook setting exactly
			// that field is covered by an edit of its parent.
			if hp == ep || strings.HasPrefix(ep, hp+".") || strings.HasPrefix(hp, ep+".") {
				return true
			}
		}
	}
	return false
}

// PatchPaths lists the fields a hook patch sets, so an edit can be told apart from
// one the product already maintains.
func PatchPaths(patchType, patch string) []string {
	switch patchType {
	case "json":
		var ops []struct {
			Path string `json:"path"`
		}
		if json.Unmarshal([]byte(patch), &ops) != nil {
			return nil
		}
		out := make([]string, 0, len(ops))
		for _, op := range ops {
			if p := strings.TrimPrefix(op.Path, "/"); p != "" {
				out = append(out, strings.ReplaceAll(p, "/", "."))
			}
		}
		sort.Strings(out)
		return out
	case "strategic":
		// Consistent with drift.apply: not guessed at. A wrong path list here would
		// mark a real finding as covered, which is the one direction that loses
		// information.
		return nil
	default:
		var doc map[string]any
		if json.Unmarshal([]byte(patch), &doc) != nil {
			return nil
		}
		var out []string
		walkJSON("", doc, &out)
		sort.Strings(out)
		return out
	}
}

func walkJSON(prefix string, node map[string]any, out *[]string) {
	for k, v := range node {
		path := k
		if prefix != "" {
			path = prefix + "." + k
		}
		if child, ok := v.(map[string]any); ok && len(child) > 0 {
			walkJSON(path, child, out)
			continue
		}
		*out = append(*out, path)
	}
}

// SummariseManual counts what the dashboard needs: the number that should be zero.
func SummariseManual(edits []ManualEdit) (unmaintained, byHand, foreign int) {
	for _, e := range edits {
		switch {
		case e.Kind == Human && !e.Covered:
			unmaintained++
		case e.Kind == Human:
			byHand++
		default:
			foreign++
		}
	}
	return
}
