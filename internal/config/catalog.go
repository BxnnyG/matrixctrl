package config

import (
	"strings"

	"gopkg.in/yaml.v3"
)

// ExtractComments walks a commented YAML document and returns a map of
// dot-path → cleaned head-comment. This turns the heavily-commented ESS
// values.yaml template into per-field help text for the settings UI.
//
// Example: the comment block above `serverName:` becomes comments["serverName"].
func ExtractComments(yamlSrc string) map[string]string {
	out := map[string]string{}
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(yamlSrc), &doc); err != nil {
		return out
	}
	if len(doc.Content) == 0 {
		return out
	}
	walkComments(doc.Content[0], "", out)
	return out
}

// walkComments recurses a mapping node, recording head comments per key path.
func walkComments(node *yaml.Node, prefix string, out map[string]string) {
	if node == nil || node.Kind != yaml.MappingNode {
		return
	}
	// MappingNode.Content is [key0, val0, key1, val1, ...]
	for i := 0; i+1 < len(node.Content); i += 2 {
		key := node.Content[i]
		val := node.Content[i+1]

		path := key.Value
		if prefix != "" {
			path = prefix + "." + key.Value
		}

		if c := cleanComment(key.HeadComment); c != "" {
			out[path] = c
		} else if c := cleanComment(val.HeadComment); c != "" {
			out[path] = c
		}

		if val.Kind == yaml.MappingNode {
			walkComments(val, path, out)
		}
	}
}

// cleanComment strips YAML comment markers (#, ##) and normalises whitespace
// into a single readable help string.
func cleanComment(raw string) string {
	if raw == "" {
		return ""
	}
	var lines []string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimLeft(line, "#")
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, " ")
}

// YAMLToMap parses a YAML string into a map, returning an empty map on error.
func YAMLToMap(s string) map[string]interface{} {
	m := map[string]interface{}{}
	if strings.TrimSpace(s) == "" {
		return m
	}
	_ = yaml.Unmarshal([]byte(s), &m)
	if m == nil {
		m = map[string]interface{}{}
	}
	return m
}

// OverlayEdit describes a change set for the Easy-Mode overlay slice: paths to
// set, and paths to remove (reset to the base value).
type OverlayEdit struct {
	Changes  map[string]interface{} `json:"changes"`
	Removals []string               `json:"removals"`
}

// ApplyOverlayEdit merges an edit into the existing overlay YAML and returns the
// new overlay content. Existing overlay values are preserved unless overwritten
// or removed.
func ApplyOverlayEdit(existing string, edit OverlayEdit) (string, error) {
	root := map[string]interface{}{}
	if strings.TrimSpace(existing) != "" {
		if err := yaml.Unmarshal([]byte(existing), &root); err != nil {
			// Corrupt overlay — start fresh rather than fail the user's save.
			root = map[string]interface{}{}
		}
	}
	if root == nil {
		root = map[string]interface{}{}
	}

	for path, v := range edit.Changes {
		if v == nil {
			continue
		}
		setPath(root, strings.Split(path, "."), v)
	}
	for _, path := range edit.Removals {
		deletePath(root, strings.Split(path, "."))
	}

	var sb strings.Builder
	sb.WriteString("# Managed by MatrixCtrl Easy Mode — generated, do not edit by hand.\n")
	sb.WriteString("# Each key here overrides the base config slices.\n")
	if len(root) == 0 {
		return sb.String(), nil
	}
	enc := yaml.NewEncoder(&sb)
	enc.SetIndent(2)
	if err := enc.Encode(root); err != nil {
		return "", err
	}
	_ = enc.Close()
	return sb.String(), nil
}

// deletePath removes a nested key, pruning now-empty parent maps.
func deletePath(root map[string]interface{}, parts []string) {
	if len(parts) == 0 {
		return
	}
	if len(parts) == 1 {
		delete(root, parts[0])
		return
	}
	child, ok := root[parts[0]].(map[string]interface{})
	if !ok {
		return
	}
	deletePath(child, parts[1:])
	if len(child) == 0 {
		delete(root, parts[0])
	}
}
