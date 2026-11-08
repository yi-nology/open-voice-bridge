package api

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/oekit/logx"
	mw "github.com/oekit/middleware"
	oeerrors "github.com/oekit/pkg/errors"

	api "github.com/yi-nology/open-voice-bridge/biz/model/api"
)

var log = logx.Delegating("api-handler")

// Backend is implemented by the local agent and binds generated handlers to
// the running service.
type Backend interface {
	Health(context.Context) (*api.HealthResp, error)
	Ready(context.Context) (*api.ReadyResp, error)
	Status(context.Context) (*api.StatusResp, error)
	Apply(context.Context, map[string]string) (*api.OperationResp, error)
	CaptureStart(context.Context) (*api.OperationResp, error)
	CaptureStop(context.Context) (*api.OperationResp, error)
	Reconnect(context.Context) (*api.OperationResp, error)
	RestartMic(context.Context) (*api.OperationResp, error)
	SwitchASR(context.Context, string) (*api.OperationResp, error)
	GetASRConfig(context.Context) (*api.ASRConfigResp, error)
	UpdateASRConfig(context.Context, *api.ASRConfig) (*api.OperationResp, error)
	TestASR(context.Context, string) (*api.ASRTestResp, error)
	GetLLMConfig(context.Context) (*api.LLMConfigResp, error)
	UpdateLLMConfig(context.Context, *api.LLMConfig) (*api.OperationResp, error)
	TestLLM(context.Context, string, bool) (*api.LLMTestResp, error)
	SaveLLMKey(context.Context, string) (*api.OperationResp, error)
	GetHistory(context.Context) (*api.HistoryResp, error)
	DeleteHistory(context.Context, string) (*api.OperationResp, error)
	ClearHistory(context.Context) (*api.OperationResp, error)
	GetTextConfig(context.Context) (*api.TextConfigResp, error)
	UpdateTextConfig(context.Context, *api.TextConfig) (*api.OperationResp, error)
	KeymapProfiles(context.Context) (*api.KeymapProfilesResp, error)
	ApplyKeymapProfile(context.Context, string) (*api.OperationResp, error)
	SaveKeymapProfile(context.Context, string, map[string]string) (*api.OperationResp, error)
	DeleteKeymapProfile(context.Context, string) (*api.OperationResp, error)
	GetDevices(context.Context) (*api.DevicesResp, error)
	DiscoverDevices(context.Context) (*api.DiscoveredResp, error)
	SelectDevice(context.Context, string) (*api.OperationResp, error)
	ToggleDevice(context.Context, string, bool) (*api.OperationResp, error)
	SaveDevice(context.Context, *api.DeviceSaveReq) (*api.OperationResp, error)
	DeleteDevice(context.Context, string) (*api.OperationResp, error)
	ConnectDevice(context.Context, string, bool) (*api.OperationResp, error)
}

var backend Backend

func SetBackend(value Backend) { backend = value }

func requireBackend(ctx context.Context, c *app.RequestContext) bool {
	if backend == nil {
		mw.Fail(ctx, c, oeerrors.NewBizErrorWithStatus(5002, consts.StatusServiceUnavailable, "service not ready"))
		return false
	}
	return true
}

func respond(ctx context.Context, c *app.RequestContext, resp any, err error) {
	if err != nil {
		log.CtxError(ctx, "api request failed", "err", err)
		mw.Fail(ctx, c, oeerrors.NewBizErrorWithStatus(5001, consts.StatusBadGateway, err.Error()))
		return
	}
	c.JSON(consts.StatusOK, resp)
}
