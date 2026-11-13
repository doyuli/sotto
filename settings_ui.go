package main

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"sotto/internal/config"
	"sotto/internal/keys"
	"sotto/internal/languages"
	"sotto/internal/naming"
)

var (
	shieldSVG = ui.MustParseSVG([]byte(`<svg viewBox="0 0 24 24"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>`))
)

type settingsPalette struct {
	dark        bool
	panel       ui.Color
	sidebar     ui.Color
	card        ui.Color
	cardHead    ui.Color
	field       ui.Color
	control     ui.Color
	pill        ui.Color
	hover       ui.Color
	border      ui.Color
	fg          ui.Color
	muted       ui.Color
	badgeInk    ui.Color
	accent      ui.Color
	accentText  ui.Color
	accentSoft  ui.Color
	primaryBg   ui.Color
	onAccent    ui.Color
	success     ui.Color
	warning     ui.Color
	warningSoft ui.Color
	error       ui.Color
	selection   ui.Color
	theme       *ui.Theme
}

var lightSettingsPalette = func() settingsPalette {
	p := settingsPalette{
		dark:        false,
		panel:       ui.Hex("#ffffff"),
		sidebar:     ui.Hex("#fbfcfd"),
		card:        ui.Hex("#ffffff"),
		cardHead:    ui.Hex("#f8f9fb"),
		field:       ui.Hex("#f4f5f7"),
		control:     ui.Hex("#f1f1f1"),
		pill:        ui.Hex("#e7e7e7"),
		hover:       ui.Hex("#e9e9e9"),
		border:      ui.Hex("#e2e5ea"),
		fg:          ui.Hex("#202020"),
		muted:       ui.Hex("#6b6b6b"),
		badgeInk:    ui.Hex("#5c5c5c"),
		accent:      ui.Hex("#2f7fe8"),
		accentText:  ui.Hex("#1a5fae"),
		accentSoft:  ui.RGBA(47, 127, 232, 0.14),
		primaryBg:   ui.Hex("#1a5fae"),
		onAccent:    ui.Hex("#ffffff"),
		success:     ui.Hex("#15803d"),
		warning:     ui.Hex("#b45309"),
		warningSoft: ui.RGBA(217, 119, 6, 0.12),
		error:       ui.Hex("#c62828"),
		selection:   ui.RGBA(31, 111, 208, 0.18),
	}
	t := ui.LightTheme()
	t.Background = p.panel
	t.Surface = p.field
	t.SurfaceHover = p.hover
	t.SurfacePressed = p.pill
	t.Border = p.border
	t.Text = p.fg
	t.TextMuted = p.muted
	t.Accent = p.accent
	t.AccentText = p.onAccent
	t.Scrollbar = p.pill
	t.ScrollbarWidth = 6
	t.Danger = p.error
	p.theme = t
	return p
}()

var darkSettingsPalette = func() settingsPalette {
	p := settingsPalette{
		dark:        true,
		panel:       ui.Hex("#222325"),
		sidebar:     ui.Hex("#1a1b1d"),
		card:        ui.Hex("#26272a"),
		cardHead:    ui.Hex("#2b2c30"),
		field:       ui.Hex("#1e1f22"),
		control:     ui.Hex("#2c2d2f"),
		pill:        ui.Hex("#3a3b3d"),
		hover:       ui.Hex("#3a3b3d"),
		border:      ui.Hex("#2e3035"),
		fg:          ui.Hex("#f3f4f5"),
		muted:       ui.Hex("#a0a3a7"),
		badgeInk:    ui.Hex("#b4b7bb"),
		accent:      ui.Hex("#7db4ff"),
		accentText:  ui.Hex("#7db4ff"),
		accentSoft:  ui.RGBA(125, 180, 255, 0.18),
		primaryBg:   ui.Hex("#7db4ff"),
		onAccent:    ui.Hex("#0d1b2b"),
		success:     ui.Hex("#4ade80"),
		warning:     ui.Hex("#fbbf24"),
		warningSoft: ui.RGBA(251, 191, 36, 0.14),
		error:       ui.Hex("#ff8a8a"),
		selection:   ui.RGBA(125, 180, 255, 0.28),
	}
	t := ui.DarkTheme()
	t.Background = p.panel
	t.Surface = p.field
	t.SurfaceHover = p.hover
	t.SurfacePressed = p.pill
	t.Border = p.border
	t.Text = p.fg
	t.TextMuted = p.muted
	t.Accent = p.accent
	t.AccentText = p.onAccent
	t.Scrollbar = ui.Hex("#4a4c50")
	t.ScrollbarWidth = 6
	t.Danger = p.error
	p.theme = t
	return p
}()

func settingsPaletteForTheme(t *ui.Theme) settingsPalette {
	if t != nil && t.Dark {
		return darkSettingsPalette
	}
	return lightSettingsPalette
}

type settingsTab string

const (
	tabGeneral  settingsTab = "general"
	tabServices settingsTab = "services"
	tabHotkeys  settingsTab = "hotkeys"
	tabNaming   settingsTab = "naming"
)

type settingsNavItem struct {
	id    settingsTab
	label string
}

var settingsNavItems = []settingsNavItem{
	{id: tabGeneral, label: "通用"},
	{id: tabServices, label: "翻译服务"},
	{id: tabHotkeys, label: "快捷键"},
	{id: tabNaming, label: "命名转换"},
}

type serviceCredentialField struct {
	name        string
	label       string
	placeholder string
}

var serviceCredentialFields = map[string][]serviceCredentialField{
	"deepl":       {{name: keys.DeeplKey, label: "API Key", placeholder: "免费版以 :fx 结尾"}},
	"openai":      {{name: keys.OpenAIKey, label: "API Key", placeholder: "sk-…"}},
	"siliconflow": {{name: keys.SiliconFlowKey, label: "API Key", placeholder: "硅基流动控制台 API Key"}},
	"zhipu":       {{name: keys.ZhipuKey, label: "API Key", placeholder: "智谱开放平台 API Key"}},
	"baidu": {
		{name: keys.BaiduAppID, label: "APP ID", placeholder: "控制台 APP ID"},
		{name: keys.BaiduKey, label: "密钥", placeholder: "控制台密钥"},
	},
	"volcengine": {
		{name: keys.VolcAccessKey, label: "Access Key ID", placeholder: "AKLT…"},
		{name: keys.VolcSecretKey, label: "Secret Access Key", placeholder: "控制台生成的密钥"},
	},
	"tencent": {
		{name: keys.TencentSecretID, label: "SecretId", placeholder: "AKID…"},
		{name: keys.TencentSecretKey, label: "SecretKey", placeholder: "控制台密钥"},
	},
	"youdao": {
		{name: keys.YoudaoAppKey, label: "应用 ID", placeholder: "控制台 appKey"},
		{name: keys.YoudaoSecret, label: "应用密钥", placeholder: "控制台密钥"},
	},
	"azure": {
		{name: keys.AzureKey, label: "订阅密钥", placeholder: "Key 1 或 Key 2"},
		{name: keys.AzureRegion, label: "区域", placeholder: "如 eastasia"},
	},
	"google":          nil,
	"index-translate": nil,
}

