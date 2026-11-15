package phone

// The boundary checks this surface needs arrive from the edge at the
// composition root (architecture §4: the consumer declares the seam;
// edge/netguard implements it). This package must not import the edge —
// R2 is enforced mechanically and the fail-closed defaults make a missing
// wire-up reject rather than admit.

// SameOrigin reports whether a request may change state. Default: reject.
var SameOrigin = func(method, host, origin, site string) bool { return false }

// WriteSecHeaders applies this surface's security policy through a generic
// setter (the two surfaces differ on microphone CSP; see edge/netguard).
// Default: write nothing.
var WriteSecHeaders = func(set func(key, value string)) {}
