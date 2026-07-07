package docs

import (
	"embed"
)

//go:embed *.md *.yaml examples specs
var FS embed.FS