var serviceNotes = map[string]string{
	"siliconflow": "需实名认证，费用及限流以平台为准。",
	"zhipu":       "费用及限流以平台为准。",
	"baidu":       "需开通「通用文本翻译」。",
	"volcengine":  "需开通「机器翻译」并授权 Access Key。",
}

var defaultServiceModels = func() map[string]string {
	cfg := config.Default()
	return map[string]string{"openai": serviceModel(cfg, "openai"), "siliconflow": serviceModel(cfg, "siliconflow"), "zhipu": serviceModel(cfg, "zhipu")}
}()

type copyFormatOption struct {
	style   string
	label   string
	example string
}

var settingsCopyFormats = []copyFormatOption{
	{style: naming.LowerCamel, label: "小驼峰", example: "userName"},
	{style: naming.UpperCamel, label: "大驼峰", example: "UserName"},
	{style: naming.Snake, label: "下划线", example: "user_name"},
	{style: naming.Constant, label: "常量", example: "USER_NAME"},
	{style: naming.Kebab, label: "短横线", example: "user-name"},
}

func (v *settingsView) render(c *ui.Context) {
	p := settingsPaletteForTheme(c.Theme())
	c.SetTheme(p.theme)

	v.mu.Lock()
	toastMsg := v.pendingToast
	v.pendingToast = ""
	active := v.activeTab
	listening := v.hotkeyListening
	armed := v.hotkeyCaptureArmed
	v.mu.Unlock()

	if toastMsg != "" {
		c.Toast(toastMsg)
	}
	if active == tabHotkeys && listening != "" {
		if !armed {
			// Skip keys held on the frame the user clicked the recorder button.
			v.mu.Lock()
			v.hotkeyCaptureArmed = true
			v.mu.Unlock()
		} else {
			v.handleHotkeyCapture(c, listening)
		}
	}

	c.Root().Background(p.panel).Padding(0).Children(func() {
		ui.Row(c).Fill().Children(func() {
			v.renderSidebar(c, p, active)
			ui.Box(c).Width(1).FillHeight().Background(p.border)
			ui.Scroll(c).Grow(1).FillHeight().Children(func() {
				ui.Column(c).Padding(24, 28).MaxWidth(620).Gap(14).Children(func() {
					switch active {
					case tabGeneral:
						v.renderGeneral(c, p)
					case tabServices:
						v.renderServices(c, p)
					case tabHotkeys:
						v.renderHotkeys(c, p)
					case tabNaming:
						v.renderNaming(c, p)
					}
				})
			})
		})
	})
}

func (v *settingsView) renderSidebar(c *ui.Context, p settingsPalette, active settingsTab) {
	ui.Column(c).Width(176).FillHeight().Background(p.sidebar).Padding(16, 12).Gap(4).Children(func() {
		for _, item := range settingsNavItems {
			isActive := item.id == active
			btn := ui.ButtonBase(c).
				Label(item.label).
				Cursor(ui.CursorPointer).
				FillWidth().
				Radius(8).
				Padding(9, 10)
			if isActive {
				btn.Background(p.control)
			} else if btn.Hovered() {
				btn.Background(p.hover)
			}
			if btn.Clicked() {
				v.setActiveTab(item.id)
			}
			btn.Children(func() {
				color := p.muted
				weight := 400
				if isActive {
					color = p.accentText
					weight = 600
				}
				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
					barColor := ui.Transparent
					if isActive {
						barColor = p.accent
					}
					ui.Box(c).Size(3, 14).Radius(999).Background(barColor)
					ui.Text(c, item.label).FontSize(13).FontWeight(weight).TextColor(color).SingleLine()
				})
			})
		}

		ui.Spacer(c)

		ui.Row(c).
			FillWidth().
			Padding(7, 10).
			Radius(9).
			Background(p.field).
			AlignItems(ui.Center).
			Gap(6).
			Tooltip("凭据由 Windows Credential Manager 本地加密保护").
			Children(func() {
				ui.Icon(c, shieldSVG).Size(13, 13).TextColor(p.accent)
				ui.Text(c, "Windows 凭据保护").FontSize(11).TextColor(p.muted).SingleLine()
			})
	})
}

func (v *settingsView) renderGeneral(c *ui.Context, p settingsPalette) {
	v.mu.Lock()
	cfg := cloneSettings(v.settings)
	v.mu.Unlock()

	ui.Text(c, "通用").FontSize(18).FontWeight(600).TextColor(p.fg)

	ui.Column(c).Radius(12).Background(p.card).Border(1, p.border).Clip().Children(func() {
		// Target language
		v.renderSettingRow(c, p, "默认翻译到", "未指定目标时气泡首选的翻译语言", true, func() {
			selected := cfg.TargetLanguage
			parts := ui.SelectBase(c, &selected)
			trigger := parts.Trigger.Label("选择默认翻译语言").
				Cursor(ui.CursorPointer).MinWidth(176).
				Padding(6, 10).Radius(8).Background(p.field).Border(1, p.border)
			trigger.OnChange(func() {
				v.mu.Lock()
				next := cloneSettings(v.settings)
				next.TargetLanguage = selected
				v.enqueueSaveLocked(next)
				v.mu.Unlock()
			})
			if trigger.Hovered() {
				trigger.Background(p.hover)
			}
			languageLabel := func(code string) string {
				if code == "auto" {
					return "自动（中文↔英文）"
				}
				return bubbleLangName(code)
			}
			trigger.Children(func() {
				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(6).Children(func() {
					ui.Text(c, languageLabel(selected)).Grow(1).FontSize(12).TextColor(p.fg).SingleLine()
					ui.Icon(c, chevronDownSVG).Size(12, 12).TextColor(p.muted)
				})
			})
			parts.Popup(func(panel ui.Element) {
				// Use the same anchored select as the bubble, including toggle-close
				// behavior and a popup whose width follows the committed trigger.
				bounds := trigger.Bounds()
				panel.Label("默认翻译语言选项").Width(bounds.W).Padding(4).
					Margin(4, 0, 0, 0).Radius(10).Background(p.panel).
					Border(1, p.border).Shadow(0, 4, 12, 0, ui.RGBA(0, 0, 0, 0.14))
				codes := append([]string{"auto"}, languages.List...)
				_, windowH := c.Size()
				room := max(bounds.Y-8, windowH-bounds.Y-bounds.H-8)
				ui.Scroll(c).Height(min(float32(len(codes))*30, max(30, room-10))).Children(func() {
					for _, code := range codes {
						item := parts.Item(code).Height(30).Padding(0, 8).Radius(6).Cursor(ui.CursorPointer)
						if item.Highlighted() {
							item.Background(p.hover)
						} else if code == selected {
							item.Background(p.control)
						}
						item.Children(func() { ui.Text(c, languageLabel(code)).FontSize(12).TextColor(p.fg).SingleLine() })
					}
				})
			})
		})

		openAtLogin := cfg.OpenAtLogin
		v.renderSettingRow(c, p, "开机自启", "登录后常驻托盘", true, func() {
			sw := ui.Switch(c, &openAtLogin).Label("切换开机自启")
			if sw.Changed() {
				v.mu.Lock()
				next := cloneSettings(v.settings)
				next.OpenAtLogin = openAtLogin
				v.enqueueSaveLocked(next)
				v.mu.Unlock()
			}
		})

		autoHide := cfg.Bubble.AutoHide
		v.renderSettingRow(c, p, "失焦自动隐藏", "未钉住的气泡失去焦点即隐藏", true, func() {
			sw := ui.Switch(c, &autoHide).Label("切换失焦自动隐藏")
			if sw.Changed() {
				v.mu.Lock()
				next := cloneSettings(v.settings)
				next.Bubble.AutoHide = autoHide
				v.enqueueSaveLocked(next)
				v.mu.Unlock()
			}
		})

		autoUpdate := cfg.AutoUpdate
		v.renderSettingRow(c, p, "自动更新", "后台检查新版本，可在更新窗口中确认安装", false, func() {
			sw := ui.Switch(c, &autoUpdate).Label("切换自动更新")
			if sw.Changed() {
				v.mu.Lock()
				next := cloneSettings(v.settings)
				next.AutoUpdate = autoUpdate
				v.enqueueSaveLocked(next)
				v.mu.Unlock()
			}
		})
	})
}

