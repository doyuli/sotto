package main

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"

	"sotto/internal/config"
	"sotto/internal/keys"
	"sotto/internal/languages"
	"sotto/internal/naming"
)

var (
	shieldSVG  = ui.MustParseSVG([]byte(`<svg viewBox="0 0 24 24"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>`))
	arrowUpSVG = ui.MustParseSVG([]byte(`<svg viewBox="0 0 24 24"><path d="m18 15-6-6-6 6" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>`))
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

var defaultServiceModels = map[string]string{
	"openai":      "gpt-4o-mini",
	"siliconflow": "tencent/Hunyuan-MT-7B",
	"zhipu":       "glm-4.7-flash",
}

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

type settingsActions struct {
	getSettings  func() config.Settings
	saveSettings func(config.Settings) error
	credentials  func() (CredentialStatus, error)
	setSecret    func(name, value string) error
	testService  func(ctx context.Context, name string) string
	openKeyPage  func(service string) error
	deeplUsage   func() (DeeplUsage, error)
	hotkeyBusy   func(accel string) bool
	invalidate   func()
}

type serviceTestStatus struct {
	testing bool
	result  string
	isError bool
}

type settingsView struct {
	mu      sync.Mutex
	actions settingsActions

	activeTab     settingsTab
	settings      config.Settings
	pendingWrites int
	saveFailed    bool
	savePending   []config.Settings
	saveWake      chan struct{}
	saveStop      chan struct{}
	pendingToast  string
	disposed      bool

	credentialsLoaded bool
	credentials       CredentialStatus
	credentialsErr    string

	expanded       map[string]bool
	serviceDrafts  map[string]map[string]string
	baseURLDraft   *string
	modelDrafts    map[string]string
	credentialBusy map[string]bool
	testStatuses   map[string]serviceTestStatus
	deeplUsageText string
	deeplUsagePct  float64
	orderStatus    string

	hotkeyListening    string
	hotkeyConflicts    map[string]string
	hotkeyCaptureArmed bool
}

func newSettingsView(initialTab settingsTab, cfg config.Settings, actions settingsActions) *settingsView {
	if initialTab == "" {
		initialTab = tabGeneral
	}
	v := &settingsView{
		actions:         actions,
		activeTab:       initialTab,
		settings:        cloneSettings(cfg),
		saveWake:        make(chan struct{}, 1),
		saveStop:        make(chan struct{}),
		expanded:        make(map[string]bool),
		serviceDrafts:   make(map[string]map[string]string),
		modelDrafts:     make(map[string]string),
		credentialBusy:  make(map[string]bool),
		testStatuses:    make(map[string]serviceTestStatus),
		deeplUsageText:  "额度查询中…",
		hotkeyConflicts: make(map[string]string),
	}
	go v.runSaveLoop()
	v.refreshCredentialsAsync()
	return v
}

func cloneSettings(cfg config.Settings) config.Settings {
	out := cfg
	out.Services.Enabled = slices.Clone(cfg.Services.Enabled)
	out.Services.Order = slices.Clone(cfg.Services.Order)
	out.CopyFormats = slices.Clone(cfg.CopyFormats)
	return out
}

func (v *settingsView) dispose() {
	v.mu.Lock()
	if v.disposed {
		v.mu.Unlock()
		return
	}
	v.flushTextDraftsLocked()
	v.disposed = true
	close(v.saveStop)
	v.mu.Unlock()
}

func (v *settingsView) invalidate() {
	if v.actions.invalidate != nil {
		v.actions.invalidate()
	}
}

func (v *settingsView) setActiveTab(tab settingsTab) {
	v.mu.Lock()
	if tab != tabHotkeys {
		v.hotkeyListening = ""
		v.hotkeyCaptureArmed = false
	}
	if tab != tabServices {
		v.flushTextDraftsLocked()
	}
	v.activeTab = tab
	disposed := v.disposed
	v.mu.Unlock()
	if !disposed {
		v.invalidate()
	}
}

func (v *settingsView) applyExternalSettings(cfg config.Settings) {
	v.mu.Lock()
	disposed := v.disposed
	if v.pendingWrites == 0 && !disposed {
		v.settings = cloneSettings(cfg)
	}
	v.mu.Unlock()
	if !disposed {
		v.invalidate()
	}
}

func (v *settingsView) flushTextDraftsLocked() {
	if v.baseURLDraft == nil && len(v.modelDrafts) == 0 {
		return
	}
	next := cloneSettings(v.settings)
	if v.baseURLDraft != nil {
		next.Services.OpenAI.BaseURL = strings.TrimSpace(*v.baseURLDraft)
		v.baseURLDraft = nil
	}
	for svc, val := range v.modelDrafts {
		trimmed := strings.TrimSpace(val)
		switch svc {
		case "openai":
			next.Services.OpenAI.Model = trimmed
		case "siliconflow":
			next.Services.SiliconFlow.Model = trimmed
		case "zhipu":
			next.Services.Zhipu.Model = trimmed
		}
		delete(v.modelDrafts, svc)
	}
	v.enqueueSaveLocked(next)
}

func (v *settingsView) enqueueSaveLocked(next config.Settings) {
	v.settings = cloneSettings(next)
	if v.disposed {
		return
	}
	v.pendingWrites++
	v.savePending = append(v.savePending, cloneSettings(next))
	select {
	case v.saveWake <- struct{}{}:
	default:
	}
}

func (v *settingsView) runSaveLoop() {
	for {
		v.mu.Lock()
		if len(v.savePending) == 0 {
			disposed := v.disposed
			v.mu.Unlock()
			if disposed {
				return
			}
			select {
			case <-v.saveWake:
			case <-v.saveStop:
			}
			continue
		}
		snap := v.savePending[0]
		v.savePending = v.savePending[1:]
		v.mu.Unlock()

		var err error
		if v.actions.saveSettings != nil {
			err = v.actions.saveSettings(snap)
		}
		v.mu.Lock()
		v.pendingWrites--
		v.saveFailed = err != nil
		pending := v.pendingWrites
		disposed := v.disposed
		if err != nil {
			v.pendingToast = "保存失败：" + err.Error()
		}
		if pending == 0 && !disposed && v.actions.getSettings != nil {
			v.settings = cloneSettings(v.actions.getSettings())
		}
		v.mu.Unlock()
		if !disposed {
			v.invalidate()
		}
	}
}

func (v *settingsView) refreshCredentialsAsync() {
	if v.actions.credentials == nil {
		return
	}
	go func() {
		status, err := v.actions.credentials()
		v.mu.Lock()
		disposed := v.disposed
		if !disposed {
			if err != nil {
				v.credentialsErr = "读取服务状态失败：" + err.Error()
				v.pendingToast = v.credentialsErr
			} else {
				v.credentialsLoaded = true
				v.credentials = status
				v.credentialsErr = ""
			}
		}
		v.mu.Unlock()
		if !disposed {
			v.invalidate()
		}
	}()
}

func (v *settingsView) loadDeeplUsageAsync() {
	if v.actions.deeplUsage == nil {
		return
	}
	v.mu.Lock()
	v.deeplUsageText = "额度查询中…"
	v.deeplUsagePct = 0
	v.mu.Unlock()
	go func() {
		usage, err := v.actions.deeplUsage()
		v.mu.Lock()
		disposed := v.disposed
		if !disposed {
			if err != nil {
				v.deeplUsageText = "额度查询失败"
			} else {
				pct := 0.0
				if usage.Limit > 0 {
					pct = min(float64(usage.Used)/float64(usage.Limit), 1.0)
				}
				v.deeplUsagePct = pct
				v.deeplUsageText = fmt.Sprintf("本月已用 %d / %d 字符", usage.Used, usage.Limit)
			}
		}
		v.mu.Unlock()
		if !disposed {
			v.invalidate()
		}
	}()
}

func (v *settingsView) serviceOnLocked(name string) bool {
	fields := serviceCredentialFields[name]
	keyless := len(fields) == 0
	return slices.Contains(v.settings.Services.Enabled, name) &&
		(keyless || slices.Contains(v.credentials.Configured, name))
}

func (v *settingsView) hasServiceDraftLocked(name string) bool {
	for _, val := range v.serviceDrafts[name] {
		if strings.TrimSpace(val) != "" {
			return true
		}
	}
	return false
}

func (v *settingsView) saveCredentials(name string) {
	v.mu.Lock()
	if v.credentialBusy[name] || v.testStatuses[name].testing || !v.hasServiceDraftLocked(name) {
		v.mu.Unlock()
		return
	}
	fields := serviceCredentialFields[name]
	toWrite := make(map[string]string)
	for _, f := range fields {
		if val := strings.TrimSpace(v.serviceDrafts[name][f.name]); val != "" {
			toWrite[f.name] = val
		}
	}
	v.credentialBusy[name] = true
	v.testStatuses[name] = serviceTestStatus{}
	v.mu.Unlock()
	v.invalidate()

	go func() {
		var writeErr error
		for _, f := range fields {
			val, ok := toWrite[f.name]
			if !ok {
				continue
			}
			if v.actions.setSecret != nil {
				if err := v.actions.setSecret(f.name, val); err != nil {
					writeErr = err
					break
				}
			}
		}
		var status CredentialStatus
		var credErr error
		if v.actions.credentials != nil {
			status, credErr = v.actions.credentials()
		}

		v.mu.Lock()
		disposed := v.disposed
		if !disposed {
			v.credentialBusy[name] = false
			if writeErr != nil {
				v.testStatuses[name] = serviceTestStatus{
					result:  "更新失败：" + writeErr.Error(),
					isError: true,
				}
			} else {
				for key := range toWrite {
					if v.serviceDrafts[name] != nil {
						delete(v.serviceDrafts[name], key)
					}
				}
				v.testStatuses[name] = serviceTestStatus{result: "凭证已保存"}
			}
			if credErr != nil {
				v.credentialsErr = "读取服务状态失败：" + credErr.Error()
				v.pendingToast = v.credentialsErr
			} else if v.actions.credentials != nil {
				v.credentialsLoaded = true
				v.credentials = status
				v.credentialsErr = ""
			}
		}
		loadUsage := !disposed && name == "deepl" && slices.Contains(v.credentials.Stored, keys.DeeplKey)
		v.mu.Unlock()
		if loadUsage {
			v.loadDeeplUsageAsync()
		}
		if !disposed {
			v.invalidate()
		}
	}()
}

func (v *settingsView) clearSecret(name, key string) {
	v.mu.Lock()
	if v.credentialBusy[name] || v.testStatuses[name].testing {
		v.mu.Unlock()
		return
	}
	v.credentialBusy[name] = true
	v.testStatuses[name] = serviceTestStatus{}
	v.mu.Unlock()
	v.invalidate()

	go func() {
		var writeErr error
		if v.actions.setSecret != nil {
			writeErr = v.actions.setSecret(key, "")
		}
		var status CredentialStatus
		var credErr error
		if v.actions.credentials != nil {
			status, credErr = v.actions.credentials()
		}
		var refreshedSettings *config.Settings
		if v.actions.getSettings != nil {
			s := v.actions.getSettings()
			refreshedSettings = &s
		}

		v.mu.Lock()
		disposed := v.disposed
		if !disposed {
			v.credentialBusy[name] = false
			if writeErr != nil {
				v.testStatuses[name] = serviceTestStatus{
					result:  "更新失败：" + writeErr.Error(),
					isError: true,
				}
			} else {
				if v.serviceDrafts[name] != nil {
					delete(v.serviceDrafts[name], key)
				}
				v.testStatuses[name] = serviceTestStatus{result: "凭证已清除"}
			}
			if credErr != nil {
				v.credentialsErr = "读取服务状态失败：" + credErr.Error()
				v.pendingToast = v.credentialsErr
			} else if v.actions.credentials != nil {
				v.credentialsLoaded = true
				v.credentials = status
				v.credentialsErr = ""
			}
			if refreshedSettings != nil && v.pendingWrites == 0 {
				v.settings = cloneSettings(*refreshedSettings)
			}
		}
		v.mu.Unlock()
		if !disposed {
			v.invalidate()
		}
	}()
}

func (v *settingsView) testConnection(name string) {
	v.mu.Lock()
	if v.credentialBusy[name] || v.testStatuses[name].testing || v.hasServiceDraftLocked(name) {
		v.mu.Unlock()
		return
	}
	v.testStatuses[name] = serviceTestStatus{testing: true, result: "正在测试…"}
	v.mu.Unlock()
	v.invalidate()

	go func() {
		// Wait briefly for any queued settings writes (e.g. OpenAI model/baseURL) before testing.
		var disposed, saveFailed bool
		for {
			v.mu.Lock()
			pending := v.pendingWrites
			saveFailed = v.saveFailed
			disposed = v.disposed
			v.mu.Unlock()
			if pending == 0 || disposed {
				break
			}
			time.Sleep(15 * time.Millisecond)
		}
		if disposed {
			return
		}
		if saveFailed {
			v.mu.Lock()
			disposed = v.disposed
			if !disposed {
				v.testStatuses[name] = serviceTestStatus{result: "设置保存失败，请重新保存后测试", isError: true}
			}
			v.mu.Unlock()
			if !disposed {
				v.invalidate()
			}
			return
		}
		var errMsg string
		if v.actions.testService != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			errMsg = v.actions.testService(ctx, name)
			cancel()
		}
		v.mu.Lock()
		disposed = v.disposed
		if !disposed {
			if errMsg != "" {
				v.testStatuses[name] = serviceTestStatus{result: errMsg, isError: true}
			} else {
				v.testStatuses[name] = serviceTestStatus{result: "连接成功"}
			}
		}
		v.mu.Unlock()
		if !disposed {
			v.invalidate()
		}
	}()
}

