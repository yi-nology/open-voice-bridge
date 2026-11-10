package ui

import (
	"context"
	"encoding/json"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

// Inputs lists active input sources (BLE remote plus connected phones) for the
// desktop dashboard. Management-plane only: the Security middleware already
// confines it to loopback + same-site.
func (s *Server) Inputs(ctx context.Context, c *app.RequestContext) {
	if s.deps.Inputs == nil {
		c.JSON(consts.StatusServiceUnavailable, map[string]any{"error": "inputs unavailable"})
		return
	}
	c.JSON(consts.StatusOK, s.deps.Inputs(ctx))
}

// PhonePairing mints or refreshes a short-lived pairing code for a phone.
func (s *Server) PhonePairing(ctx context.Context, c *app.RequestContext) {
	if s.deps.PhonePairing == nil {
		c.JSON(consts.StatusServiceUnavailable, map[string]any{"error": "phone pairing unavailable"})
		return
	}
	c.JSON(consts.StatusOK, s.deps.PhonePairing(ctx))
}

// PhoneDevices returns paired devices plus their current connection state.
func (s *Server) PhoneDevices(ctx context.Context, c *app.RequestContext) {
	if s.deps.PhoneDevices == nil {
		c.JSON(consts.StatusServiceUnavailable, map[string]any{"error": "phone devices unavailable"})
		return
	}
	c.JSON(consts.StatusOK, s.deps.PhoneDevices(ctx))
}

// PhoneRevoke removes a paired device and any live session for it.
func (s *Server) PhoneRevoke(ctx context.Context, c *app.RequestContext) {
	if s.deps.PhoneRevoke == nil {
		c.JSON(consts.StatusServiceUnavailable, map[string]any{"error": "phone revocation unavailable"})
		return
	}
	var req struct {
		DeviceID string `json:"device_id"`
	}
	if err := json.Unmarshal(c.Request.Body(), &req); err != nil || req.DeviceID == "" {
		c.JSON(consts.StatusBadRequest, map[string]any{"ok": false, "error": "device_id required"})
		return
	}
	c.JSON(consts.StatusOK, s.deps.PhoneRevoke(ctx, req.DeviceID))
}
