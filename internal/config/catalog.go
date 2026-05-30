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

