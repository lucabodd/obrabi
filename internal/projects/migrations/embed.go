// Package migrations embeds the SQL migrations of the projects schema.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