func (v *settingsView) renderSettingRow(c *ui.Context, p settingsPalette, title, sub string, divider bool, control func()) {
	ui.Column(c).FillWidth().Children(func() {
		ui.Row(c).FillWidth().Padding(13, 16).AlignItems(ui.Center).Gap(16).Children(func() {
			ui.Column(c).Grow(1).Gap(3).Children(func() {
				ui.Text(c, title).FontSize(13).FontWeight(500).TextColor(p.fg)
				if sub != "" {
					ui.Text(c, sub).FontSize(12).LineHeight(1.5).TextColor(p.muted)
				}
			})
			control()
		})
		if divider {
			ui.Box(c).FillWidth().Height(1).Background(p.border)
		}
	})
}

func (v *settingsView) renderServices(c *ui.Context, p settingsPalette) {
	v.mu.Lock()
	loaded := v.credentialsLoaded
	credErr := v.credentialsErr
	cfg := cloneSettings(v.settings)
	_, hasNewCustom := v.customDrafts["new"]
	var enabledNames, disabledNames []string
	for _, svc := range cfg.Services.Order {
		_, isBuiltIn := serviceCredentialFields[svc]
		_, isCustom := cfg.Services.FindCustom(svc)
		if !isBuiltIn && !isCustom {
			continue
		}
		if v.serviceOnLocked(svc) {
			enabledNames = append(enabledNames, svc)
		} else {
			disabledNames = append(disabledNames, svc)
		}
	}
	if hasNewCustom {
		disabledNames = append(disabledNames, "new")
	}
	displayEnabled := enabledNames
	if sameServiceSet(v.drag.preview, enabledNames) {
		displayEnabled = slices.Clone(v.drag.preview)
	} else {
		v.drag.preview = nil
	}
	v.mu.Unlock()

	ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
		ui.Text(c, "翻译服务").FontSize(18).FontWeight(600).TextColor(p.fg).Grow(1)
		if loaded {
			v.renderSecondaryButton(c, p, "+ 添加自定义服务", func() {
				v.mu.Lock()
				if _, ok := v.customDrafts["new"]; !ok {
					v.customDrafts["new"] = customDraft{}
				}
				v.expanded["new"] = true
				v.mu.Unlock()
			})
		}
	})

	if !loaded {
		ui.Row(c).Padding(14).Radius(12).Background(p.card).Border(1, p.border).AlignItems(ui.Center).Gap(12).Children(func() {
			msg := "正在读取服务状态…"
			color := p.muted
			if credErr != "" {
				msg = credErr
				color = p.error
			}
			ui.Text(c, msg).FontSize(12).TextColor(color).Grow(1)
			if credErr != "" {
				v.renderSecondaryButton(c, p, "重试", func() {
					v.refreshCredentialsAsync()
				})
			}
		})
		return
	}

	ui.Column(c).Gap(3).Children(func() {
		ui.Text(c, "已启用的服务会同时翻译，序号即结果顺序。").FontSize(13).TextColor(p.fg)
		ui.Text(c, "拖动序号调整顺序，凭证保存在系统密钥环。").FontSize(12).TextColor(p.muted)
	})

	v.renderServiceGroupHeader(c, p, "已启用", len(displayEnabled))
	if len(displayEnabled) > 0 {
		anyDragging := false
		hoveredTarget := ""
		listCol := ui.Column(c).Gap(8)
		if droppedSvc, ok := ui.Drop[string](listCol); ok && droppedSvc != "" {
			v.mu.Lock()
			v.finishDragLocked(enabledNames)
			v.mu.Unlock()
		}
		listCol.Children(func() {
			for i, svc := range displayEnabled {
				draggingCard, overCard := v.renderServiceCard(c.Key("svc:"+svc), p, svc, true, i+1, displayEnabled)
				if draggingCard {
					anyDragging = true
				}
				if overCard != "" {
					hoveredTarget = overCard
				}
			}
		})
		v.mu.Lock()
		if hoveredTarget == "" {
			v.drag.target = ""
		}
		if !anyDragging && v.drag.preview != nil {
			v.drag.clear()
		}
		v.mu.Unlock()
	} else {
		ui.Row(c).Padding(14).Radius(12).Background(p.field).Border(1, p.border).Children(func() {
			ui.Text(c, "还没有启用的服务。打开下方开关即可加入翻译。").FontSize(12).TextColor(p.muted)
		})
	}

	if len(disabledNames) > 0 {
		v.renderServiceGroupHeader(c, p, "未启用", len(disabledNames))
		ui.Column(c).Gap(8).Children(func() {
			for _, svc := range disabledNames {
				v.renderServiceCard(c.Key("svc:"+svc), p, svc, false, 0, nil)
			}
		})
	}
}

func (v *settingsView) renderServiceGroupHeader(c *ui.Context, p settingsPalette, title string, count int) {
	ui.Row(c).Padding(6, 0, 0, 0).AlignItems(ui.Center).Gap(6).Children(func() {
		ui.Text(c, title).FontSize(12).FontWeight(600).TextColor(p.muted)
		ui.Row(c).Padding(1, 7).Radius(999).Background(p.pill).Children(func() {
			ui.Text(c, fmt.Sprintf("%d", count)).FontSize(11).TextColor(p.badgeInk)
		})
	})
}

