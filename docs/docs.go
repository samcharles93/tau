package docs

import (
	"embed"
)

//go:embed *.md *.yaml
var FS embed.FS
