// Package migrations exposes immutable SQL migrations to the migration
// command. SQL remains the source of truth and is embedded into the binary.
package migrations

import "embed"

// Files contains all forward migrations.
//
//go:embed *.up.sql
var Files embed.FS