func (v *settingsView) renderServiceCard(c *ui.Context, p settingsPalette, svc string, enabled bool, position int, displayEnabled []string) (bool, string) {
	isCustom := svc == "new" || config.IsCustomService(svc)
	fields := serviceCredentialFields[svc]
	keyless := !isCustom && len(fields) == 0

	v.mu.Lock()
	title := bubbleServiceName(svc)
	if isCustom {
		title = "自定义服务"
		if draft, ok := v.customDrafts[svc]; ok && strings.TrimSpace(draft.service.Name) != "" {
			title = strings.TrimSpace(draft.service.Name)
		} else if cs, ok := v.settings.Services.FindCustom(svc); ok && cs.Name != "" {
			title = cs.Name
		}
	}
	isConfigured := v.isServiceConfiguredLocked(svc)
	cs, hasSavedCustom := v.settings.Services.FindCustom(svc)
	paused := hasSavedCustom && cs.IsConfigured() && !isConfigured
	isExpanded := v.expanded[svc]
	busy := v.credentialBusy[svc]
	dragAnimating := v.drag.preview != nil
	customHasKey := isCustom && slices.Contains(v.credentials.Stored, keys.CustomAPIKey(svc))
	v.mu.Unlock()

	stateHint := "填写凭证后可启用"
	if isCustom {
		stateHint = "填写名称、地址和模型后可启用"
		if enabled {
			if customHasKey {
				stateHint = "已保存密钥"
			} else {
				stateHint = "无需密钥"
			}
		} else if isConfigured {
			stateHint = "打开开关后参与翻译"
		} else if paused {
			stateHint = "服务已暂停，请重新填写或清除 API Key"
		}
	} else {
		if enabled {
			if keyless {
				stateHint = "无需凭据"
			} else {
				stateHint = "凭证已保存"
			}
		} else if keyless || isConfigured {
			stateHint = "打开开关后参与翻译"
		}
	}

	card := ui.Column(c).
		Radius(12).
		Background(p.card).
		Border(1, p.border)
	if dragAnimating {
		card.Transition(ui.ElementTransition{Duration: 150 * time.Millisecond, Ease: ui.EaseOut})
	}

	dragging := false
	overTarget := ""
	if enabled {
		if !isExpanded {
			card.Drag(svc)
		}
		if card.Dragging() {
			dragging = true
			card.Opacity(0.4).Border(1.5, p.accent).Background(p.accentSoft)
		} else {
			if droppedSvc, ok := ui.Drop[string](card); ok && droppedSvc != "" {
				v.mu.Lock()
				v.finishDragLocked(displayEnabled)
				v.mu.Unlock()
			}
			if overSvc, over := ui.DragOver[string](card); over && overSvc != "" && overSvc != svc {
				overTarget = svc
				v.mu.Lock()
				_, pointerY, _ := c.Root().PointerPosition()
				_, localY, _ := card.PointerPosition()
				bounds := card.LayoutBounds()
				from, to := slices.Index(displayEnabled, overSvc), slices.Index(displayEnabled, svc)
				crossed := (from > to && localY <= bounds.H/2+2) || (from < to && localY >= bounds.H/2-2)
				moved := pointerY-v.drag.pointerY >= 1 || v.drag.pointerY-pointerY >= 1
				// Layout animation must not swap the same pair back under a still
				// pointer; actual pointer motion can cross it again in either direction.
				if crossed && (v.drag.target != svc || moved) {
					v.drag.target = svc
					v.drag.pointerY = pointerY
					nextPreview := moveSliceItem(displayEnabled, overSvc, svc)
					if !slices.Equal(v.drag.preview, nextPreview) {
						v.drag.preview = nextPreview
						c.AnimationFrame()
					}
				}
				v.mu.Unlock()
			}
		}
	}

	card.Children(func() {
		head := ui.Row(c).FillWidth().Padding(12, 16).AlignItems(ui.Center).Gap(12)
		if !keyless && isExpanded {
			head.Background(p.cardHead)
		}
		if enabled && isExpanded {
			head.Drag(svc)
			if head.Dragging() {
				dragging = true
				card.Opacity(0.4).Border(1.5, p.accent).Background(p.accentSoft)
			}
		}
		head.Children(func() {
			if enabled {
				grip := ui.Row(c).
					Label("拖动"+title).
					Size(26, 26).
					Radius(7).
					Background(p.field).
					Border(1, p.border).
					Center().
					Cursor(ui.CursorPointer).
					Tooltip("拖动排序")
				gripNumColor := p.badgeInk
				if grip.Hovered() {
					grip.Background(p.hover).Border(1, p.accent.Alpha(0.45))
					gripNumColor = p.fg
				}
				grip.Children(func() {
					ui.Text(c, fmt.Sprintf("%d", position)).FontSize(11).FontWeight(600).TextColor(gripNumColor)
				})
			}

			ui.Column(c).Grow(1).Gap(2).Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
					ui.Text(c, title).FontSize(13).FontWeight(600).TextColor(p.fg)
					if !enabled {
						badgeText := "未配置"
						badgeBg := p.warningSoft
						badgeFg := p.warning
						if isCustom {
							if isConfigured {
								badgeText = "自定义"
								badgeBg = p.accentSoft
								badgeFg = p.accentText
							} else if paused {
								badgeText = "已暂停"
							}
						} else if keyless {
							badgeText = "免配置"
							badgeBg = p.pill
							badgeFg = p.badgeInk
						} else if isConfigured {
							badgeText = "已配置"
							badgeBg = p.accentSoft
							badgeFg = p.accentText
						}
						ui.Row(c).Padding(2, 8).Radius(999).Background(badgeBg).Children(func() {
							ui.Text(c, badgeText).FontSize(11).FontWeight(600).TextColor(badgeFg).SingleLine()
						})
					}
				})
				ui.Text(c, stateHint).FontSize(12).TextColor(p.muted)
			})

			ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
				if !keyless {
					cfgLabel := "配置"
					accessLabel := "配置" + title
					if isExpanded {
						cfgLabel = "收起"
						accessLabel = "收起" + title
					}
					v.renderSecondaryButtonDisabled(c, p, cfgLabel, accessLabel, isCustom && busy, func() {
						v.mu.Lock()
						if svc == "new" && v.expanded["new"] {
							delete(v.customDrafts, "new")
							delete(v.expanded, "new")
							v.mu.Unlock()
							return
						}
						nextExpanded := !v.expanded[svc]
						if !nextExpanded {
							v.flushTextDraftsLocked()
						}
						v.expanded[svc] = nextExpanded
						loadUsage := nextExpanded && svc == "deepl" && slices.Contains(v.credentials.Stored, keys.DeeplKey)
						v.mu.Unlock()
						if loadUsage {
							v.loadDeeplUsageAsync()
						}
					})
				}

				if svc != "new" {
					v.mu.Lock()
					handle := v.switchHandles[svc]
					if handle == nil {
						handle = &ui.Handle{}
						v.switchHandles[svc] = handle
					}
					// A rollback can move the same switch back to its old parent.
					focus := v.focusService == svc || handle.Focused(c)
					if focus {
						v.focusService = ""
					}
					v.mu.Unlock()
					if focus {
						handle.Focus(c)
					}
					on := enabled
					sw := ui.Switch(c, &on).
						Label("启用" + title).
						Bind(handle).
						Disabled(busy || (!keyless && !isConfigured))
					if sw.Changed() {
						v.mu.Lock()
						v.focusService = svc
						next := cloneSettings(v.settings)
						next.Services.Enabled = slices.DeleteFunc(slices.Clone(next.Services.Enabled), func(s string) bool {
							return s == svc
						})
						if on {
							next.Services.Enabled = append(next.Services.Enabled, svc)
						} else {
							v.flushTextDraftsLocked()
							v.expanded[svc] = false
						}
						v.enqueueSaveLocked(next)
						v.mu.Unlock()
					}
				}
			})
		})

		if isExpanded {
			if isCustom {
				v.renderCustomServiceDrawer(c, p, svc, isConfigured)
			} else if !keyless {
				v.renderServiceDrawer(c, p, svc, fields, isConfigured)
			}
		}
	})
	return dragging, overTarget
}

