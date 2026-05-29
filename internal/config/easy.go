package config

import (
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// EasyField describes one Easy-Mode control: a UI widget bound to a dot-path in
// the merged ESS values. Easy Mode writes its changes to a dedicated overlay
// slice (easy.yaml, last in merge order) so the hand-commented base slices are
// never rewritten.
type EasyField struct {
	Path    string   `json:"path"`              // dot-path, e.g. "matrixRTC.sfu.hostNetwork"
	Label   string   `json:"label"`             // human label
	Help    string   `json:"help"`              // one-line explanation
	Type    string   `json:"type"`              // "bool" | "string" | "select"
	Group   string   `json:"group"`             // UI grouping
	Options []string `json:"options,omitempty"` // for "select"
}

// EasyFields is the curated registry. Phase 1 focuses on the WebRTC fields that
// are the project's core pain — setting these makes the post-upgrade SFU patch
// hooks redundant (they can then be disabled).
var EasyFields = []EasyField{
	{
		Path:  "matrixRTC.sfu.hostNetwork",
		Label: "SFU hostNetwork",
		Help:  "Bindet den LiveKit-SFU direkt ans Host-Netzwerk. Nötig für WebRTC-Calling hinter NAT — ersetzt den manuellen hostNetwork-Patch nach jedem Upgrade.",
		Type:  "bool",
		Group: "WebRTC / Anrufe",
	},
	{
		Path:    "matrixRTC.sfu.exposedServices.turn.externalTrafficPolicy",
		Label:   "TURN externalTrafficPolicy",
		Help:    "Auf 'Local' setzen, damit der TURN-Service die echte Client-IP sieht (sonst bricht Calling). Ersetzt den manuellen Service-Patch.",
		Type:    "select",
		Group:   "WebRTC / Anrufe",
		Options: []string{"Local", "Cluster"},
	},
	{
		Path:    "matrixRTC.sfu.exposedServices.rtcTcp.externalTrafficPolicy",
		Label:   "RTC-TCP externalTrafficPolicy",
		Help:    "Wie oben, für den TCP-Service der RTC-Media-Pfade.",
		Type:    "select",
		Group:   "WebRTC / Anrufe",
		Options: []string{"Local", "Cluster"},
	},
	{
		Path:    "matrixRTC.sfu.exposedServices.rtcMuxedUdp.externalTrafficPolicy",
		Label:   "RTC-Muxed-UDP externalTrafficPolicy",
		Help:    "Wie oben, für den gemultiplexten UDP-Service.",
		Type:    "select",
		Group:   "WebRTC / Anrufe",
		Options: []string{"Local", "Cluster"},
	},
	{
		Path:  "matrixRTC.sfu.useStunToDiscoverPublicIP",
		Label: "STUN für Public-IP-Discovery",
		Help:  "Lässt den SFU per STUN seine öffentliche IP ermitteln. Sinnvoll bei dynamischer IP / hinter NAT.",
		Type:  "bool",
		Group: "WebRTC / Anrufe",
	},
}

// GetEasyValues reads the current value of each registered field from the merged
// config map. Missing paths return nil (rendered as "unset" in the UI).
func GetEasyValues(merged map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(EasyFields))
	for _, f := range EasyFields {
		out[f.Path] = lookupPath(merged, strings.Split(f.Path, "."))
	}
	return out
}

// EasyOverlayYAML turns a flat map of {dot-path: value} into nested YAML suitable
// for the easy.yaml overlay slice. Only known fields are written; values that are
// nil or empty string are skipped so Easy Mode never forces a key it shouldn't.
func EasyOverlayYAML(values map[string]interface{}) (string, error) {
	known := make(map[string]bool, len(EasyFields))
	typeOf := make(map[string]string, len(EasyFields))
	for _, f := range EasyFields {
		known[f.Path] = true
		typeOf[f.Path] = f.Type
	}

	root := map[string]interface{}{}
	for path, raw := range values {
		if !known[path] || raw == nil {
			continue
		}
		v := coerce(raw, typeOf[path])
		if v == nil {
			continue
		}
		setPath(root, strings.Split(path, "."), v)
	}

	var sb strings.Builder
	sb.WriteString("# Managed by MatrixCtrl Easy Mode — generated, do not edit by hand.\n")
	sb.WriteString("# Overrides the base slices; remove a key here to fall back to the base value.\n")
	enc := yaml.NewEncoder(&sb)
	enc.SetIndent(2)
	if err := enc.Encode(root); err != nil {
		return "", err
	}
	_ = enc.Close()
	return sb.String(), nil
}

// coerce normalises a JSON-decoded value to the field's declared type.
func coerce(raw interface{}, t string) interface{} {
	switch t {
	case "bool":
		switch x := raw.(type) {
		case bool:
			return x
		case string:
			b, err := strconv.ParseBool(x)
			if err != nil {
				return nil
			}
			return b
		}
	case "string", "select":
		if s, ok := raw.(string); ok {
			if s == "" {
				return nil
			}
			return s
		}
	}
	return raw
}

func lookupPath(m map[string]interface{}, parts []string) interface{} {
	cur := interface{}(m)
	for _, p := range parts {
		asMap, ok := cur.(map[string]interface{})
		if !ok {
			return nil
		}
		cur, ok = asMap[p]
		if !ok {
			return nil
		}
	}
	return cur
}

func setPath(root map[string]interface{}, parts []string, value interface{}) {
	cur := root
	for i, p := range parts {
		if i == len(parts)-1 {
			cur[p] = value
			return
		}
		next, ok := cur[p].(map[string]interface{})
		if !ok {
			next = map[string]interface{}{}
			cur[p] = next
		}
		cur = next
	}
}
