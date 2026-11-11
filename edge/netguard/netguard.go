// Package netguard holds the guards both network boundaries share: one
// same-origin rule, one loopback rule, one header writer. The admin UI and the
// phone listener used to carry their own copies, so a hole fixed in one stayed
// open in the other (ARC-04, R4).
package netguard

import (
	"net"
	"net/url"
	"strings"
)

// Policy is the page-specific half of the security headers. The two boundaries
// deliberately differ: the admin page denies the microphone, the phone page
// must allow it for getUserMedia, and WebSockets need ws:/wss: spelled out
// because CSP treats them as separate schemes from the page origin.
type Policy struct {
	ContentSecurity string
	Permissions     string
}

var (
	Admin = Policy{
		ContentSecurity: "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self' 'unsafe-inline'; connect-src 'self'; object-src 'none'; base-uri 'none'",
		Permissions:     "camera=(), microphone=(), geolocation=()",
	}
	Phone = Policy{
		ContentSecurity: "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; " +
			"connect-src 'self' ws: wss:; media-src 'self'; worker-src 'self'; " +
			"frame-ancestors 'none'; base-uri 'none'; form-action 'self'",
		Permissions: "microphone=(self), camera=(), geolocation=(), display-capture=()",
	}
)

// WriteHeaders sets the guards that do not depend on the page. Cache-Control is
// left to the caller: the admin only forbids caching on /api/, the phone on
// everything.
func WriteHeaders(p Policy, set func(key, value string)) {
	set("X-Content-Type-Options", "nosniff")
	set("Referrer-Policy", "no-referrer")
	set("Content-Security-Policy", p.ContentSecurity)
	set("Permissions-Policy", p.Permissions)
}

// SameOrigin reports whether an unsafe request carries same-origin evidence.
// host is the request's own Host header, origin and site are the Origin and
// Sec-Fetch-Site headers.
//
// I9: an empty Origin is not evidence. Without it a bare request (curl, a
// foreign process) reaches POST endpoints whose worst case on the admin side is
// executing asr.command.binary, and on the phone side spending a pairing code.
func SameOrigin(method, host, origin, site string) bool {
	switch method {
	case "POST", "PUT", "PATCH", "DELETE":
	default:
		return true
	}
	if origin == "" {
		return site == "same-origin"
	}
	if site != "" && site != "same-origin" && site != "none" {
		return false
	}
	parsed, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return parsed.Host == host
}

// IsLoopback reports whether a Host header names this machine. The admin
// listener binds loopback only, so a request that arrived under any other name
// came through something that should not be trusted with it.
func IsLoopback(host string) bool {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	switch host {
	case "127.0.0.1", "localhost", "::1":
		return true
	default:
		return false
	}
}
