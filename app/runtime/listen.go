package runtime

import (
	"net"

	"github.com/oekit/logx"

	"github.com/yi-nology/open-voice-bridge/internal/phone"
)

// listenPhone binds before it announces. A port conflict used to be reported
// as a running listener and only surfaced later as "phone server stopped";
// now it turns phone input off at startup, so the panel does not offer a
// pairing link to a port nobody is listening on (OPS-08).
func listenPhone(logger logx.Logger, srv *phone.Server, tlsMode string) (net.Listener, bool) {
	listener, err := srv.Listen()
	if err != nil {
		logger.Error("phone input listener unavailable; disabling phone input", "addr", srv.Addr(), "tls", tlsMode, "err", err)
		return nil, false
	}
	logger.Info("phone input listener started", "addr", listener.Addr().String(), "tls", tlsMode)
	return listener, true
}