func (v *settingsView) renderServiceDrawer(c *ui.Context, p settingsPalette, svc string, fields []serviceCredentialField, isConfigured bool) {
	v.mu.Lock()
	busy := v.credentialBusy[svc]
	testStatus := v.testStatuses[svc]
	locked := busy || testStatus.testing
	hasDraft := v.hasServiceDraftLocked(svc)
	baseURLVal := v.settings.Services.OpenAI.BaseURL
	if v.baseURLDraft != nil {
		baseURLVal = *v.baseURLDraft
	}
	modelVal, ok := v.modelDrafts[svc]
	if !ok {
		modelVal = serviceModel(v.settings, svc)
	}

	storedMap := make(map[string]bool, len(fields))
	draftMap := make(map[string]string, len(fields))
	for _, f := range fields {
		storedMap[f.name] = slices.Contains(v.credentials.Stored, f.name)
		if v.serviceDrafts[svc] != nil {
			draftMap[f.name] = v.serviceDrafts[svc][f.name]
		}
	}
	deeplHasKey := slices.Contains(v.credentials.Stored, keys.DeeplKey)
	deeplText := v.deeplUsageText
	deeplPct := v.deeplUsagePct
	v.mu.Unlock()

	ui.Box(c).FillWidth().Height(1).Background(p.border)
	ui.Column(c).FillWidth().Background(p.card).Padding(14, 16).Gap(12).Children(func() {
		// Link to console
		ui.Row(c).Children(func() {
			link := ui.ButtonBase(c).Label("前往控制台获取凭证").Cursor(ui.CursorPointer).Padding(2, 0)
			if link.Clicked() {
				v.openKeyPageAsync(svc)
			}
			link.Children(func() {
				txt := ui.Text(c, "前往控制台获取凭证 →").FontSize(12).TextColor(p.accentText)
				if link.Hovered() {
					txt.Underline()
				}
			})
		})

		if svc == "openai" {
			v.mu.Lock()
			hasBaseURLDraft := v.baseURLDraft != nil
			v.mu.Unlock()
			v.renderDrawerInputRow(c, p, "Base URL", "任何 OpenAI 兼容接口", &baseURLVal, "https://api.openai.com/v1", locked, hasBaseURLDraft,
				func(val string) {
					v.mu.Lock()
					copyVal := val
					v.baseURLDraft = &copyVal
					v.testStatuses[svc] = serviceTestStatus{}
					v.mu.Unlock()
				},
				func(val string) {
					v.mu.Lock()
					next := cloneSettings(v.settings)
					next.Services.OpenAI.BaseURL = strings.TrimSpace(val)
					v.baseURLDraft = nil
					v.testStatuses[svc] = serviceTestStatus{}
					v.enqueueSaveLocked(next)
					v.mu.Unlock()
				},
			)
		}

		if defaultModel, hasModel := defaultServiceModels[svc]; hasModel {
			v.mu.Lock()
			_, hasModelDraft := v.modelDrafts[svc]
			v.mu.Unlock()
			v.renderDrawerInputRow(c, p, "模型", "留空恢复默认模型", &modelVal, defaultModel, locked, hasModelDraft,
				func(val string) {
					v.mu.Lock()
					v.modelDrafts[svc] = val
					v.testStatuses[svc] = serviceTestStatus{}
					v.mu.Unlock()
				},
				func(val string) {
					v.mu.Lock()
					next := cloneSettings(v.settings)
					trimmed := strings.TrimSpace(val)
					setServiceModel(&next, svc, trimmed)
					delete(v.modelDrafts, svc)
					v.testStatuses[svc] = serviceTestStatus{}
					v.enqueueSaveLocked(next)
					v.mu.Unlock()
				},
			)
		}

		for _, f := range fields {
			isStored := storedMap[f.name]
			sub := ""
			if f.name == keys.AzureRegion {
				sub = "可选，全局资源留空"
			}
			ph := f.placeholder
			if isStored {
				ph = "已保存，留空不变"
			}
			val := draftMap[f.name]
			field := f
			v.renderSecretInputRow(c, p, field.label, sub, &val, ph, locked, isStored,
				func(val string) {
					v.mu.Lock()
					if v.serviceDrafts[svc] == nil {
						v.serviceDrafts[svc] = make(map[string]string)
					}
					v.serviceDrafts[svc][field.name] = val
					v.testStatuses[svc] = serviceTestStatus{}
					v.mu.Unlock()
				},
				func() { v.saveCredentials(svc) },
				func() { v.clearSecret(svc, field.name) },
			)
		}

		if note, ok := serviceNotes[svc]; ok {
			ui.Text(c, note).FontSize(11).TextColor(p.muted)
		}

		if svc == "deepl" && deeplHasKey {
			ui.Column(c).FillWidth().Gap(4).Children(func() {
				ui.Text(c, deeplText).FontSize(11).TextColor(p.muted)
				ui.Progress(c, deeplPct).FillWidth().Height(6)
			})
		}

		// Footer
		ui.Row(c).FillWidth().Padding(4, 0, 0, 0).AlignItems(ui.Center).Gap(8).Children(func() {
			v.renderServiceTestStatus(c, p, testStatus)

			testDisabled := !isConfigured || locked || hasDraft
			v.renderSecondaryButtonDisabled(c, p, "测试连接", "测试连接", testDisabled, func() {
				v.testConnection(svc)
			})

			v.renderDrawerSaveButton(c, p, "保存凭证", !hasDraft || locked, func() {
				v.saveCredentials(svc)
			})
		})
	})
}

