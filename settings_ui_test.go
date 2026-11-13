package main

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/egoist/mygo/ui"

	"sotto/internal/config"
	"sotto/internal/keys"
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

	v := newSettingsView(tabGeneral, current, settingsActions{
		getSettings: func() config.Settings {
			mu.Lock()
			defer mu.Unlock()
			return current
		},
		saveSettings: func(s config.Settings) error {
			mu.Lock()
			defer mu.Unlock()
			current = s
			saved = append(saved, s)
			return nil
		},
		credentials: func() (CredentialStatus, error) {
			return CredentialStatus{Stored: []string{keys.OpenAIKey}, Configured: []string{"openai"}}, nil
		},
	})
	defer v.dispose()

	tt := ui.NewTester(v.render, 780, 600)
	tt.Frame()

	if !tt.HasText("通用") || !tt.HasText("Windows 凭据保护") {
		t.Fatalf("missing general tab content: %v", tt.Texts())
	}

	if err := tt.Click("切换开机自启"); err != nil {
		t.Fatalf("Click 切换开机自启: %v", err)
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
		return len(saved) == 3
	}, "expected 3 saved snapshots for general switches")

	mu.Lock()
	last := saved[len(saved)-1]
	mu.Unlock()
	if !last.OpenAtLogin || last.Bubble.AutoHide || !last.AutoUpdate {
		t.Fatalf("unexpected saved flags: OpenAtLogin=%v AutoHide=%v AutoUpdate=%v", last.OpenAtLogin, last.Bubble.AutoHide, last.AutoUpdate)
	}
}

