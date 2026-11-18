// Package matrixctrl only carries the embedded CHANGELOG.md (go:embed cannot reach
// upwards, so the file is embedded from the module root).
package matrixctrl

import _ "embed"

// Changelog is CHANGELOG.md as shipped in this build.
//
//go:embed CHANGELOG.md
var Changelog string
