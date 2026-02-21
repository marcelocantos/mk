package mk

import "embed"

//go:embed std/*.mk
var stdlibFS embed.FS
