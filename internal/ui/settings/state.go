package settings

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"

	"sotto/internal/config"
	"sotto/internal/hotkey"
	"sotto/internal/keys"
	"sotto/internal/ui/shared"
	"sotto/internal/usermessage"
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
	GetSettings            func() config.Settings
	SaveSettings           func(config.Settings) error
	SaveMinimalTranslation func(bool) error
	Credentials            func(service string) (CredentialStatus, error)
	SaveService            func(name string, changes ServiceChanges) (ServiceSaveResult, error)
	TestService            func(ctx context.Context, name string) string
	OpenKeyPage            func(service string) error
	DeeplUsage             func(context.Context) (DeeplUsage, error)
	SaveHotkey             func(action, accel string) error
	HotkeyErrors           func() map[string]string
	Invalidate             func()
}

// ServiceChanges contains only edited fields. Nil options are unchanged,
// empty options restore defaults, and empty credential values remove credentials.
type ServiceChanges struct {
	BaseURL     *string
	Model       *string
	Credentials map[string]string
}

type ServiceSaveResult struct {
	Disabled bool
}

type serviceDraft struct {
	baseURL     *string
	model       *string
	credentials map[string]credentialDraft
}

type credentialDraft struct {
	value  string
	remove bool
}

type serviceTestStatus struct {
	testing bool
	result  string
	isError bool
}

type serviceRowHandles struct {
	setup, ready, toggle ui.Handle
}

