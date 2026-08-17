package main

import (
	"embed"
	"io/fs"
)

// webDist contains the compiled React frontend (built from web/).
// The Makefile copies web/dist → cmd/matrixctrl/dist before go build.
//
// Only `dist/.gitkeep` is tracked; the built assets are generated and gitignored
// (etappe 50). They used to be committed, and the committed copy was found sixteen
// days and some fifteen etappes stale — a plain `go build` produced a binary serving
// a UI with no moderation screen at all, and said nothing. An artefact that can
// silently disagree with its source is worse than the noisy diffs P2-2 was opened
// about.
//
// .gitkeep exists because `//go:embed all:dist` is a *build error* on an empty or
// missing directory, which would break `go test ./...` for anyone who has not built
// the frontend.
//
//go:embed all:dist
var webDist embed.FS

// frontendBuilt reports whether a real frontend was embedded, as opposed to the
// .gitkeep placeholder that keeps the package compiling.
//
// This is the difference between "built wrong" and "not built", and the binary is
// expected to say which — a missing UI that answers 404 reads like a routing bug and
// sends the reader to the wrong file.
//
// Takes the FS rather than reading the package-level webDist so it describes the
// thing it was handed, and so it can be tested without a build that has no frontend.
func frontendBuilt(f fs.FS) bool {
	_, err := fs.Stat(f, "dist/index.html")
	return err == nil
}
