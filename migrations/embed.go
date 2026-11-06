// Package migrations embeds the initial schema for each supported database.
package migrations

import "embed"

//go:embed sqlite/*.sql postgres/*.sql
var Files embed.FS
