package settings

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"runtime"
	"slices"
	"sync"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"github.com/egoist/mygo/ui"

	"sotto/internal/config"
	"sotto/internal/keys"
	"sotto/internal/ui/shared"
)

func waitForCondition(t *testing.T, timeout time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal(msg)
}

func testSettingsConfig() config.Settings {
	cfg := config.Default()
	cfg.TargetLanguage = "zh-HANS"
	cfg.Services.Enabled = []string{"google", "openai"}
	cfg.Services.Order = []string{"google", "openai", "deepl", "baidu", "index-translate"}
	return cfg
}

func TestSettingsViewGeneralTab(t *testing.T) {
	var mu sync.Mutex
	saved := []config.Settings{}
	current := testSettingsConfig()

	v := NewView(TabGeneral, current, Actions{
		GetSettings: func() config.Settings {
			mu.Lock()
			defer mu.Unlock()
			return current
		},
		SaveSettings: func(s config.Settings) error {
			mu.Lock()
			defer mu.Unlock()
			s.OpenAtLogin = current.OpenAtLogin
			s.MinimalTranslation = current.MinimalTranslation
			current = s
			saved = append(saved, s)
			return nil
		},
		SaveOpenAtLogin: func(enabled bool) error {
			mu.Lock()
			defer mu.Unlock()
			current.OpenAtLogin = enabled
			saved = append(saved, current)
			return nil
		},
		SaveMinimalTranslation: func(enabled bool) error {
			mu.Lock()
			defer mu.Unlock()
			current.MinimalTranslation = enabled
			saved = append(saved, current)
			return nil
		},
		Credentials: func(string) (CredentialStatus, error) {
			return CredentialStatus{Stored: []string{keys.OpenAIKey}, Configured: []string{"openai"}}, nil
		},
	})
	defer v.Dispose()

	tt := ui.NewTester(v.Render, 780, 600)
	tt.Frame()

	if !tt.HasText("通用") {
		t.Fatalf("missing general tab content: %v", tt.Texts())
	}

	if err := tt.Click("切换开机自启"); err != nil {
		t.Fatalf("Click 切换开机自启: %v", err)
	}
	if err := tt.Click("切换简约翻译模式"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("切换失焦自动隐藏"); err != nil {
		t.Fatalf("Click 切换失焦自动隐藏: %v", err)
	}
	if err := tt.Click("切换自动更新"); err != nil {
		t.Fatalf("Click 切换自动更新: %v", err)
	}

	waitForCondition(t, time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(saved) == 4
	}, "expected 4 saved snapshots for general switches")

	mu.Lock()
	last := saved[len(saved)-1]
	mu.Unlock()
	if !last.OpenAtLogin || last.Bubble.AutoHide || !last.AutoUpdate || !last.MinimalTranslation {
		t.Fatalf("unexpected saved flags: OpenAtLogin=%v AutoHide=%v AutoUpdate=%v MinimalTranslation=%v",
			last.OpenAtLogin, last.Bubble.AutoHide, last.AutoUpdate, last.MinimalTranslation)
	}
}

func TestSettingsOpenAtLoginReflectsSystemState(t *testing.T) {
	// The saved preference may be on while the OS disables or rejects the login item.
	current := testSettingsConfig()
	current.OpenAtLogin = true
	var systemEnabled bool
	var attempts []bool
	reject := true
	refreshed := make(chan struct{}, 1)
	v := NewView(TabGeneral, current, Actions{
		Invalidate:      func() { refreshed <- struct{}{} },
		GetSettings:     func() config.Settings { return current },
		ReadOpenAtLogin: func() bool { return systemEnabled },
		SaveOpenAtLogin: func(enabled bool) error {
			attempts = append(attempts, enabled)
			current.OpenAtLogin = enabled
			if reject {
				return errors.New("login item rejected")
			}
			systemEnabled = enabled
			return nil
		},
	})
	defer v.Dispose()
	tt := ui.NewTester(v.Render, 780, 600)
	check := func(want bool) {
		t.Helper()
		select {
		case <-refreshed:
		case <-time.After(time.Second):
			t.Fatal("settings did not publish the login state")
		}
		tt.Frame()
		sw, found := tt.AccessByRole(ui.RoleSwitch, "切换开机自启")
		if !found || sw.Checked != want {
			t.Fatalf("login switch found=%v checked=%v, want %v", found, sw.Checked, want)
		}
	}
	v.RefreshOpenAtLogin()
	check(false)
	if len(attempts) != 0 || !current.OpenAtLogin {
		t.Fatal("reading system status changed the saved preference or requested a system write")
	}
	if err := tt.Click("切换开机自启"); err != nil {
		t.Fatal(err)
	}
	check(false)
	if !slices.Equal(attempts, []bool{true}) {
		t.Fatalf("one click should retry enabling the login item: %v", attempts)
	}
	reject = false
	if err := tt.Click("切换开机自启"); err != nil {
		t.Fatal(err)
	}
	check(true)
	systemEnabled = false
	v.RefreshOpenAtLogin()
	check(false)
	v.ApplyExternalSettings(current)
	check(false)
}

