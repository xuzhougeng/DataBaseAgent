package frontend

import "embed"

//go:embed index.html styles.css workbench.js
var Assets embed.FS
