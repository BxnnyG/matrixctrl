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

// execInPod runs one command in a container and wires the streams.
//
// Three callers wanted this before it existed and each built its own request, executor
// and error handling — the shape that put `pods/exec` in the archive path twice and
// would have put a third copy here (rule 3). stderr is always collected: when a command
// in a container refuses, its one line is the entire explanation, and a stream that ends
// early with no reason is the failure mode this project keeps meeting.
func (c *Client) execInPod(ctx context.Context, namespace, pod, container string,
	cmd []string, stdin io.Reader, stdout io.Writer) (string, error) {

	if c == nil || c.Static == nil || c.rest == nil {
		return "", fmt.Errorf("no cluster connection")
	}
	req := c.Static.CoreV1().RESTClient().Post().
		Resource("pods").Namespace(namespace).Name(pod).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: container,
			Command:   cmd,
			Stdin:     stdin != nil,
			Stdout:    stdout != nil,
			Stderr:    true,
		}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(c.rest, "POST", req.URL())
	if err != nil {
		return "", fmt.Errorf("exec into %s/%s: %w", namespace, pod, err)
	}
	var errOut strings.Builder
	err = exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin: stdin, Stdout: stdout, Stderr: &errOut,
	})
	msg := strings.TrimSpace(errOut.String())
	if err != nil {
		if msg != "" {
			return msg, fmt.Errorf("%s in %s/%s: %s", cmd[0], namespace, pod, msg)
		}
		return msg, fmt.Errorf("%s in %s/%s: %w", cmd[0], namespace, pod, err)
	}
	return msg, nil
}

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
	// `-C dir .` rather than tarring an absolute path: the archive then contains
	// relative names and unpacks into whatever directory it is given, which is what a
	// restore onto a differently-laid-out pod needs.
	_, err := c.execInPod(ctx, namespace, pod, container,
		[]string{"tar", "-cf", "-", "-C", dir, "."}, nil, out)
	return err
}

// TarIntoPod streams a tar back in, unpacking it in the container.
//
// The counterpart of TarFromPod and the reason the archive stores the media as one tar
// member rather than unpacking it: what came out goes back in as a stream, and a
// homeserver with a hundred gigabytes of uploads is never held anywhere in between.
//
// It unpacks into `dir` without deleting anything first. The caller stages into a
// directory of its own and moves the result into place, because /media is a mount
// point: nothing can be created beside it and it cannot be renamed — the same shape
// that broke the config restore in etappe 73.
func (c *Client) TarIntoPod(ctx context.Context, namespace, pod, container, dir string, in io.Reader) error {
	_, err := c.execInPod(ctx, namespace, pod, container,
		[]string{"tar", "-xf", "-", "-C", dir}, in, io.Discard)
	return err
}

// RunInPod runs a command and returns its output, for the small filesystem steps a
// restore needs around the tar: making a staging directory, moving it into place,
// asking how much room is left.
func (c *Client) RunInPod(ctx context.Context, namespace, pod, container string, cmd ...string) (string, error) {
	var out strings.Builder
	if _, err := c.execInPod(ctx, namespace, pod, container, cmd, nil, &out); err != nil {
		return strings.TrimSpace(out.String()), err
	}
	return strings.TrimSpace(out.String()), nil
}

// FreeSpaceInPod reports the bytes left on the filesystem holding dir.
//
// Asked before a restore writes anything: running out of room halfway through a media
// restore is a failure that leaves half the uploads in a staging directory, and a
// number beforehand turns it into a sentence the operator reads instead.
func (c *Client) FreeSpaceInPod(ctx context.Context, namespace, pod, container, dir string) (int64, error) {
	out, err := c.RunInPod(ctx, namespace, pod, container, "df", "-Pk", dir)
	if err != nil {
		return 0, err
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	fields := strings.Fields(lines[len(lines)-1])
	if len(fields) < 4 {
		return 0, fmt.Errorf("could not read the free space on %s: %q", dir, out)
	}
	var kb int64
	if _, err := fmt.Sscanf(fields[3], "%d", &kb); err != nil {
		return 0, fmt.Errorf("could not read the free space on %s: %q", dir, out)
	}
	return kb * 1024, nil
}

// DirSizeInPod reports how large a directory is, so the choice to include it can be
// made against a number instead of a feeling.
func (c *Client) DirSizeInPod(ctx context.Context, namespace, pod, container, dir string) (int64, error) {
	out, err := c.RunInPod(ctx, namespace, pod, container, "du", "-sk", dir)
	if err != nil {
		return 0, err
	}
	var kb int64
	if _, err := fmt.Sscanf(out, "%d", &kb); err != nil {
		return 0, fmt.Errorf("could not read the size of %s: %q", dir, out)
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
