// Package web embeds the static frontend assets so the gateway ships as a
// single binary with no extra files to deploy.
package web

import "embed"

//go:embed index.html app.js style.css
var FS embed.FS
