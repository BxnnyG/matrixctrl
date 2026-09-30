// Package selfupdate updates MatrixCtrl from its own panel (etappe 116).
//
// The upgrade itself runs in a Job — see helm.UpgradeSelf for why it cannot run in the
// panel's pod. This package holds what the Job and the API share: which values carry
// over, and how the Job is described.
package selfupdate

// CarryValues are the release's own values for the upgrade, minus the image tag.
//
// Same rule as `install.sh carry_values`, and for the same reason: a `tag` carried over
// pins the old version, and the "update" re-applies what is already running (§4.103).
// `repository` and `pullPolicy` stay — a private mirror is configuration, the version is
// not. An `image` map left empty is dropped entirely: an empty map merged over the
// chart's image block is harmless, but the shell twin has to remove it to avoid YAML
// null, and the two paths should leave the same values behind.
func CarryValues(values map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(values))
	for k, v := range values {
		out[k] = v
	}
	img, ok := out["image"].(map[string]interface{})
	if !ok {
		return out
	}
	kept := make(map[string]interface{}, len(img))
	for k, v := range img {
		if k != "tag" {
			kept[k] = v
		}
	}
	if len(kept) == 0 {
		delete(out, "image")
	} else {
		out["image"] = kept
	}
	return out
}
