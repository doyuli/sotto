package settings

import (
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/egoist/mygo/ui"

	"sotto/internal/config"
	"sotto/internal/keys"
	"sotto/internal/languages"
	"sotto/internal/naming"
	"sotto/internal/platform"
	"sotto/internal/translate"
	"sotto/internal/ui/shared"
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

type Tab string

const (
	TabGeneral  Tab = "general"
	TabServices Tab = "services"
	TabHotkeys  Tab = "hotkeys"
	TabNaming   Tab = "naming"
)

type settingsNavItem struct {
	id    Tab
	label string
}

var settingsNavItems = []settingsNavItem{
	{id: TabGeneral, label: "通用"},
	{id: TabServices, label: "翻译服务"},
	{id: TabHotkeys, label: "快捷键"},
	{id: TabNaming, label: "命名转换"},
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

var defaultServiceSettings = config.Default().Services

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

func (v *View) Render(c *ui.Context) {
	p := settingsPaletteForTheme(c.Theme())
	c.SetTheme(p.theme)

	v.mu.Lock()
	toastMsg := v.pendingToast
	v.pendingToast = ""
	active := v.activeTab
	v.mu.Unlock()

	if toastMsg != "" {
		c.Toast(toastMsg)
	}

	c.Root().Background(p.panel).Padding(0).Children(func() {
		ui.Row(c).Fill().Children(func() {
			v.renderSidebar(c, p, active)
			ui.Box(c).Width(1).FillHeight().Background(p.border)
			ui.Scroll(c).Grow(1).FillHeight().Children(func() {
				ui.Column(c).Padding(24, 28).MaxWidth(620).Gap(14).Children(func() {
					switch active {
					case TabGeneral:
						v.renderGeneral(c, p)
					case TabServices:
						v.renderServices(c, p)
					case TabHotkeys:
						v.renderHotkeys(c, p)
					case TabNaming:
						v.renderNaming(c, p)
					}
				})
			})
		})
	})
}

func (v *View) renderSidebar(c *ui.Context, p settingsPalette, active Tab) {
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
				v.SetActiveTab(item.id)
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
	})
}

