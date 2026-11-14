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

// PhoneEnabled turns the phone input source on or off in the saved
// configuration (OPS-01). The listener starts with the service, so the
// response carries restart_required — the same contract as a config import.
func (s *Server) PhoneEnabled(ctx context.Context, c *app.RequestContext) {
	if s.deps.SetPhoneEnabled == nil {
		c.JSON(consts.StatusServiceUnavailable, map[string]any{"ok": false, "error": "phone toggle unavailable"})
		return
	}
	var req struct {
		Enabled *bool `json:"enabled"`
	}
	if err := json.Unmarshal(c.Request.Body(), &req); err != nil || req.Enabled == nil {
		c.JSON(consts.StatusBadRequest, map[string]any{"ok": false, "error": "enabled required"})
		return
	}
	if err := s.deps.SetPhoneEnabled(*req.Enabled); err != nil {
		c.JSON(consts.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	c.JSON(consts.StatusOK, map[string]any{"ok": true, "restart_required": true})
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

// PhonePowerKey reports the dangerous-key opt-in (OPS-15).
func (s *Server) PhonePowerKey(ctx context.Context, c *app.RequestContext) {
	if s.deps.PhonePowerKey == nil {
		c.JSON(consts.StatusServiceUnavailable, map[string]any{"ok": false, "error": "power toggle unavailable"})
		return
	}
	c.JSON(consts.StatusOK, map[string]any{"ok": true, "allow": s.deps.PhonePowerKey()})
}

// PhonePowerKeySave stores the dangerous-key opt-in and asks for a restart
// like every other phone.* setting (ARC-12).
func (s *Server) PhonePowerKeySave(ctx context.Context, c *app.RequestContext) {
	if s.deps.SetPhonePowerKey == nil {
		c.JSON(consts.StatusServiceUnavailable, map[string]any{"ok": false, "error": "power toggle unavailable"})
		return
	}
	var req struct {
		Allow bool `json:"allow"`
	}
	if err := json.Unmarshal(c.Request.Body(), &req); err != nil {
		c.JSON(consts.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if err := s.deps.SetPhonePowerKey(req.Allow); err != nil {
		c.JSON(consts.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	c.JSON(consts.StatusOK, map[string]any{"ok": true, "restart_required": true})
}
