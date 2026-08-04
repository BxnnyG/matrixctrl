package k8s

import (
	"context"
	"fmt"
	"sort"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Field ownership is recorded by the API server itself, in every object's
// `metadata.managedFields`. That makes "who set this field" a question the cluster
// answers rather than one the product has to infer by diffing — see
// internal/drift/manual.go for why the diffing approach was rejected.
//
// It lives in metadata, so a metadata-only list returns everything needed and none
// of the spec. On this cluster that is the difference between a few kilobytes and
// most of a megabyte per poll.

// ManagedFieldsEntry is one manager's ownership record, kept free of k8s types so
// the consumer can stay testable with plain JSON.
type ManagedFieldsEntry struct {
	Manager     string
	Operation   string
	Time        string
	Subresource string
	FieldsV1    []byte
}

// ObjectOwnership is one object and everyone who has written to it.
type ObjectOwnership struct {
	Resource  string
	Namespace string
	Name      string
	Entries   []ManagedFieldsEntry
}

// OwnershipTypes is the surface worth checking for hand-edits. Deployments alone
// would have missed both real cases: the SFU patches live on a Deployment, but the
// 69-day-old `ingressClassName: disabled` was on an Ingress, and the
// externalTrafficPolicy patches are on Services.
var OwnershipTypes = []string{"deployment", "statefulset", "service", "ingress", "configmap"}

// ListOwnership returns field ownership for every object of the given types in the
// namespace.
//
// A type that cannot be listed is skipped with its error collected rather than
// failing the whole report: one missing CRD or one RBAC gap should cost that type's
// answer, not every type's. The caller is told which failed so it can say "partial"
// instead of implying completeness.
func (c *Client) ListOwnership(ctx context.Context, namespace string, resourceTypes []string) ([]ObjectOwnership, []error) {
	if c == nil || c.Meta == nil {
		return nil, []error{fmt.Errorf("metadata client unavailable")}
	}

	var out []ObjectOwnership
	var problems []error

	for _, rt := range resourceTypes {
		gvr, ok := knownGVRs[rt]
		if !ok {
			problems = append(problems, fmt.Errorf("unknown resource type: %s", rt))
			continue
		}

		list, err := c.Meta.Resource(gvr).Namespace(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			problems = append(problems, fmt.Errorf("list %s: %w", rt, err))
			continue
		}

		for i := range list.Items {
			item := &list.Items[i]
			entries := make([]ManagedFieldsEntry, 0, len(item.ManagedFields))
			for _, mf := range item.ManagedFields {
				e := ManagedFieldsEntry{
					Manager:     mf.Manager,
					Operation:   string(mf.Operation),
					Subresource: mf.Subresource,
				}
				if mf.Time != nil {
					e.Time = mf.Time.UTC().Format("2006-01-02T15:04:05Z")
				}
				if mf.FieldsV1 != nil {
					e.FieldsV1 = mf.FieldsV1.Raw
				}
				entries = append(entries, e)
			}
			out = append(out, ObjectOwnership{
				Resource: rt, Namespace: item.Namespace, Name: item.Name, Entries: entries,
			})
		}
	}

	// Stable order so a report polled twice does not reshuffle.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Resource != out[j].Resource {
			return out[i].Resource < out[j].Resource
		}
		return out[i].Name < out[j].Name
	})
	return out, problems
}
