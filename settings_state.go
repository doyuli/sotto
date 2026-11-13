package main

import (
	"context"
	"fmt"
	"github.com/egoist/mygo/ui"
	"slices"
	"sotto/internal/config"
	"sotto/internal/keys"
	"strings"
	"sync"
	"time"
)

type settingsActions struct {
	getSettings        func() config.Settings
	saveSettings       func(config.Settings) error
	saveCustomSettings func(config.Settings, string, *string) error
	credentials        func() (CredentialStatus, error)
	setSecret          func(name, value string) error
	testService        func(ctx context.Context, name string) string
	openKeyPage        func(service string) error
	deeplUsage         func() (DeeplUsage, error)
	hotkeyBusy         func(accel string) bool
	invalidate         func()
}

type serviceTestStatus struct {
	testing bool
	result  string
	isError bool
}

type customDraft struct {
	service     config.CustomService // Retain its ID across failed save attempts.
	apiKey      string
	deleteUntil time.Time
}

type settingsView struct {
	mu      sync.Mutex
	actions settingsActions

	activeTab        settingsTab
	settings         config.Settings
	pendingWrites    int
	saveFailed       bool
	tasks            []func()
	taskWake         chan struct{}
	taskStop         chan struct{}
	taskDone         chan struct{}
	quitWaiting      bool
	pendingToast     string
	disposed         bool
	usageGeneration  uint64
	windowGeneration uint64
	probeCancels     map[string]context.CancelFunc
	switchHandles    map[string]*ui.Handle
	focusService     string

	credentialsLoaded bool
	credentials       CredentialStatus
	credentialsErr    string

	expanded       map[string]bool
	serviceDrafts  map[string]map[string]string
	baseURLDraft   *string
	modelDrafts    map[string]string
	customDrafts   map[string]customDraft
	credentialBusy map[string]bool
	testStatuses   map[string]serviceTestStatus
	deeplUsageText string
	deeplUsagePct  float64
	drag           settingsDragState

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
		probeCancels:    make(map[string]context.CancelFunc),
		switchHandles:   make(map[string]*ui.Handle),
		activeTab:       initialTab,
		settings:        cloneSettings(cfg),
		taskWake:        make(chan struct{}, 1),
		taskStop:        make(chan struct{}),
		taskDone:        make(chan struct{}),
		expanded:        make(map[string]bool),
		serviceDrafts:   make(map[string]map[string]string),
		modelDrafts:     make(map[string]string),
		customDrafts:    make(map[string]customDraft),
		credentialBusy:  make(map[string]bool),
		testStatuses:    make(map[string]serviceTestStatus),
		deeplUsageText:  "额度查询中…",
		hotkeyConflicts: make(map[string]string),
	}
	go v.runTaskLoop()
	v.refreshCredentialsAsync()
	return v
}

func cloneSettings(cfg config.Settings) config.Settings {
	out := cfg
	out.Services.Enabled = slices.Clone(cfg.Services.Enabled)
	out.Services.Order = slices.Clone(cfg.Services.Order)
	out.Services.Custom = slices.Clone(cfg.Services.Custom)
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
	v.usageGeneration++
	for _, cancel := range v.probeCancels {
		cancel()
	}
	close(v.taskStop)
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
		setServiceModel(&next, svc, trimmed)
		delete(v.modelDrafts, svc)
	}
	v.enqueueSaveLocked(next)
}

// Tasks serialize persistent mutations and their readbacks across window reopenings.
func (v *settingsView) enqueueTaskLocked(task func()) {
	if v.disposed {
		return
	}
	v.tasks = append(v.tasks, task)
	select {
	case v.taskWake <- struct{}{}:
	default:
	}
}

func (v *settingsView) enqueueTask(task func()) {
	v.mu.Lock()
	v.enqueueTaskLocked(task)
	v.mu.Unlock()
}

func (v *settingsView) enqueueSaveLocked(next config.Settings) {
	v.queueSaveLocked(next, v.actions.saveSettings, nil)
}

