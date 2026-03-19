// Package skills embeds skill template files for distribution in the binary.
package skills

import "embed"

// Templates contains all *.md.tmpl skill template files.
//
//go:embed *.md.tmpl
var Templates embed.FS
