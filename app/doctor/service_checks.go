package doctor

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/yi-nology/open-voice-bridge/internal/config"
	"github.com/yi-nology/open-voice-bridge/internal/core"
)

// configChecks covers the phone listener and the two TCP surfaces (OPS-04):
// the admin port, the phone port, and — when the phone input is on — the
// TLS material it needs. All platform-neutral.
func configChecks(cfg config.Config, configPath string) []check {
	var checks []check
	checks = append(checks, portCheck("ui", cfg.UIAddr)...)
	checks = append(checks, portCheck("phone", cfg.Phone.Addr)...)
	if !cfg.Phone.Enabled {
		return append(checks, ok("phone input disabled"))
	}
	checks = append(checks, ok(fmt.Sprintf("phone listener %s (tls=%s)", cfg.Phone.Addr, cfg.Phone.TLS)))
	dir := filepath.Dir(configPath)
	if dir == "" {
		return checks
	}
	info, err := os.Stat(dir)
	if err != nil {
		return append(checks, gap(core.DepPhone, "phone cert dir missing: "+dir,
			"mkdir -p "+dir))
	}
	if !info.IsDir() {
		return append(checks, gap(core.DepPhone, "phone cert dir is not a directory: "+dir, ""))
	}
	checks = append(checks, ok("phone cert dir writable: "+dir))
	return checks
}

// portCheck reports whether the configured port is free, serving /healthz
// (our service running), or taken by something else.
func portCheck(label, addr string) []check {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return []check{warn(label+" addr "+addr+" is not host:port", "set it to e.g. 127.0.0.1:8787")}
	}
	target := net.JoinHostPort("127.0.0.1", port)
	conn, err := net.DialTimeout("tcp", target, 300*time.Millisecond)
	if err != nil {
		return []check{ok(label + " port " + port + " free")}
	}
	_ = conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+target+"/healthz", nil)
	resp, err := http.DefaultClient.Do(req)
	if err == nil {
		_ = resp.Body.Close()
		return []check{ok(label + " port " + port + " serving")}
	}
	return []check{warn(label+" port "+port+" is taken by another process",
		"lsof -i :"+port)}
}