// complete runs under mu after persistence; UI cleanup must wait for its result.
func (v *settingsView) queueSaveLocked(next config.Settings, write func(config.Settings) error, complete func(error)) {
	if v.disposed {
		return
	}
	v.settings = cloneSettings(next)
	v.pendingWrites++
	snap := cloneSettings(next)
	v.enqueueTaskLocked(func() {
		var err error
		if write != nil {
			err = write(snap)
		}
		v.mu.Lock()
		v.pendingWrites--
		v.saveFailed = err != nil
		if err != nil {
			v.pendingToast = "保存失败：" + err.Error()
		}
		if v.pendingWrites == 0 && !v.disposed && v.actions.getSettings != nil {
			v.settings = cloneSettings(v.actions.getSettings())
		}
		if complete != nil {
			complete(err)
		}
		disposed := v.disposed
		v.mu.Unlock()
		if !disposed {
			v.invalidate()
		}
	})
}

func (v *settingsView) runTaskLoop() {
	defer close(v.taskDone)
	for {
		v.mu.Lock()
		if len(v.tasks) == 0 {
			disposed := v.disposed
			v.mu.Unlock()
			if disposed {
				return
			}
			select {
			case <-v.taskWake:
			case <-v.taskStop:
			}
			continue
		}
		task := v.tasks[0]
		v.tasks[0] = nil
		v.tasks = v.tasks[1:]
		v.mu.Unlock()
		task()
	}
}

// detachWindow preserves the application's optimistic settings and writer while
// releasing transient UI state. A new native window reuses this controller.
func (v *settingsView) detachWindow() {
	v.mu.Lock()
	v.flushTextDraftsLocked()
	v.windowGeneration++
	v.usageGeneration++
	for name, cancel := range v.probeCancels {
		cancel()
		delete(v.probeCancels, name)
	}
	v.hotkeyListening = ""
	v.hotkeyCaptureArmed = false
	v.serviceDrafts = make(map[string]map[string]string)
	v.customDrafts = make(map[string]customDraft)
	v.expanded = make(map[string]bool)
	v.testStatuses = make(map[string]serviceTestStatus)
	v.drag.clear()
	v.focusService = ""
	v.hotkeyConflicts = make(map[string]string)
	v.mu.Unlock()
}

func (v *settingsView) refreshCredentialsAsync() {
	if v.actions.credentials == nil {
		return
	}
	v.enqueueTask(func() {
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
	})
}

func (v *settingsView) openKeyPageAsync(service string) {
	v.mu.Lock()
	if v.disposed || v.actions.openKeyPage == nil {
		v.mu.Unlock()
		return
	}
	generation := v.windowGeneration
	v.mu.Unlock()

	// Shell startup may block; keep it independent of the UI and settings writer.
	go func() {
		if err := v.actions.openKeyPage(service); err != nil {
			v.mu.Lock()
			stale := v.disposed || generation != v.windowGeneration
			if !stale {
				v.pendingToast = err.Error()
			}
			v.mu.Unlock()
			if !stale {
				v.invalidate()
			}
		}
	}()
}

