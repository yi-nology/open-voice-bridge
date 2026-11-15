package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/yi-nology/open-voice-bridge/biz/service"
	"github.com/yi-nology/open-voice-bridge/internal/core"
)

func TestUnitStateMapping(t *testing.T) {
	cases := []struct {
		out  string
		err  error
		want string
	}{
		{"active", nil, "active"},
		{"enabled", nil, "enabled"},
		{"inactive", nil, "inactive"},
		{"failed", nil, "inactive"},
		{"disabled", nil, "inactive"},
		{"static", nil, "unknown"},
		{"", errors.New("no systemctl"), "unknown"},
	}
	for _, c := range cases {
		if got := unitState(c.out, c.err); got != c.want {
			t.Errorf("unitState(%q, %v) = %q, want %q", c.out, c.err, got, c.want)
		}
	}
}

func TestAccessStateMapping(t *testing.T) {
	cases := []struct {
		out  string
		err  error
		want string
	}{
		{"Finder", nil, "granted"},
		{"osascript is not allowed assistive access. (-1743)", errors.New("exit 1"), "denied"},
		{"execution error: System Events got an error: not allowed", errors.New("exit 1"), "denied"},
		{"executable file not found", errors.New("exit 1"), "unknown"},
	}
	for _, c := range cases {
		if got := accessState(c.out, c.err); got != c.want {
			t.Errorf("accessState(%q, %v) = %q, want %q", c.out, c.err, got, c.want)
		}
	}
}

func itemBy(t *testing.T, view core.PermissionsView, id string) core.PermissionItem {
	t.Helper()
	for _, item := range view.Items {
		if item.ID == id {
			return item
		}
	}
	t.Fatalf("no item %q in %+v", id, view.Items)
	return core.PermissionItem{}
}

func TestLinuxPermissionsClassifyHelperAndAutostart(t *testing.T) {
	view := permissionsFrom(permProbes{
		platform: "linux", addr: "127.0.0.1:8787",
		helperOK: true, active: "active", enabled: "enabled",
	})
	if !view.OK || view.Platform != "linux" || len(view.Items) != 3 {
		t.Fatalf("view = %+v", view)
	}
	if got := itemBy(t, view, "helper"); got.Status != core.PermOK {
		t.Errorf("helper = %+v, want ok", got)
	}
	if got := itemBy(t, view, "agent"); got.Detail != "本服务正在运行，监听 127.0.0.1:8787" {
		t.Errorf("agent = %+v", got)
	}
	if got := itemBy(t, view, "autostart"); got.Status != core.PermOK {
		t.Errorf("autostart = %+v, want ok", got)
	}

	todo := permissionsFrom(permProbes{
		platform:  "linux",
		helperErr: "dial unix: no such file", active: "unknown", enabled: "inactive",
	})
	if got := itemBy(t, todo, "helper"); got.Status != core.PermTodo || got.Fix != "sudo make install-remap" {
		t.Errorf("helper todo = %+v", got)
	}
	if got := itemBy(t, todo, "autostart"); got.Status != core.PermTodo || got.Fix == "" {
		t.Errorf("autostart todo = %+v", got)
	}
}

func TestDarwinPermissionsMarkWhatCannotApply(t *testing.T) {
	unknown := permissionsFrom(permProbes{platform: "darwin", access: "unknown"})
	if len(unknown.Items) != 5 {
		t.Fatalf("items = %d, want 5", len(unknown.Items))
	}
	if got := itemBy(t, unknown, "keymap"); got.Status != core.PermNA {
		t.Errorf("keymap = %+v, want na", got)
	}
	if got := itemBy(t, unknown, "access"); got.Status != core.PermUnknown {
		t.Errorf("access = %+v, want unknown", got)
	}

	denied := permissionsFrom(permProbes{platform: "darwin", access: "denied"})
	if got := itemBy(t, denied, "access"); got.Status != core.PermTodo || got.Fix == "" {
		t.Errorf("access denied = %+v", got)
	}
	granted := permissionsFrom(permProbes{platform: "darwin", access: "granted"})
	if got := itemBy(t, granted, "access"); got.Status != core.PermOK {
		t.Errorf("access granted = %+v", got)
	}
}

type permHelper struct{ err error }

func (h permHelper) Call(context.Context, any) (map[string]any, error) { return nil, h.err }

func TestBuildPermissionsAlwaysReturnsABoard(t *testing.T) {
	view := BuildPermissions(context.Background(), service.ViewInputs{
		Helper: permHelper{err: errors.New("helper down")},
	})
	if !view.OK || len(view.Items) == 0 {
		t.Fatalf("view = %+v", view)
	}
}
