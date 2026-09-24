// Package migrations embeds the SQL migrations of the feedback schema.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
