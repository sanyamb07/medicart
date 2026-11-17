// Package migrations embeds the SQL migration files into the binary.
//
// The embed directive must live in the same directory as the files it embeds
// (go:embed cannot reach into a parent directory), which is why this one-file
// package sits alongside the .sql files rather than in internal/db.
package migrations

import "embed"

// FS holds every migration, applied by internal/db.
//
//go:embed *.sql
var FS embed.FS
