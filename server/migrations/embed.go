// Package migrations holds the SQL schema history, embedded into the binary so
// a released image can migrate itself without carrying loose files.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