func (v *settingsView) renderCustomServiceDrawer(c *ui.Context, p settingsPalette, svc string, isConfigured bool) {
	v.mu.Lock()
	busy := v.credentialBusy[svc]
	testStatus := v.testStatuses[svc]
	locked := busy || testStatus.testing

	draft, ok := v.customDrafts[svc]
	saved, hasSaved := v.settings.Services.FindCustom(svc)
	if !ok {
		draft = customDraft{service: saved}
		v.customDrafts[svc] = draft
	}

	keyName := keys.CustomAPIKey(svc)
	isStored := svc != "new" && keyName != "" && slices.Contains(v.credentials.Stored, keyName)
	v.mu.Unlock()

	nameVal := draft.service.Name
	baseURLVal := draft.service.BaseURL
	modelVal := draft.service.Model
	apiKeyVal := draft.apiKey

	service := draft.service.Normalized()
	hasUnsaved := !hasSaved || service != saved || strings.TrimSpace(apiKeyVal) != ""
	canSave := !locked && service.IsConfigured()
	confirmingDelete := time.Now().Before(draft.deleteUntil)

	updateDraft := func(fn func(*customDraft)) {
		v.mu.Lock()
		d := v.customDrafts[svc]
		fn(&d)
		v.customDrafts[svc] = d
		v.testStatuses[svc] = serviceTestStatus{}
		v.mu.Unlock()
	}

	ui.Box(c).FillWidth().Height(1).Background(p.border)
	ui.Column(c).FillWidth().Background(p.card).Padding(14, 16).Gap(12).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(12).Children(func() {
			ui.Column(c).Width(136).Gap(2).Children(func() {
				ui.Text(c, "接口格式").FontSize(13).FontWeight(500).TextColor(p.fg)
			})
			ui.Row(c).Padding(4, 10).Radius(6).Background(p.field).Children(func() {
				ui.Text(c, "OpenAI 兼容（Chat Completions）").FontSize(12).TextColor(p.muted).SingleLine()
			})
		})

		v.renderDrawerInputRow(c, p, "服务名称", "显示在译文卡片标题", &nameVal, "例如：DeepSeek、Ollama", locked, false,
			func(val string) { updateDraft(func(d *customDraft) { d.service.Name = val }) },
			nil,
		)
		v.renderDrawerInputRow(c, p, "Base URL", "填到 /v1 即可", &baseURLVal, "https://api.openai.com/v1", locked, false,
			func(val string) { updateDraft(func(d *customDraft) { d.service.BaseURL = val }) },
			nil,
		)
		v.renderDrawerInputRow(c, p, "模型名称", "目标推理模型 ID", &modelVal, "例如：gpt-4o-mini、qwen2.5:7b", locked, false,
			func(val string) { updateDraft(func(d *customDraft) { d.service.Model = val }) },
			nil,
		)

		ph := "可选，本地模型可留空"
		if isStored {
			ph = "已保存，留空不变"
		}
		canClear := isStored || (hasSaved && saved.IsConfigured() && !isConfigured)
		v.renderSecretInputRow(c, p, "API Key", "可选，存入系统密钥环", &apiKeyVal, ph, locked, canClear,
			func(val string) { updateDraft(func(d *customDraft) { d.apiKey = val }) },
			nil,
			func() { v.clearSecret(svc, keyName) },
		)

		// Footer
		ui.Row(c).FillWidth().Padding(4, 0, 0, 0).AlignItems(ui.Center).Gap(8).Children(func() {
			v.renderServiceTestStatus(c, p, testStatus)

			if !confirmingDelete {
				delBtn := ui.ButtonBase(c).Label("删除服务").Disabled(locked).Cursor(ui.CursorPointer).Padding(5, 8)
				if delBtn.Clicked() {
					if svc == "new" {
						v.deleteCustomService("new")
					} else {
						v.mu.Lock()
						d := v.customDrafts[svc]
						d.deleteUntil = time.Now().Add(4 * time.Second)
						v.customDrafts[svc] = d
						v.mu.Unlock()
					}
				}
				delBtn.Children(func() {
					col := p.error
					if locked {
						col = p.muted.Alpha(0.5)
					}
					txt := ui.Text(c, "删除").FontSize(12).TextColor(col)
					if !locked && delBtn.Hovered() {
						txt.Underline()
					}
				})
			} else {
				confirmBtn := ui.ButtonBase(c).
					Label("确认删除").
					Disabled(locked).
					Cursor(ui.CursorPointer).
					Padding(5, 10).
					Radius(8).
					Background(p.error)
				if confirmBtn.Clicked() {
					v.deleteCustomService(svc)
				}
				confirmBtn.Children(func() {
					ui.Text(c, "确认删除").FontSize(12).FontWeight(600).TextColor(p.onAccent).SingleLine()
				})
				v.renderSecondaryButtonDisabled(c, p, "取消", "取消删除", locked, func() {
					v.mu.Lock()
					d := v.customDrafts[svc]
					d.deleteUntil = time.Time{}
					v.customDrafts[svc] = d
					v.mu.Unlock()
				})
			}

			if svc != "new" {
				testDisabled := !isConfigured || locked || hasUnsaved
				v.renderSecondaryButtonDisabled(c, p, "测试连接", "测试连接", testDisabled, func() {
					v.testConnection(svc)
				})
			}

			v.renderDrawerSaveButton(c, p, "保存", !canSave, func() {
				v.saveCustomService(svc)
			})
		})
	})
}

func (v *settingsView) renderSecretInputRow(
	c *ui.Context, p settingsPalette, label, sub string, val *string, placeholder string,
	disabled, canClear bool, onDraft func(string), onSubmit, onClear func(),
) {
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(12).Children(func() {
		ui.Column(c).Width(136).Gap(2).Children(func() {
			ui.Text(c, label).FontSize(13).FontWeight(500).TextColor(p.fg)
			if sub != "" {
				ui.Text(c, sub).FontSize(12).TextColor(p.muted)
			}
		})
		ui.Row(c).Grow(1).AlignItems(ui.Center).Gap(8).Children(func() {
			inp := ui.TextInput(c, val).
				Label("编辑"+label).
				Password().
				Placeholder(placeholder).
				Disabled(disabled).
				Grow(1).
				Padding(7, 10).
				Radius(8).
				FontSize(13)
			if inp.Changed() && onDraft != nil {
				onDraft(*val)
			}
			if inp.Submitted() && onSubmit != nil {
				onSubmit()
			}
			if canClear {
				clr := ui.ButtonBase(c).Label("清除"+label).Disabled(disabled).Cursor(ui.CursorPointer).Padding(4, 6)
				if clr.Clicked() && onClear != nil {
					onClear()
				}
				clr.Children(func() {
					col := p.accentText
					if disabled {
						col = p.muted.Alpha(0.5)
					}
					txt := ui.Text(c, "清除").FontSize(12).TextColor(col)
					if !disabled && clr.Hovered() {
						txt.Underline()
					}
				})
			}
		})
	})
}

