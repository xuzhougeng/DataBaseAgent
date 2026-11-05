package frontend

import "embed"

//go:embed index.html styles.css i18n.js workbench.js
var Assets embed.FS