func TestSettingsViewServicesTab(t *testing.T) {
	var mu sync.Mutex
	current := testSettingsConfig()
	stored := []string{keys.OpenAIKey, keys.DeeplKey}
	configured := []string{"openai", "deepl"}
	credFails := true
	var openedPage string
	var testedSvc string
	usageCalls := 0

	v := NewView(TabServices, current, Actions{
		GetSettings: func() config.Settings {
			mu.Lock()
			defer mu.Unlock()
			return current
		},
		SaveSettings: func(s config.Settings) error {
			mu.Lock()
			defer mu.Unlock()
			current = s
			return nil
		},
		Credentials: func(string) (CredentialStatus, error) {
			mu.Lock()
			defer mu.Unlock()
			if credFails {
				return CredentialStatus{}, errors.New("keyring locked")
			}
			return CredentialStatus{
				Stored:     slices.Clone(stored),
				Configured: slices.Clone(configured),
			}, nil
		},
		SaveService: func(_ string, changes ServiceChanges) (ServiceSaveResult, error) {
			mu.Lock()
			defer mu.Unlock()
			for name, value := range changes.Credentials {
				if value == "" {
					stored = slices.DeleteFunc(stored, func(s string) bool { return s == name })
					if name == keys.OpenAIKey {
						configured = slices.DeleteFunc(configured, func(s string) bool { return s == "openai" })
					}
				} else {
					if !slices.Contains(stored, name) {
						stored = append(stored, name)
					}
					if name == keys.BaiduAppID || name == keys.BaiduKey {
						if slices.Contains(stored, keys.BaiduAppID) && slices.Contains(stored, keys.BaiduKey) && !slices.Contains(configured, "baidu") {
							configured = append(configured, "baidu")
						}
					}
				}
			}
			return ServiceSaveResult{}, nil
		},
		TestService: func(_ context.Context, name string) string {
			mu.Lock()
			testedSvc = name
			mu.Unlock()
			return ""
		},
		OpenKeyPage: func(svc string) error {
			mu.Lock()
			openedPage = svc
			mu.Unlock()
			return nil
		},
		DeeplUsage: func(context.Context) (DeeplUsage, error) {
			mu.Lock()
			usageCalls++
			mu.Unlock()
			return DeeplUsage{Used: 1200, Limit: 500000}, nil
		},
	})
	defer v.Dispose()

	tt := ui.NewTester(v.Render, 780, 600)
	waitForCondition(t, time.Second, func() bool {
		tt.Frame()
		return tt.HasText("重试")
	}, "expected initial credential error retry button")

	mu.Lock()
	credFails = false
	mu.Unlock()
	if err := tt.Click("重试"); err != nil {
		t.Fatalf("Click 重试: %v", err)
	}
	waitForCondition(t, time.Second, func() bool {
		tt.Frame()
		return tt.HasText("已启用")
	}, "expected services list after credential retry")

	// Drag OpenAI up over Google to reorder in real time.
	openaiGrip, ok := tt.Find("拖动OpenAI")
	if !ok {
		t.Fatal("missing 拖动OpenAI handle")
	}
	googleGrip, ok := tt.Find("拖动Google")
	if !ok {
		t.Fatal("missing 拖动Google handle")
	}
	sx, sy := openaiGrip.X+openaiGrip.W/2, openaiGrip.Y+openaiGrip.H/2
	tx, ty := googleGrip.X+googleGrip.W/2, googleGrip.Y+googleGrip.H/2
	tt.Press(sx, sy)
	tt.Move(sx, sy-8)
	tt.Move(tx, ty)
	tt.Release(tx, ty)
	waitForCondition(t, time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(current.Services.Order) >= 2 && current.Services.Order[0] == "openai"
	}, "expected OpenAI dragged to first position")

	// Expand DeepL drawer and check usage.
	if err := tt.Click("配置DeepL"); err != nil {
		t.Fatalf("Click 配置DeepL: %v", err)
	}
	waitForCondition(t, time.Second, func() bool {
		tt.Frame()
		return tt.HasText("本月已用 1200 / 500000 字符")
	}, "expected DeepL usage text")

	// Collapsing and expanding the same card must retain its completed quota query.
	settingsClick(t, tt, "收起DeepL")
	settingsClick(t, tt, "配置DeepL")
	if !tt.HasText("本月已用 1200 / 500000 字符") {
		t.Fatal("re-expanding DeepL discarded cached usage")
	}
	mu.Lock()
	gotUsageCalls := usageCalls
	mu.Unlock()
	if gotUsageCalls != 1 {
		t.Fatalf("re-expanding DeepL queried usage %d times", gotUsageCalls)
	}

	if err := tt.Click("前往控制台获取凭证"); err != nil {
		t.Fatalf("Click console link: %v", err)
	}
	waitForCondition(t, time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return openedPage == "deepl"
	}, "expected DeepL console page to open")

	if err := tt.Click("测试连接"); err != nil {
		t.Fatalf("Click 测试连接: %v", err)
	}
	waitForCondition(t, time.Second, func() bool {
		tt.Frame()
		return tt.HasText("连接成功")
	}, "expected connection test success")
	mu.Lock()
	gotTested := testedSvc
	mu.Unlock()
	if gotTested != "deepl" {
		t.Fatalf("testedSvc = %q, want deepl", gotTested)
	}

	if err := tt.Click("收起DeepL"); err != nil {
		t.Fatalf("Click 收起DeepL: %v", err)
	}

	// Expand Baidu, enter both credentials, save, and verify configured status updates.
	if err := tt.Click("配置百度翻译"); err != nil {
		t.Fatalf("Click 配置百度翻译: %v", err)
	}
	tt.Frame()
	if err := tt.Click("编辑APP ID"); err != nil {
		t.Fatalf("focus APP ID: %v", err)
	}
	tt.Type("app-123")
	if err := tt.Click("编辑密钥"); err != nil {
		t.Fatalf("focus 密钥: %v", err)
	}
	tt.Type("sec-456")
	tt.Frame()
	settingsScrollClick(t, tt, "保存配置", 780, 600)
	waitForCondition(t, time.Second, func() bool {
		tt.Frame()
		mu.Lock()
		defer mu.Unlock()
		return slices.Contains(configured, "baidu") && slices.Contains(stored, keys.BaiduAppID) && slices.Contains(stored, keys.BaiduKey)
	}, "expected credential saved status")
}

func TestSettingsConsolePageDoesNotBlockUIOrSettingsWrites(t *testing.T) {
	// A wall-clock failsafe used to unblock the shell during slow CI rendering,
	// falsely blaming UI dispatch. Only the test releases the blocked operation.
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	opened := make(chan string, 1)
	written := make(chan config.Settings, 1)
	invalidated := make(chan struct{}, 1)
	current := testSettingsConfig()
	v := NewView(TabServices, current, Actions{
		Credentials:  func(string) (CredentialStatus, error) { return CredentialStatus{}, nil },
		GetSettings:  func() config.Settings { return current },
		SaveSettings: func(s config.Settings) error { current = s; written <- s; return nil },
		Invalidate: func() {
			select {
			case invalidated <- struct{}{}:
			default:
			}
		},
		OpenKeyPage: func(service string) error {
			opened <- service
			<-release
			return errors.New("打开控制台失败")
		},
	})
	defer v.Dispose()
	for {
		v.mu.Lock()
		loaded := v.credentialsLoaded
		v.mu.Unlock()
		if loaded {
			break
		}
		<-invalidated
	}
	tt := ui.NewTester(v.Render, 780, 900)
	settingsClick(t, tt, "配置OpenAI")
	settingsClick(t, tt, "前往控制台获取凭证")
	if service := <-opened; service != "openai" {
		t.Fatalf("opened service = %q, want openai", service)
	}

	// Both the clicks and the persistent write must finish before shell release.
	settingsClick(t, tt, "命名转换")
	settingsClick(t, tt, "切换自动拆分标识符")
	if settings := <-written; settings.Identifier.AutoSplit {
		t.Fatal("settings write did not apply")
	}
	unblock()
	for {
		v.mu.Lock()
		toast := v.pendingToast
		v.mu.Unlock()
		if toast == "打开控制台失败" {
			break
		}
		<-invalidated
	}
	tt.Frame()
	if !tt.HasText("打开控制台失败") {
		t.Fatal("browser failure toast was not rendered")
	}
}

func TestSettingsConsolePageResultsRespectWindowLifetime(t *testing.T) {
	for _, outcome := range []string{"success", "error", "detach", "Dispose"} {
		t.Run(outcome, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				release := make(chan struct{})
				invalidations := 0
				v := NewView(TabServices, testSettingsConfig(), Actions{
					OpenKeyPage: func(string) error {
						<-release
						if outcome == "success" {
							return nil
						}
						return errors.New("打开控制台失败")
					},
					Invalidate: func() { invalidations++ },
				})
				defer v.Dispose()
				v.openKeyPageAsync("openai")
				synctest.Wait()
				switch outcome {
				case "detach":
					v.DetachWindow()
					v.SetActiveTab(TabServices)
				case "Dispose":
					v.Dispose()
				}
				before := invalidations
				close(release)
				synctest.Wait()
				v.mu.Lock()
				toast := v.pendingToast
				v.mu.Unlock()
				if outcome == "error" {
					if toast != "打开控制台失败" || invalidations != before+1 {
						t.Fatalf("error not published: toast=%q invalidations=%d", toast, invalidations-before)
					}
				} else if toast != "" || invalidations != before {
					t.Fatalf("unexpected UI update: toast=%q invalidations=%d", toast, invalidations-before)
				}
			})
		})
	}
}

func TestSettingsViewHotkeysTab(t *testing.T) {
	var mu sync.Mutex
	current := testSettingsConfig()
	current.Hotkeys.Selection = capturedHotkey(ui.Ctrl|ui.Alt, "B", runtime.GOOS)

	v := NewView(TabHotkeys, current, Actions{
		GetSettings: func() config.Settings {
			mu.Lock()
			defer mu.Unlock()
			return current
		},
		SaveSettings: func(s config.Settings) error {
			mu.Lock()
			defer mu.Unlock()
			current = s
			return nil
		},
	})
	defer v.Dispose()

	tt := ui.NewTester(v.Render, 780, 600)
	tt.Frame()

	if err := tt.Click("翻译面板快捷键"); err != nil {
		t.Fatalf("Click 翻译面板快捷键: %v", err)
	}
	tt.Frame()
	if !tt.HasText("按下组合键…") {
		t.Fatal("expected listening label 按下组合键…")
	}

	// Escape cancels listening.
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if tt.HasText("按下组合键…") {
		t.Fatal("Escape did not cancel listening")
	}

	// Immediate Escape right after clicking (no extra tt.Frame() in between) cancels listening.
	if err := tt.Click("划词翻译快捷键"); err != nil {
		t.Fatalf("Click 划词翻译快捷键: %v", err)
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if tt.HasText("按下组合键…") {
		t.Fatal("immediate Escape did not cancel listening")
	}

	// Switching tabs cancels listening.
	if err := tt.Click("划词翻译快捷键"); err != nil {
		t.Fatalf("Click 划词翻译快捷键: %v", err)
	}
	if err := tt.Click("通用"); err != nil {
		t.Fatalf("Click 通用 tab: %v", err)
	}
	if err := tt.Click("快捷键"); err != nil {
		t.Fatalf("Click 快捷键 tab: %v", err)
	}
	if tt.HasText("按下组合键…") {
		t.Fatal("tab switch did not cancel listening")
	}

	// Busy key reports conflict.
	if err := tt.Click("翻译面板快捷键"); err != nil {
		t.Fatalf("Click 翻译面板快捷键: %v", err)
	}
	tt.Frame()
	tt.Key(ui.Ctrl|ui.Alt, ui.KeyB)
	tt.Frame()
	if !tt.HasText("该组合键已用于划词翻译，请换一个组合键") {
		t.Fatalf("expected hotkey conflict text, texts=%v", tt.Texts())
	}

	// Valid key updates and saves.
	if err := tt.Click("翻译面板快捷键"); err != nil {
		t.Fatalf("Click 翻译面板快捷键: %v", err)
	}
	tt.Frame()
	tt.Key(ui.Ctrl|ui.Shift, ui.KeyK)
	waitForCondition(t, time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return current.Hotkeys.Panel == "Ctrl+Shift+K"
	}, "expected panel hotkey saved as Ctrl+Shift+K")
}

