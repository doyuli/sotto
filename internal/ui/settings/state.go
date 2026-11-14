package settings

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"

	"sotto/internal/config"
	"sotto/internal/hotkey"
	"sotto/internal/keys"
)

// CredentialStatus contains presence information without exposing stored values.
type CredentialStatus struct {
	Stored     []string `json:"stored"`
	Configured []string `json:"configured"`
}

// DeeplUsage is DeepL's monthly character count and limit.
type DeeplUsage struct {
	Used  int `json:"used"`
	Limit int `json:"limit"`
}
type Actions struct {
	GetSettings  func() config.Settings
	SaveSettings func(config.Settings) error
	Credentials  func() (CredentialStatus, error)
	SetSecret    func(name, value string) error
	TestService  func(ctx context.Context, name string) string
	OpenKeyPage  func(service string) error
	DeeplUsage   func() (DeeplUsage, error)
	SaveHotkey   func(action, accel string) error
	HotkeyErrors func() map[string]string
	Invalidate   func()
}

type serviceTestStatus struct {
	testing bool
	result  string
	isError bool
}

type View struct {
	mu      sync.Mutex
	actions Actions

	activeTab        Tab
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
	credentialBusy map[string]bool
	testStatuses   map[string]serviceTestStatus
	deeplUsageText string
	deeplUsagePct  float64
	drag           settingsDragState

	hotkeyListening string
	hotkeyPending   string
	hotkeyEditor    string
	hotkeyDraft     hotkey.Shortcut
	hotkeyConflicts map[string]string
	hotkeyRecorders map[string]*ui.Handle
}

func NewView(initialTab Tab, cfg config.Settings, actions Actions) *View {
	if initialTab == "" {
		initialTab = TabGeneral
	}
	v := &View{
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
		credentialBusy:  make(map[string]bool),
		testStatuses:    make(map[string]serviceTestStatus),
		deeplUsageText:  "额度查询中…",
		hotkeyConflicts: make(map[string]string),
		hotkeyRecorders: map[string]*ui.Handle{"panel": {}, "selection": {}, "ocr": {}},
	}
	go v.runTaskLoop()
	v.RefreshCredentialsAsync()
	return v
}

func cloneSettings(cfg config.Settings) config.Settings {
	out := cfg
	out.Services.Enabled = slices.Clone(cfg.Services.Enabled)
	out.Services.Order = slices.Clone(cfg.Services.Order)
	out.CopyFormats = slices.Clone(cfg.CopyFormats)
	return out
}