func (v *settingsView) renderServiceTestStatus(c *ui.Context, p settingsPalette, testStatus serviceTestStatus) {
	statusText := "未测试"
	statusColor := p.muted
	dotColor := p.muted.Alpha(0.5)
	if testStatus.result != "" {
		statusText = testStatus.result
		if testStatus.isError {
			statusColor = p.error
			dotColor = p.error
		} else if !testStatus.testing {
			statusColor = p.success
			dotColor = p.success
		} else {
			dotColor = p.accent
		}
	}
	ui.Row(c).Grow(1).AlignItems(ui.Center).Gap(6).Children(func() {
		ui.Box(c).Size(6, 6).Radius(999).Background(dotColor)
		ui.Text(c, statusText).FontSize(12).TextColor(statusColor)
	})
}

func (v *settingsView) renderDrawerSaveButton(c *ui.Context, p settingsPalette, label string, disabled bool, onClick func()) {
	btn := ui.ButtonBase(c).
		Label(label).
		Disabled(disabled).
		Cursor(ui.CursorPointer).
		Padding(6, 14).
		Radius(8).
		Background(p.primaryBg)
	if disabled {
		btn.Opacity(0.45)
	} else if btn.Hovered() {
		if p.dark {
			btn.Background(p.accent.Alpha(0.88))
		} else {
			btn.Background(p.accent)
		}
	}
	if btn.Clicked() && onClick != nil {
		onClick()
	}
	btn.Children(func() {
		ui.Text(c, label).FontSize(12).FontWeight(600).TextColor(p.onAccent).SingleLine()
	})
}

func (v *settingsView) renderDrawerInputRow(
	c *ui.Context,
	p settingsPalette,
	label, sub string,
	val *string,
	placeholder string,
	disabled, hasDraft bool,
	onDraft func(string),
	onCommit func(string),
) {
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(12).Children(func() {
		ui.Column(c).Width(136).Gap(2).Children(func() {
			ui.Text(c, label).FontSize(13).FontWeight(500).TextColor(p.fg)
			if sub != "" {
				ui.Text(c, sub).FontSize(12).TextColor(p.muted)
			}
		})
		inp := ui.TextInput(c, val).
			Label("编辑"+label).
			Placeholder(placeholder).
			Disabled(disabled).
			Grow(1).
			Padding(7, 10).
			Radius(8).
			FontSize(13)
		if inp.Changed() && onDraft != nil {
			onDraft(*val)
		}
		if (inp.Submitted() || (hasDraft && !inp.Focused())) && onCommit != nil {
			onCommit(*val)
		}
	})
}

func (v *settingsView) renderSecondaryButton(c *ui.Context, p settingsPalette, label string, onClick func()) ui.Element {
	return v.renderSecondaryButtonDisabled(c, p, label, label, false, onClick)
}

func (v *settingsView) renderSecondaryButtonDisabled(c *ui.Context, p settingsPalette, textLabel, accessLabel string, disabled bool, onClick func()) ui.Element {
	btn := ui.ButtonBase(c).
		Label(accessLabel).
		Disabled(disabled).
		Cursor(ui.CursorPointer).
		Padding(5, 11).
		Radius(8).
		Background(p.control).
		Border(1, p.border)
	if !disabled && btn.Hovered() {
		btn.Background(p.hover)
	}
	if disabled {
		btn.Opacity(0.45)
	}
	if btn.Clicked() && onClick != nil {
		onClick()
	}
	btn.Children(func() {
		ui.Text(c, textLabel).FontSize(12).TextColor(p.fg).SingleLine()
	})
	return btn
}

type hotkeyItem struct {
	id    string
	label string
}

var settingsHotkeys = []hotkeyItem{
	{id: "panel", label: "翻译面板"},
	{id: "selection", label: "划词翻译"},
	{id: "ocr", label: "截图翻译"},
}

func (v *settingsView) renderHotkeys(c *ui.Context, p settingsPalette) {
	v.mu.Lock()
	cfg := cloneSettings(v.settings)
	listening := v.hotkeyListening
	conflicts := make(map[string]string, len(v.hotkeyConflicts))
	for k, val := range v.hotkeyConflicts {
		conflicts[k] = val
	}
	v.mu.Unlock()

	ui.Text(c, "快捷键").FontSize(18).FontWeight(600).TextColor(p.fg)

	ui.Row(c).FillWidth().Padding(11, 14).Radius(10).Background(p.field).Children(func() {
		ui.Text(c, "全局快捷键会覆盖其他应用中的相同组合。注册冲突时会自动尝试候选键位，并同步这里显示的当前生效值；激活快捷键按钮可更换，按 Esc 取消。").
			FontSize(12).LineHeight(1.6).TextColor(p.muted)
	})

	ui.Column(c).Radius(12).Background(p.card).Border(1, p.border).Clip().Children(func() {
		for i, item := range settingsHotkeys {
			current := hotkeyValue(cfg.Hotkeys, item.id)
			isListening := listening == item.id
			conflict := conflicts[item.id]
			divider := i < len(settingsHotkeys)-1

			ui.Column(c).FillWidth().Children(func() {
				ui.Row(c).FillWidth().Padding(13, 16).AlignItems(ui.Center).Gap(16).Children(func() {
					ui.Column(c).Grow(1).Gap(2).Children(func() {
						ui.Text(c, item.label).FontSize(13).FontWeight(500).TextColor(p.fg)
						if conflict != "" {
							ui.Text(c, conflict).FontSize(12).TextColor(p.error)
						}
					})

					btnText := current
					if isListening {
						btnText = "按下组合键…"
					}
					btn := ui.ButtonBase(c).
						Label(item.label+"快捷键").
						Tooltip("激活后按下新组合键").
						Cursor(ui.CursorPointer).
						MinWidth(136).
						Padding(7, 14).
						Radius(8).
						Center()
					if isListening {
						btn.Background(p.accentSoft).Border(1, p.accent)
					} else if btn.Hovered() {
						btn.Background(p.hover).Border(1, p.border)
					} else {
						btn.Background(p.field).Border(1, p.border)
					}
					if btn.Clicked() {
						v.mu.Lock()
						v.hotkeyConflicts[item.id] = ""
						v.hotkeyListening = item.id
						// Only defer capture if a key (e.g. Space/Enter activating the button) is still held.
						v.hotkeyCaptureArmed = len(c.KeysDown()) == 0
						v.mu.Unlock()
						c.AnimationFrame()
					}
					btn.Children(func() {
						col := p.fg
						if isListening {
							col = p.accentText
						}
						ui.Text(c, btnText).Font("monospace").FontSize(12).TextColor(col).SingleLine()
					})
				})
				if divider {
					ui.Box(c).FillWidth().Height(1).Background(p.border)
				}
			})
		}
	})
}