func TestSettingsViewNamingTabAndThemeFollow(t *testing.T) {
	current := testSettingsConfig()
	current.CopyFormats = []string{"lowerCamel", "snake"}

	v := NewView(TabNaming, current, Actions{
		GetSettings: func() config.Settings { return current },
		SaveSettings: func(s config.Settings) error {
			current = config.Normalize(s)
			return nil
		},
	})
	defer v.Dispose()

	tt := ui.NewTester(v.Render, 780, 600)
	tt.Frame()

	// The last enabled switch used to be locked on.
	for _, step := range []struct {
		label string
		want  []string
	}{
		{"切换下划线", []string{"lowerCamel"}},
		{"切换小驼峰", []string{}},
		{"切换下划线", []string{"snake"}},
	} {
		settingsClick(t, tt, step.label)
		waitSettingsIdle(t, v)
		if !slices.Equal(current.CopyFormats, step.want) {
			t.Fatalf("after %s: copy formats = %v, want %v", step.label, current.CopyFormats, step.want)
		}
	}

	// Verify light/dark theme switching updates root panel color dynamically.
	lightPx := tt.Image().RGBAAt(220, 5)
	if lightPx.R != lightSettingsPalette.panel.R || lightPx.G != lightSettingsPalette.panel.G || lightPx.B != lightSettingsPalette.panel.B {
		t.Fatalf("light panel pixel = %+v, want %+v", lightPx, lightSettingsPalette.panel)
	}
	tt.SetDark(true)
	tt.Frame()
	darkPx := tt.Image().RGBAAt(220, 5)
	if darkPx.R != darkSettingsPalette.panel.R || darkPx.G != darkSettingsPalette.panel.G || darkPx.B != darkSettingsPalette.panel.B {
		t.Fatalf("dark panel pixel = %+v, want %+v", darkPx, darkSettingsPalette.panel)
	}

	// External update reconciles when no write is pending.
	ext := current
	ext.Identifier.StripMarkdown = true
	v.ApplyExternalSettings(ext)
	tt.Frame()
	v.mu.Lock()
	gotStrip := v.settings.Identifier.StripMarkdown
	v.mu.Unlock()
	if !gotStrip {
		t.Fatal("expected external settings update to reconcile")
	}
}

func TestSettingsServiceDraftSavesOnlyOnExplicitAction(t *testing.T) {
	// Blurring and folding a card used to save text independently from credentials.
	current := testSettingsConfig()
	var writes []ServiceChanges
	v := NewView(TabServices, current, Actions{
		GetSettings: func() config.Settings { return current },
		SaveService: func(name string, changes ServiceChanges) (ServiceSaveResult, error) {
			writes = append(writes, changes)
			applyServiceTestOptions(&current, name, changes)
			return ServiceSaveResult{}, nil
		},
		Credentials: func(string) (CredentialStatus, error) {
			return CredentialStatus{Stored: []string{keys.OpenAIKey}, Configured: []string{"openai"}}, nil
		},
	})
	defer v.Dispose()
	waitSettingsIdle(t, v)
	tt := ui.NewTester(v.Render, 780, 660)
	settingsClick(t, tt, "配置OpenAI")
	settingsClick(t, tt, "编辑Base URL")
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("https://proxy.example/v1")
	settingsClick(t, tt, "编辑API Key")
	tt.Type("replacement")
	tt.Key(0, ui.KeyEnter)
	settingsClick(t, tt, "收起OpenAI")
	tt.Frame()
	waitSettingsIdle(t, v)
	if len(writes) != 0 || !tt.HasText("有未保存的更改") {
		t.Fatal("collapsing committed or lost the draft")
	}
	settingsClick(t, tt, "配置OpenAI")
	settingsScrollClick(t, tt, "保存配置", 780, 660)
	waitSettingsIdle(t, v)
	tt.Frame()
	if len(writes) != 1 || writes[0].BaseURL == nil || *writes[0].BaseURL != "https://proxy.example/v1" ||
		writes[0].Credentials[keys.OpenAIKey] != "replacement" || !tt.HasText("配置已保存") {
		t.Fatal("the service's options and credentials were not saved together")
	}
}

func TestSettingsServiceDraftDiscardedOnLeave(t *testing.T) {
	// Leaving the editor used to retain both its unsaved options and credentials.
	for _, closeWindow := range []bool{false, true} {
		t.Run(fmt.Sprintf("closeWindow=%t", closeWindow), func(t *testing.T) {
			cfg := testSettingsConfig()
			v := NewView(TabServices, cfg, Actions{
				SaveService: func(string, ServiceChanges) (ServiceSaveResult, error) {
					t.Error("leaving the editor must not save a draft")
					return ServiceSaveResult{}, nil
				},
				Credentials: func(string) (CredentialStatus, error) {
					return CredentialStatus{Stored: []string{keys.OpenAIKey}, Configured: []string{"openai"}}, nil
				},
			})
			defer v.Dispose()
			waitSettingsIdle(t, v)
			tt := ui.NewTester(v.Render, 780, 660)
			settingsClick(t, tt, "配置OpenAI")
			settingsClick(t, tt, "编辑Base URL")
			tt.Key(ui.Cmd, ui.KeyA)
			tt.Type("https://draft.example/v1")
			settingsClick(t, tt, "清除API Key")
			if _, ok := tt.Find("撤销清除API Key"); !ok {
				t.Fatal("credential removal was not staged")
			}

			if closeWindow {
				v.DetachWindow()
				v.SetActiveTab(TabServices)
				tt = ui.NewTester(v.Render, 780, 660)
				settingsClick(t, tt, "配置OpenAI")
			} else {
				settingsClick(t, tt, "通用")
				settingsClick(t, tt, "翻译服务")
			}
			tt.Frame()
			waitSettingsIdle(t, v)
			endpoint, found := tt.AccessByRole(ui.RoleTextField, "编辑Base URL")
			if !found || endpoint.Value != cfg.Services.OpenAI.BaseURL {
				t.Fatalf("editor did not restore the saved endpoint: %+v", endpoint)
			}
			save, found := tt.AccessByRole(ui.RoleButton, "保存配置")
			if !found || !save.Disabled {
				t.Fatal("save is still available after leaving the editor")
			}
			if _, ok := tt.Find("清除API Key"); !ok {
				t.Fatal("editor retained a pending credential removal")
			}
		})
	}
}

func applyServiceTestOptions(current *config.Settings, name string, changes ServiceChanges) {
	if changes.BaseURL != nil {
		current.Services.OpenAI.BaseURL = *changes.BaseURL
	}
	if changes.Model != nil {
		*current.Services.Model(name) = *changes.Model
	}
	*current = config.Normalize(*current)
}

