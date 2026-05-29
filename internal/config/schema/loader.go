package schema

import (
	"embed"
	"fmt"
	"io/fs"
	"path"
	"strings"
)

//go:embed schemas
var schemasFS embed.FS

// Get returns the JSON Schema for the given ESS chart version.
// The version is matched against the schema file names (e.g. "26.5.1" → "26.5.x.json").
// Falls back to the latest available schema if no exact major.minor match is found.
func Get(version string) ([]byte, error) {
	// Derive the major.minor prefix to match a schema file, e.g. "26.5.1" → "26.5"
	majorMinor := majorMinorOf(version)

	entries, err := fs.ReadDir(schemasFS, "schemas")
	if err != nil {
		return nil, fmt.Errorf("read schema dir: %w", err)
	}

	var fallback string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		// Replace the trailing .x.json suffix to get the version prefix, e.g. "26.5.x.json" → "26.5"
		prefix := strings.TrimSuffix(name, ".x.json")
		prefix = strings.TrimSuffix(prefix, ".json")
		if prefix == majorMinor {
			return schemasFS.ReadFile(path.Join("schemas", name))
		}
		fallback = name
	}

	if fallback != "" {
		return schemasFS.ReadFile(path.Join("schemas", fallback))
	}
	return nil, fmt.Errorf("no schema found for ESS version %s", version)
}

// majorMinorOf extracts the "major.minor" part of a semver string, e.g. "26.5.1" → "26.5".
func majorMinorOf(v string) string {
	parts := strings.SplitN(v, ".", 3)
	if len(parts) >= 2 {
		return parts[0] + "." + parts[1]
	}
	return v
}
