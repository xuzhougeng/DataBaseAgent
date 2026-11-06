package frontend

import "embed"

//go:embed index.html styles.css i18n.js conversations.js mcp.js workbench.js
var Assets embed.FS