func TestSettingsViewSaveFIFOAndDisposeDrain(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		started := make(chan struct{})
		var saved []string
		v := NewView(TabServices, testSettingsConfig(), Actions{
			SaveSettings: func(s config.Settings) error {
				if len(saved) == 0 {
					close(started)
					<-release
				}
				saved = append(saved, s.Services.OpenAI.BaseURL)
				return nil
			},
		})
		defer v.Dispose()
		var want []string
		for i := 0; i < 25; i++ {
			next := testSettingsConfig()
			next.Services.OpenAI.BaseURL = fmt.Sprintf("https://seq.example/%d", i)
			want = append(want, next.Services.OpenAI.BaseURL)
			v.mu.Lock()
			v.enqueueSaveLocked(next)
			v.mu.Unlock()
		}
		<-started
		// Closing drains the explicit writes already in flight.
		v.Dispose()
		synctest.Wait()
		if len(saved) != 0 {
			t.Fatal("save escaped the barrier")
		}
		close(release)
		synctest.Wait()
		if !slices.Equal(saved, want) {
			t.Fatalf("saved snapshots = %v, want FIFO %v", saved, want)
		}
		v.mu.Lock()
		pending := v.pendingWrites
		v.mu.Unlock()
		if pending != 0 {
			t.Fatalf("pending writes after drain = %d", pending)
		}
	})
}

func settingsClick(t *testing.T, tt *ui.Tester, label string) {
	t.Helper()
	if err := tt.Click(label); err != nil {
		t.Fatal(err)
	}
}

func settingsScrollClick(t *testing.T, tt *ui.Tester, label string, width, height int) {
	t.Helper()
	// Font metrics differ across platforms; scroll the real viewport instead of
	// assuming every expanded drawer fits inside the test window.
	for range 8 {
		bounds, ok := tt.Find(label)
		if !ok {
			t.Fatalf("missing control %q", label)
		}
		if bounds.Y >= 8 && bounds.Y+bounds.H <= float32(height)-8 {
			settingsClick(t, tt, label)
			return
		}
		dy := bounds.Y + bounds.H/2 - float32(height)/2
		tt.Scroll(float32(width)-24, float32(height)/2, 0, dy)
	}
	t.Fatalf("control %q is still outside the viewport after scrolling", label)
}

func TestSettingsViewModelsIndependentAndNormalized(t *testing.T) {
	for _, svc := range []string{"openai", "siliconflow", "zhipu"} {
		t.Run(svc, func(t *testing.T) {
			current := config.Default()
			var writes int
			v := NewView(TabServices, current, Actions{
				GetSettings: func() config.Settings { return current },
				SaveService: func(name string, changes ServiceChanges) (ServiceSaveResult, error) {
					writes++
					applyServiceTestOptions(&current, name, changes)
					if len(changes.Credentials) != 0 {
						t.Error("model-only edit modified credentials")
					}
					return ServiceSaveResult{}, nil
				},
				Credentials: func(string) (CredentialStatus, error) { return CredentialStatus{Configured: []string{svc}}, nil },
			})
			defer v.Dispose()
			waitSettingsIdle(t, v)
			tt := ui.NewTester(v.Render, 780, 500)
			click := func(label string) { settingsScrollClick(t, tt, label, 780, 500) }
			click("配置" + shared.ServiceName(svc))
			click("编辑模型")
			tt.Key(ui.Cmd, ui.KeyA)
			tt.Type(" custom-model ")
			click("保存配置")
			waitSettingsIdle(t, v)
			tt.Frame()
			want := config.Default()
			*want.Services.Model(svc) = "custom-model"
			if current.Services.OpenAI != want.Services.OpenAI || current.Services.SiliconFlow != want.Services.SiliconFlow || current.Services.Zhipu != want.Services.Zhipu {
				t.Fatalf("model edit leaked to other services: %+v", current.Services)
			}
			click("编辑模型")
			tt.Key(ui.Cmd, ui.KeyA)
			tt.Key(0, ui.KeyBackspace)
			click("保存配置")
			waitSettingsIdle(t, v)
			defaults := config.Default()
			if current.Services.OpenAI != defaults.Services.OpenAI || current.Services.SiliconFlow != defaults.Services.SiliconFlow || current.Services.Zhipu != defaults.Services.Zhipu || writes != 2 {
				t.Fatalf("blank model did not restore defaults: %+v", current.Services)
			}
		})
	}
}

func TestSettingsViewCredentialClearAndReorderPreserveDraft(t *testing.T) {
	current := testSettingsConfig()
	stored := []string{keys.OpenAIKey}
	var cleared string
	v := NewView(TabServices, current, Actions{
		GetSettings:  func() config.Settings { return current },
		SaveSettings: func(s config.Settings) error { current = s; return nil },
		Credentials: func(string) (CredentialStatus, error) {
			status := CredentialStatus{Stored: slices.Clone(stored)}
			if len(stored) > 0 {
				status.Configured = []string{"openai"}
			}
			return status, nil
		},
		SaveService: func(_ string, changes ServiceChanges) (ServiceSaveResult, error) {
			value, ok := changes.Credentials[keys.OpenAIKey]
			if !ok || value != "" {
				t.Error("expected credential deletion")
			}
			cleared = keys.OpenAIKey
			stored = nil
			current.Services.Enabled = []string{"google"}
			return ServiceSaveResult{}, nil
		},
	})
	defer v.Dispose()
	waitSettingsIdle(t, v)
	tt := ui.NewTester(v.Render, 780, 900)
	settingsClick(t, tt, "配置OpenAI")
	settingsClick(t, tt, "编辑API Key")
	tt.Type("unfinished-secret")
	v.mu.Lock()
	v.applyDragOrderLocked([]string{"openai", "google"})
	v.mu.Unlock()
	waitSettingsIdle(t, v)
	tt.Frame()
	v.mu.Lock()
	draft := v.serviceDrafts["openai"].credentials[keys.OpenAIKey].value
	expanded := v.expanded["openai"]
	v.mu.Unlock()
	if !tt.Focused("编辑API Key") {
		t.Fatal("reordering lost editor focus")
	}
	if current.Services.Order[0] != "openai" || draft != "unfinished-secret" || !expanded || tt.HasText("unfinished-secret") {
		t.Fatalf("reorder: order=%v draft=%q expanded=%v hasText=%v", current.Services.Order, draft, expanded, tt.HasText("unfinished-secret"))
	}
	settingsClick(t, tt, "启用OpenAI")
	waitSettingsIdle(t, v)
	tt.Frame()
	settingsClick(t, tt, "配置OpenAI")
	v.mu.Lock()
	draft = v.serviceDrafts["openai"].credentials[keys.OpenAIKey].value
	v.mu.Unlock()
	if draft != "unfinished-secret" {
		t.Fatal("group change lost credential draft")
	}
	settingsClick(t, tt, "清除API Key")
	settingsClick(t, tt, "撤销清除API Key")
	if cleared != "" {
		t.Fatal("staging and undoing removal modified the stored credential")
	}
	settingsClick(t, tt, "清除API Key")
	settingsClick(t, tt, "放弃更改")
	if cleared != "" {
		t.Fatal("discarding removal modified the stored credential")
	}
	settingsClick(t, tt, "清除API Key")
	settingsClick(t, tt, "保存配置")
	waitSettingsIdle(t, v)
	tt.Frame()
	v.mu.Lock()
	draft = v.serviceDrafts["openai"].credentials[keys.OpenAIKey].value
	configured := slices.Contains(v.credentials.Configured, "openai")
	v.mu.Unlock()
	if cleared != keys.OpenAIKey || configured || draft != "" || slices.Contains(current.Services.Enabled, "openai") {
		t.Fatal("credential clear did not refresh presence, draft and enablement")
	}
}

func TestSettingsUndoCredentialRemovalRestoresReplacement(t *testing.T) {
	// Staging removal used to overwrite the replacement before undo could restore it.
	var saved string
	v := NewView(TabServices, testSettingsConfig(), Actions{
		Credentials: func(string) (CredentialStatus, error) {
			return CredentialStatus{Stored: []string{keys.OpenAIKey}, Configured: []string{"openai"}}, nil
		},
		SaveService: func(_ string, changes ServiceChanges) (ServiceSaveResult, error) {
			saved = changes.Credentials[keys.OpenAIKey]
			return ServiceSaveResult{}, nil
		},
	})
	defer v.Dispose()
	waitSettingsIdle(t, v)
	tt := ui.NewTester(v.Render, 780, 660)
	settingsClick(t, tt, "配置OpenAI")
	settingsClick(t, tt, "编辑API Key")
	tt.Type("replacement")
	settingsClick(t, tt, "清除API Key")
	settingsClick(t, tt, "撤销清除API Key")
	settingsClick(t, tt, "保存配置")
	waitSettingsIdle(t, v)
	if saved != "replacement" {
		t.Fatal("undo did not restore the replacement credential for saving")
	}
}

