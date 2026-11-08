// Package web embeds the desktop UI served by the agent.
package web

import "embed"

// FS contains ui.html and the static assets referenced by it.
//
//go:embed ui.html assets
var FS embed.FS
