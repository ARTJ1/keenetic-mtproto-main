package web

import "embed"

//go:embed all:dist
var uiFS embed.FS