func (v *View) renderGeneral(c *ui.Context, p settingsPalette) {
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
				return shared.LanguageName(code)
			}
			trigger.Children(func() {
				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(6).Children(func() {
					ui.Text(c, languageLabel(selected)).Grow(1).FontSize(12).TextColor(p.fg).SingleLine()
					ui.Icon(c, shared.ChevronDown).Size(12, 12).TextColor(p.muted)
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

		minimalTranslation := cfg.MinimalTranslation
		v.renderSettingRow(c, p, "简约翻译模式", "划词和截图时仅显示译文，使用排在首位的已启用服务。", true, func() {
			ui.Switch(c, &minimalTranslation).Label("切换简约翻译模式").OnClick(func() {
				v.mu.Lock()
				defer v.mu.Unlock()
				v.enqueueMinimalTranslationLocked(minimalTranslation)
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
		v.renderSettingRow(c, p, "自动更新", platform.UpdateDescription(), false, func() {
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

func (v *View) renderSettingRow(c *ui.Context, p settingsPalette, title, sub string, divider bool, control func()) {
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

func (v *View) renderServices(c *ui.Context, p settingsPalette) {
	v.mu.Lock()
	loaded := v.credentialsLoaded
	credErr := v.credentialErrors[""]
	readingCredentials := v.credentialReads[""]
	cfg := cloneSettings(v.settings)
	var enabledNames, disabledNames []string
	for _, svc := range cfg.Services.Order {
		if _, ok := serviceCredentialFields[svc]; !ok {
			continue
		}
		if v.serviceOnLocked(svc) {
			enabledNames = append(enabledNames, svc)
		} else {
			disabledNames = append(disabledNames, svc)
		}
	}
	displayEnabled := enabledNames
	if sameServiceSet(v.drag.preview, enabledNames) {
		displayEnabled = slices.Clone(v.drag.preview)
	} else {
		v.drag.preview = nil
	}
	v.mu.Unlock()

	ui.Text(c, "翻译服务").FontSize(18).FontWeight(600).TextColor(p.fg)

	if !loaded || credErr != "" {
		ui.Row(c).Padding(14).Radius(12).Background(p.card).Border(1, p.border).AlignItems(ui.Center).Gap(12).Children(func() {
			msg := "正在读取服务状态…"
			color := p.muted
			if credErr != "" {
				msg = credErr
				color = p.error
			}
			ui.Text(c, msg).FontSize(12).TextColor(color).Grow(1)
			if credErr != "" {
				v.renderSecondaryButtonDisabled(c, p, "重试", "重试", readingCredentials, v.RefreshCredentialsAsync)
			}
		})
		if !loaded {
			return
		}
	}

	description := "已启用的服务同时翻译，拖动序号调整结果顺序，"
	if cfg.MinimalTranslation {
		description = "简约翻译使用首个已启用服务，拖动序号调整顺序，"
	}
	ui.Text(c, description+"凭证保存在系统密钥环。").FontSize(12).TextColor(p.muted)

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

func (v *View) renderServiceGroupHeader(c *ui.Context, p settingsPalette, title string, count int) {
	ui.Row(c).Padding(6, 0, 0, 0).AlignItems(ui.Center).Gap(6).Children(func() {
		ui.Text(c, title).FontSize(12).FontWeight(600).TextColor(p.muted)
		ui.Row(c).Padding(1, 7).Radius(999).Background(p.pill).Children(func() {
			ui.Text(c, fmt.Sprintf("%d", count)).FontSize(11).TextColor(p.badgeInk)
		})
	})
}

func (v *View) renderServiceCard(c *ui.Context, p settingsPalette, svc string, enabled bool, position int, displayEnabled []string) (bool, string) {
	title := shared.ServiceName(svc)
	fields := serviceCredentialFields[svc]
	keyless := len(fields) == 0

	v.mu.Lock()
	credentialError := v.credentialErrors[svc] != ""
	isConfigured := keyless || slices.Contains(v.credentials.Configured, svc)
	editable := !keyless || svc == "openai" || v.settings.Services.Model(svc) != nil
	isExpanded := v.expanded[svc]
	_, dirty := v.serviceDrafts[svc]
	busy := v.serviceSaving[svc]
	dragAnimating := v.drag.preview != nil
	v.mu.Unlock()

	stateHint := "填写凭证后可启用"
	if enabled {
		if keyless {
			stateHint = "无需凭据"
		} else {
			stateHint = "凭证已保存"
		}
	} else if keyless || isConfigured {
		stateHint = "打开开关后参与翻译"
	}

	if credentialError {
		stateHint = "凭证状态待确认"
	}
	if !isExpanded && dirty {
		stateHint = "有未保存的更改"
	}
	if !isExpanded && busy {
		stateHint = "正在保存配置…"
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
		if editable && isExpanded {
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
						if keyless {
							badgeText = "免配置"
							badgeBg = p.pill
							badgeFg = p.badgeInk
						} else if credentialError {
							badgeText = "读取失败"
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
				if editable {
					cfgLabel := "配置"
					accessLabel := "配置" + title
					if isExpanded {
						cfgLabel = "收起"
						accessLabel = "收起" + title
					}
					v.renderSecondaryButtonDisabled(c, p, cfgLabel, accessLabel, false, func() {
						v.mu.Lock()
						nextExpanded := !v.expanded[svc]
						v.expanded[svc] = nextExpanded
						loadUsage := nextExpanded && svc == "deepl" && slices.Contains(v.credentials.Stored, keys.DeeplKey)
						v.mu.Unlock()
						if loadUsage {
							v.loadDeeplUsageAsync()
						}
					})
				}

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
					Disabled(busy || (!enabled && (!isConfigured || credentialError)))
				if sw.Changed() {
					v.mu.Lock()
					if on && v.credentialErrors[svc] != "" {
						v.mu.Unlock()
						return
					}
					v.focusService = svc
					next := cloneSettings(v.settings)
					next.Services.Enabled = slices.DeleteFunc(slices.Clone(next.Services.Enabled), func(s string) bool {
						return s == svc
					})
					if on {
						next.Services.Enabled = append(next.Services.Enabled, svc)
					} else {
						v.expanded[svc] = false
					}
					v.enqueueSaveLocked(next)
					v.mu.Unlock()
				}
			})
		})

		if editable && isExpanded {
			v.renderServiceDrawer(c, p, svc, fields, enabled, isConfigured)
		}
	})
	return dragging, overTarget
}

func (v *View) renderServiceDrawer(c *ui.Context, p settingsPalette, svc string, fields []serviceCredentialField, enabled, isConfigured bool) {
	v.mu.Lock()
	busy := v.serviceSaving[svc]
	status := v.testStatuses[svc]
	credErr := v.credentialErrors[svc]
	readingCredentials := v.credentialReads[svc]
	locked := busy || status.testing
	cfg := v.settings
	saved := cfg.Services
	draft, dirty := v.serviceDrafts[svc]
	draft.credentials = maps.Clone(draft.credentials)
	stored := v.credentials.Stored
	deeplText := v.deeplUsageText
	deeplPct := v.deeplUsagePct
	v.mu.Unlock()

	willDisable := enabled && slices.ContainsFunc(fields, func(field serviceCredentialField) bool {
		return draft.credentials[field.name].remove && !slices.Contains(translate.WithoutCredential(cfg, field.name), svc)
	})
	ui.Box(c).FillWidth().Height(1).Background(p.border)
	ui.Column(c).FillWidth().Background(p.card).Padding(14, 16).Gap(12).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Text(c, "保存后应用更改").FontSize(12).TextColor(p.muted).Grow(1)
			if len(fields) != 0 {
				link := ui.ButtonBase(c.Key("console")).Label("前往控制台获取凭证").Cursor(ui.CursorPointer).Padding(2, 0)
				link.OnClick(func() { v.openKeyPageAsync(svc) })
				link.Children(func() {
					txt := ui.Text(c, "前往控制台获取凭证").FontSize(12).TextColor(p.accentText)
					if link.Hovered() {
						txt.Underline()
					}
				})
			}
		})

		if credErr != "" {
			ui.Row(c.Key("credential-error")).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
				ui.Text(c, credErr).FontSize(12).TextColor(p.error).Grow(1)
				v.renderSecondaryButtonDisabled(c, p, "重试", "重试读取"+shared.ServiceName(svc)+"状态", readingCredentials, func() {
					v.refreshCredentialsAsync(svc)
				})
			})
		}

		if svc == "openai" {
			value := saved.OpenAI.BaseURL
			if draft.baseURL != nil {
				value = *draft.baseURL
			}
			v.renderServiceField(c.Key("baseURL"), p, svc, serviceEditorField{
				label: "Base URL", help: "兼容接口地址",
				placeholder: defaultServiceSettings.OpenAI.BaseURL, value: value,
			}, locked, func(value string) { v.editServiceBaseURL(svc, value) })
		}
		if model := saved.Model(svc); model != nil {
			value := *model
			if draft.model != nil {
				value = *draft.model
			}
			v.renderServiceField(c.Key("model"), p, svc, serviceEditorField{
				label: "模型", help: "留空恢复默认模型",
				placeholder: *defaultServiceSettings.Model(svc), value: value,
			}, locked, func(value string) { v.editServiceModel(svc, value) })
		}

		for _, field := range fields {
			isStored := slices.Contains(stored, field.name)
			edit := draft.credentials[field.name]
			value := edit.value
			help := ""
			if field.name == keys.AzureRegion {
				help = "可选，全局资源留空"
			}
			placeholder := field.placeholder
			if isStored {
				placeholder = "已保存，留空保留"
			}
			if edit.remove {
				placeholder = "保存后清除"
				value = ""
			}
			v.renderServiceField(c.Key(field.name), p, svc, serviceEditorField{
				credential: field.name, label: field.label, help: help, placeholder: placeholder, value: value,
				stored: isStored, remove: edit.remove,
			}, locked, func(value string) { v.editServiceCredential(svc, field.name, value) })
		}

		if note, ok := serviceNotes[svc]; ok {
			ui.Text(c, note).FontSize(12).TextColor(p.muted)
		}

		if svc == "deepl" && slices.Contains(stored, keys.DeeplKey) {
			ui.Column(c).FillWidth().Gap(4).Children(func() {
				ui.Text(c, deeplText).FontSize(12).TextColor(p.muted)
				ui.Progress(c, deeplPct).FillWidth().Height(6)
			})
		}

		ui.Box(c).FillWidth().Height(1).Background(p.border)
		ui.Column(c).FillWidth().Gap(8).Children(func() {
			message, color := status.result, p.muted
			switch {
			case busy:
				message = "正在保存配置…"
			case status.isError:
				color = p.error
			case willDisable:
				message, color = "保存后将停用此服务", p.warning
			case dirty:
				message = "有未保存的更改"
			case message != "" && !status.testing:
				color = p.success
			}
			if message != "" {
				ui.Text(c.Key("status"), message).FontSize(12).TextColor(color)
			}
			ui.Row(c.Key("actions")).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
				v.renderSecondaryButtonDisabled(c.Key("test"), p, "测试连接", "测试连接", !isConfigured || locked || dirty || credErr != "", func() {
					v.testConnection(svc)
				})
				ui.Box(c).Grow(1)
				v.renderSecondaryButtonDisabled(c.Key("discard"), p, "放弃更改", "放弃更改", !dirty || locked, func() {
					v.discardService(svc)
				})

				disabled := !dirty || locked
				save := ui.ButtonBase(c.Key("save")).
					Label("保存配置").
					Disabled(disabled).
					Cursor(ui.CursorPointer).
					Padding(6, 14).
					Radius(8).
					Background(p.primaryBg)
				ink := p.onAccent
				if disabled {
					save.Background(p.control)
					ink = p.muted
				} else if save.Hovered() {
					save.Background(p.primaryBg.Mix(p.onAccent, 0.08))
				}
				save.OnClick(func() { v.saveService(svc) })
				save.Children(func() {
					ui.Text(c, "保存配置").FontSize(12).FontWeight(600).TextColor(ink).SingleLine()
				})
			})
		})
	})
}

type serviceEditorField struct {
	credential, label, help, placeholder, value string
	stored, remove                              bool
}

func (v *View) renderServiceField(c *ui.Context, p settingsPalette, svc string, field serviceEditorField, locked bool, onChange func(string)) {
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(12).Children(func() {
		ui.Column(c).Width(136).Gap(2).Children(func() {
			ui.Text(c, field.label).FontSize(13).FontWeight(500).TextColor(p.fg)
			if field.help != "" {
				ui.Text(c, field.help).FontSize(12).TextColor(p.muted)
			}
		})
		input := ui.TextInput(c.Key("input"), &field.value).
			Label("编辑"+field.label).
			Placeholder(field.placeholder).
			Disabled(locked).
			Grow(1).
			Padding(7, 10).
			Radius(8).
			FontSize(13)
		if field.credential != "" {
			input.Password()
		}
		if input.Changed() && !locked {
			onChange(field.value)
		}
		if field.stored {
			action := "清除"
			if field.remove {
				action = "撤销清除"
			}
			v.renderSecondaryButtonDisabled(c.Key("remove"), p, action, action+field.label, locked, func() {
				v.toggleCredentialRemoval(svc, field.credential)
			})
		}
	})
}

func (v *View) renderSecondaryButton(c *ui.Context, p settingsPalette, label string, onClick func()) ui.Element {
	return v.renderSecondaryButtonDisabled(c, p, label, label, false, onClick)
}

func (v *View) renderSecondaryButtonDisabled(c *ui.Context, p settingsPalette, textLabel, accessLabel string, disabled bool, onClick func()) ui.Element {
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

func (v *View) renderNaming(c *ui.Context, p settingsPalette) {
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
