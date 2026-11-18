package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/sebdanielsson/piecemeal/internal/engine"
	"github.com/sebdanielsson/piecemeal/internal/logbuf"
	"github.com/sebdanielsson/piecemeal/internal/natpmp"
	"github.com/sebdanielsson/piecemeal/internal/settings"
)

// SystemInfo is static information about the running instance.
type SystemInfo struct {
	DataDir      string   `json:"dataDir"`
	DownloadDir  string   `json:"downloadDir"`
	WatchDir     string   `json:"watchDir,omitempty"`
	AllowedRoots []string `json:"allowedRoots"`
}

// System is GET /system.
type System struct {
	SystemInfo
	Version     string          `json:"version"`
	StartedAt   time.Time       `json:"startedAt"`
	Port        int             `json:"port" doc:"The port peers connect to."`
	ListenAddrs []string        `json:"listenAddrs"`
	NATPMP      *natpmp.Mapping `json:"natpmp,omitempty" doc:"Present when NAT-PMP port forwarding is configured."`
}

type systemOutput struct{ Body System }

type transferOutput struct{ Body engine.TransferStatus }

// SettingsResponse is the current settings and which are pinned.
type SettingsResponse struct {
	Settings settings.Settings `json:"settings"`
	Locked   map[string]string `json:"locked" doc:"Settings set by environment variables, which can't be changed here, mapped to the variable."`
}

type settingsOutput struct{ Body SettingsResponse }

// SettingsPatch is a partial settings object; absent settings are left as
// they are.
type SettingsPatch settings.Settings

// Schema makes every property optional.
func (SettingsPatch) Schema(r huma.Registry) *huma.Schema {
	s := *r.Schema(reflectTypeOf[settings.Settings](), false, "")
	s.Required = nil
	s.Title = "SettingsPatch"
	return &s
}

type patchSettingsInput struct {
	Body    SettingsPatch
	RawBody []byte
}

// TransferLimits changes the global speed limits. Absent fields are left
// as they are; limits are bytes per second, 0 for unlimited.
type TransferLimits struct {
	AltSpeedEnabled  *bool  `json:"altSpeedEnabled,omitempty"`
	DownloadLimit    *int64 `json:"downloadLimit,omitempty" minimum:"0"`
	UploadLimit      *int64 `json:"uploadLimit,omitempty" minimum:"0"`
	AltDownloadLimit *int64 `json:"altDownloadLimit,omitempty" minimum:"0"`
	AltUploadLimit   *int64 `json:"altUploadLimit,omitempty" minimum:"0"`
}

type transferLimitsInput struct{ Body TransferLimits }

type logInput struct {
	After int64  `query:"after" doc:"Only entries with a higher id."`
	Level string `query:"level" enum:"INFO,WARN,ERROR" default:"INFO"`
}

type logOutput struct {
	Body struct {
		Items []logbuf.LogEntry `json:"items"`
	}
}

func (a *API) registerSystem() {
	const tag = "System"
	huma.Register(a.huma, a.operation(http.MethodGet, "/system", "getSystem", "Get version and network status", tag, accessUser),
		func(context.Context, *struct{}) (*systemOutput, error) {
			s := System{SystemInfo: a.System, Version: a.Version, StartedAt: a.StartedAt, Port: a.Engine.Port(), ListenAddrs: []string{}}
			for _, addr := range a.Engine.ListenAddrs() {
				s.ListenAddrs = append(s.ListenAddrs, addr.Network()+"://"+addr.String())
			}
			if a.NATPMP != nil {
				st := a.NATPMP.Status()
				s.NATPMP = &st
			}
			return &systemOutput{Body: s}, nil
		})

	huma.Register(a.huma, a.operation(http.MethodGet, "/transfer", "getTransfer", "Get transfer rates and totals", tag, accessUser),
		func(context.Context, *struct{}) (*transferOutput, error) {
			return &transferOutput{Body: a.Engine.Transfer()}, nil
		})

	op := a.operation(http.MethodPatch, "/transfer", "updateTransfer", "Change the speed limits", tag, accessUser)
	op.Description = "Changes the global and alternative limits and the alternative speed toggle. " +
		"Unlike PATCH /settings, any signed-in user may do this."
	op.Errors = append(op.Errors, http.StatusUnprocessableEntity)
	huma.Register(a.huma, op, func(ctx context.Context, in *transferLimitsInput) (*transferOutput, error) {
		values := map[string]any{}
		b := in.Body
		if b.AltSpeedEnabled != nil {
			values["altSpeedEnabled"] = *b.AltSpeedEnabled
		}
		for name, v := range map[string]*int64{
			"downloadLimit": b.DownloadLimit, "uploadLimit": b.UploadLimit,
			"altDownloadLimit": b.AltDownloadLimit, "altUploadLimit": b.AltUploadLimit,
		} {
			if v != nil {
				values[name] = *v
			}
		}
		if len(values) > 0 {
			if _, err := a.Settings.Set(ctx, values); err != nil {
				return nil, apiError(err)
			}
			detail, _ := json.Marshal(values)
			a.Auth.Audit(ctx, actor(ctx), "transfer.update", string(detail))
		}
		return &transferOutput{Body: a.Engine.Transfer()}, nil
	})

	huma.Register(a.huma, a.operation(http.MethodGet, "/settings", "getSettings", "Get settings", tag, accessUser),
		func(context.Context, *struct{}) (*settingsOutput, error) {
			return &settingsOutput{Body: SettingsResponse{Settings: a.Settings.Get(), Locked: a.Settings.Locked()}}, nil
		})

	op = a.operation(http.MethodPatch, "/settings", "updateSettings", "Change settings", tag, accessAdmin)
	op.Errors = append(op.Errors, http.StatusUnprocessableEntity)
	huma.Register(a.huma, op, func(ctx context.Context, in *patchSettingsInput) (*settingsOutput, error) {
		s, err := a.Settings.Update(ctx, json.RawMessage(in.RawBody))
		if err != nil {
			return nil, apiError(err)
		}
		a.Auth.Audit(ctx, actor(ctx), "settings.update", string(in.RawBody))
		return &settingsOutput{Body: SettingsResponse{Settings: s, Locked: a.Settings.Locked()}}, nil
	})

	huma.Register(a.huma, a.operation(http.MethodGet, "/log", "getLog", "Get recent log entries", tag, accessAdmin),
		func(_ context.Context, in *logInput) (*logOutput, error) {
			var level slog.Level
			_ = level.UnmarshalText([]byte(in.Level))
			out := &logOutput{}
			out.Body.Items = a.Logs.Since(in.After, level)
			return out, nil
		})
}