func TestSettingsServiceReadbackPreservesNewerPublishedSettings(t *testing.T) {
	// A service save could overwrite preferences published during its readback.
	synctest.Test(t, func(t *testing.T) {
		committed := testSettingsConfig()
		committed.Services.OpenAI.Model = "custom-model"
		newer := committed
		newer.OpenAtLogin = !committed.OpenAtLogin
		read := make(chan struct{})
		release := make(chan struct{})
		v := NewView(TabServices, testSettingsConfig(), Actions{
			GetSettings: func() config.Settings {
				close(read)
				<-release
				return committed
			},
			SaveService: func(string, ServiceChanges) (ServiceSaveResult, error) {
				return ServiceSaveResult{}, nil
			},
		})
		defer v.Dispose()
		v.editServiceModel("openai", "custom-model")
		v.saveService("openai")
		<-read
		// Attempt the external publication before the readback completes.
		// If readback owns the view lock, publication must run after it.
		if v.mu.TryLock() {
			v.mu.Unlock()
			v.ApplyExternalSettings(newer)
			close(release)
		} else {
			close(release)
			v.ApplyExternalSettings(newer)
		}
		synctest.Wait()
		if v.settings.OpenAtLogin != newer.OpenAtLogin || v.settings.Services.OpenAI.Model != "custom-model" {
			t.Fatal("service save did not retain the newer settings")
		}
	})
}

func TestSettingsCredentialRemovalSurvivesReadbackFailure(t *testing.T) {
	// A failed readback used to hide the successful disablement and show the deleted key.
	current := testSettingsConfig()
	var reads []string
	var v *View
	v = NewView(TabServices, current, Actions{
		GetSettings: func() config.Settings { return current },
		Credentials: func(name string) (CredentialStatus, error) {
			reads = append(reads, name)
			switch len(reads) {
			case 1:
				return CredentialStatus{
					Stored: []string{keys.OpenAIKey, keys.DeeplKey}, Configured: []string{"openai", "deepl"},
				}, nil
			case 2:
				return CredentialStatus{}, errors.New("keyring locked")
			default:
				return CredentialStatus{}, nil
			}
		},
		SaveService: func(string, ServiceChanges) (ServiceSaveResult, error) {
			current.Services.Enabled = []string{"google"}
			v.ApplyExternalSettings(current)
			return ServiceSaveResult{Disabled: true}, nil
		},
	})
	defer v.Dispose()
	waitSettingsIdle(t, v)
	v.toggleCredentialRemoval("openai", keys.OpenAIKey)
	v.saveService("openai")
	waitSettingsIdle(t, v)
	v.mu.Lock()
	toast := v.pendingToast
	enabled := slices.Contains(v.settings.Services.Enabled, "openai")
	v.mu.Unlock()
	if toast != "OpenAI 已停用" || enabled {
		t.Fatal("the status refresh hid the disablement acknowledgement")
	}

	tt := ui.NewTester(v.Render, 780, 900)
	settingsClick(t, tt, "配置OpenAI")
	if _, found := tt.Find("清除API Key"); found {
		t.Fatal("the deleted API key still appears saved")
	}
	settingsClick(t, tt, "重试读取OpenAI状态")
	waitForCondition(t, time.Second, func() bool {
		v.mu.Lock()
		defer v.mu.Unlock()
		return len(reads) == 3 && !v.credentialReads["openai"]
	}, "service credential retry did not finish")
	tt.Frame()
	if _, found := tt.Find("重试读取OpenAI状态"); found {
		t.Fatal("successful retry did not clear the readback error")
	}
	if !slices.Equal(reads, []string{"", "openai", "openai"}) ||
		!slices.Equal(v.credentials.Stored, []string{keys.DeeplKey}) ||
		!slices.Equal(v.credentials.Configured, []string{"deepl"}) {
		t.Fatalf("scoped read affected other services: reads=%v status=%+v", reads, v.credentials)
	}
}

func TestSettingsViewProbeWaitsForSavedModelAndIgnoresDisposedResult(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		current := testSettingsConfig()
		saveRelease := make(chan struct{})
		probeRelease := make(chan struct{})
		var tested []string
		v := NewView(TabServices, current, Actions{
			GetSettings: func() config.Settings { return current },
			SaveService: func(name string, changes ServiceChanges) (ServiceSaveResult, error) {
				<-saveRelease
				applyServiceTestOptions(&current, name, changes)
				return ServiceSaveResult{}, nil
			},
			TestService: func(_ context.Context, svc string) string {
				tested = append(tested, current.Services.OpenAI.Model)
				<-probeRelease
				return ""
			},
		})
		defer v.Dispose()
		v.editServiceModel("openai", "saved-model")
		v.saveService("openai")
		v.testConnection("openai")
		synctest.Wait()
		if len(tested) != 0 {
			t.Fatal("probe started before the model was saved")
		}
		close(saveRelease)
		synctest.Wait()
		v.testConnection("openai")
		synctest.Wait()
		if !slices.Equal(tested, []string{"saved-model"}) {
			t.Fatalf("probe used models %v", tested)
		}
		v.Dispose()
		close(probeRelease)
		synctest.Wait()
		v.mu.Lock()
		result := v.testStatuses["openai"].result
		v.mu.Unlock()
		if result != "正在测试…" {
			t.Fatalf("disposed view accepted probe result %q", result)
		}
	})
}

// Waiting under the view mutex also observes completed backend writes before
// assertions inspect the fake backend. Tester itself stays on the test goroutine.
func waitSettingsIdle(t *testing.T, v *View) {
	t.Helper()
	waitForCondition(t, time.Second, func() bool {
		v.mu.Lock()
		defer v.mu.Unlock()
		if v.pendingWrites != 0 || (v.actions.Credentials != nil && !v.credentialsLoaded) {
			return false
		}
		for _, busy := range v.serviceSaving {
			if busy {
				return false
			}
		}
		return true
	}, "settings work did not settle")
}

func TestSettingsQueuedPreferencePreservesServiceSave(t *testing.T) {
	// A preference queued during a service save used to carry a whole stale
	// service snapshot, which could restore the old endpoint and enablement.
	synctest.Test(t, func(t *testing.T) {
		current := testSettingsConfig()
		release := make(chan struct{})
		v := NewView(TabServices, current, Actions{
			GetSettings: func() config.Settings { return current },
			SaveService: func(name string, changes ServiceChanges) (ServiceSaveResult, error) {
				<-release
				applyServiceTestOptions(&current, name, changes)
				current.Services.Enabled = []string{"google"}
				return ServiceSaveResult{}, nil
			},
			SaveSettings: func(next config.Settings) error { current = next; return nil },
		})
		defer v.Dispose()
		v.editServiceBaseURL("openai", "https://proxy.example/v1")
		v.toggleCredentialRemoval("openai", keys.OpenAIKey)
		v.saveService("openai")
		synctest.Wait()

		v.mu.Lock()
		next := cloneSettings(v.settings)
		next.TargetLanguage = "ja"
		v.enqueueSaveLocked(next)
		v.mu.Unlock()
		close(release)
		synctest.Wait()
		if current.TargetLanguage != "ja" || current.Services.OpenAI.BaseURL != "https://proxy.example/v1" ||
			slices.Contains(current.Services.Enabled, "openai") {
			t.Fatal("the unrelated preference overwrote part of the service save")
		}
	})
}

