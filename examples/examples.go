// Package examples embeds the design-demo files in the app binary, so every
// installed build can preview them without a repository checkout.
// catalog.json lists what the in-app Examples page shows.
package examples

import "embed"

//go:embed catalog.json static collect
var FS embed.FS