func TestSettingsViewServicesTab(t *testing.T) {
	var mu sync.Mutex
	current := testSettingsConfig()
	stored := []string{keys.OpenAIKey, keys.DeeplKey}
	configured := []string{"openai", "deepl"}
	credFails := true
	var openedPage string
	var testedSvc string

	v := newSettingsView(tabServices, current, settingsActions{
		getSettings: func() config.Settings {
			mu.Lock()
			defer mu.Unlock()
			return current
		},
		saveSettings: func(s config.Settings) error {
			mu.Lock()
			defer mu.Unlock()
			current = s
			return nil
		},
		credentials: func() (CredentialStatus, error) {
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
		setSecret: func(name, value string) error {
			mu.Lock()
			defer mu.Unlock()
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
			return nil
		},
		testService: func(_ context.Context, name string) string {
			mu.Lock()
			testedSvc = name
			mu.Unlock()
			return ""
		},
		openKeyPage: func(svc string) error {
			mu.Lock()
			openedPage = svc
			mu.Unlock()
			return nil
		},
		deeplUsage: func() (DeeplUsage, error) {
			return DeeplUsage{Used: 1200, Limit: 500000}, nil
		},
	})
	defer v.dispose()

	tt := ui.NewTester(v.render, 780, 600)
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
	settingsScrollClick(t, tt, "保存凭证", 780, 600)
	waitForCondition(t, time.Second, func() bool {
		tt.Frame()
		mu.Lock()
		defer mu.Unlock()
		return slices.Contains(configured, "baidu") && slices.Contains(stored, keys.BaiduAppID) && slices.Contains(stored, keys.BaiduKey)
	}, "expected credential saved status")
}

func TestSettingsConsolePageDoesNotBlockUIOrSettingsWrites(t *testing.T) {
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	// Release a synchronous regression so it fails instead of hanging the suite.
	deadline := time.AfterFunc(time.Second, unblock)
	defer deadline.Stop()
	opened := make(chan string, 1)
	current := testSettingsConfig()
	v := newSettingsView(tabServices, current, settingsActions{
		getSettings:  func() config.Settings { return current },
		saveSettings: func(s config.Settings) error { current = s; return nil },
		credentials:  func() (CredentialStatus, error) { return CredentialStatus{}, nil },
		openKeyPage: func(service string) error {
			opened <- service
			<-release
			return errors.New("打开控制台失败")
		},
	})
	defer v.dispose()
	waitSettingsIdle(t, v)
	tt := ui.NewTester(v.render, 780, 900)
	settingsClick(t, tt, "配置OpenAI")
	settingsClick(t, tt, "前往控制台获取凭证")
	select {
	case service := <-opened:
		if service != "openai" {
			t.Fatalf("opened service = %q, want openai", service)
		}
	case <-time.After(time.Second):
		t.Fatal("console page was not opened")
	}

	// A stalled shell must allow both further UI input and persistent writes.
	settingsClick(t, tt, "命名转换")
	settingsClick(t, tt, "切换自动拆分标识符")
	waitSettingsIdle(t, v)
	if current.Identifier.AutoSplit {
		t.Fatal("settings write was blocked by browser startup")
	}
	select {
	case <-release:
		t.Fatal("UI or settings writes waited for browser startup")
	default:
	}
	deadline.Stop()
	unblock()
	waitForCondition(t, time.Second, func() bool {
		v.mu.Lock()
		defer v.mu.Unlock()
		return v.pendingToast == "打开控制台失败"
	}, "browser failure was not published")
	tt.Frame()
	if !tt.HasText("打开控制台失败") {
		t.Fatal("browser failure toast was not rendered")
	}
}

func TestSettingsConsolePageResultsRespectWindowLifetime(t *testing.T) {
	for _, outcome := range []string{"success", "error", "detach", "dispose"} {
		t.Run(outcome, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				release := make(chan struct{})
				invalidations := 0
				v := newSettingsView(tabServices, testSettingsConfig(), settingsActions{
					openKeyPage: func(string) error {
						<-release
						if outcome == "success" {
							return nil
						}
						return errors.New("打开控制台失败")
					},
					invalidate: func() { invalidations++ },
				})
				defer v.dispose()
				v.openKeyPageAsync("openai")
				synctest.Wait()
				switch outcome {
				case "detach":
					v.detachWindow()
					v.setActiveTab(tabServices)
				case "dispose":
					v.dispose()
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

	v := newSettingsView(tabHotkeys, current, settingsActions{
		getSettings: func() config.Settings {
			mu.Lock()
			defer mu.Unlock()
			return current
		},
		saveSettings: func(s config.Settings) error {
			mu.Lock()
			defer mu.Unlock()
			current = s
			return nil
		},
		hotkeyBusy: func(accel string) bool {
			return accel == "Ctrl+Alt+B"
		},
	})
	defer v.dispose()

	tt := ui.NewTester(v.render, 780, 600)
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
	if !tt.HasText("该组合键已被其他应用占用") {
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
	var mu sync.Mutex
	current := testSettingsConfig()
	current.CopyFormats = []string{"lowerCamel", "snake"}

	v := newSettingsView(tabNaming, current, settingsActions{
		getSettings: func() config.Settings {
			mu.Lock()
			defer mu.Unlock()
			return current
		},
		saveSettings: func(s config.Settings) error {
			mu.Lock()
			defer mu.Unlock()
			current = s
			return nil
		},
	})
	defer v.dispose()

	tt := ui.NewTester(v.render, 780, 600)
	tt.Frame()

	// Disable snake; lowerCamel remains as last one and becomes disabled.
	if err := tt.Click("切换下划线"); err != nil {
		t.Fatalf("Click 切换下划线: %v", err)
	}
	waitForCondition(t, time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return slices.Equal(current.CopyFormats, []string{"lowerCamel"})
	}, "expected CopyFormats to contain only lowerCamel")

	// Clicking the last remaining format must not remove it.
	_ = tt.Click("切换小驼峰")
	tt.Frame()
	mu.Lock()
	formats := slices.Clone(current.CopyFormats)
	mu.Unlock()
	if !slices.Equal(formats, []string{"lowerCamel"}) {
		t.Fatalf("last remaining format was modified: %v", formats)
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
	v.applyExternalSettings(ext)
	tt.Frame()
	v.mu.Lock()
	gotStrip := v.settings.Identifier.StripMarkdown
	v.mu.Unlock()
	if !gotStrip {
		t.Fatal("expected external settings update to reconcile")
	}
}

func TestSettingsViewDraftFlushAndSaveRollback(t *testing.T) {
	var mu sync.Mutex
	current := testSettingsConfig()
	failNextSave := false

	v := newSettingsView(tabServices, current, settingsActions{
		getSettings: func() config.Settings {
			mu.Lock()
			defer mu.Unlock()
			return current
		},
		saveSettings: func(s config.Settings) error {
			mu.Lock()
			defer mu.Unlock()
			if failNextSave {
				failNextSave = false
				return errors.New("disk full")
			}
			current = s
			return nil
		},
		credentials: func() (CredentialStatus, error) {
			return CredentialStatus{
				Stored:     []string{keys.OpenAIKey},
				Configured: []string{"openai"},
			}, nil
		},
	})

	defer v.dispose()
	tt := ui.NewTester(v.render, 780, 600)
	waitForCondition(t, time.Second, func() bool {
		tt.Frame()
		return tt.HasText("配置")
	}, "expected services loaded")

	if err := tt.Click("配置OpenAI"); err != nil {
		t.Fatalf("Click 配置OpenAI: %v", err)
	}
	if err := tt.Click("编辑Base URL"); err != nil {
		t.Fatalf("focus 编辑Base URL: %v", err)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("https://proxy.example/v1")
	tt.Frame()

	// Collapsing the drawer must flush the pending Base URL draft.
	if err := tt.Click("收起OpenAI"); err != nil {
		t.Fatalf("Click 收起OpenAI: %v", err)
	}
	wantBaseURL := "https://proxy.example/v1"
	waitForCondition(t, time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return current.Services.OpenAI.BaseURL == wantBaseURL
	}, "expected Base URL draft flushed on drawer collapse")

	// Failed save rolls back from getSettings.
	mu.Lock()
	failNextSave = true
	mu.Unlock()
	v.setActiveTab(tabNaming)
	tt.Frame()
	if err := tt.Click("切换自动拆分标识符"); err != nil {
		t.Fatalf("Click 切换自动拆分标识符: %v", err)
	}
	waitForCondition(t, time.Second, func() bool {
		v.mu.Lock()
		defer v.mu.Unlock()
		return v.pendingWrites == 0 && v.settings.Identifier.AutoSplit == current.Identifier.AutoSplit
	}, "expected failed save to reconcile back to saved settings")
}

func TestSettingsViewSaveFIFOAndDisposeDrain(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		started := make(chan struct{})
		var saved []string
		v := newSettingsView(tabServices, testSettingsConfig(), settingsActions{
			saveSettings: func(s config.Settings) error {
				if len(saved) == 0 {
					close(started)
					<-release
				}
				saved = append(saved, s.Services.OpenAI.BaseURL)
				return nil
			},
		})
		defer v.dispose()
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
		// Keep the first write in flight while closing, including an uncommitted draft.
		draft := "https://final.example/v1"
		v.mu.Lock()
		v.baseURLDraft = &draft
		v.mu.Unlock()
		v.dispose()
		synctest.Wait()
		if len(saved) != 0 {
			t.Fatal("save escaped the barrier")
		}
		close(release)
		synctest.Wait()
		want = append(want, draft)
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
			var saved []config.Settings
			written := map[string]string{keys.DeeplKey: "existing-deepl-key"}
			v := newSettingsView(tabServices, current, settingsActions{
				getSettings: func() config.Settings { return current },
				saveSettings: func(s config.Settings) error {
					current = config.Normalize(s)
					saved = append(saved, cloneSettings(current))
					return nil
				},
				setSecret:   func(key, value string) error { written[key] = value; return nil },
				credentials: func() (CredentialStatus, error) { return CredentialStatus{Configured: []string{svc}}, nil },
			})
			defer v.dispose()
			waitSettingsIdle(t, v)
			tt := ui.NewTester(v.render, 780, 500)
			click := func(label string) { settingsScrollClick(t, tt, label, 780, 500) }
			click("配置" + bubbleServiceName(svc))
			click("编辑API Key")
			tt.Type("unfinished-secret")
			click("编辑模型")
			tt.Key(ui.Cmd, ui.KeyA)
			tt.Type(" custom-model ")
			tt.Key(0, ui.KeyEnter)
			waitSettingsIdle(t, v)
			tt.Frame()
			want := config.Default()
			switch svc {
			case "openai":
				want.Services.OpenAI.Model = "custom-model"
			case "siliconflow":
				want.Services.SiliconFlow.Model = "custom-model"
			case "zhipu":
				want.Services.Zhipu.Model = "custom-model"
			}
			if current.Services.OpenAI != want.Services.OpenAI || current.Services.SiliconFlow != want.Services.SiliconFlow || current.Services.Zhipu != want.Services.Zhipu {
				t.Fatalf("model edit leaked to other services: %+v", current.Services)
			}
			click("编辑模型")
			tt.Key(ui.Cmd, ui.KeyA)
			tt.Key(0, ui.KeyBackspace)
			tt.Key(0, ui.KeyEnter)
			waitSettingsIdle(t, v)
			tt.Frame()
			defaults := config.Default()
			if current.Services.OpenAI != defaults.Services.OpenAI || current.Services.SiliconFlow != defaults.Services.SiliconFlow || current.Services.Zhipu != defaults.Services.Zhipu {
				t.Fatalf("blank model did not restore defaults: %+v", current.Services)
			}
			v.mu.Lock()
			draft := v.serviceDrafts[svc][serviceCredentialFields[svc][0].name]
			expanded := v.expanded[svc]
			v.mu.Unlock()
			field, ok := tt.AccessByRole(ui.RoleTextField, "编辑API Key")
			if !ok || !field.Password || field.Value != "" {
				t.Fatalf("credential field is not masked: %+v", field)
			}
			if draft != "unfinished-secret" || !expanded || tt.HasText("unfinished-secret") {
				t.Fatalf("normalization: draft=%q expanded=%v hasText=%v", draft, expanded, tt.HasText("unfinished-secret"))
			}
			if len(saved) != 2 {
				t.Fatalf("saved %d snapshots, want 2", len(saved))
			}
			click("保存凭证")
			waitSettingsIdle(t, v)
			key := serviceCredentialFields[svc][0].name
			if written[key] != "unfinished-secret" || written[keys.DeeplKey] != "existing-deepl-key" || len(written) != 2 {
				t.Fatalf("credential write crossed service boundaries: %v", written)
			}
			v.mu.Lock()
			remaining := v.serviceDrafts[svc][key]
			v.mu.Unlock()
			if remaining != "" {
				t.Fatal("saved credential draft was not cleared")
			}
			tt.Frame()
		})
	}
}

func TestSettingsViewCredentialClearAndReorderPreserveDraft(t *testing.T) {
	current := testSettingsConfig()
	stored := []string{keys.OpenAIKey}
	var cleared string
	v := newSettingsView(tabServices, current, settingsActions{
		getSettings:  func() config.Settings { return current },
		saveSettings: func(s config.Settings) error { current = s; return nil },
		credentials: func() (CredentialStatus, error) {
			status := CredentialStatus{Stored: slices.Clone(stored)}
			if len(stored) > 0 {
				status.Configured = []string{"openai"}
			}
			return status, nil
		},
		setSecret: func(key, value string) error {
			if value != "" {
				t.Error("expected credential deletion")
			}
			cleared = key
			stored = nil
			current.Services.Enabled = []string{"google"}
			return nil
		},
	})
	defer v.dispose()
	waitSettingsIdle(t, v)
	tt := ui.NewTester(v.render, 780, 900)
	settingsClick(t, tt, "配置OpenAI")
	settingsClick(t, tt, "编辑API Key")
	tt.Type("unfinished-secret")
	v.mu.Lock()
	v.applyDragOrderLocked([]string{"openai", "google"})
	v.mu.Unlock()
	waitSettingsIdle(t, v)
	tt.Frame()
	v.mu.Lock()
	draft := v.serviceDrafts["openai"][keys.OpenAIKey]
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
	draft = v.serviceDrafts["openai"][keys.OpenAIKey]
	v.mu.Unlock()
	if draft != "unfinished-secret" {
		t.Fatal("group change lost credential draft")
	}
	settingsClick(t, tt, "清除API Key")
	waitSettingsIdle(t, v)
	tt.Frame()
	v.mu.Lock()
	draft = v.serviceDrafts["openai"][keys.OpenAIKey]
	configured := slices.Contains(v.credentials.Configured, "openai")
	v.mu.Unlock()
	if cleared != keys.OpenAIKey || configured || draft != "" || slices.Contains(current.Services.Enabled, "openai") {
		t.Fatal("credential clear did not refresh presence, draft and enablement")
	}
}

func TestSettingsViewProbeWaitsForSavedModelAndIgnoresDisposedResult(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		current := testSettingsConfig()
		saveRelease := make(chan struct{})
		probeRelease := make(chan struct{})
		var tested []string
		v := newSettingsView(tabServices, current, settingsActions{
			getSettings:  func() config.Settings { return current },
			saveSettings: func(s config.Settings) error { <-saveRelease; current = s; return nil },
			testService: func(_ context.Context, svc string) string {
				tested = append(tested, current.Services.OpenAI.Model)
				<-probeRelease
				return ""
			},
		})
		defer v.dispose()
		v.mu.Lock()
		next := cloneSettings(v.settings)
		next.Services.OpenAI.Model = "saved-model"
		v.enqueueSaveLocked(next)
		v.mu.Unlock()
		v.testConnection("openai")
		synctest.Wait()
		if len(tested) != 0 {
			t.Fatal("probe started before the model was saved")
		}
		close(saveRelease)
		time.Sleep(20 * time.Millisecond)
		synctest.Wait()
		if !slices.Equal(tested, []string{"saved-model"}) {
			t.Fatalf("probe used models %v", tested)
		}
		v.dispose()
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
func waitSettingsIdle(t *testing.T, v *settingsView) {
	t.Helper()
	waitForCondition(t, time.Second, func() bool {
		v.mu.Lock()
		defer v.mu.Unlock()
		if v.pendingWrites != 0 || (v.actions.credentials != nil && !v.credentialsLoaded) {
			return false
		}
		for _, busy := range v.credentialBusy {
			if busy {
				return false
			}
		}
		return true
	}, "settings work did not settle")
}

func TestSettingsViewFailedSaveDoesNotDropNextSnapshot(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		current := testSettingsConfig()
		var attempts []string
		v := newSettingsView(tabGeneral, current, settingsActions{
			getSettings: func() config.Settings { return current },
			saveSettings: func(s config.Settings) error {
				attempts = append(attempts, s.TargetLanguage)
				if len(attempts) == 1 {
					return errors.New("disk full")
				}
				current = s
				return nil
			},
		})
		defer v.dispose()
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
		if v.pendingWrites != 0 || v.settings.TargetLanguage != "fr" || v.pendingToast != "保存失败：disk full" {
			t.Fatal("save failure did not reconcile the final state and report the error")
		}
	})
}

func TestSettingsViewDisposedCredentialsCannotUpdateView(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		invalidations := 0
		v := newSettingsView(tabServices, testSettingsConfig(), settingsActions{
			credentials: func() (CredentialStatus, error) {
				<-release
				return CredentialStatus{Stored: []string{keys.OpenAIKey}}, nil
			},
			invalidate: func() { invalidations++ },
		})
		v.dispose()
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
		v := newSettingsView(tabServices, current, settingsActions{
			getSettings: func() config.Settings { return current },
			saveSettings: func(s config.Settings) error {
				if failed {
					return errors.New("disk full")
				}
				current = s
				return nil
			},
			testService: func(_ context.Context, _ string) string { probes++; return "" },
		})
		defer v.dispose()
		v.mu.Lock()
		next := cloneSettings(v.settings)
		next.Services.OpenAI.Model = "new-model"
		v.enqueueSaveLocked(next)
		v.mu.Unlock()
		v.testConnection("openai")
		time.Sleep(20 * time.Millisecond)
		synctest.Wait()
		v.mu.Lock()
		status := v.testStatuses["openai"]
		v.mu.Unlock()
		if probes != 0 || status.testing || !status.isError {
			t.Fatalf("failed save still probed: calls=%d status=%+v", probes, status)
		}
		failed = false
		v.mu.Lock()
		v.enqueueSaveLocked(next)
		v.mu.Unlock()
		synctest.Wait()
		v.testConnection("openai")
		synctest.Wait()
		if probes != 1 || current.Services.OpenAI.Model != "new-model" {
			t.Fatal("successful save did not unblock the probe")
		}
	})
}

