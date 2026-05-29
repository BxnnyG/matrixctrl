package config

import (
	"fmt"

	"github.com/xeipuuv/gojsonschema"
	"gopkg.in/yaml.v3"
)

// ValidationError holds a single schema violation.
type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// ParseYAML parses YAML content for syntax errors only.
func ParseYAML(content string) error {
	var doc interface{}
	return yaml.Unmarshal([]byte(content), &doc)
}

// ValidateYAMLWithSchema validates YAML content against a JSON Schema passed as bytes.
func ValidateYAMLWithSchema(content string, schemaJSON []byte) ([]ValidationError, error) {
	var doc interface{}
	if err := yaml.Unmarshal([]byte(content), &doc); err != nil {
		return []ValidationError{{Field: "(root)", Message: "invalid YAML: " + err.Error()}}, nil
	}
	doc = normalizeForJSON(doc)

	schemaLoader := gojsonschema.NewBytesLoader(schemaJSON)
	docLoader := gojsonschema.NewGoLoader(doc)

	result, err := gojsonschema.Validate(schemaLoader, docLoader)
	if err != nil {
		// Schema itself is broken — don't block the user, just warn
		return nil, fmt.Errorf("schema validation internal error: %w", err)
	}

	var errs []ValidationError
	for _, e := range result.Errors() {
		errs = append(errs, ValidationError{
			Field:   e.Field(),
			Message: e.Description(),
		})
	}
	return errs, nil
}

// ValidateYAML validates YAML content against a JSON schema file.
// Returns an empty slice if valid. schemaPath is a file:// URI or absolute path.
func ValidateYAML(content, schemaPath string) ([]ValidationError, error) {
	// Parse YAML → Go value (JSON-compatible).
	var doc interface{}
	if err := yaml.Unmarshal([]byte(content), &doc); err != nil {
		return nil, fmt.Errorf("yaml parse: %w", err)
	}
	doc = normalizeForJSON(doc)

	schemaLoader := gojsonschema.NewReferenceLoader("file://" + schemaPath)
	docLoader := gojsonschema.NewGoLoader(doc)

	result, err := gojsonschema.Validate(schemaLoader, docLoader)
	if err != nil {
		return nil, fmt.Errorf("schema load: %w", err)
	}

	var errs []ValidationError
	for _, e := range result.Errors() {
		errs = append(errs, ValidationError{
			Field:   e.Field(),
			Message: e.Description(),
		})
	}
	return errs, nil
}

// normalizeForJSON converts map[interface{}]interface{} (yaml.v3 artefact) to
// map[string]interface{} recursively so gojsonschema can process it.
func normalizeForJSON(v interface{}) interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(val))
		for k, v2 := range val {
			out[k] = normalizeForJSON(v2)
		}
		return out
	case map[interface{}]interface{}:
		out := make(map[string]interface{}, len(val))
		for k, v2 := range val {
			out[fmt.Sprintf("%v", k)] = normalizeForJSON(v2)
		}
		return out
	case []interface{}:
		for i, item := range val {
			val[i] = normalizeForJSON(item)
		}
		return val
	default:
		return v
	}
}
