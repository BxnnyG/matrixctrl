package config

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// Merge deep-merges a slice of YAML strings in order (later entries win).
// Returns the merged result as a YAML string.
func Merge(contents []string) (string, error) {
	m, err := MergeToMap(contents)
	if err != nil {
		return "", err
	}
	out, err := yaml.Marshal(m)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// MergeToMap deep-merges a slice of YAML strings and returns the result as a
// map[string]interface{} suitable for passing to helm.sh/helm/v3 upgrade actions.
func MergeToMap(contents []string) (map[string]interface{}, error) {
	merged := make(map[string]interface{})
	for _, c := range contents {
		if c == "" {
			continue
		}
		var m map[string]interface{}
		if err := yaml.Unmarshal([]byte(c), &m); err != nil {
			return nil, fmt.Errorf("yaml parse: %w", err)
		}
		deepMerge(merged, m)
	}
	return merged, nil
}

// deepMerge merges src into dst recursively. Values in src overwrite dst.
func deepMerge(dst, src map[string]interface{}) {
	for k, sv := range src {
		if dv, ok := dst[k]; ok {
			dMap, dIsMap := dv.(map[string]interface{})
			sMap, sIsMap := sv.(map[string]interface{})
			if dIsMap && sIsMap {
				deepMerge(dMap, sMap)
				continue
			}
		}
		dst[k] = sv
	}
}