type View struct {
	mu      sync.Mutex
	actions Actions

	activeTab        Tab
	settings         config.Settings
	pendingWrites    int
	tasks            []func()
	taskWake         chan struct{}
	taskStop         chan struct{}
	taskDone         chan struct{}
	quitWaiting      bool
	pendingToast     string
	disposed         bool
	usageGeneration  uint64
	usageCancel      context.CancelFunc
	usageLoaded      bool
	windowGeneration uint64
	probeCancels     map[string]context.CancelFunc
	serviceHandles   map[string]*serviceRowHandles
	focusService     string

	credentialsLoaded bool
	credentials       CredentialStatus
	credentialErrors  map[string]string
	credentialReads   map[string]bool

	expanded       map[string]bool
	serviceDrafts  map[string]serviceDraft
	serviceSaving  map[string]bool
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
		actions:          actions,
		probeCancels:     make(map[string]context.CancelFunc),
		credentialErrors: make(map[string]string),
		credentialReads:  make(map[string]bool),
		serviceHandles:   make(map[string]*serviceRowHandles),
		activeTab:        initialTab,
		settings:         cloneSettings(cfg),
		taskWake:         make(chan struct{}, 1),
		taskStop:         make(chan struct{}),
		taskDone:         make(chan struct{}),
		expanded:         make(map[string]bool),
		serviceDrafts:    make(map[string]serviceDraft),
		serviceSaving:    make(map[string]bool),
		testStatuses:     make(map[string]serviceTestStatus),
		deeplUsageText:   "额度查询中…",
		hotkeyConflicts:  make(map[string]string),
		hotkeyRecorders:  make(map[string]*ui.Handle, len(settingsHotkeys)),
	}
	for _, item := range settingsHotkeys {
		v.hotkeyRecorders[item.id] = &ui.Handle{}
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
	v.disposed = true
	clear(v.serviceDrafts)
	v.resetUsageLocked()
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
	if v.activeTab == TabServices && tab != TabServices {
		for name := range v.serviceDrafts {
			delete(v.testStatuses, name)
		}
		clear(v.serviceDrafts)
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
	previousEnabled := v.settings.Services.Enabled
	snap := cloneSettings(next)
	v.enqueueSettingsWriteLocked(next, func() error {
		if v.actions.SaveSettings == nil {
			return nil
		}
		// Ordinary settings edits must not restore an older shortcut snapshot.
		if v.actions.GetSettings != nil {
			current := v.actions.GetSettings()
			snap.Hotkeys = current.Hotkeys
			// Apply only this action's enablement change. A queued service
			// save may have removed credentials and disabled another service.
			desired := snap.Services
			snap.Services = current.Services
			snap.Services.Order = desired.Order
			snap.Services.Enabled = slices.DeleteFunc(slices.Clone(current.Services.Enabled), func(name string) bool {
				return slices.Contains(previousEnabled, name) && !slices.Contains(desired.Enabled, name)
			})
			for _, name := range desired.Enabled {
				if !slices.Contains(previousEnabled, name) && !slices.Contains(snap.Services.Enabled, name) {
					snap.Services.Enabled = append(snap.Services.Enabled, name)
				}
			}
		}
		return v.actions.SaveSettings(snap)
	})
}

func (v *View) enqueueMinimalTranslationLocked(enabled bool) {
	next := v.settings
	next.MinimalTranslation = enabled
	if v.actions.SaveMinimalTranslation == nil {
		v.enqueueSaveLocked(next)
		return
	}
	v.enqueueSettingsWriteLocked(next, func() error { return v.actions.SaveMinimalTranslation(enabled) })
}

func (v *View) enqueueSettingsWriteLocked(next config.Settings, save func() error) {
	if v.disposed {
		return
	}
	v.settings = cloneSettings(next)
	v.pendingWrites++
	v.enqueueTaskLocked(func() {
		err := save()
		v.mu.Lock()
		v.pendingWrites--
		if err != nil {
			v.pendingToast = usermessage.Format("保存失败", err)
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
	v.windowGeneration++
	clear(v.serviceDrafts)
	v.resetUsageLocked()
	for name, cancel := range v.probeCancels {
		cancel()
		delete(v.probeCancels, name)
	}
	v.hotkeyListening = ""
	v.hotkeyEditor = ""
	v.expanded = make(map[string]bool)
	v.testStatuses = make(map[string]serviceTestStatus)
	v.drag.clear()
	v.focusService = ""
	v.hotkeyConflicts = make(map[string]string)
	v.mu.Unlock()
}

func (v *View) RefreshCredentialsAsync() {
	v.refreshCredentialsAsync("")
}

func (v *View) refreshCredentialsAsync(name string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.disposed || v.actions.Credentials == nil || v.credentialReads[name] {
		return
	}
	v.credentialReads[name] = true
	v.enqueueTaskLocked(func() {
		status, err := v.actions.Credentials(name)
		v.mu.Lock()
		disposed := v.disposed
		delete(v.credentialReads, name)
		if !disposed {
			v.applyCredentialStatusLocked(name, status, err)
		}
		loadUsage := !disposed && err == nil && (name == "" || name == "deepl") &&
			v.expanded["deepl"] && slices.Contains(v.credentials.Stored, keys.DeeplKey)
		v.mu.Unlock()
		if loadUsage {
			v.loadDeeplUsageAsync()
		}
		if !disposed {
			v.invalidate()
		}
	})
}

func (v *View) applyCredentialStatusLocked(name string, status CredentialStatus, err error) {
	if err != nil {
		v.credentialErrors[name] = usermessage.Format("读取服务状态失败", err)
		return
	}
	if name == "" {
		v.credentialsLoaded = true
		v.credentials = status
		clear(v.credentialErrors)
		return
	}

	delete(v.credentialErrors, name)
	fields := keys.ForServices(name)
	stored := slices.DeleteFunc(slices.Clone(v.credentials.Stored), func(key string) bool {
		return slices.Contains(fields, key)
	})
	for _, key := range fields {
		if slices.Contains(status.Stored, key) {
			stored = append(stored, key)
		}
	}
	configured := slices.DeleteFunc(slices.Clone(v.credentials.Configured), func(service string) bool {
		return service == name
	})
	if slices.Contains(status.Configured, name) {
		configured = append(configured, name)
	}
	v.credentials = CredentialStatus{Stored: stored, Configured: configured}
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
				v.pendingToast = usermessage.Format("打开控制台失败", err)
			}
			v.mu.Unlock()
			if !stale {
				v.invalidate()
			}
		}
	}()
}

// Reset on credential changes and window closure. Cancellation alone cannot
// reject a result which was already completing, so retain the generation check.
func (v *View) resetUsageLocked() {
	v.usageGeneration++
	if v.usageCancel != nil {
		v.usageCancel()
		v.usageCancel = nil
	}
	v.usageLoaded = false
	v.deeplUsageText = "额度查询中…"
	v.deeplUsagePct = 0
}

func (v *View) loadDeeplUsageAsync() {
	if v.actions.DeeplUsage == nil {
		return
	}
	v.mu.Lock()
	if v.disposed || !v.expanded["deepl"] || v.usageCancel != nil || v.usageLoaded ||
		v.serviceSaving["deepl"] || v.credentialErrors["deepl"] != "" {
		v.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	v.usageCancel = cancel
	v.usageGeneration++
	generation := v.usageGeneration
	v.deeplUsageText = "额度查询中…"
	v.deeplUsagePct = 0
	v.mu.Unlock()
	go func() {
		defer cancel()
		usage, err := v.actions.DeeplUsage(ctx)
		v.mu.Lock()
		disposed := v.disposed || generation != v.usageGeneration
		if !disposed {
			v.usageCancel = nil
			v.usageLoaded = err == nil
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

func (v *View) canEditServiceLocked(name string) bool {
	return !v.disposed && v.activeTab == TabServices && !v.serviceSaving[name] && !v.testStatuses[name].testing
}

func (v *View) updateServiceDraftLocked(name string, draft serviceDraft) {
	if draft.baseURL == nil && draft.model == nil && len(draft.credentials) == 0 {
		delete(v.serviceDrafts, name)
	} else {
		v.serviceDrafts[name] = draft
	}
	delete(v.testStatuses, name)
}

func changedOption(value, saved string) *string {
	if value == saved {
		return nil
	}
	return &value
}

func (v *View) editServiceBaseURL(name, value string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.canEditServiceLocked(name) || name != "openai" {
		return
	}
	draft := v.serviceDrafts[name]
	draft.baseURL = changedOption(value, v.settings.Services.OpenAI.BaseURL)
	v.updateServiceDraftLocked(name, draft)
}

func (v *View) editServiceModel(name, value string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	saved := v.settings.Services.Model(name)
	if !v.canEditServiceLocked(name) || saved == nil {
		return
	}
	draft := v.serviceDrafts[name]
	draft.model = changedOption(value, *saved)
	v.updateServiceDraftLocked(name, draft)
}

func (v *View) editServiceCredential(name, field, value string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.canEditServiceLocked(name) {
		return
	}
	draft := v.serviceDrafts[name]
	if strings.TrimSpace(value) == "" {
		delete(draft.credentials, field)
	} else {
		if draft.credentials == nil {
			draft.credentials = make(map[string]credentialDraft)
		}
		draft.credentials[field] = credentialDraft{value: value}
	}
	v.updateServiceDraftLocked(name, draft)
}

func (v *View) toggleCredentialRemoval(name, field string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.canEditServiceLocked(name) {
		return
	}
	draft := v.serviceDrafts[name]
	edit := draft.credentials[field]
	edit.remove = !edit.remove
	if !edit.remove && edit.value == "" {
		delete(draft.credentials, field)
	} else {
		if draft.credentials == nil {
			draft.credentials = make(map[string]credentialDraft)
		}
		draft.credentials[field] = edit
	}
	v.updateServiceDraftLocked(name, draft)
}

func (v *View) discardService(name string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.serviceSaving[name] {
		delete(v.serviceDrafts, name)
		delete(v.testStatuses, name)
	}
}

func (v *View) saveService(name string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	draft, dirty := v.serviceDrafts[name]
	if !v.canEditServiceLocked(name) || !dirty {
		return
	}
	changes := ServiceChanges{BaseURL: draft.baseURL, Model: draft.model}
	if len(draft.credentials) != 0 {
		changes.Credentials = make(map[string]string, len(draft.credentials))
		for field, edit := range draft.credentials {
			if edit.remove {
				changes.Credentials[field] = ""
			} else {
				changes.Credentials[field] = strings.TrimSpace(edit.value)
			}
		}
	}
	if name == "deepl" {
		v.resetUsageLocked()
	}
	v.serviceSaving[name] = true
	v.testStatuses[name] = serviceTestStatus{}
	v.enqueueTaskLocked(func() {
		var result ServiceSaveResult
		var writeErr error
		if v.actions.SaveService != nil {
			result, writeErr = v.actions.SaveService(name, changes)
		} else {
			writeErr = errors.New("无法写入服务配置")
		}
		var status CredentialStatus
		var credErr error
		readCredentials := len(changes.Credentials) != 0 && v.actions.Credentials != nil
		if readCredentials {
			status, credErr = v.actions.Credentials(name)
		}
		v.mu.Lock()
		disposed := v.disposed
		if !disposed {
			v.serviceSaving[name] = false
			if writeErr != nil {
				v.testStatuses[name] = serviceTestStatus{result: usermessage.Format("保存失败", writeErr), isError: true}
			} else {
				delete(v.serviceDrafts, name)
				v.testStatuses[name] = serviceTestStatus{result: "配置已保存"}
				if result.Disabled {
					v.pendingToast = shared.ServiceName(name) + " 已停用"
				}
			}
			if readCredentials {
				v.applyCredentialStatusLocked(name, status, credErr)
			}
			if credErr != nil && writeErr == nil {
				// The write succeeded. Keep confirmed edits visible even if
				// the keyring cannot confirm the rest of the service's status.
				stored := slices.DeleteFunc(slices.Clone(v.credentials.Stored), func(key string) bool {
					_, changed := changes.Credentials[key]
					return changed
				})
				for key, value := range changes.Credentials {
					if value != "" {
						stored = append(stored, key)
					}
				}
				v.credentials.Stored = stored
				if result.Disabled {
					v.credentials.Configured = slices.DeleteFunc(slices.Clone(v.credentials.Configured), func(service string) bool {
						return service == name
					})
				}
			}
			if v.pendingWrites == 0 && v.actions.GetSettings != nil {
				v.settings = cloneSettings(v.actions.GetSettings())
			}
		}
		loadUsage := !disposed && credErr == nil && name == "deepl" && v.expanded[name] && slices.Contains(v.credentials.Stored, keys.DeeplKey)
		v.mu.Unlock()
		if loadUsage {
			v.loadDeeplUsageAsync()
		}
		if !disposed {
			v.invalidate()
		}
	})
}

func (v *View) testConnection(name string) {
	v.mu.Lock()
	_, dirty := v.serviceDrafts[name]
	if !v.canEditServiceLocked(name) || dirty || v.credentialErrors[name] != "" {
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
		v.mu.Unlock()
		if disposed {
			cancel()
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
