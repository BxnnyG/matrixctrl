package k8s

import (
	"context"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EventInfo is a trimmed-down Kubernetes Event for the UI activity feed.
type EventInfo struct {
	Type      string `json:"type"`   // "Normal" | "Warning"
	Reason    string `json:"reason"` // BackOff, Unhealthy, Killing, FailedMount, …
	Message   string `json:"message"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Component string `json:"component,omitempty"` // owning workload, best-effort
	Count     int32  `json:"count"`
	FirstSeen string `json:"first_seen,omitempty"`
	LastSeen  string `json:"last_seen,omitempty"`
}

// eventTime picks the most recent timestamp an Event carries. Kubernetes has
// three generations of these fields and which ones are populated depends on
// whether the emitting controller uses the legacy or the events/v1 API.
func eventTime(e *corev1.Event) time.Time {
	if !e.LastTimestamp.IsZero() {
		return e.LastTimestamp.Time
	}
	if !e.EventTime.IsZero() {
		return e.EventTime.Time
	}
	if e.Series != nil && !e.Series.LastObservedTime.IsZero() {
		return e.Series.LastObservedTime.Time
	}
	return e.FirstTimestamp.Time
}

func fmtTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// podNameToComponent strips the ReplicaSet/StatefulSet suffix off a pod name so
// events can be grouped under the workload the UI shows.
// "ess-synapse-main-0" → "ess-synapse-main", "ess-haproxy-88db7b567-lcr88" → "ess-haproxy".
func podNameToComponent(name string) string {
	parts := strings.Split(name, "-")
	if len(parts) < 2 {
		return name
	}
	// StatefulSet pods end in an ordinal.
	last := parts[len(parts)-1]
	if isAllDigits(last) {
		return strings.Join(parts[:len(parts)-1], "-")
	}
	// Deployment pods end in <replicaset-hash>-<pod-hash>.
	if len(parts) >= 3 {
		return strings.Join(parts[:len(parts)-2], "-")
	}
	return name
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func toEventInfo(e *corev1.Event) EventInfo {
	kind := e.InvolvedObject.Kind
	name := e.InvolvedObject.Name
	component := name
	if kind == "Pod" {
		component = podNameToComponent(name)
	}
	count := e.Count
	if count == 0 && e.Series != nil {
		count = e.Series.Count
	}
	if count == 0 {
		count = 1
	}
	return EventInfo{
		Type:      e.Type,
		Reason:    e.Reason,
		Message:   e.Message,
		Kind:      kind,
		Name:      name,
		Component: component,
		Count:     count,
		FirstSeen: fmtTime(e.FirstTimestamp.Time),
		LastSeen:  fmtTime(eventTime(e)),
	}
}

// ListEvents returns the most recent events in a namespace, newest first.
// Warnings are what matter operationally, so callers can filter with warningsOnly.
func (c *Client) ListEvents(ctx context.Context, namespace string, limit int, warningsOnly bool) ([]EventInfo, error) {
	list, err := c.Static.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{Limit: 600})
	if err != nil {
		return nil, err
	}
	out := make([]EventInfo, 0, len(list.Items))
	for i := range list.Items {
		e := &list.Items[i]
		if warningsOnly && e.Type != corev1.EventTypeWarning {
			continue
		}
		out = append(out, toEventInfo(e))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastSeen > out[j].LastSeen })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// ListEventsForPods returns events involving any of the given pod names, newest first.
func (c *Client) ListEventsForPods(ctx context.Context, namespace string, podNames []string, limit int) ([]EventInfo, error) {
	if len(podNames) == 0 {
		return nil, nil
	}
	want := make(map[string]bool, len(podNames))
	for _, n := range podNames {
		want[n] = true
	}
	list, err := c.Static.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{Limit: 600})
	if err != nil {
		return nil, err
	}
	out := make([]EventInfo, 0, 16)
	for i := range list.Items {
		e := &list.Items[i]
		if e.InvolvedObject.Kind == "Pod" && want[e.InvolvedObject.Name] {
			out = append(out, toEventInfo(e))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastSeen > out[j].LastSeen })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
