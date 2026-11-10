// Package web embeds the desktop UI served by the agent.
package web

import "embed"

// FS contains the desktop UI, the phone PWA and the static assets referenced
// by them.
//
//go:embed ui.html phone.html phone.css phone_app.js phone_worklet.js assets
var FS embed.FS
