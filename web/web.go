// Package web exports the embedded frontend static files.
// The dist/ directory is populated by the Vite build step and is git-ignored.
// A .gitkeep placeholder is committed so the directory exists for `go:embed`.
package web

import "embed"

//go:embed all:dist
var Static embed.FS