func TestSettingsViewFailedSaveDoesNotDropNextSnapshot(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		current := testSettingsConfig()
		var attempts []string
		v := NewView(TabGeneral, current, Actions{
			GetSettings: func() config.Settings { return current },
			SaveSettings: func(s config.Settings) error {
				attempts = append(attempts, s.TargetLanguage)
				if len(attempts) == 1 {
					// A failed save used to include the underlying file path in the toast.
					return &fs.PathError{Op: "write", Path: "/Users/private/settings.json", Err: syscall.ENOSPC}
				}
				current = s
				return nil
			},
		})
		defer v.Dispose()
		v.mu.Lock()
		next := cloneSettings(v.settings)
		next.TargetLanguage = "ja"
		v.enqueueSaveLocked(next)
		next.TargetLanguage = "fr"
		next.OpenAtLogin = true
		v.enqueueSaveLocked(next)
		v.mu.Unlock()
		synctest.Wait()
		if !slices.Equal(attempts, []string{"ja", "fr"}) || current.TargetLanguage != "fr" || !current.OpenAtLogin {
			t.Fatalf("queued save after failure was lost: attempts=%v settings=%+v", attempts, current)
		}
		v.mu.Lock()
		defer v.mu.Unlock()
		if v.pendingWrites != 0 || v.settings.TargetLanguage != "fr" || v.pendingToast != "保存失败：磁盘空间不足，请清理空间后重试" {
			t.Fatal("save failure did not reconcile the final state and report the error")
		}
	})
}

func TestSettingsViewDisposedCredentialsCannotUpdateView(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		invalidations := 0
		v := NewView(TabServices, testSettingsConfig(), Actions{
			Credentials: func(string) (CredentialStatus, error) {
				<-release
				return CredentialStatus{Stored: []string{keys.OpenAIKey}}, nil
			},
			Invalidate: func() { invalidations++ },
		})
		v.Dispose()
		close(release)
		synctest.Wait()
		v.mu.Lock()
		defer v.mu.Unlock()
		if v.credentialsLoaded || len(v.credentials.Stored) != 0 || invalidations != 0 {
			t.Fatal("late credential result updated a disposed view")
		}
	})
}

func TestSettingsViewFailedModelSaveStopsProbeUntilNextSave(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		current := testSettingsConfig()
		failed := true
		probes := 0
		v := NewView(TabServices, current, Actions{
			GetSettings: func() config.Settings { return current },
			SaveService: func(name string, changes ServiceChanges) (ServiceSaveResult, error) {
				if failed {
					return ServiceSaveResult{}, errors.New("disk full")
				}
				applyServiceTestOptions(&current, name, changes)
				return ServiceSaveResult{}, nil
			},
			TestService: func(_ context.Context, _ string) string { probes++; return "" },
		})
		defer v.Dispose()
		v.editServiceModel("openai", "new-model")
		v.saveService("openai")
		synctest.Wait()
		v.testConnection("openai")
		synctest.Wait()
		status := v.testStatuses["openai"]
		if probes != 0 || status.testing || !status.isError || current.Services.OpenAI.Model == "new-model" {
			t.Fatalf("failed save changed settings or probed: calls=%d status=%+v", probes, status)
		}
		failed = false
		v.saveService("openai")
		synctest.Wait()
		v.testConnection("openai")
		synctest.Wait()
		if probes != 1 || current.Services.OpenAI.Model != "new-model" {
			t.Fatal("retry lost the draft or did not unblock the probe")
		}
	})
}

func TestSettingsViewDragOnlySavesCompletedDrop(t *testing.T) {
	for _, cancel := range []bool{true, false} {
		t.Run(fmt.Sprintf("cancel=%t", cancel), func(t *testing.T) {
			current := testSettingsConfig()
			var saved []config.Settings
			v := NewView(TabServices, current, Actions{
				GetSettings:  func() config.Settings { return current },
				SaveSettings: func(s config.Settings) error { current = s; saved = append(saved, cloneSettings(s)); return nil },
				Credentials:  func(string) (CredentialStatus, error) { return CredentialStatus{Configured: []string{"openai"}}, nil },
			})
			defer v.Dispose()
			waitSettingsIdle(t, v)
			tt := ui.NewTester(v.Render, 780, 900)
			settingsClick(t, tt, "配置OpenAI")
			settingsClick(t, tt, "编辑API Key")
			tt.Type("draft-secret")
			from, ok := tt.Find("拖动OpenAI")
			if !ok {
				t.Fatal("missing drag handle")
			}
			to, ok := tt.Find("Google")
			if !ok {
				t.Fatal("missing Google target")
			}
			x, y := from.X+from.W/2, from.Y+from.H/2
			tx, ty := to.X+to.W/2, to.Y+to.H/2
			tt.Press(x, y)
			tt.Move(x+10, y)
			tt.Move(tx, ty)
			v.mu.Lock()
			pending := v.pendingWrites
			v.mu.Unlock()
			if pending != 0 || len(saved) != 0 {
				t.Fatal("drag preview saved before drop")
			}
			if cancel {
				tt.Key(0, ui.KeyEscape)
			}
			tt.Release(tx, ty)
			waitSettingsIdle(t, v)
			tt.Frame()
			if cancel && len(saved) != 0 {
				t.Fatal("cancelled drag was saved")
			}
			if !cancel && (len(saved) != 1 || current.Services.Order[0] != "openai") {
				t.Fatalf("drop did not reorder: saved=%d order=%v", len(saved), current.Services.Order)
			}
			v.mu.Lock()
			draft := v.serviceDrafts["openai"].credentials[keys.OpenAIKey].value
			expanded := v.expanded["openai"]
			v.mu.Unlock()
			if draft != "draft-secret" || !expanded || !slices.Contains(current.Services.Enabled, "openai") {
				t.Fatal("drag lost the drawer, draft or enablement")
			}
		})
	}
}

func TestSettingsViewDragBackToOriginalPosition(t *testing.T) {
	for _, reduceMotion := range []bool{false, true} {
		for _, down := range []bool{false, true} {
			t.Run(fmt.Sprintf("reduceMotion=%t/down=%t", reduceMotion, down), func(t *testing.T) {
				current := testSettingsConfig()
				saves := 0
				v := NewView(TabServices, current, Actions{
					GetSettings:  func() config.Settings { return current },
					SaveSettings: func(s config.Settings) error { current = s; saves++; return nil },
					Credentials:  func(string) (CredentialStatus, error) { return CredentialStatus{Configured: []string{"openai"}}, nil },
				})
				defer v.Dispose()
				waitSettingsIdle(t, v)
				tt := ui.NewTesterWith(v.Render, 780, 900, ui.TesterOptions{Preferences: ui.Preferences{ReduceMotion: reduceMotion}})
				source, target := "OpenAI", "Google"
				if down {
					source, target = target, source
				}
				from, ok := tt.Find("拖动" + source)
				if !ok {
					t.Fatal("missing source")
				}
				to, ok := tt.Find("拖动" + target)
				if !ok {
					t.Fatal("missing target")
				}
				x, y := from.X+from.W/2, from.Y+from.H/2
				tx, ty := to.X+to.W/2, to.Y+to.H/2
				tt.Press(x, y)
				tt.Move(x+8, y)
				tt.Move(tx, ty)
				for cycle := 0; cycle < 3; cycle++ {
					tt.Advance(200 * time.Millisecond)
					tt.Frame()
					v.mu.Lock()
					preview := slices.Clone(v.drag.preview)
					v.mu.Unlock()
					if !slices.Equal(preview, []string{"openai", "google"}) {
						t.Fatalf("preview before return = %v", preview)
					}
					tt.Move(x, y)
					tt.Advance(200 * time.Millisecond)
					tt.Frame()
					v.mu.Lock()
					preview = slices.Clone(v.drag.preview)
					pending := v.pendingWrites
					v.mu.Unlock()
					if !slices.Equal(preview, []string{"google", "openai"}) {
						t.Fatalf("preview after returning = %v", preview)
					}
					if pending != 0 || saves != 0 {
						t.Fatal("preview was saved before release")
					}
					if cycle < 2 {
						tt.Move(tx, ty)
					}
				}
				tt.Release(x, y)
				waitSettingsIdle(t, v)
				tt.Frame()
				if saves != 0 || !slices.Equal(current.Services.Order, testSettingsConfig().Services.Order) {
					t.Fatal("returning to the original position saved a different order")
				}
			})
		}
	}
}

