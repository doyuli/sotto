package settings

import (
	"context"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"sotto/internal/config"
	"sotto/internal/history"
)

func TestHistoryRetentionConfirmationInSettings(t *testing.T) {
	now := time.Date(2026, 11, 16, 10, 0, 0, 0, time.Local)
	store, err := history.Open(t.Context(), t.TempDir(), history.Policy{Enabled: true, Days: 30}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	r := store.Begin(history.Key{Text: "old", SourceLang: "en", TargetLang: "zh-HANS"}, []string{"deepl", "google"})
	defer r.Done()
	for _, provider := range []string{"deepl", "google"} {
		if ok, err := store.Record(t.Context(), r, provider, "旧译文", history.ResultTranslation); !ok || err != nil {
			t.Fatalf("fixture: %v, %v", ok, err)
		}
	}
	now = now.Add(8 * 24 * time.Hour)
	saved := config.Default()
	v := NewView(TabGeneral, saved, Actions{
		GetSettings: func() config.Settings { return saved },
		SaveHistory: func(cfg config.HistorySettings, approved int64) error {
			return store.UpdatePolicy(t.Context(), history.Policy{Enabled: cfg.Enabled, Days: cfg.RetentionDays}, approved, func() error {
				saved.History = cfg
				return nil
			})
		},
		ClearHistory: func() error { return store.Clear(context.Background()) },
	})
	defer v.Dispose()
	tt := ui.NewTester(v.Render, 780, 900)
	click := func(label string) {
		t.Helper()
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
		drainSettingsTasks(v)
		tt.Frame()
	}
	click("选择历史保留期限")
	click("7 天")
	if !tt.HasText("将删除 1 条原文记录及其译文，且无法恢复") {
		t.Fatalf("confirmation did not count original records: %v", tt.Texts())
	}
	click("取消")
	if n, err := store.Count(t.Context()); err != nil || n != 1 || saved.History.RetentionDays != 30 {
		t.Fatalf("cancel changed data or policy: %d, %d, %v", n, saved.History.RetentionDays, err)
	}
	click("选择历史保留期限")
	click("7 天")
	click("确认更改")
	if n, err := store.Count(t.Context()); err != nil || n != 0 || saved.History.RetentionDays != 7 {
		t.Fatalf("confirmed change: %d, %d, %v", n, saved.History.RetentionDays, err)
	}
	click("切换记录翻译历史")
	if saved.History.Enabled {
		t.Fatal("recording remained enabled")
	}
}

func TestHistoryWriteReadsBackConcurrentModeChange(t *testing.T) {
	for _, clearHistory := range []bool{false, true} {
		saved := config.Default()
		started, release := make(chan struct{}), make(chan struct{})
		v := NewView(TabGeneral, saved, Actions{GetSettings: func() config.Settings { return saved }})
		// The shortcut can publish while the history operation is the last pending settings write.
		next := saved.History
		next.Enabled = false
		v.runHistoryChange(next, func() error {
			close(started)
			<-release
			if !clearHistory {
				saved.History = next
			}
			return nil
		}, clearHistory)
		<-started
		saved.MinimalTranslation = true
		v.ApplyExternalSettings(saved)
		close(release)
		drainSettingsTasks(v)
		v.mu.Lock()
		got := v.settings
		v.mu.Unlock()
		v.Dispose()
		if !got.MinimalTranslation || got.History != saved.History {
			t.Fatalf("clear=%v: stale settings: %+v", clearHistory, got)
		}
	}
}

func TestHistoryShortcutCanBeCustomized(t *testing.T) {
	var calls []string
	cfg := testSettingsConfig()
	v := NewView(TabHotkeys, cfg, Actions{
		SaveHotkey: func(action, key string) error {
			calls = append(calls, action+"="+key)
			return nil
		},
	})
	defer v.Dispose()
	tt := ui.NewTester(v.Render, 780, 760)
	if !tt.HasText(displayHotkey(cfg.Hotkeys.History)) {
		t.Fatal("history default is missing from settings")
	}
	if err := tt.Click("翻译历史快捷键"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Ctrl|ui.Shift, ui.KeyH)
	drainSettingsTasks(v)
	tt.Frame()
	if _, ok := tt.Find("取消绑定翻译历史快捷键"); ok {
		t.Fatal("history still offers to clear its shortcut")
	}
	if len(calls) != 1 || calls[0] != "history=Ctrl+Shift+H" || v.settings.Hotkeys.History != "Ctrl+Shift+H" {
		t.Fatalf("saved calls: %v, configuration: %+v", calls, v.settings.Hotkeys)
	}
}

func TestHistoryClearConfirmationInSettings(t *testing.T) {
	cleared := 0
	v := NewView(TabGeneral, config.Default(), Actions{
		SaveHistory:  func(config.HistorySettings, int64) error { return nil },
		ClearHistory: func() error { cleared++; return nil },
	})
	defer v.Dispose()
	tt := ui.NewTester(v.Render, 780, 900)
	click := func(label string) {
		t.Helper()
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
		drainSettingsTasks(v)
		tt.Frame()
	}
	click("清空翻译历史")
	if !tt.Focused("取消") || !tt.HasText("所有原文和译文都将删除，且无法恢复") {
		t.Fatalf("unexpected clear confirmation state: texts=%v", tt.Texts())
	}
	tt.Key(0, ui.KeyEscape)
	if cleared != 0 || tt.HasText("确认清空") {
		t.Fatalf("Escape left modal open or cleared history: cleared=%d", cleared)
	}
	click("清空翻译历史")
	click("取消")
	if cleared != 0 || tt.HasText("确认清空") {
		t.Fatalf("Cancel left modal open or cleared history: cleared=%d", cleared)
	}
	click("清空翻译历史")
	click("确认清空")
	if cleared != 1 || tt.HasText("确认清空") {
		t.Fatalf("confirm did not clear history cleanly: cleared=%d", cleared)
	}
}