func (v *settingsView) moveServiceRelative(name string, delta int) {
	v.mu.Lock()
	enabled := make([]string, 0, len(v.settings.Services.Order))
	for _, s := range v.settings.Services.Order {
		if _, ok := serviceCredentialFields[s]; ok && v.serviceOnLocked(s) {
			enabled = append(enabled, s)
		}
	}
	idx := slices.Index(enabled, name)
	targetIdx := idx + delta
	if idx < 0 || targetIdx < 0 || targetIdx >= len(enabled) {
		v.mu.Unlock()
		return
	}
	targetName := enabled[targetIdx]
	v.mu.Unlock()
	v.moveService(name, targetName, delta > 0)
}

func (v *settingsView) moveService(name, target string, after bool) {
	v.mu.Lock()
	order := slices.Clone(v.settings.Services.Order)
	from := slices.Index(order, name)
	targetIdx := slices.Index(order, target)
	if from < 0 || targetIdx < 0 {
		v.mu.Unlock()
		return
	}
	insertion := targetIdx
	if after {
		insertion++
	}
	to := insertion
	if from < insertion {
		to = insertion - 1
	}
	if from == to {
		v.mu.Unlock()
		return
	}
	order = slices.Delete(order, from, from+1)
	order = slices.Insert(order, to, name)

	pos := 0
	for i := 0; i <= to && i < len(order); i++ {
		if v.serviceOnLocked(order[i]) {
			pos++
		}
	}
	v.orderStatus = fmt.Sprintf("%s 已移至第 %d 位", bubbleServiceName(name), pos)
	next := cloneSettings(v.settings)
	next.Services.Order = order
	v.enqueueSaveLocked(next)
	v.mu.Unlock()
	v.invalidate()
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
				ui.Column(c).Padding(20).MaxWidth(600).Gap(12).Children(func() {
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
	ui.Column(c).Width(170).FillHeight().Background(p.sidebar).Padding(12).Gap(4).Children(func() {
		for _, item := range settingsNavItems {
			isActive := item.id == active
			btn := ui.ButtonBase(c).
				Label(item.label).
				Cursor(ui.CursorPointer).
				FillWidth().
				Radius(8).
				Padding(8, 10)
			if isActive {
				btn.Background(p.control).Border(1, p.border)
			} else if btn.Hovered() {
				btn.Background(p.hover).Border(1, ui.Transparent)
			} else {
				btn.Border(1, ui.Transparent)
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
				ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
					ui.Text(c, item.label).FontSize(12).FontWeight(weight).TextColor(color).SingleLine()
				})
			})
		}

		ui.Spacer(c)

		ui.Row(c).
			FillWidth().
			Padding(6, 10).
			Radius(9).
			Background(p.field).
			Border(1, p.border).
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

	ui.Text(c, "通用").FontSize(16).FontWeight(600).TextColor(p.fg)

	ui.Column(c).Radius(12).Background(p.card).Border(1, p.border).Clip().Children(func() {
		// Target language
		v.renderSettingRow(c, p, "默认翻译到", "未指定目标时气泡首选的翻译语言", true, func() {
			label := "自动（中文↔英文）"
			if cfg.TargetLanguage != "auto" {
				label = bubbleLangName(cfg.TargetLanguage)
			}
			sel := ui.ButtonBase(c).
				Label("默认翻译到").
				Cursor(ui.CursorPointer).
				Padding(6, 10).
				Radius(8).
				Background(p.field).
				Border(1, p.border).
				Menu(func(m *ui.Menu) {
					if m.Item("自动（中文↔英文）").Checked(cfg.TargetLanguage == "auto").Chosen() {
						v.mu.Lock()
						next := cloneSettings(v.settings)
						next.TargetLanguage = "auto"
						v.enqueueSaveLocked(next)
						v.mu.Unlock()
					}
					m.Separator()
					for _, code := range languages.List {
						if m.Item(bubbleLangName(code)).Checked(cfg.TargetLanguage == code).Chosen() {
							v.mu.Lock()
							next := cloneSettings(v.settings)
							next.TargetLanguage = code
							v.enqueueSaveLocked(next)
							v.mu.Unlock()
						}
					}
				})
			if sel.Hovered() {
				sel.Background(p.hover)
			}
			sel.Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
					ui.Text(c, label).FontSize(12).TextColor(p.fg).SingleLine()
					ui.Icon(c, chevronDownSVG).Size(12, 12).TextColor(p.muted)
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
		ui.Row(c).FillWidth().Padding(11, 14).AlignItems(ui.Center).Gap(16).Children(func() {
			ui.Column(c).Grow(1).Gap(2).Children(func() {
				ui.Text(c, title).FontSize(12).FontWeight(600).TextColor(p.fg)
				if sub != "" {
					ui.Text(c, sub).FontSize(11).TextColor(p.muted)
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
	orderStatus := v.orderStatus
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
	v.mu.Unlock()

	ui.Text(c, "翻译服务").FontSize(16).FontWeight(600).TextColor(p.fg)

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

	ui.Column(c).Gap(2).Children(func() {
		ui.Text(c, "已启用的服务会同时翻译，序号即结果顺序。").FontSize(12).TextColor(p.fg)
		ui.Text(c, "拖动序号或点击上下按钮调整顺序，凭证保存在系统密钥环。").FontSize(11).TextColor(p.muted)
		if orderStatus != "" {
			ui.Text(c, orderStatus).FontSize(11).TextColor(p.accentText)
		}
	})

	v.renderServiceGroupHeader(c, p, "已启用", len(enabledNames))
	if len(enabledNames) > 0 {
		ui.Column(c).Gap(8).Children(func() {
			for i, svc := range enabledNames {
				v.renderServiceCard(c.Key("svc:"+svc), p, svc, true, i+1, len(enabledNames))
			}
		})
	} else {
		ui.Row(c).Padding(14).Radius(12).Background(p.field).Border(1, p.border).Children(func() {
			ui.Text(c, "还没有启用的服务。打开下方开关即可加入翻译。").FontSize(12).TextColor(p.muted)
		})
	}

	if len(disabledNames) > 0 {
		v.renderServiceGroupHeader(c, p, "未启用", len(disabledNames))
		ui.Column(c).Gap(8).Children(func() {
			for _, svc := range disabledNames {
				v.renderServiceCard(c.Key("svc:"+svc), p, svc, false, 0, 0)
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

func (v *settingsView) renderServiceCard(c *ui.Context, p settingsPalette, svc string, enabled bool, position, totalEnabled int) {
	title := bubbleServiceName(svc)
	fields := serviceCredentialFields[svc]
	keyless := len(fields) == 0

	v.mu.Lock()
	isConfigured := slices.Contains(v.credentials.Configured, svc)
	isExpanded := v.expanded[svc]
	busy := v.credentialBusy[svc]
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

	card := ui.Column(c).Radius(12).Background(p.card).Border(1, p.border).Clip()
	if enabled {
		if dragged, ok := ui.Drop[string](card); ok && dragged != "" && dragged != svc {
			v.mu.Lock()
			fromIdx := slices.Index(v.settings.Services.Order, dragged)
			toIdx := slices.Index(v.settings.Services.Order, svc)
			v.mu.Unlock()
			v.moveService(dragged, svc, toIdx > fromIdx)
		} else if _, over := ui.DragOver[string](card); over {
			card.Border(1, p.accent)
		}
	}

	card.Children(func() {
		ui.Row(c).FillWidth().Padding(11, 14).AlignItems(ui.Center).Gap(12).Children(func() {
			if enabled {
				grip := ui.Row(c).
					Label("拖动"+title).
					Size(24, 24).
					Radius(6).
					Background(p.field).
					Border(1, p.border).
					Center().
					Cursor(ui.CursorPointer).
					Tooltip("拖动排序").
					Drag(svc)
				grip.Children(func() {
					ui.Text(c, fmt.Sprintf("%d", position)).FontSize(11).FontWeight(600).TextColor(p.muted)
				})
			}

			ui.Column(c).Grow(1).Gap(2).Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
					ui.Text(c, title).FontSize(13).FontWeight(600).TextColor(p.fg)
					if !enabled {
						badgeText := "未配置"
						badgeBg := p.warningSoft
						badgeFg := p.warning
						if keyless {
							badgeText = "免配置"
							badgeBg = p.pill
							badgeFg = p.badgeInk
						} else if isConfigured {
							badgeText = "已配置"
							badgeBg = p.accentSoft
							badgeFg = p.accentText
						}
						ui.Row(c).Padding(1, 7).Radius(999).Background(badgeBg).Children(func() {
							ui.Text(c, badgeText).FontSize(10).FontWeight(600).TextColor(badgeFg).SingleLine()
						})
					}
				})
				ui.Text(c, stateHint).FontSize(11).TextColor(p.muted)
			})

			ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
				if enabled && totalEnabled > 1 {
					upBtn := ui.ButtonBase(c).
						Label("上移"+title).
						Tooltip("上移").
						Disabled(position <= 1).
						Cursor(ui.CursorPointer).
						Size(24, 24).
						Radius(6).
						Center()
					if position > 1 && upBtn.Hovered() {
						upBtn.Background(p.hover)
					}
					if upBtn.Clicked() {
						v.moveServiceRelative(svc, -1)
					}
					upBtn.Children(func() {
						col := p.muted
						if position <= 1 {
							col = p.muted.Alpha(0.35)
						}
						ui.Icon(c, arrowUpSVG).Size(13, 13).TextColor(col)
					})

					downBtn := ui.ButtonBase(c).
						Label("下移"+title).
						Tooltip("下移").
						Disabled(position >= totalEnabled).
						Cursor(ui.CursorPointer).
						Size(24, 24).
						Radius(6).
						Center()
					if position < totalEnabled && downBtn.Hovered() {
						downBtn.Background(p.hover)
					}
					if downBtn.Clicked() {
						v.moveServiceRelative(svc, 1)
					}
					downBtn.Children(func() {
						col := p.muted
						if position >= totalEnabled {
							col = p.muted.Alpha(0.35)
						}
						ui.Icon(c, chevronDownSVG).Size(13, 13).TextColor(col)
					})
				}

				if !keyless {
					cfgLabel := "配置"
					accessLabel := "配置" + title
					if isExpanded {
						cfgLabel = "收起"
						accessLabel = "收起" + title
					}
					v.renderSecondaryButtonDisabled(c, p, cfgLabel, accessLabel, false, func() {
						v.mu.Lock()
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

				on := enabled
				sw := ui.Switch(c, &on).
					Label("启用" + title).
					Disabled(busy || (!keyless && !isConfigured))
				if sw.Changed() {
					v.mu.Lock()
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
			})
		})

		if !keyless && isExpanded {
			v.renderServiceDrawer(c, p, svc, fields, isConfigured)
		}
	})
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
	modelVal := ""
	if draft, ok := v.modelDrafts[svc]; ok {
		modelVal = draft
	} else {
		switch svc {
		case "openai":
			modelVal = v.settings.Services.OpenAI.Model
		case "siliconflow":
			modelVal = v.settings.Services.SiliconFlow.Model
		case "zhipu":
			modelVal = v.settings.Services.Zhipu.Model
		}
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
	ui.Column(c).FillWidth().Background(p.cardHead).Padding(12, 14).Gap(10).Children(func() {
		// Link to console
		ui.Row(c).Children(func() {
			link := ui.ButtonBase(c).Label("前往控制台获取凭证").Cursor(ui.CursorPointer).Padding(2, 0)
			if link.Clicked() && v.actions.openKeyPage != nil {
				if err := v.actions.openKeyPage(svc); err != nil {
					c.Toast(err.Error())
				}
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
			v.renderDrawerInputRow(c, p, "Base URL", "任何 OpenAI 兼容接口", &baseURLVal, "https://api.openai.com/v1", false, locked, hasBaseURLDraft,
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
			v.renderDrawerInputRow(c, p, "模型", "留空恢复默认模型", &modelVal, defaultModel, false, locked, hasModelDraft,
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
					switch svc {
					case "openai":
						next.Services.OpenAI.Model = trimmed
					case "siliconflow":
						next.Services.SiliconFlow.Model = trimmed
					case "zhipu":
						next.Services.Zhipu.Model = trimmed
					}
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
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(12).Children(func() {
				ui.Column(c).Width(130).Gap(2).Children(func() {
					ui.Text(c, field.label).FontSize(12).FontWeight(600).TextColor(p.fg)
					if sub != "" {
						ui.Text(c, sub).FontSize(11).TextColor(p.muted)
					}
				})
				ui.Row(c).Grow(1).AlignItems(ui.Center).Gap(8).Children(func() {
					inp := ui.TextInput(c, &val).
						Label("编辑" + field.label).
						Password().
						Placeholder(ph).
						Disabled(locked).
						Grow(1).
						FontSize(12)
					if inp.Changed() {
						v.mu.Lock()
						if v.serviceDrafts[svc] == nil {
							v.serviceDrafts[svc] = make(map[string]string)
						}
						v.serviceDrafts[svc][field.name] = val
						v.testStatuses[svc] = serviceTestStatus{}
						v.mu.Unlock()
					}
					if inp.Submitted() {
						v.saveCredentials(svc)
					}
					if isStored {
						clr := ui.ButtonBase(c).Label("清除"+field.label).Disabled(locked).Cursor(ui.CursorPointer).Padding(4, 6)
						if clr.Clicked() {
							v.clearSecret(svc, field.name)
						}
						clr.Children(func() {
							col := p.accentText
							if locked {
								col = p.muted.Alpha(0.5)
							}
							txt := ui.Text(c, "清除").FontSize(12).TextColor(col)
							if !locked && clr.Hovered() {
								txt.Underline()
							}
						})
					}
				})
			})
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
			statusText := "未测试"
			statusColor := p.muted
			if testStatus.result != "" {
				statusText = testStatus.result
				if testStatus.isError {
					statusColor = p.error
				} else if !testStatus.testing {
					statusColor = p.success
				}
			}
			ui.Text(c, statusText).FontSize(11).TextColor(statusColor).Grow(1)

			testDisabled := !isConfigured || locked || hasDraft
			v.renderSecondaryButtonDisabled(c, p, "测试连接", "测试连接", testDisabled, func() {
				v.testConnection(svc)
			})

			saveDisabled := !hasDraft || locked
			saveBtn := ui.ButtonBase(c).
				Label("保存凭证").
				Disabled(saveDisabled).
				Cursor(ui.CursorPointer).
				Padding(6, 12).
				Radius(8).
				Background(p.primaryBg)
			if saveDisabled {
				saveBtn.Opacity(0.45)
			}
			if saveBtn.Clicked() {
				v.saveCredentials(svc)
			}
			saveBtn.Children(func() {
				ui.Text(c, "保存凭证").FontSize(12).FontWeight(600).TextColor(p.onAccent).SingleLine()
			})
		})
	})
}

func (v *settingsView) renderDrawerInputRow(
	c *ui.Context,
	p settingsPalette,
	label, sub string,
	val *string,
	placeholder string,
	password, disabled, hasDraft bool,
	onDraft func(string),
	onCommit func(string),
) {
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(12).Children(func() {
		ui.Column(c).Width(130).Gap(2).Children(func() {
			ui.Text(c, label).FontSize(12).FontWeight(600).TextColor(p.fg)
			if sub != "" {
				ui.Text(c, sub).FontSize(11).TextColor(p.muted)
			}
		})
		inp := ui.TextInput(c, val).
			Label("编辑" + label).
			Placeholder(placeholder).
			Disabled(disabled).
			Grow(1).
			FontSize(12)
		if password {
			inp.Password()
		}
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

	ui.Text(c, "快捷键").FontSize(16).FontWeight(600).TextColor(p.fg)

	ui.Row(c).FillWidth().Padding(9, 12).Radius(12).Background(p.field).Border(1, p.border).Children(func() {
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
				ui.Row(c).FillWidth().Padding(11, 14).AlignItems(ui.Center).Gap(16).Children(func() {
					ui.Column(c).Grow(1).Gap(2).Children(func() {
						ui.Text(c, item.label).FontSize(12).FontWeight(600).TextColor(p.fg)
						if conflict != "" {
							ui.Text(c, conflict).FontSize(11).TextColor(p.error)
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
						MinWidth(132).
						Padding(6, 12).
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

	ui.Text(c, "命名转换").FontSize(16).FontWeight(600).TextColor(p.fg)

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

	ui.Column(c).Padding(6, 0, 0, 0).Gap(4).Children(func() {
		ui.Text(c, "复制格式").FontSize(13).FontWeight(600).TextColor(p.fg)
		ui.Text(c, "英语译文下方会显示已开启的复制格式。至少开启一种。").FontSize(11).TextColor(p.muted)
	})

	ui.Column(c).Radius(12).Background(p.card).Border(1, p.border).Clip().Children(func() {
		for i, opt := range settingsCopyFormats {
			isChecked := slices.Contains(cfg.CopyFormats, opt.style)
			isOnlyOne := isChecked && len(cfg.CopyFormats) == 1
			divider := i < len(settingsCopyFormats)-1

			ui.Column(c).FillWidth().Children(func() {
				ui.Row(c).FillWidth().Padding(10, 14).AlignItems(ui.Center).Gap(12).Children(func() {
					ui.Text(c, opt.label).FontSize(12).FontWeight(600).TextColor(p.fg).Width(64)
					ui.Row(c).Padding(2, 8).Radius(6).Background(p.field).Children(func() {
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