func TestSettingsControllerSurvivesWindowReopen(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		current := testSettingsConfig()
		release := make(chan struct{})
		v := NewView(TabGeneral, current, Actions{GetSettings: func() config.Settings { return current }, SaveSettings: func(s config.Settings) error { <-release; current = s; return nil }})
		defer v.Dispose()
		v.mu.Lock()
		next := cloneSettings(v.settings)
		next.OpenAtLogin = true
		v.enqueueSaveLocked(next)
		v.mu.Unlock()
		synctest.Wait()
		v.DetachWindow()
		v.SetActiveTab(TabGeneral)
		v.mu.Lock()
		next = cloneSettings(v.settings)
		next.AutoUpdate = true
		v.enqueueSaveLocked(next)
		v.mu.Unlock()
		close(release)
		synctest.Wait()
		if !current.OpenAtLogin || !current.AutoUpdate {
			t.Fatal("reopening lost an old or new edit")
		}
	})
}

func TestSettingsCredentialJobsPublishInOrder(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		stored := map[string]bool{}
		release := make(chan struct{})
		calls := 0
		v := NewView(TabServices, testSettingsConfig(), Actions{
			SaveService: func(_ string, changes ServiceChanges) (ServiceSaveResult, error) {
				for key := range changes.Credentials {
					stored[key] = true
				}
				return ServiceSaveResult{}, nil
			},
			Credentials: func(string) (CredentialStatus, error) {
				calls++
				status := CredentialStatus{}
				for k := range stored {
					status.Stored = append(status.Stored, k)
				}
				if stored[keys.OpenAIKey] {
					status.Configured = append(status.Configured, "openai")
				}
				if stored[keys.DeeplKey] {
					status.Configured = append(status.Configured, "deepl")
				}
				if calls == 2 {
					<-release
				}
				return status, nil
			},
		})
		defer v.Dispose()
		synctest.Wait()
		v.editServiceCredential("openai", keys.OpenAIKey, "a")
		v.editServiceCredential("deepl", keys.DeeplKey, "b")
		v.saveService("openai")
		synctest.Wait()
		v.saveService("deepl")
		synctest.Wait()
		if stored[keys.DeeplKey] {
			t.Fatal("second credential job overtook the first refresh")
		}
		close(release)
		synctest.Wait()
		v.mu.Lock()
		configured := slices.Clone(v.credentials.Configured)
		v.mu.Unlock()
		if !slices.Contains(configured, "deepl") || !slices.Contains(configured, "openai") {
			t.Fatalf("lost credential state: %v", configured)
		}
	})
}

func TestSettingsKeylessServiceSwitchSavesEnablement(t *testing.T) {
	// Registering a provider with the runner must also make it available in settings.
	for _, service := range []string{"dictionary", "youdaoweb"} {
		t.Run(service, func(t *testing.T) {
			testSettingsKeylessServiceSwitch(t, service)
		})
	}
}

func testSettingsKeylessServiceSwitch(t *testing.T, service string) {
	t.Helper()
	defaults := config.Default()
	initiallyEnabled := slices.Contains(defaults.Services.Enabled, service)
	saved := make(chan config.Settings, 2)
	v := NewView(TabServices, defaults, Actions{
		SaveSettings: func(next config.Settings) error {
			saved <- next
			return nil
		},
		Credentials: func(string) (CredentialStatus, error) { return CredentialStatus{}, nil },
	})
	defer v.Dispose()
	waitSettingsIdle(t, v)
	tt := ui.NewTester(v.Render, 780, 1200)
	for _, wantEnabled := range []bool{!initiallyEnabled, initiallyEnabled} {
		settingsClick(t, tt, "启用"+shared.ServiceName(service))
		select {
		case cfg := <-saved:
			if slices.Contains(cfg.Services.Enabled, service) != wantEnabled {
				t.Fatalf("saved services = %v, want %s enabled = %t", cfg.Services.Enabled, service, wantEnabled)
			}
		case <-time.After(time.Second):
			t.Fatalf("no settings saved for %s enabled = %t", service, wantEnabled)
		}
	}
}

func TestSettingsConfiguredServiceMovesAheadOfKeylessAfterSave(t *testing.T) {
	// Saved services used to be buried among unused providers in catalogue order.
	cfg := config.Default()
	original := cloneSettings(cfg)
	status := CredentialStatus{
		Stored:     []string{keys.BaiduAppID, keys.DeeplKey},
		Configured: []string{"deepl", "google", "dictionary", "index-translate", "youdaoweb"},
	}
	release := make(chan struct{})
	var releaseOnce sync.Once
	finishSave := func() { releaseOnce.Do(func() { close(release) }) }
	defer finishSave()
	v := NewView(TabServices, cfg, Actions{
		GetSettings: func() config.Settings { return cfg },
		Credentials: func(string) (CredentialStatus, error) { return status, nil },
		SaveService: func(name string, changes ServiceChanges) (ServiceSaveResult, error) {
			<-release
			if name != "baidu" || changes.Credentials[keys.BaiduKey] != "example-key" {
				return ServiceSaveResult{}, errors.New("unexpected service edit")
			}
			status.Configured = append(slices.Clone(status.Configured), name)
			status.Stored = append(slices.Clone(status.Stored), keys.BaiduKey)
			return ServiceSaveResult{}, nil
		},
		SaveSettings: func(next config.Settings) error { cfg = next; return nil },
	})
	defer v.Dispose()
	waitSettingsIdle(t, v)
	tt := ui.NewTester(v.Render, 660, 500)
	assertAbove := func(first, second string) {
		t.Helper()
		a, okA := tt.Find(first)
		b, okB := tt.Find(second)
		if !okA || !okB || a.Y >= b.Y {
			t.Fatalf("expected %q above %q", first, second)
		}
	}
	assertAbove("启用DeepL", "启用Google")
	assertAbove("启用Google", "配置百度翻译")
	if _, available := tt.Find("启用百度翻译"); available {
		t.Fatal("a partial credential set offered enablement")
	}
	click := func(label string) { settingsScrollClick(t, tt, label, 660, 500) }
	click("配置百度翻译")
	click("编辑密钥")
	tt.Type("example-key")
	click("保存配置")
	tt.Frame()
	finishSave()
	waitSettingsIdle(t, v)
	tt.Frame()
	assertAbove("启用百度翻译", "启用DeepL")
	if !slices.Equal(cfg.Services.Enabled, original.Services.Enabled) || !slices.Equal(cfg.Services.Order, original.Services.Order) {
		t.Fatal("promoting the configured service changed translation settings")
	}
	if !tt.Focused("启用百度翻译") {
		t.Fatal("saving the pending service did not bring its switch into keyboard focus")
	}
	tt.Key(0, ui.KeySpace)
	waitSettingsIdle(t, v)
	if !slices.Contains(cfg.Services.Enabled, "baidu") || !slices.Equal(cfg.Services.Order, original.Services.Order) {
		t.Fatal("enabling the promoted service did not preserve translation order")
	}
}

func TestSettingsCredentialRemovalKeepsFocusedServiceVisible(t *testing.T) {
	// Removing the last required key moved the open editor and its save feedback offscreen.
	for _, moveFocus := range []bool{false, true} {
		t.Run(fmt.Sprintf("moveFocus=%t", moveFocus), func(t *testing.T) {
			status := CredentialStatus{Stored: []string{keys.AzureKey, keys.DeeplKey}, Configured: []string{"azure", "deepl"}}
			release := make(chan struct{})
			finishSave := sync.OnceFunc(func() { close(release) })
			defer finishSave()
			v := NewView(TabServices, config.Default(), Actions{
				Credentials: func(string) (CredentialStatus, error) { return status, nil },
				SaveService: func(_ string, changes ServiceChanges) (ServiceSaveResult, error) {
					<-release
					if key, cleared := changes.Credentials[keys.AzureKey]; !cleared || key != "" {
						return ServiceSaveResult{}, errors.New("expected subscription key removal")
					}
					status = CredentialStatus{Stored: []string{keys.DeeplKey}, Configured: []string{"deepl"}}
					return ServiceSaveResult{}, nil
				},
			})
			defer v.Dispose()
			waitSettingsIdle(t, v)
			tt := ui.NewTester(v.Render, 660, 500)
			click := func(label string) { settingsScrollClick(t, tt, label, 660, 500) }
			click("配置微软翻译")
			click("清除订阅密钥")
			click("保存配置")
			if moveFocus {
				click("配置DeepL")
			}
			finishSave()
			waitSettingsIdle(t, v)
			tt.Frame()
			if _, ok := tt.Find("启用微软翻译"); ok {
				t.Fatal("service still offers enablement after removing its key")
			}
			if moveFocus {
				if !tt.Focused("收起DeepL") {
					t.Fatal("save completion took focus from the other service")
				}
				return
			}
			if !tt.Focused("收起微软翻译") {
				t.Fatal("focus did not follow the service to its setup row")
			}
			r, ok := tt.Find("配置已保存")
			if !ok || r.H <= 0 || r.Y < 0 || r.Y+r.H > 500 {
				t.Fatalf("save feedback outside the viewport: %+v", r)
			}
			tt.Key(0, ui.KeySpace)
			if _, ok := tt.Find("配置微软翻译"); !ok {
				t.Fatal("the moved drawer could not be collapsed from the keyboard")
			}
		})
	}
}

