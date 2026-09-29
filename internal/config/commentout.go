package config

import (
	"context"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// CommentOut disables one setting in a section file by turning its lines into comments
// — "dem Chart folgen lassen" (etappe 109). Everything else stays byte for byte:
// the documentation around it, the other settings, the order.
//
// Commented rather than deleted, so the old value is still there to read and to take
// back. And never leaving an empty parent behind: `image:` with nothing but comments
// under it is `image: null`, and in Helm a null does not mean "use the default", it
// *removes* the default — the whole image block of that component gone. So a parent
// that would be left empty is commented out with it, up to the component.
//
// Returns the new content and the dotted paths that were commented out.
func CommentOut(content string, path []string) (string, []string, error) {
	if len(path) == 0 {
		return content, nil, fmt.Errorf("empty path")
	}
	lines := strings.Split(content, "\n")
	var done []string

	for p := path; len(p) > 0; p = p[:len(p)-1] {
		var doc yaml.Node
		if err := yaml.Unmarshal([]byte(strings.Join(lines, "\n")), &doc); err != nil {
			return content, nil, err
		}
		key, value, siblings := find(&doc, p)
		if key == nil {
			if len(done) == 0 {
				return content, nil, fmt.Errorf("%s: nicht gesetzt", strings.Join(p, "."))
			}
			break
		}
		// After the first step, stop as soon as the parent still has content.
		if len(done) > 0 && !isEmpty(value) {
			break
		}
		for _, s := range siblings {
			if s != key && s.Line == key.Line {
				return content, nil, fmt.Errorf("%s steht in einer Zeile mit anderen Einstellungen und lässt sich nicht einzeln auskommentieren", strings.Join(p, "."))
			}
		}
		first, last := key.Line, lastLine(value, key.Line)
		for i := first; i <= last && i <= len(lines); i++ {
			l := lines[i-1]
			t := strings.TrimSpace(l)
			if t == "" || strings.HasPrefix(t, "#") {
				continue
			}
			indent := l[:len(l)-len(strings.TrimLeft(l, " "))]
			lines[i-1] = indent + "# " + strings.TrimLeft(l, " ")
		}
		lines[first-1] += "  # folgt dem Chart"
		done = append(done, strings.Join(p, "."))
	}
	return strings.Join(lines, "\n"), done, nil
}

// find returns the key node, its value and the key nodes of its mapping.
func find(doc *yaml.Node, path []string) (key, value *yaml.Node, siblings []*yaml.Node) {
	n := doc
	if n.Kind == yaml.DocumentNode && len(n.Content) > 0 {
		n = n.Content[0]
	}
	for depth, name := range path {
		if n == nil || n.Kind != yaml.MappingNode {
			return nil, nil, nil
		}
		var next *yaml.Node
		var keys []*yaml.Node
		for i := 0; i+1 < len(n.Content); i += 2 {
			keys = append(keys, n.Content[i])
			if n.Content[i].Value == name {
				key, next = n.Content[i], n.Content[i+1]
			}
		}
		if next == nil {
			return nil, nil, nil
		}
		if depth == len(path)-1 {
			return key, next, keys
		}
		n = next
	}
	return nil, nil, nil
}

func isEmpty(n *yaml.Node) bool {
	if n == nil {
		return true
	}
	switch n.Kind {
	case yaml.MappingNode, yaml.SequenceNode:
		return len(n.Content) == 0 && n.Style&yaml.FlowStyle == 0
	case yaml.ScalarNode:
		return n.Tag == "!!null" && (n.Value == "" || n.Value == "~" || n.Value == "null")
	}
	return false
}

// lastLine is the last line a value occupies — the deepest, last descendant.
func lastLine(n *yaml.Node, floor int) int {
	last := floor
	if n == nil {
		return last
	}
	if n.Line > last {
		last = n.Line
	}
	if n.Kind == yaml.ScalarNode && (n.Style&yaml.LiteralStyle != 0 || n.Style&yaml.FoldedStyle != 0) {
		last += strings.Count(strings.TrimRight(n.Value, "\n"), "\n") + 1
	}
	for _, c := range n.Content {
		if l := lastLine(c, last); l > last {
			last = l
		}
	}
	return last
}

// FollowChart lets one component's image come from the chart again: the pinned tag is
// commented out, or — when the chart has no image for the component any more — the
// whole image block (etappe 109). Written to the working tree, not committed: it shows
// up as a pending change, and the operator applies it like any other.
func (s *Store) FollowChart(ctx context.Context, component string, wholeImage bool) ([]string, error) {
	slices, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	path := []string{component, "image", "tag"}
	if wholeImage {
		path = []string{component, "image"}
	}
	for _, sl := range slices {
		var top map[string]interface{}
		if yaml.Unmarshal([]byte(sl.Content), &top) != nil {
			continue
		}
		if _, ok := top[component]; !ok {
			continue
		}
		out, done, err := CommentOut(sl.Content, path)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", sl.File, err)
		}
		if err := s.Put(ctx, sl.Name, out); err != nil {
			return nil, err
		}
		for i := range done {
			done[i] = sl.File + ": " + done[i]
		}
		return done, nil
	}
	return nil, fmt.Errorf("%s steht in keiner Abschnittsdatei", component)
}
