// Package supabase embeds this project's Supabase CLI-managed migrations
// (internal/supabase/migrations/*.sql) so the Oracle can apply them on
// startup without a separate migration tool or manual `psql` step.
//
// The glob pattern picks up every .sql file in the directory, so a new
// migration added later via `supabase migration new <name>` is embedded
// automatically on the next build — nothing here needs to change.
package supabase

import (
	"embed"
	"sort"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrations returns every migration file's SQL, ordered by filename —
// which is also chronological order, given the Supabase CLI's
// <timestamp>_<name>.sql naming convention. Each statement is written to
// be safely re-run on every startup (CREATE TABLE IF NOT EXISTS, ADD
// COLUMN IF NOT EXISTS, ...) rather than tracked with a separate
// migration-versioning table.
func Migrations() ([]string, error) {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return nil, err
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	out := make([]string, 0, len(names))
	for _, name := range names {
		data, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return nil, err
		}
		out = append(out, string(data))
	}
	return out, nil
}
