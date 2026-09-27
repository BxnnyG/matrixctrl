package config

import (
	"regexp"
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

		if c := ownComment(key.HeadComment, path); c != "" {
			out[path] = c
		} else if c := ownComment(val.HeadComment, path); c != "" {
			out[path] = c
		}

		if val.Kind == yaml.MappingNode {
			walkComments(val, path, out)
		}
	}
}

// ownComment returns the documentation that belongs to a key.
//
// yaml.v3 hands a key every comment line above it, back to the previous key. In the ESS
// section files that stretch routinely holds commented-out settings (`# port: 5432`) and
// *their* documentation, so joining all of it gave MAS's `additional` a description
// about PostgreSQL ports, usernames and two secrets (etappe 107). What belongs to a key
// is the block directly above it: read upwards from the key, stop at a blank line or at
// a line that is itself a commented-out setting.
//
// The documentation keeps its shape. Wrapped prose is joined into paragraphs; a `##`
// line on its own is a paragraph break; YAML written as an example stays on its own
// lines with its indentation — flattened, "additional: 0-customConfig: config: |" is
// neither prose nor YAML.
func ownComment(raw, path string) string {
	if raw == "" {
		return ""
	}
	// The key and its ancestors. A commented-out block headed by one of these names is
	// an example of this very setting — ESS writes `## docs`, then `# service:` with
	// indented sample values, then `service: {}` — so it is stepped over and the
	// documentation above it kept. A commented-out setting with any other name belongs
	// to a sibling, and the block ends there.
	own := map[string]bool{}
	for _, seg := range strings.Split(path, ".") {
		own[seg] = true
	}
	all := strings.Split(raw, "\n")
	var block []string
	for i := len(all) - 1; i >= 0; i-- {
		line := strings.TrimSpace(all[i])
		if line == "" {
			if len(block) > 0 {
				break
			}
			continue
		}
		if !strings.HasPrefix(line, "#") {
			break
		}
		if !strings.HasPrefix(line, "##") {
			body := stripMarker(all[i])
			if strings.HasPrefix(body, " ") {
				continue // indented sample value inside an example block
			}
			if m := commentedOutSetting.FindStringSubmatch(strings.TrimSpace(body)); m != nil {
				if own[strings.Trim(m[1], `"'`)] {
					continue // the head of an example of this key
				}
				break // a sibling setting: everything above documents it, not us
			}
		}
		block = append(block, all[i])
	}
	// Collected bottom-up.
	for l, r := 0, len(block)-1; l < r; l, r = l+1, r-1 {
		block[l], block[r] = block[r], block[l]
	}

	var out []string
	var para []string
	flush := func() {
		if len(para) > 0 {
			out = append(out, strings.Join(para, " "))
			para = nil
		}
	}
	for _, line := range block {
		body := stripMarker(line)
		switch {
		case strings.TrimSpace(body) == "":
			flush()
		case isExampleLine(body):
			flush()
			out = append(out, strings.TrimRight(body, " "))
		default:
			para = append(para, strings.TrimSpace(body))
		}
	}
	flush()
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// commentedOutSetting matches a disabled setting written as a comment: `key:`,
// `key: value` or a list item. Checked only on single-`#` lines — `##` is the chart's
// documentation marker, and documentation may legitimately quote YAML.
var commentedOutSetting = regexp.MustCompile(`^(?:-\s|([A-Za-z0-9_.\-"']+):(?:\s|$))`)

// exampleLine matches YAML inside documentation: `key:`, `key: token`, `key: |`, or a
// list item. A sentence with a colon ("Note: changing this restarts…") has more than
// one word after it and stays prose.
var exampleLine = regexp.MustCompile(`^[A-Za-z0-9_.\-"'<>]+:( \S+)?$|^- `)

func isExampleLine(body string) bool {
	if strings.HasPrefix(body, "  ") {
		return true // indented under an example key
	}
	return exampleLine.MatchString(strings.TrimSpace(body))
}

// stripMarker removes the leading hashes and exactly one following space, keeping any
// further indentation — the difference between an example's structure and none.
func stripMarker(line string) string {
	t := strings.TrimLeft(strings.TrimSpace(line), "#")
	return strings.TrimPrefix(t, " ")
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