func TestSettingsServiceSwitchKeepsFocusAcrossGroups(t *testing.T) {
	v := NewView(TabServices, testSettingsConfig(), Actions{})
	defer v.Dispose()
	v.mu.Lock()
	v.credentialsLoaded = true
	v.credentials = CredentialStatus{Configured: []string{"openai"}}
	v.mu.Unlock()
	tt := ui.NewTester(v.Render, 780, 900)
	settingsClick(t, tt, "启用OpenAI")
	if !tt.Focused("启用OpenAI") {
		t.Fatal("switch lost focus after moving to disabled group")
	}
	tt.Key(0, ui.KeySpace)
	tt.Frame()
	v.mu.Lock()
	enabled := slices.Contains(v.settings.Services.Enabled, "openai")
	v.mu.Unlock()
	if !enabled || !tt.Focused("启用OpenAI") {
		t.Fatal("focused switch could not move back to enabled group")
	}
}

func TestSettingsUsageRejectsOldRequests(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		first := make(chan struct{})
		second := make(chan struct{})
		calls := 0
		v := NewView(TabServices, testSettingsConfig(), Actions{DeeplUsage: func(context.Context) (DeeplUsage, error) {
			calls++
			id := calls
			if id == 1 {
				<-first
			} else {
				<-second
			}
			return DeeplUsage{Used: id, Limit: 10}, nil
		}})
		defer v.Dispose()
		v.expanded["deepl"] = true
		v.loadDeeplUsageAsync()
		synctest.Wait()
		// A new window may start a query before the canceled old one returns.
		v.DetachWindow()
		v.expanded["deepl"] = true
		v.loadDeeplUsageAsync()
		synctest.Wait()
		close(second)
		synctest.Wait()
		close(first)
		synctest.Wait()
		v.mu.Lock()
		text := v.deeplUsageText
		v.mu.Unlock()
		if text != "本月已用 2 / 10 字符" {
			t.Fatalf("old usage overwrote latest: %s", text)
		}
	})
}

func TestSettingsDetachRejectsProbeAndUsageResults(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		cancelled := false
		v := NewView(TabServices, testSettingsConfig(), Actions{
			TestService: func(ctx context.Context, _ string) string {
				<-ctx.Done()
				cancelled = true
				<-release
				return "old result"
			},
			DeeplUsage: func(context.Context) (DeeplUsage, error) { <-release; return DeeplUsage{Used: 5, Limit: 10}, nil },
		})
		defer v.Dispose()
		v.testConnection("openai")
		v.expanded["deepl"] = true
		v.loadDeeplUsageAsync()
		synctest.Wait()
		v.DetachWindow()
		synctest.Wait()
		if !cancelled {
			t.Fatal("closing window did not cancel the probe")
		}
		v.SetActiveTab(TabServices)
		close(release)
		synctest.Wait()
		v.mu.Lock()
		status := v.testStatuses["openai"]
		usage := v.deeplUsageText
		v.mu.Unlock()
		if status.result != "" || usage != "额度查询中…" {
			t.Fatal("old window result reached reopened UI")
		}
	})
}

func TestSettingsServiceSwitchKeepsFocusAfterRollback(t *testing.T) {
	release := make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	cfg := testSettingsConfig()
	v := NewView(TabServices, cfg, Actions{
		GetSettings:  func() config.Settings { return cfg },
		SaveSettings: func(config.Settings) error { <-release; return errors.New("disk full") },
		Credentials:  func(string) (CredentialStatus, error) { return CredentialStatus{Configured: []string{"openai"}}, nil },
	})
	defer v.Dispose()
	waitSettingsIdle(t, v)
	tt := ui.NewTester(v.Render, 780, 900)
	settingsClick(t, tt, "启用OpenAI")
	if !tt.Focused("启用OpenAI") {
		t.Fatal("optimistic switch lost focus")
	}
	releaseOnce.Do(func() { close(release) })
	waitSettingsIdle(t, v)
	tt.Frame()
	if !tt.Focused("启用OpenAI") {
		t.Fatal("rollback lost switch focus")
	}
	// Moving focus to another control must still work normally.
	settingsClick(t, tt, "配置OpenAI")
	settingsClick(t, tt, "编辑API Key")
	tt.Frame()
	if !tt.Focused("编辑API Key") {
		t.Fatal("switch reclaimed focus from editor")
	}
}

func TestSettingsQuitWaitsForQueuedWrites(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		current := testSettingsConfig()
		retries := 0
		v := NewView(TabServices, current, Actions{SaveSettings: func(s config.Settings) error { <-release; current = s; return nil }})
		v.mu.Lock()
		next := cloneSettings(v.settings)
		next.TargetLanguage = "ja"
		v.enqueueSaveLocked(next)
		v.mu.Unlock()
		v.editServiceBaseURL("openai", "https://unsaved.example/v1")
		retry := func() { retries++ }
		if v.PrepareQuit(retry) {
			t.Fatal("quit proceeded with a queued write")
		}
		if v.PrepareQuit(retry) {
			t.Fatal("repeated quit bypassed the pending write")
		}
		synctest.Wait()
		if retries != 0 {
			t.Fatal("quit retried before writes finished")
		}
		close(release)
		synctest.Wait()
		if retries != 1 || current.TargetLanguage != "ja" || current.Services.OpenAI.BaseURL != config.Default().Services.OpenAI.BaseURL || !v.PrepareQuit(retry) {
			t.Fatal("quit did not drain writes and retry exactly once")
		}
	})
}

func TestSettingsTargetLanguageSelectAlignmentAndToggle(t *testing.T) {
	for _, width := range []int{660, 780} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			v := NewView(TabGeneral, testSettingsConfig(), Actions{})
			defer v.Dispose()
			tt := ui.NewTester(v.Render, width, 600)
			trigger, ok := tt.Find("选择默认翻译语言")
			if !ok {
				t.Fatal("missing language selector")
			}
			settingsClick(t, tt, "选择默认翻译语言")
			popup, ok := tt.Find("默认翻译语言选项")
			if !ok || popup.X != trigger.X || popup.W != trigger.W || popup.Y < trigger.Y+trigger.H {
				t.Fatalf("popup=%+v found=%v trigger=%+v", popup, ok, trigger)
			}
			// The open popup owns pointer input; click the trigger coordinates through its dismissal layer.
			tt.ClickAt(trigger.X+trigger.W/2, trigger.Y+trigger.H/2)
			if _, ok := tt.Find("默认翻译语言选项"); ok {
				t.Fatal("second click did not close popup")
			}
			settingsClick(t, tt, "选择默认翻译语言")
			settingsClick(t, tt, "自动（中文↔英文）")
			waitSettingsIdle(t, v)
			v.mu.Lock()
			selected := v.settings.TargetLanguage
			v.mu.Unlock()
			if selected != "auto" {
				t.Fatalf("selected language=%q", selected)
			}
			if _, ok := tt.Find("默认翻译语言选项"); ok {
				t.Fatal("choosing a language left popup open")
			}
			settingsClick(t, tt, "选择默认翻译语言")
			tt.Key(0, ui.KeyEscape)
			if _, ok := tt.Find("默认翻译语言选项"); ok {
				t.Fatal("Escape did not close popup")
			}
		})
	}
}