func TestSettingsViewDragOnlySavesCompletedDrop(t *testing.T) {
	for _, cancel := range []bool{true, false} {
		t.Run(fmt.Sprintf("cancel=%t", cancel), func(t *testing.T) {
			current := testSettingsConfig()
			var saved []config.Settings
			v := newSettingsView(tabServices, current, settingsActions{
				getSettings:  func() config.Settings { return current },
				saveSettings: func(s config.Settings) error { current = s; saved = append(saved, cloneSettings(s)); return nil },
				credentials:  func() (CredentialStatus, error) { return CredentialStatus{Configured: []string{"openai"}}, nil },
			})
			defer v.dispose()
			waitSettingsIdle(t, v)
			tt := ui.NewTester(v.render, 780, 900)
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
			draft := v.serviceDrafts["openai"][keys.OpenAIKey]
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
				v := newSettingsView(tabServices, current, settingsActions{
					getSettings:  func() config.Settings { return current },
					saveSettings: func(s config.Settings) error { current = s; saves++; return nil },
					credentials:  func() (CredentialStatus, error) { return CredentialStatus{Configured: []string{"openai"}}, nil },
				})
				defer v.dispose()
				waitSettingsIdle(t, v)
				tt := ui.NewTesterWith(v.render, 780, 900, ui.TesterOptions{Preferences: ui.Preferences{ReduceMotion: reduceMotion}})
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
		v := newSettingsView(tabGeneral, current, settingsActions{getSettings: func() config.Settings { return current }, saveSettings: func(s config.Settings) error { <-release; current = s; return nil }})
		defer v.dispose()
		v.mu.Lock()
		next := cloneSettings(v.settings)
		next.OpenAtLogin = true
		v.enqueueSaveLocked(next)
		v.mu.Unlock()
		synctest.Wait()
		v.detachWindow()
		v.setActiveTab(tabGeneral)
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
		v := newSettingsView(tabServices, testSettingsConfig(), settingsActions{
			setSecret: func(k, val string) error { stored[k] = true; return nil },
			credentials: func() (CredentialStatus, error) {
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
		defer v.dispose()
		synctest.Wait()
		v.mu.Lock()
		v.serviceDrafts["openai"] = map[string]string{keys.OpenAIKey: "a"}
		v.serviceDrafts["deepl"] = map[string]string{keys.DeeplKey: "b"}
		v.mu.Unlock()
		v.saveCredentials("openai")
		synctest.Wait()
		v.saveCredentials("deepl")
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

func TestSettingsServiceSwitchKeepsFocusAcrossGroups(t *testing.T) {
	v := newSettingsView(tabServices, testSettingsConfig(), settingsActions{})
	defer v.dispose()
	v.mu.Lock()
	v.credentialsLoaded = true
	v.credentials = CredentialStatus{Configured: []string{"openai"}}
	v.mu.Unlock()
	tt := ui.NewTester(v.render, 780, 900)
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
		v := newSettingsView(tabServices, testSettingsConfig(), settingsActions{deeplUsage: func() (DeeplUsage, error) {
			calls++
			id := calls
			if id == 1 {
				<-first
			} else {
				<-second
			}
			return DeeplUsage{Used: id, Limit: 10}, nil
		}})
		defer v.dispose()
		v.loadDeeplUsageAsync()
		synctest.Wait()
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
		v := newSettingsView(tabServices, testSettingsConfig(), settingsActions{
			testService: func(ctx context.Context, _ string) string {
				<-ctx.Done()
				cancelled = true
				<-release
				return "old result"
			},
			deeplUsage: func() (DeeplUsage, error) { <-release; return DeeplUsage{Used: 5, Limit: 10}, nil },
		})
		defer v.dispose()
		v.testConnection("openai")
		v.loadDeeplUsageAsync()
		synctest.Wait()
		v.detachWindow()
		synctest.Wait()
		if !cancelled {
			t.Fatal("closing window did not cancel the probe")
		}
		v.setActiveTab(tabServices)
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
	v := newSettingsView(tabServices, cfg, settingsActions{
		getSettings:  func() config.Settings { return cfg },
		saveSettings: func(config.Settings) error { <-release; return errors.New("disk full") },
		credentials:  func() (CredentialStatus, error) { return CredentialStatus{Configured: []string{"openai"}}, nil },
	})
	defer v.dispose()
	waitSettingsIdle(t, v)
	tt := ui.NewTester(v.render, 780, 900)
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
		v := newSettingsView(tabGeneral, current, settingsActions{saveSettings: func(s config.Settings) error { <-release; current = s; return nil }})
		v.mu.Lock()
		draft := "https://quit.example/v1"
		v.baseURLDraft = &draft
		v.mu.Unlock()
		retry := func() { retries++ }
		if v.prepareQuit(retry) {
			t.Fatal("quit proceeded with an unsaved draft")
		}
		if v.prepareQuit(retry) {
			t.Fatal("repeated quit bypassed the pending write")
		}
		synctest.Wait()
		if retries != 0 {
			t.Fatal("quit retried before writes finished")
		}
		close(release)
		synctest.Wait()
		if retries != 1 || current.Services.OpenAI.BaseURL != draft || !v.prepareQuit(retry) {
			t.Fatal("quit did not drain writes and retry exactly once")
		}
	})
}

func TestSettingsKeyChangeInvalidatesUsageBeforeWriteCompletes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		usageRelease := make(chan struct{})
		writeRelease := make(chan struct{})
		v := newSettingsView(tabServices, testSettingsConfig(), settingsActions{
			deeplUsage:  func() (DeeplUsage, error) { <-usageRelease; return DeeplUsage{Used: 9, Limit: 10}, nil },
			setSecret:   func(string, string) error { <-writeRelease; return nil },
			credentials: func() (CredentialStatus, error) { return CredentialStatus{}, nil },
		})
		defer v.dispose()
		synctest.Wait()
		v.loadDeeplUsageAsync()
		synctest.Wait()
		v.mu.Lock()
		v.serviceDrafts["deepl"] = map[string]string{keys.DeeplKey: "new-key"}
		v.mu.Unlock()
		v.saveCredentials("deepl")
		synctest.Wait()
		close(usageRelease)
		synctest.Wait()
		v.mu.Lock()
		usage := v.deeplUsageText
		v.mu.Unlock()
		if usage != "额度查询中…" {
			t.Fatal("old key's usage applied during new key write")
		}
		close(writeRelease)
		synctest.Wait()
	})
}

func TestSettingsTargetLanguageSelectAlignmentAndToggle(t *testing.T) {
	for _, width := range []int{660, 780} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			v := newSettingsView(tabGeneral, testSettingsConfig(), settingsActions{})
			defer v.dispose()
			tt := ui.NewTester(v.render, width, 600)
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

func TestSettingsViewCustomServiceLifecycle(t *testing.T) {
	var mu sync.Mutex
	saved := []config.Settings{}
	writtenSecrets := make(map[string]string)
	current := testSettingsConfig()

	v := newSettingsView(tabServices, current, settingsActions{
		getSettings: func() config.Settings {
			mu.Lock()
			defer mu.Unlock()
			return current
		},
		saveSettings: func(s config.Settings) error {
			mu.Lock()
			defer mu.Unlock()
			current = s
			saved = append(saved, cloneSettings(s))
			return nil
		},
		saveCustomSettings: func(s config.Settings, id string, credential *string) error {
			mu.Lock()
			defer mu.Unlock()
			if _, exists := s.Services.FindCustom(id); !exists {
				writtenSecrets[keys.CustomAPIKey(id)] = ""
			} else if credential != nil {
				writtenSecrets[keys.CustomAPIKey(id)] = *credential
			}
			current = s
			saved = append(saved, cloneSettings(s))
			return nil
		},
		setSecret: func(name, value string) error {
			mu.Lock()
			defer mu.Unlock()
			writtenSecrets[name] = value
			return nil
		},
		credentials: func() (CredentialStatus, error) {
			mu.Lock()
			defer mu.Unlock()
			stored := []string{}
			for k, v := range writtenSecrets {
				if v != "" {
					stored = append(stored, k)
				}
			}
			configured := []string{"openai"}
			for _, cs := range current.Services.Custom {
				if cs.IsConfigured() {
					configured = append(configured, cs.ID)
				}
			}
			return CredentialStatus{Stored: stored, Configured: configured}, nil
		},
	})
	defer v.dispose()
	waitSettingsIdle(t, v)

	tt := ui.NewTester(v.render, 780, 900)

	// 1. Click "+ 添加自定义服务" to show inline draft card in unconfigured group.
	if err := tt.Click("+ 添加自定义服务"); err != nil {
		t.Fatalf("Click + 添加自定义服务: %v", err)
	}
	tt.Frame()

	if !tt.HasText("服务名称") || !tt.HasText("Base URL") || !tt.HasText("模型名称") {
		t.Fatal("new custom service drawer not opened")
	}

	// 2. Fill in the fields.
	settingsClick(t, tt, "编辑服务名称")
	tt.Type("DeepSeek Local")
	settingsClick(t, tt, "编辑Base URL")
	tt.Type("https://api.deepseek.com/v1")
	settingsClick(t, tt, "编辑模型名称")
	tt.Type("deepseek-chat")
	settingsClick(t, tt, "编辑API Key")
	tt.Type("sk-secret-123")
	tt.Frame()

	// 3. Save the new custom service via bottom Save button.
	if err := tt.Click("保存"); err != nil {
		t.Fatalf("Click 保存: %v", err)
	}
	waitSettingsIdle(t, v)
	tt.Frame()

	mu.Lock()
	if len(current.Services.Custom) != 1 {
		mu.Unlock()
		t.Fatalf("custom services count = %d, want 1", len(current.Services.Custom))
	}
	cs := current.Services.Custom[0]
	secretVal := writtenSecrets[keys.CustomAPIKey(cs.ID)]
	mu.Unlock()

	if cs.Name != "DeepSeek Local" || cs.BaseURL != "https://api.deepseek.com/v1" || cs.Model != "deepseek-chat" {
		t.Fatalf("saved custom service mismatch: %+v", cs)
	}
	if secretVal != "sk-secret-123" {
		t.Fatalf("saved custom secret = %q, want sk-secret-123", secretVal)
	}

	// 4. Verify enablement toggle.
	settingsClick(t, tt, "启用"+cs.Name)
	waitSettingsIdle(t, v)
	tt.Frame()

	mu.Lock()
	enabled := slices.Contains(current.Services.Enabled, cs.ID)
	mu.Unlock()
	if !enabled {
		t.Fatal("custom service not enabled after switch click")
	}

	// 5. Delete service with confirmation.
	if err := tt.Click("删除"); err != nil {
		t.Fatalf("Click 删除: %v", err)
	}
	tt.Frame()
	if err := tt.Click("确认删除"); err != nil {
		t.Fatalf("Click 确认删除: %v", err)
	}
	waitSettingsIdle(t, v)
	tt.Frame()

	mu.Lock()
	deleted := len(current.Services.Custom) == 0 && !slices.Contains(current.Services.Enabled, cs.ID)
	clearedSecret := writtenSecrets[keys.CustomAPIKey(cs.ID)] == ""
	mu.Unlock()

	if !deleted {
		t.Fatalf("custom service was not deleted: %+v", current.Services.Custom)
	}
	if !clearedSecret {
		t.Fatal("custom service secret was not deleted")
	}
}

func TestSettingsViewCustomServiceDetachCleansDrafts(t *testing.T) {
	current := testSettingsConfig()
	v := newSettingsView(tabServices, current, settingsActions{
		getSettings:  func() config.Settings { return current },
		saveSettings: func(s config.Settings) error { current = s; return nil },
		credentials: func() (CredentialStatus, error) {
			return CredentialStatus{Configured: []string{"openai"}}, nil
		},
	})
	defer v.dispose()
	waitSettingsIdle(t, v)

	tt := ui.NewTester(v.render, 780, 900)
	if err := tt.Click("+ 添加自定义服务"); err != nil {
		t.Fatalf("Click + 添加自定义服务: %v", err)
	}
	tt.Frame()
	settingsClick(t, tt, "编辑服务名称")
	tt.Type("Unsaved Draft")
	tt.Frame()

	v.mu.Lock()
	if len(v.customDrafts) == 0 {
		v.mu.Unlock()
		t.Fatal("custom draft not present")
	}
	v.mu.Unlock()

	// Simulate closing the window.
	v.detachWindow()

	v.mu.Lock()
	draftCount := len(v.customDrafts)
	v.mu.Unlock()
	if draftCount != 0 {
		t.Fatalf("detachWindow did not clean customDrafts: %d left", draftCount)
	}

	tt.Frame()
	if tt.HasText("Unsaved Draft") {
		t.Fatal("re-rendered window still has unsaved draft")
	}
}
