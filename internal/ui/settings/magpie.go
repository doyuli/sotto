package settings

import (
	"context"
	"strconv"

	"github.com/egoist/mygo/ui"

	"sotto/internal/config"
	"sotto/internal/translate"
)

type magpieStatus struct {
	requestedPort int
	gateway       translate.MagpieGateway
	err           string
}

func (v *View) cancelMagpieLocked() {
	if v.magpieCancel != nil {
		v.magpieCancel()
		v.magpieCancel = nil
	}
}

func (v *View) RefreshMagpieAsync() {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.refreshMagpieLocked()
}

func (v *View) refreshMagpieLocked() {
	port := v.settings.Services.Magpie.Port
	if v.disposed || v.activeTab != TabServices || v.actions.DiscoverMagpie == nil ||
		(v.magpieCancel != nil && v.magpie.requestedPort == port) {
		return
	}
	v.cancelMagpieLocked()
	if v.magpie.requestedPort != port {
		v.magpie = magpieStatus{requestedPort: port}
	}
	ctx, cancel := context.WithCancel(context.Background())
	v.magpieCancel = cancel

	go func() {
		defer cancel()
		gateway, err := v.actions.DiscoverMagpie(ctx, port)
		v.mu.Lock()
		if ctx.Err() != nil || v.disposed {
			v.mu.Unlock()
			return
		}
		v.magpieCancel = nil
		v.magpie = magpieStatus{requestedPort: port, gateway: gateway, err: translate.UserMessage(err)}
		v.mu.Unlock()
		v.invalidate()
	}()
}

func (v *View) magpieReadyLocked() bool {
	ready, _ := v.magpieReadinessLocked()
	return ready
}

func (v *View) magpieReadinessLocked() (bool, string) {
	options := v.settings.Services.Magpie
	switch {
	case v.magpie.requestedPort != options.Port:
		return false, "正在检测本机网关…"
	case v.magpieCancel != nil && v.magpie.gateway.Port == 0:
		return false, "正在检测本机网关…"
	case v.magpie.err != "":
		return false, v.magpie.err
	case v.magpie.gateway.Port == 0:
		return false, "请先启动 Magpie"
	default:
		_, err := v.magpie.gateway.SelectModel(options.Model)
		if err != nil {
			return false, err.Error()
		}
		return true, "已检测到网关"
	}
}

func (v *View) renderMagpieOptions(c *ui.Context, p settingsPalette, saved config.MagpieOptions, draft serviceDraft, status magpieStatus, locked bool) {
	port := magpiePortText(saved.Port)
	if draft.port != nil {
		port = *draft.port
	}
	placeholder := "自动发现"
	if status.gateway.Port != 0 && status.requestedPort == saved.Port {
		placeholder += " · " + strconv.Itoa(status.gateway.Port)
	}
	v.renderServiceField(c.Key("port"), p, "magpie", serviceEditorField{
		label: "端口", placeholder: placeholder, value: port,
	}, locked, v.editMagpiePort)

	model := saved.Model
	if draft.model != nil {
		model = *draft.model
	}
	models := status.gateway.Models
	if status.requestedPort != saved.Port {
		models = nil
	}
	if len(models) == 1 {
		models = append([]string{""}, models...)
	}
	ui.Row(c.Key("model")).FillWidth().AlignItems(ui.Center).Gap(12).Children(func() {
		ui.Text(c, "模型").Width(136).FontSize(13).FontWeight(500).TextColor(p.fg)
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			renderGeneralSelect(c.Key("picker"), p, model, models, func(id string) string {
				if id != "" {
					return id
				}
				if len(models) == 2 && models[0] == "" {
					return "自动 · " + models[1]
				}
				return "选择模型"
			}, "选择 Magpie 模型", "Magpie 模型选项", locked || draft.port != nil || len(models) == 0, func(id string) {
				v.editServiceModel("magpie", id)
			})
		})
		v.renderSecondaryButtonDisabled(c.Key("refresh"), p, "刷新", "刷新 Magpie 模型", locked || draft.port != nil, v.RefreshMagpieAsync)
	})
}

func (v *View) editMagpiePort(value string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.canEditServiceLocked("magpie") {
		return
	}
	draft := v.serviceDrafts["magpie"]
	draft.port = changedOption(value, magpiePortText(v.settings.Services.Magpie.Port))
	v.updateServiceDraftLocked("magpie", draft)
}

func magpiePortText(port int) string {
	if port == 0 {
		return ""
	}
	return strconv.Itoa(port)
}
