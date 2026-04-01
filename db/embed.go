// Package db provides the embedded migration files for the Nexus database.
package db

import "embed"

//go:embed migrations/mysql/*.sql
var MigrationsFS embed.FS
