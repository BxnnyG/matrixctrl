package config

import (
	"fmt"
	"sort"

	"gopkg.in/yaml.v3"
)

// generalKeys are top-level config keys that are global/cross-cutting rather than
// a single ESS component. They are bundled into general.yaml; every other
// top-level key gets its own <key>.yaml section file.
var generalKeys = map[string]bool{
	"serverName":        true,
	"labels":            true,
	"certManager":       true,
	"ingress":           true,
	"matrixTools":       true,
	"global":            true,
	"deploymentMarkers": true,
	"imagePullSecrets":  true,
}

// NamedSlice is a config slice (name + raw YAML) in merge order.
type NamedSlice struct {
	Name    string
	Content string
}

// BuildSectionFiles converts the legacy slices (values/hostnames/rtc/tls, in merge
// order) into per-section files. The first slice is treated as the comment-rich
// base; later slices merge their *values* in (last wins) while the base comments
// are preserved. Returns filename → YAML content.
//
// This is a pure function so it can be verified on a copy before touching the repo.
func BuildSectionFiles(slices []NamedSlice) (map[string]string, error) {
	// section top-level key → document node (carrying comments)
	docs := map[string]*sectionDoc{}

	for i, sl := range slices {
		if sl.Content == "" {
			continue
		}
		if i == 0 {
			// Comment-rich base: split by top-level key, keep nodes verbatim.
			node, err := ParseYAMLNode(sl.Content)
			if err != nil {
				return nil, fmt.Errorf("parse %s: %w", sl.Name, err)
			}
			for key, doc := range SplitTopLevel(node) {
				docs[key] = &sectionDoc{node: doc}
			}
			continue
		}
		// Override slice: merge values into the section docs, preserving comments.
		m := YAMLToMap(sl.Content)
		for _, pv := range flattenLeaves(m, nil) {
			top := pv.path[0]
			d, ok := docs[top]
			if !ok {
				d = &sectionDoc{node: emptyDoc()}
				docs[top] = d
			}
			if err := SetNodeValue(d.node, pv.path, pv.value); err != nil {
				return nil, fmt.Errorf("merge %s at %v: %w", sl.Name, pv.path, err)
			}
		}
	}

	// Partition into files: general keys → general.yaml, rest → own file.
	general := emptyDoc()
	files := map[string]string{}

	// Stable ordering for deterministic output.
	keys := make([]string, 0, len(docs))
	for k := range docs {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, key := range keys {
		if generalKeys[key] {
			// Append this key's (key,value) pair into the general mapping.
			src := mappingRoot(docs[key].node)
			gm := mappingRoot(general)
			gm.Content = append(gm.Content, src.Content...)
			continue
		}
		out, err := MarshalNode(docs[key].node)
		if err != nil {
			return nil, err
		}
		files[key+".yaml"] = out
	}

	if len(mappingRoot(general).Content) > 0 {
		out, err := MarshalNode(general)
		if err != nil {
			return nil, err
		}
		files["general.yaml"] = out
	}

	return files, nil
}

type sectionDoc struct{ node *yaml.Node }

// flattenLeaves walks a decoded map into (path, leaf-value) pairs. Slices and
// scalars are leaves; nested maps recurse.
type pathValue struct {
	path  []string
	value interface{}
}

func flattenLeaves(m map[string]interface{}, prefix []string) []pathValue {
	var out []pathValue
	for k, v := range m {
		p := append(append([]string{}, prefix...), k)
		if child, ok := v.(map[string]interface{}); ok && len(child) > 0 {
			out = append(out, flattenLeaves(child, p)...)
		} else {
			out = append(out, pathValue{path: p, value: v})
		}
	}
	return out
}
