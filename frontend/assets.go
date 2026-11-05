package frontend

import "embed"

//go:embed index.html styles.css desktop.css app.js workbench.js
var Assets embed.FS
