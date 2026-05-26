package k8s

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

// knownGVRs maps resource type names to GroupVersionResource.
var knownGVRs = map[string]schema.GroupVersionResource{
	"deployment":  {Group: "apps", Version: "v1", Resource: "deployments"},
	"service":     {Group: "", Version: "v1", Resource: "services"},
	"statefulset": {Group: "apps", Version: "v1", Resource: "statefulsets"},
	"daemonset":   {Group: "apps", Version: "v1", Resource: "daemonsets"},
	"configmap":   {Group: "", Version: "v1", Resource: "configmaps"},
}

func (c *Client) Patch(ctx context.Context, resourceType, namespace, name string, patchType types.PatchType, data []byte) error {
	gvr, ok := knownGVRs[resourceType]
	if !ok {
		return fmt.Errorf("unknown resource type: %s", resourceType)
	}

	_, err := c.Dynamic.Resource(gvr).Namespace(namespace).Patch(
		ctx, name, patchType, data, metav1.PatchOptions{},
	)
	if err != nil {
		return fmt.Errorf("patch %s/%s: %w", namespace, name, err)
	}
	return nil
}

// WaitForRollout polls until the deployment's ready replicas match desired.
func (c *Client) WaitForRollout(ctx context.Context, namespace, name string) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		d, err := c.Static.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("get deployment: %w", err)
		}

		desired := int32(1)
		if d.Spec.Replicas != nil {
			desired = *d.Spec.Replicas
		}

		if d.Status.UpdatedReplicas == desired &&
			d.Status.ReadyReplicas == desired &&
			d.Status.AvailableReplicas == desired {
			return nil
		}
	}
}
