package runtime

// Live status for the permissions page (design-qa P2 授权状态展示): each
// permission/service row gets its real state instead of instructions only.
// Probes are on-demand and cheap; the classification is pure so it can be
// tested without a host to poke (permissions_test.go).

import (
	"context"
	"os/exec"
	"runtime"
	"strings"

	"github.com/yi-nology/open-voice-bridge/biz/service"
	"github.com/yi-nology/open-voice-bridge/internal/core"
)

// probe seams — swapped in tests.
var (
	runProbe = func(ctx context.Context, name string, args ...string) (string, error) {
		out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}
)

type permProbes struct {
	platform  string
	addr      string
	helperOK  bool
	helperErr string
	active    string // user unit: active | inactive | unknown
	enabled   string // user unit: enabled | disabled | unknown
	access    string // darwin assistive access: granted | denied | unknown
}

// BuildPermissions measures the host's grants for the permissions page.
func BuildPermissions(ctx context.Context, in service.ViewInputs) core.PermissionsView {
	p := permProbes{platform: runtime.GOOS, addr: in.Cfg.UIAddr, active: "unknown", enabled: "unknown", access: "unknown"}
	if _, err := in.Helper.Call(ctx, map[string]string{"cmd": "status"}); err != nil {
		p.helperErr = err.Error()
	} else {
		p.helperOK = true
	}
	switch p.platform {
	case "linux":
		p.active = unitState(runProbe(ctx, "systemctl", "--user", "is-active", "xiaoyao"))
		p.enabled = unitState(runProbe(ctx, "systemctl", "--user", "is-enabled", "xiaoyao"))
	case "darwin":
		p.access = accessState(runProbe(ctx, "osascript", "-e",
			`tell application "System Events" to return name of first process`))
	}
	return permissionsFrom(p)
}

// unitState maps systemctl's opinion onto the three honest states. systemctl
// prints "active"/"inactive"/"failed" or "enabled"/"disabled"/"static" and
// exits non-zero for anything it finds unlovely — treat those as unknown.
func unitState(out string, err error) string {
	if err != nil {
		return "unknown"
	}
	switch out {
	case "active", "enabled":
		return out
	case "inactive", "failed", "disabled":
		return "inactive"
	default:
		return "unknown"
	}
}

// accessState interprets the System Events probe: reading a process name
// needs assistive access on macOS, and the denial is a distinct, greppable
// error. Anything else (no osascript, TCC oddities) stays unknown.
func accessState(out string, err error) string {
	if err == nil {
		return "granted"
	}
	low := strings.ToLower(out)
	if strings.Contains(low, "assistive") || strings.Contains(low, "not allowed") {
		return "denied"
	}
	return "unknown"
}

func permissionsFrom(p permProbes) core.PermissionsView {
	items := make([]core.PermissionItem, 0, 5)
	switch p.platform {
	case "linux":
		items = append(items,
			linuxHelperItem(p),
			core.PermissionItem{
				ID:     "agent",
				Title:  "听写服务",
				Status: core.PermOK,
				Detail: "本服务正在运行，监听 " + p.addr,
			},
			linuxAutostartItem(p),
		)
	default:
		items = append(items,
			core.PermissionItem{
				ID:     "bt",
				Title:  "蓝牙",
				Status: core.PermUnknown,
				Detail: "无法从本进程探测，请在系统设置 → 隐私与安全性 → 蓝牙确认",
			},
			macAccessItem(p),
			core.PermissionItem{
				ID:     "keymap",
				Title:  "按键映射不适用",
				Status: core.PermNA,
				Detail: "macOS 按键走遥控器原生 HID，不注入",
			},
			core.PermissionItem{
				ID:     "mic",
				Title:  "虚拟麦克风不适用",
				Status: core.PermNA,
				Detail: "ASR 直采遥控器音频，无需虚拟麦",
			},
			core.PermissionItem{
				ID:     "autostart",
				Title:  "开机自启",
				Status: core.PermUnknown,
				Detail: "launchd 配置不在探测范围；可运行 xiaoyao doctor 复核",
			},
		)
	}
	return core.PermissionsView{OK: true, Platform: p.platform, Items: items}
}

func linuxHelperItem(p permProbes) core.PermissionItem {
	item := core.PermissionItem{ID: "helper", Title: "按键映射助手"}
	if p.helperOK {
		item.Status = core.PermOK
		item.Detail = "root helper 就绪，自定义键位生效"
		return item
	}
	item.Status = core.PermTodo
	item.Detail = "helper 未安装或未运行：" + p.helperErr
	item.Fix = "sudo make install-remap"
	return item
}

func linuxAutostartItem(p permProbes) core.PermissionItem {
	item := core.PermissionItem{ID: "autostart", Title: "开机自启"}
	switch p.enabled {
	case "enabled":
		item.Status = core.PermOK
		item.Detail = "xiaoyao.service 已设为开机自启"
	case "inactive":
		item.Status = core.PermTodo
		item.Detail = "未设开机自启"
		item.Fix = "sudo make install-user"
	default:
		item.Status = core.PermUnknown
		item.Detail = "systemctl 不可用或单元未安装；终端直跑也可用"
	}
	return item
}

func macAccessItem(p permProbes) core.PermissionItem {
	item := core.PermissionItem{ID: "access", Title: "辅助功能"}
	switch p.access {
	case "granted":
		item.Status = core.PermOK
		item.Detail = "已授权，识别结果可自动上屏"
	case "denied":
		item.Status = core.PermTodo
		item.Detail = "未授权——字幕与历史照常，只是不自动粘贴"
		item.Fix = "系统设置 → 隐私与安全性 → 辅助功能"
	default:
		item.Status = core.PermUnknown
		item.Detail = "无法探测，请在系统设置 → 隐私与安全性 → 辅助功能确认"
	}
	return item
}
