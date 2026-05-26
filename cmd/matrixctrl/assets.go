package main

import "embed"

// webDist contains the compiled React frontend (built from web/).
// The Makefile copies web/dist → cmd/matrixctrl/dist before go build.
//
//go:embed all:dist
var webDist embed.FS