func (v *settingsView) handleHotkeyCapture(c *ui.Context, id string) {
	if c.Shortcut(0, ui.KeyEscape) {
		v.mu.Lock()
		v.hotkeyListening = ""
		v.hotkeyCaptureArmed = false
		v.mu.Unlock()
		return
	}
	mods := c.Modifiers()
	if mods&(ui.Ctrl|ui.Alt|ui.Shift) == 0 {
		return
	}
	var keyName string
	for _, k := range c.KeysDown() {
		if name := formatHotkeyKey(k); name != "" {
			keyName = name
			break
		}
	}
	if keyName == "" {
		return
	}
	var parts []string
	if mods&ui.Ctrl != 0 {
		parts = append(parts, "Ctrl")
	}
	if mods&ui.Alt != 0 {
		parts = append(parts, "Alt")
	}
	if mods&ui.Shift != 0 {
		parts = append(parts, "Shift")
	}
	parts = append(parts, keyName)
	accel := strings.Join(parts, "+")

	v.mu.Lock()
	current := hotkeyValue(v.settings.Hotkeys, id)
	v.hotkeyListening = ""
	v.hotkeyCaptureArmed = false
	v.mu.Unlock()

	if accel != current && v.actions.hotkeyBusy != nil && v.actions.hotkeyBusy(accel) {
		v.mu.Lock()
		v.hotkeyConflicts[id] = "该组合键已被其他应用占用"
		v.mu.Unlock()
		return
	}

	v.mu.Lock()
	next := cloneSettings(v.settings)
	setHotkeyValue(&next.Hotkeys, id, accel)
	v.enqueueSaveLocked(next)
	v.mu.Unlock()
}

func hotkeyValue(h config.HotkeySettings, id string) string {
	switch id {
	case "panel":
		return h.Panel
	case "selection":
		return h.Selection
	case "ocr":
		return h.Ocr
	default:
		return ""
	}
}

func setHotkeyValue(h *config.HotkeySettings, id, val string) {
	switch id {
	case "panel":
		h.Panel = val
	case "selection":
		h.Selection = val
	case "ocr":
		h.Ocr = val
	}
}

func formatHotkeyKey(k ui.Key) string {
	switch {
	case k >= ui.KeyA && k <= ui.KeyZ:
		return string(rune('A' + (k - ui.KeyA)))
	case k >= ui.Key0 && k <= ui.Key9:
		return string(rune('0' + (k - ui.Key0)))
	case k >= ui.KeyF1 && k <= ui.KeyF12:
		return fmt.Sprintf("F%d", int(k-ui.KeyF1)+1)
	case k == ui.KeySpace:
		return "Space"
	default:
		return ""
	}
}

func (v *settingsView) renderNaming(c *ui.Context, p settingsPalette) {
	v.mu.Lock()
	cfg := cloneSettings(v.settings)
	v.mu.Unlock()

	ui.Text(c, "命名转换").FontSize(18).FontWeight(600).TextColor(p.fg)

	ui.Column(c).Radius(12).Background(p.card).Border(1, p.border).Clip().Children(func() {
		autoSplit := cfg.Identifier.AutoSplit
		v.renderSettingRow(c, p, "自动拆分标识符", "英文输入自动识别 camelCase / snake_case / kebab-case 并拆词后翻译", true, func() {
			sw := ui.Switch(c, &autoSplit).Label("切换自动拆分标识符")
			if sw.Changed() {
				v.mu.Lock()
				next := cloneSettings(v.settings)
				next.Identifier.AutoSplit = autoSplit
				v.enqueueSaveLocked(next)
				v.mu.Unlock()
			}
		})

		stripComments := cfg.Identifier.StripCodeComments
		v.renderSettingRow(c, p, "自动去除代码注释符号", "去掉 //、/* … */ 和 # 等注释符号，保留注释正文", true, func() {
			sw := ui.Switch(c, &stripComments).Label("切换自动去除代码注释符号")
			if sw.Changed() {
				v.mu.Lock()
				next := cloneSettings(v.settings)
				next.Identifier.StripCodeComments = stripComments
				v.enqueueSaveLocked(next)
				v.mu.Unlock()
			}
		})

		flattenNewlines := cfg.Identifier.FlattenNewlines
		v.renderSettingRow(c, p, "自动将换行符替换为空格", "将换行处合并为一个空格", true, func() {
			sw := ui.Switch(c, &flattenNewlines).Label("切换自动将换行符替换为空格")
			if sw.Changed() {
				v.mu.Lock()
				next := cloneSettings(v.settings)
				next.Identifier.FlattenNewlines = flattenNewlines
				v.enqueueSaveLocked(next)
				v.mu.Unlock()
			}
		})

		stripMD := cfg.Identifier.StripMarkdown
		v.renderSettingRow(c, p, "自动去除 Markdown 格式", "去掉常见的标题、列表、链接和强调标记，保留文字", false, func() {
			sw := ui.Switch(c, &stripMD).Label("切换自动去除 Markdown 格式")
			if sw.Changed() {
				v.mu.Lock()
				next := cloneSettings(v.settings)
				next.Identifier.StripMarkdown = stripMD
				v.enqueueSaveLocked(next)
				v.mu.Unlock()
			}
		})
	})

	ui.Column(c).Padding(8, 0, 0, 0).Gap(4).Children(func() {
		ui.Text(c, "复制格式").FontSize(13).FontWeight(600).TextColor(p.fg)
		ui.Text(c, "英语译文下方会显示已开启的复制格式。至少开启一种。").FontSize(12).TextColor(p.muted)
	})

	ui.Column(c).Radius(12).Background(p.card).Border(1, p.border).Clip().Children(func() {
		for i, opt := range settingsCopyFormats {
			isChecked := slices.Contains(cfg.CopyFormats, opt.style)
			isOnlyOne := isChecked && len(cfg.CopyFormats) == 1
			divider := i < len(settingsCopyFormats)-1

			ui.Column(c).FillWidth().Children(func() {
				ui.Row(c).FillWidth().Padding(12, 16).AlignItems(ui.Center).Gap(12).Children(func() {
					ui.Text(c, opt.label).FontSize(13).FontWeight(500).TextColor(p.fg).Width(76)
					ui.Row(c).Padding(3, 9).Radius(6).Background(p.field).Children(func() {
						ui.Text(c, opt.example).Font("monospace").FontSize(11).TextColor(p.muted).SingleLine()
					})
					ui.Spacer(c)
					on := isChecked
					sw := ui.Switch(c, &on).Label("切换" + opt.label).Disabled(isOnlyOne)
					if sw.Changed() {
						v.mu.Lock()
						next := cloneSettings(v.settings)
						next.CopyFormats = slices.DeleteFunc(slices.Clone(next.CopyFormats), func(s string) bool {
							return s == opt.style
						})
						if on {
							next.CopyFormats = append(next.CopyFormats, opt.style)
						}
						v.enqueueSaveLocked(next)
						v.mu.Unlock()
					}
				})
				if divider {
					ui.Box(c).FillWidth().Height(1).Background(p.border)
				}
			})
		}
	})
}
