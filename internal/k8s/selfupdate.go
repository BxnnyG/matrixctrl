package k8s

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// The Job that updates MatrixCtrl (etappe 116). It runs beside the panel, not in it:
// the Deployment is `Recreate`, so the panel's pod is gone before the new one starts,
// and whatever runs the upgrade must outlive it.

const (
	selfUpdateLabel       = "matrixctrl.update"
	selfUpdateVersionAnno = "matrixctrl.update/version"
)

// SelfImage is the image and service account of MatrixCtrl's own Deployment — what the
// update Job runs as. The running image, because it is certainly present on the node;
// the target version arrives as the chart, not as the Job's image.
func (c *Client) SelfImage(ctx context.Context, namespace, deployment string) (image, serviceAccount string, err error) {
	d, err := c.Static.AppsV1().Deployments(namespace).Get(ctx, deployment, metav1.GetOptions{})
	if err != nil {
		return "", "", err
	}
	spec := d.Spec.Template.Spec
	if len(spec.Containers) == 0 {
		return "", "", fmt.Errorf("deployment %s has no containers", deployment)
	}
	return spec.Containers[0].Image, spec.ServiceAccountName, nil
}

// SelfUpdateJob is one update as the panel reports it.
type SelfUpdateJob struct {
	Name     string     `json:"name"`
	Version  string     `json:"version"`
	State    string     `json:"state"` // running | succeeded | failed
	Started  time.Time  `json:"started"`
	Finished *time.Time `json:"finished,omitempty"`
	Log      string     `json:"log,omitempty"`
}

// StartSelfUpdate creates the Job, refusing while another one runs — two upgrades of the
// same release race each other into pending-upgrade.
func (c *Client) StartSelfUpdate(ctx context.Context, namespace, image, serviceAccount, release, version string) (*SelfUpdateJob, error) {
	if cur, err := c.LatestSelfUpdate(ctx, namespace); err == nil && cur != nil && cur.State == "running" {
		return nil, fmt.Errorf("ein Update auf %s läuft bereits (seit %s)", cur.Version, cur.Started.Format("15:04"))
	}
	job := selfUpdateJob(fmt.Sprintf("matrixctrl-update-%d", time.Now().Unix()), namespace, image, serviceAccount, release, version)
	created, err := c.Static.BatchV1().Jobs(namespace).Create(ctx, job, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}
	return &SelfUpdateJob{Name: created.Name, Version: version, State: "running", Started: created.CreationTimestamp.Time}, nil
}

// LatestSelfUpdate is the newest update Job with its outcome and the tail of its log.
// Nil when there has never been one (or the last is older than its TTL).
func (c *Client) LatestSelfUpdate(ctx context.Context, namespace string) (*SelfUpdateJob, error) {
	jobs, err := c.Static.BatchV1().Jobs(namespace).List(ctx, metav1.ListOptions{LabelSelector: selfUpdateLabel + "=true"})
	if err != nil {
		return nil, err
	}
	if len(jobs.Items) == 0 {
		return nil, nil
	}
	sort.Slice(jobs.Items, func(i, j int) bool {
		return jobs.Items[i].CreationTimestamp.After(jobs.Items[j].CreationTimestamp.Time)
	})
	j := jobs.Items[0]
	out := &SelfUpdateJob{
		Name: j.Name, Version: j.Annotations[selfUpdateVersionAnno],
		State: "running", Started: j.CreationTimestamp.Time,
	}
	switch {
	case j.Status.Succeeded > 0:
		out.State = "succeeded"
	case j.Status.Failed > 0:
		out.State = "failed"
	}
	if j.Status.CompletionTime != nil {
		t := j.Status.CompletionTime.Time
		out.Finished = &t
	}
	pods, err := c.Static.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: "job-name=" + j.Name})
	if err == nil && len(pods.Items) > 0 {
		if log, err := c.GetPodLogs(ctx, namespace, pods.Items[0].Name, "update", 80); err == nil {
			out.Log = strings.TrimSpace(log)
		}
	}
	return out, nil
}

func ptr[T any](v T) *T { return &v }

// selfUpdateJob describes the update Job. Pure, so what matters about it — who it runs
// as, with which image and command, how it is hardened, that it is never retried — is
// tested without a cluster.
func selfUpdateJob(name, namespace, image, serviceAccount, release, version string) *batchv1.Job {
	backoff := int32(0)
	ttl := int32(24 * 3600) // long enough for the new panel to report the outcome
	deadline := int64(15 * 60)
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name: name, Namespace: namespace,
			Labels:      map[string]string{selfUpdateLabel: "true"},
			Annotations: map[string]string{selfUpdateVersionAnno: version},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoff, // an upgrade is not retried blindly
			TTLSecondsAfterFinished: &ttl,
			ActiveDeadlineSeconds:   &deadline,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{selfUpdateLabel: "true"}},
				Spec: corev1.PodSpec{
					ServiceAccountName: serviceAccount,
					RestartPolicy:      corev1.RestartPolicyNever,
					Containers: []corev1.Container{{
						Name:  "update",
						Image: image,
						// The image's own entrypoint path (Dockerfile), with the subcommand.
						Command: []string{"/usr/local/bin/matrixctrl", "self-update",
							"--release", release, "--namespace", namespace, "--version", version},
						// Helm keeps registry config and cache under $HOME; the root file
						// system is read-only, so both go to the scratch volume.
						Env: []corev1.EnvVar{{Name: "HOME", Value: "/tmp"}, {Name: "TMPDIR", Value: "/tmp"}},
						// The pod with the most rights MatrixCtrl has gets at least the
						// panel's own hardening, not less (deployment.yaml).
						SecurityContext: &corev1.SecurityContext{
							RunAsNonRoot:             ptr(true),
							RunAsUser:                ptr(int64(65532)),
							RunAsGroup:               ptr(int64(65532)),
							SeccompProfile:           &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
							ReadOnlyRootFilesystem:   ptr(true),
							AllowPrivilegeEscalation: ptr(false),
							Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
						},
						VolumeMounts: []corev1.VolumeMount{{Name: "tmp", MountPath: "/tmp"}},
					}},
					Volumes: []corev1.Volume{{Name: "tmp", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}}},
				},
			},
		},
	}
}
