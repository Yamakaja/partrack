// Package db exports embedded database migration files for use by the
// golang-migrate runner in internal/db.
package db

import "embed"

//go:embed migrations
var Migrations embed.FS
