package k8s

import (
	"context"
	"fmt"
	"io"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
	"sigs.k8s.io/yaml"
)

// TarFromPod streams a directory out of a container as a tar.
//
// The uploaded files live on a volume that only the Synapse pod mounts, so nothing
// outside that pod can read them — which is why "the media" was listed as *not
// included* in every archive this product has ever produced. The exec subresource is
// the way in, and it is the API, not a shelled-out `kubectl` (rule 4): the same channel
// the log reader uses, negotiated over SPDY because it is bidirectional.
//
// The operator was asked before this existed, because it widens what MatrixCtrl does
// inside the cluster (etappe 102).
func (c *Client) TarFromPod(ctx context.Context, namespace, pod, container, dir string, out io.Writer) error {
	if c == nil || c.Static == nil || c.rest == nil {
		return fmt.Errorf("no cluster connection")
	}

	req := c.Static.CoreV1().RESTClient().Post().
		Resource("pods").Namespace(namespace).Name(pod).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: container,
			// `-C dir .` rather than tarring an absolute path: the archive then
			// contains relative names and unpacks into whatever directory it is given,
			// which is what a restore onto a differently-laid-out pod needs.
			Command: []string{"tar", "-cf", "-", "-C", dir, "."},
			Stdout:  true,
			Stderr:  true,
		}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(c.rest, "POST", req.URL())
	if err != nil {
		return fmt.Errorf("exec into %s/%s: %w", namespace, pod, err)
	}

	// stderr is collected rather than discarded: when tar refuses, its one line is the
	// entire explanation, and a stream that ends early with no reason is the failure
	// mode this project keeps meeting.
	var errOut strings.Builder
	if err := exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: out,
		Stderr: &errOut,
	}); err != nil {
		if msg := strings.TrimSpace(errOut.String()); msg != "" {
			return fmt.Errorf("tar in %s/%s said: %s", namespace, pod, msg)
		}
		return fmt.Errorf("streaming from %s/%s: %w", namespace, pod, err)
	}
	return nil
}

// DirSizeInPod reports how large a directory is, so the choice to include it can be
// made against a number instead of a feeling.
func (c *Client) DirSizeInPod(ctx context.Context, namespace, pod, container, dir string) (int64, error) {
	if c == nil || c.Static == nil || c.rest == nil {
		return 0, fmt.Errorf("no cluster connection")
	}
	req := c.Static.CoreV1().RESTClient().Post().
		Resource("pods").Namespace(namespace).Name(pod).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: container,
			Command:   []string{"du", "-sk", dir},
			Stdout:    true,
			Stderr:    true,
		}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(c.rest, "POST", req.URL())
	if err != nil {
		return 0, err
	}
	var outBuf, errBuf strings.Builder
	if err := exec.StreamWithContext(ctx, remotecommand.StreamOptions{Stdout: &outBuf, Stderr: &errBuf}); err != nil {
		return 0, err
	}
	var kb int64
	if _, err := fmt.Sscanf(strings.TrimSpace(outBuf.String()), "%d", &kb); err != nil {
		return 0, fmt.Errorf("could not read the size of %s: %q", dir, strings.TrimSpace(outBuf.String()))
	}
	return kb * 1024, nil
}

// SecretYAML returns a Secret as the YAML an operator could `kubectl apply`.
//
// The whole object, not a value: restoring a homeserver's keys means putting back
// every entry under the same name, and picking them out one by one is how one gets
// forgotten.
func (c *Client) SecretYAML(ctx context.Context, namespace, name string) ([]byte, error) {
	if c == nil || c.Static == nil {
		return nil, fmt.Errorf("no cluster connection")
	}
	sec, err := c.Static.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	// Server-side bookkeeping is stripped: it belongs to the cluster this came from,
	// and an apply on another one would be refused because of it.
	sec.ManagedFields = nil
	sec.ResourceVersion = ""
	sec.UID = ""
	sec.CreationTimestamp = metav1.Time{}
	sec.APIVersion, sec.Kind = "v1", "Secret"

	return yaml.Marshal(sec)
}

// PodsByLabel lists the running pods matching a selector, newest first is not needed —
// callers here want "one that mounts this volume", and any running one does.
func (c *Client) PodsByLabel(ctx context.Context, namespace, selector string) ([]string, error) {
	if c == nil || c.Static == nil {
		return nil, fmt.Errorf("no cluster connection")
	}
	list, err := c.Static.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return nil, err
	}
	var out []string
	for i := range list.Items {
		if list.Items[i].Status.Phase == corev1.PodRunning {
			out = append(out, list.Items[i].Name)
		}
	}
	return out, nil
}
