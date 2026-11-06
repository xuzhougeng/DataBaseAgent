// Package examples embeds the design-demo files in the app binary, so every
// installed build can preview them without a repository checkout.
// catalog.json lists what the in-app Examples page shows; en/ holds English
// copies of the files that contain Chinese text.
package examples

import "embed"

//go:embed catalog.json static collect en
var FS embed.FS
