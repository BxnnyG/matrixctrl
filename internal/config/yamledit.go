package config

import (
	"bytes"

	"gopkg.in/yaml.v3"
)

// Comment-preserving YAML editing on yaml.v3 Node trees.
//
// The settings UI must be able to change individual values without destroying the
// `##` documentation comments in the file (those comments are surfaced as help
// text). Round-tripping through a map[string]interface{} would drop all comments,
// so all edits operate on the Node tree and only touch the nodes they must.

// mappingRoot returns the top-level mapping node of a parsed document, creating an
// empty one if the document is blank.
func mappingRoot(root *yaml.Node) *yaml.Node {
	if root == nil {
		return &yaml.Node{Kind: yaml.MappingNode}
	}
	if root.Kind == yaml.DocumentNode {
		if len(root.Content) == 0 {
			m := &yaml.Node{Kind: yaml.MappingNode}
			root.Content = []*yaml.Node{m}
			return m
		}
		return root.Content[0]
	}
	return root
}

// findKey returns the index of a key's scalar node within a mapping's Content
// slice (keys are at even indices), or -1.
func findKey(m *yaml.Node, key string) int {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return i
		}
	}
	return -1
}

// ParseYAMLNode parses YAML text into a document Node (comments preserved).
func ParseYAMLNode(src string) (*yaml.Node, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(src), &doc); err != nil {
		return nil, err
	}
	if doc.Kind == 0 {
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode}}}
	}
	return &doc, nil
}

// MarshalNode renders a Node back to YAML text with 2-space indentation.
func MarshalNode(node *yaml.Node) (string, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(node); err != nil {
		return "", err
	}
	_ = enc.Close()
	return buf.String(), nil
}

// SetNodeValue sets the value at a dot-path within the document, creating any
// intermediate mappings. Existing keys keep their comments; only the value node
// is replaced. `value` may be any Go value (scalar, slice, map).
func SetNodeValue(root *yaml.Node, path []string, value interface{}) error {
	return setIn(mappingRoot(root), path, value)
}

func setIn(m *yaml.Node, path []string, value interface{}) error {
	key := path[0]
	idx := findKey(m, key)

	if len(path) == 1 {
		var vn yaml.Node
		if err := vn.Encode(value); err != nil {
			return err
		}
		if idx >= 0 {
			// Preserve the value node's leading comment if it had one.
			vn.HeadComment = m.Content[idx+1].HeadComment
			vn.LineComment = m.Content[idx+1].LineComment
			m.Content[idx+1] = &vn
		} else {
			kn := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
			m.Content = append(m.Content, kn, &vn)
		}
		return nil
	}

	var child *yaml.Node
	if idx >= 0 {
		child = m.Content[idx+1]
		if child.Kind != yaml.MappingNode {
			child = &yaml.Node{Kind: yaml.MappingNode}
			m.Content[idx+1] = child
		}
	} else {
		kn := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
		child = &yaml.Node{Kind: yaml.MappingNode}
		m.Content = append(m.Content, kn, child)
	}
	return setIn(child, path[1:], value)
}

// DeleteNodeValue removes a dot-path from the document, pruning emptied parents.
func DeleteNodeValue(root *yaml.Node, path []string) {
	deleteIn(mappingRoot(root), path)
}

func deleteIn(m *yaml.Node, path []string) {
	idx := findKey(m, path[0])
	if idx < 0 {
		return
	}
	if len(path) == 1 {
		m.Content = append(m.Content[:idx], m.Content[idx+2:]...)
		return
	}
	child := m.Content[idx+1]
	if child.Kind == yaml.MappingNode {
		deleteIn(child, path[1:])
		if len(child.Content) == 0 {
			m.Content = append(m.Content[:idx], m.Content[idx+2:]...)
		}
	}
}

// SplitTopLevel splits a document mapping into one document per top-level key,
// carrying each key's full subtree and comments. Used by the migrator to turn the
// monolithic values.yaml into per-section files.
func SplitTopLevel(root *yaml.Node) map[string]*yaml.Node {
	m := mappingRoot(root)
	out := map[string]*yaml.Node{}
	for i := 0; i+1 < len(m.Content); i += 2 {
		k := m.Content[i]
		v := m.Content[i+1]
		out[k.Value] = &yaml.Node{
			Kind: yaml.DocumentNode,
			Content: []*yaml.Node{{
				Kind:    yaml.MappingNode,
				Content: []*yaml.Node{k, v},
			}},
		}
	}
	return out
}

// emptyDoc returns a new empty document node with an empty root mapping.
func emptyDoc() *yaml.Node {
	return &yaml.Node{
		Kind:    yaml.DocumentNode,
		Content: []*yaml.Node{{Kind: yaml.MappingNode}},
	}
}

// TopLevelKeys returns the ordered top-level keys of a document mapping.
func TopLevelKeys(root *yaml.Node) []string {
	m := mappingRoot(root)
	var keys []string
	for i := 0; i+1 < len(m.Content); i += 2 {
		keys = append(keys, m.Content[i].Value)
	}
	return keys
}
