// Package migrations embeds the SQL schema so the server can apply it on
// startup without requiring a separate migration tool or manual `psql` step.
package migrations

import _ "embed"

//go:embed 0001_init.sql
var initSQL string

//go:embed 0002_auth.sql
var authSQL string

// All returns every migration file's SQL, in application order. Each
// statement is written to be safely re-run on every startup (CREATE TABLE
// IF NOT EXISTS, ADD COLUMN IF NOT EXISTS, ...) rather than tracked with a
// separate migration-versioning table.
func All() []string {
	return []string{initSQL, authSQL}
}