func (v *View) Dispose() {
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

func (v *View) invalidate() {
	if v.actions.Invalidate != nil {
		v.actions.Invalidate()
	}
}

func (v *View) SetActiveTab(tab Tab) {
	v.mu.Lock()
	if tab != TabHotkeys {
		v.hotkeyListening = ""
		v.hotkeyEditor = ""
	}
	if tab != TabServices {
		v.flushTextDraftsLocked()
	}
	v.activeTab = tab
	disposed := v.disposed
	v.mu.Unlock()
	if !disposed {
		v.invalidate()
	}
}

func (v *View) ApplyExternalSettings(cfg config.Settings) {
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

func (v *View) flushTextDraftsLocked() {
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
func (v *View) enqueueTaskLocked(task func()) {
	if v.disposed {
		return
	}
	v.tasks = append(v.tasks, task)
	select {
	case v.taskWake <- struct{}{}:
	default:
	}
}

func (v *View) enqueueTask(task func()) {
	v.mu.Lock()
	v.enqueueTaskLocked(task)
	v.mu.Unlock()
}

func (v *View) enqueueSaveLocked(next config.Settings) {
	if v.disposed {
		return
	}
	v.settings = cloneSettings(next)
	v.pendingWrites++
	snap := cloneSettings(next)
	v.enqueueTaskLocked(func() {
		var err error
		if v.actions.SaveSettings != nil {
			// Ordinary settings edits must not restore an older shortcut snapshot.
			if v.actions.GetSettings != nil {
				snap.Hotkeys = v.actions.GetSettings().Hotkeys
			}
			err = v.actions.SaveSettings(snap)
		}
		v.mu.Lock()
		v.pendingWrites--
		v.saveFailed = err != nil
		if err != nil {
			v.pendingToast = "保存失败：" + err.Error()
		}
		if v.pendingWrites == 0 && !v.disposed && v.actions.GetSettings != nil {
			v.settings = cloneSettings(v.actions.GetSettings())
		}
		disposed := v.disposed
		v.mu.Unlock()
		if !disposed {
			v.invalidate()
		}
	})
}

func (v *View) runTaskLoop() {
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

// DetachWindow preserves the application's optimistic settings and writer while
// releasing transient UI state. A new native window reuses this controller.
func (v *View) DetachWindow() {
	v.mu.Lock()
	v.flushTextDraftsLocked()
	v.windowGeneration++
	v.usageGeneration++
	for name, cancel := range v.probeCancels {
		cancel()
		delete(v.probeCancels, name)
	}
	v.hotkeyListening = ""
	v.hotkeyEditor = ""
	v.serviceDrafts = make(map[string]map[string]string)
	v.expanded = make(map[string]bool)
	v.testStatuses = make(map[string]serviceTestStatus)
	v.drag.clear()
	v.focusService = ""
	v.hotkeyConflicts = make(map[string]string)
	v.mu.Unlock()
}

func (v *View) RefreshCredentialsAsync() {
	if v.actions.Credentials == nil {
		return
	}
	v.enqueueTask(func() {
		status, err := v.actions.Credentials()
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

func (v *View) openKeyPageAsync(service string) {
	v.mu.Lock()
	if v.disposed || v.actions.OpenKeyPage == nil {
		v.mu.Unlock()
		return
	}
	generation := v.windowGeneration
	v.mu.Unlock()

	// Shell startup may block; keep it independent of the UI and settings writer.
	go func() {
		if err := v.actions.OpenKeyPage(service); err != nil {
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

func (v *View) loadDeeplUsageAsync() {
	if v.actions.DeeplUsage == nil {
		return
	}
	v.mu.Lock()
	v.usageGeneration++
	generation := v.usageGeneration
	v.deeplUsageText = "额度查询中…"
	v.deeplUsagePct = 0
	v.mu.Unlock()
	go func() {
		usage, err := v.actions.DeeplUsage()
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

func (v *View) serviceOnLocked(name string) bool {
	fields := serviceCredentialFields[name]
	keyless := len(fields) == 0
	return slices.Contains(v.settings.Services.Enabled, name) &&
		(keyless || slices.Contains(v.credentials.Configured, name))
}

func (v *View) hasServiceDraftLocked(name string) bool {
	for _, val := range v.serviceDrafts[name] {
		if strings.TrimSpace(val) != "" {
			return true
		}
	}
	return false
}

func (v *View) saveCredentials(name string) {
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
			if v.actions.SetSecret != nil {
				if err := v.actions.SetSecret(f.name, val); err != nil {
					writeErr = err
					break
				}
			}
		}
		var status CredentialStatus
		var credErr error
		if v.actions.Credentials != nil {
			status, credErr = v.actions.Credentials()
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
			} else if v.actions.Credentials != nil {
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

func (v *View) clearSecret(name, key string) {
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
		if v.actions.SetSecret != nil {
			writeErr = v.actions.SetSecret(key, "")
		}
		var status CredentialStatus
		var credErr error
		if v.actions.Credentials != nil {
			status, credErr = v.actions.Credentials()
		}
		var refreshedSettings *config.Settings
		if v.actions.GetSettings != nil {
			s := v.actions.GetSettings()
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
			} else if v.actions.Credentials != nil {
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

func (v *View) testConnection(name string) {
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
			if v.actions.TestService != nil {
				errMsg = v.actions.TestService(networkCtx, name)
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

func (v *View) applyDragOrderLocked(preview []string) {
	if len(preview) == 0 {
		return
	}
	order := slices.Clone(v.settings.Services.Order)
	pi := 0
	for i, s := range order {
		if _, ok := serviceCredentialFields[s]; ok && v.serviceOnLocked(s) && pi < len(preview) {
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
func (v *View) finishDragLocked(enabled []string) {
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
func (v *View) PrepareQuit(retry func()) bool {
	v.Dispose()
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
