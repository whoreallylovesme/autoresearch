package migrations

import "embed"

// FS contains ordered database schema migrations.
//
//go:embed *.sql
var FS embed.FS