func (v *settingsView) loadDeeplUsageAsync() {
	if v.actions.deeplUsage == nil {
		return
	}
	v.mu.Lock()
	v.usageGeneration++
	generation := v.usageGeneration
	v.deeplUsageText = "额度查询中…"
	v.deeplUsagePct = 0
	v.mu.Unlock()
	go func() {
		usage, err := v.actions.deeplUsage()
		v.mu.Lock()
		disposed := v.disposed || generation != v.usageGeneration
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

func (v *settingsView) isServiceConfiguredLocked(name string) bool {
	return slices.Contains(v.credentials.Configured, name)
}

func (v *settingsView) serviceOnLocked(name string) bool {
	if config.IsCustomService(name) {
		return slices.Contains(v.settings.Services.Enabled, name) && v.isServiceConfiguredLocked(name)
	}
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
	if name == "deepl" {
		v.usageGeneration++
	}
	v.credentialBusy[name] = true
	v.testStatuses[name] = serviceTestStatus{}
	v.mu.Unlock()
	v.invalidate()

	v.enqueueTask(func() {
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
	})
}

func (v *settingsView) saveCustomService(id string) {
	v.mu.Lock()
	draft, ok := v.customDrafts[id]
	if v.disposed || !ok || v.credentialBusy[id] || v.testStatuses[id].testing {
		v.mu.Unlock()
		return
	}
	service := draft.service.Normalized()
	if !service.IsConfigured() {
		v.pendingToast = "请完整填写服务名称、Base URL 与模型名称"
		v.mu.Unlock()
		v.invalidate()
		return
	}

	actualID := id
	isNew := id == "new"
	if isNew {
		actualID = service.ID
		if !config.IsCustomService(actualID) {
			actualID = fmt.Sprintf("custom:%d", time.Now().UnixNano())
			draft.service.ID = actualID
			v.customDrafts[id] = draft
		}
	}
	service.ID = actualID
	generation := v.windowGeneration
	v.credentialBusy[id] = true
	v.credentialBusy[actualID] = true
	v.testStatuses[id] = serviceTestStatus{}
	v.testStatuses[actualID] = serviceTestStatus{}
	next := cloneSettings(v.settings)
	found := false
	for i, cs := range next.Services.Custom {
		if cs.ID == actualID {
			next.Services.Custom[i] = service
			found = true
			break
		}
	}
	if !found {
		next.Services.Custom = append(next.Services.Custom, service)
		if !slices.Contains(next.Services.Order, actualID) {
			next.Services.Order = append(next.Services.Order, actualID)
		}
	}
	var credential *string
	if key := strings.TrimSpace(draft.apiKey); key != "" {
		credential = &key
	}
	v.queueSaveLocked(next, func(s config.Settings) error {
		return v.actions.saveCustomSettings(s, actualID, credential)
	}, func(err error) {
		delete(v.credentialBusy, id)
		delete(v.credentialBusy, actualID)
		if v.disposed || generation != v.windowGeneration {
			return
		}
		if err != nil {
			v.testStatuses[id] = serviceTestStatus{result: "配置保存失败：" + err.Error(), isError: true}
			return
		}
		delete(v.customDrafts, id)
		if isNew {
			delete(v.expanded, "new")
		}
		v.expanded[actualID] = true
		v.testStatuses[actualID] = serviceTestStatus{result: "配置已保存"}
	})
	v.mu.Unlock()
	v.refreshCredentialsAsync()
	v.invalidate()
}

func (v *settingsView) deleteCustomService(id string) {
	v.mu.Lock()
	if v.disposed || v.credentialBusy[id] || v.testStatuses[id].testing {
		v.mu.Unlock()
		return
	}
	if id == "new" {
		delete(v.customDrafts, "new")
		delete(v.expanded, "new")
		v.mu.Unlock()
		v.invalidate()
		return
	}
	generation := v.windowGeneration
	v.credentialBusy[id] = true
	next := cloneSettings(v.settings)
	next.Services.Custom = slices.DeleteFunc(next.Services.Custom, func(cs config.CustomService) bool { return cs.ID == id })
	next.Services.Enabled = slices.DeleteFunc(next.Services.Enabled, func(s string) bool { return s == id })
	next.Services.Order = slices.DeleteFunc(next.Services.Order, func(s string) bool { return s == id })
	v.queueSaveLocked(next, func(s config.Settings) error {
		return v.actions.saveCustomSettings(s, id, nil)
	}, func(err error) {
		delete(v.credentialBusy, id)
		if v.disposed || generation != v.windowGeneration {
			return
		}
		if err != nil {
			v.testStatuses[id] = serviceTestStatus{result: "删除失败：" + err.Error(), isError: true}
			return
		}
		delete(v.customDrafts, id)
		delete(v.expanded, id)
		delete(v.testStatuses, id)
	})
	v.mu.Unlock()
	v.refreshCredentialsAsync()
	v.invalidate()
}

func (v *settingsView) clearSecret(name, key string) {
	v.mu.Lock()
	if v.credentialBusy[name] || v.testStatuses[name].testing {
		v.mu.Unlock()
		return
	}
	if name == "deepl" {
		v.usageGeneration++
	}
	v.credentialBusy[name] = true
	v.testStatuses[name] = serviceTestStatus{}
	v.mu.Unlock()
	v.invalidate()

	v.enqueueTask(func() {
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
				if d, ok := v.customDrafts[name]; ok {
					d.apiKey = ""
					v.customDrafts[name] = d
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
	})
}

func (v *settingsView) testConnection(name string) {
	v.mu.Lock()
	if v.credentialBusy[name] || v.testStatuses[name].testing || v.hasServiceDraftLocked(name) {
		v.mu.Unlock()
		return
	}
	generation := v.windowGeneration
	ctx, cancel := context.WithCancel(context.Background())
	v.probeCancels[name] = cancel
	v.testStatuses[name] = serviceTestStatus{testing: true, result: "正在测试…"}
	v.mu.Unlock()
	v.invalidate()

	v.enqueueTask(func() {
		v.mu.Lock()
		disposed := v.disposed || generation != v.windowGeneration
		saveFailed := v.saveFailed
		v.mu.Unlock()
		if disposed {
			cancel()
			return
		}
		if saveFailed {
			cancel()
			v.mu.Lock()
			disposed = v.disposed || generation != v.windowGeneration
			if !disposed {
				v.testStatuses[name] = serviceTestStatus{result: "设置保存失败，请重新保存后测试", isError: true}
			}
			v.mu.Unlock()
			if !disposed {
				v.invalidate()
			}
			return
		}
		go func() {
			defer cancel()
			networkCtx, networkCancel := context.WithTimeout(ctx, 12*time.Second)
			defer networkCancel()
			var errMsg string
			if v.actions.testService != nil {
				errMsg = v.actions.testService(networkCtx, name)
			}
			v.mu.Lock()
			disposed = v.disposed || generation != v.windowGeneration
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
	})
}

func (v *settingsView) applyDragOrderLocked(preview []string) {
	if len(preview) == 0 {
		return
	}
	order := slices.Clone(v.settings.Services.Order)
	pi := 0
	for i, s := range order {
		if _, ok := serviceCredentialFields[s]; (ok || config.IsCustomService(s)) && v.serviceOnLocked(s) && pi < len(preview) {
			order[i] = preview[pi]
			pi++
		}
	}
	if slices.Equal(order, v.settings.Services.Order) {
		return
	}
	next := cloneSettings(v.settings)
	next.Services.Order = order
	v.enqueueSaveLocked(next)
}

func sameServiceSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for _, s := range a {
		if !slices.Contains(b, s) {
			return false
		}
	}
	return true
}

func moveSliceItem(items []string, fromSvc, toSvc string) []string {
	from := slices.Index(items, fromSvc)
	to := slices.Index(items, toSvc)
	if from < 0 || to < 0 || from == to {
		return items
	}
	out := slices.Clone(items)
	out = slices.Delete(out, from, from+1)
	out = slices.Insert(out, to, fromSvc)
	return out
}

type settingsDragState struct {
	preview  []string
	target   string
	pointerY float32
}

func (d *settingsDragState) clear() { *d = settingsDragState{} }
func (v *settingsView) finishDragLocked(enabled []string) {
	if sameServiceSet(v.drag.preview, enabled) {
		v.applyDragOrderLocked(v.drag.preview)
	}
	v.drag.clear()
}

func serviceModel(cfg config.Settings, svc string) string {
	switch svc {
	case "openai":
		return cfg.Services.OpenAI.Model
	case "siliconflow":
		return cfg.Services.SiliconFlow.Model
	case "zhipu":
		return cfg.Services.Zhipu.Model
	}
	return ""
}
func setServiceModel(cfg *config.Settings, svc, value string) {
	switch svc {
	case "openai":
		cfg.Services.OpenAI.Model = value
	case "siliconflow":
		cfg.Services.SiliconFlow.Model = value
	case "zhipu":
		cfg.Services.Zhipu.Model = value
	}
}

// Keep the native loop available while queued writes may marshal back to it.
// The quit listener must never synchronously wait for the writer on the UI thread.
func (v *settingsView) prepareQuit(retry func()) bool {
	v.dispose()
	v.mu.Lock()
	defer v.mu.Unlock()
	select {
	case <-v.taskDone:
		return true
	default:
	}
	if !v.quitWaiting {
		v.quitWaiting = true
		go func() { <-v.taskDone; retry() }()
	}
	return false
}
