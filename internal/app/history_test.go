package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo"

	"sotto/internal/config"
	"sotto/internal/history"
	"sotto/internal/keys"
	"sotto/internal/translate"
	"sotto/internal/ui/bubble"
)

// Retrying after a user-data path failure used to open the real database with
// fallback preferences, pruning old entries and recording despite an opt-out.
func TestHistoryRecoveryUsesDatabaseDirectorySettings(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	mygo.App.SetPath(mygo.PathUserData, dir)
	t.Cleanup(func() { mygo.App.SetPath(mygo.PathUserData, "") })
	now := time.Now()
	seed, err := history.Open(ctx, dir, history.Policy{Enabled: true}, func() time.Time { return now.AddDate(0, 0, -60) })
	if err != nil {
		t.Fatal(err)
	}
	key := history.Key{Text: "old", SourceLang: "en", TargetLang: "ja"}
	r := seed.Begin(key, nil)
	if ok, err := seed.Record(ctx, r, "google", "古い", history.ResultTranslation); !ok || err != nil {
		t.Fatalf("seed: %v, %v", ok, err)
	}
	r.Done()
	if err := seed.Close(); err != nil {
		t.Fatal(err)
	}

	a := &application{cfg: config.Default(), cfgPath: filepath.Join(t.TempDir(), "settings.json"),
		fallbackSettings: true, history: history.New(history.Policy{}, time.Now)}
	t.Cleanup(a.closeHistory)
	disk := config.Default()
	disk.History = config.HistorySettings{Enabled: false, RetentionDays: 0}
	path := filepath.Join(dir, "settings.json")
	// A settings read failure must not authorize cleanup. A directory in
	// place of the file fails on all hosts without relying on permissions.
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := a.initializeHistory(ctx); err == nil || a.history.IsOpen() {
		t.Fatalf("opened before settings were readable: %v", err)
	}
	if err := a.updateHistorySettings(disk.History, 0); err == nil {
		t.Fatal("accepted a history policy change while settings were unavailable")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(path, disk); err != nil {
		t.Fatal(err)
	}
	if err := a.initializeHistory(ctx); err != nil {
		t.Fatal(err)
	}
	if n, err := a.history.Count(ctx); err != nil || n != 1 {
		t.Fatalf("lost old entry on retry: count=%d, err=%v", n, err)
	}
	if a.settings().History != disk.History || a.history.Begin(key, nil) != nil {
		t.Fatal("retry did not honor the recording opt-out")
	}
	// Even a click queued before recovery cannot write to either settings file.
	if err := a.updateHistorySettings(config.Default().History, 0); err == nil || !strings.Contains(err.Error(), "重启") {
		t.Fatalf("expected restart guidance, got %v", err)
	}
	if saved, err := config.Load(path); err != nil || saved.History != disk.History {
		t.Fatalf("changed the real policy: %+v, %v", saved.History, err)
	}
	if _, err := os.Stat(a.cfgPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unexpected fallback settings file: %v", err)
	}
}

func TestHistoryDatabaseRetryKeepsSettingsEditable(t *testing.T) {
	dir := t.TempDir()
	a := &application{cfg: config.Default(), cfgPath: filepath.Join(dir, "settings.json"),
		bubbles: bubble.NewManager(nil, bubble.Actions{})}
	a.history = history.New(historyPolicy(a.cfg.History), time.Now)
	t.Cleanup(a.closeHistory)
	// A database failure alone must not put a session with valid settings into fallback mode.
	blocked := filepath.Join(dir, history.Filename)
	if err := os.Mkdir(blocked, 0700); err != nil {
		t.Fatal(err)
	}
	if err := a.initializeHistory(t.Context()); err == nil {
		t.Fatal("expected the initial database open to fail")
	}
	if err := os.Remove(blocked); err != nil {
		t.Fatal(err)
	}
	if err := a.initializeHistory(t.Context()); err != nil {
		t.Fatal(err)
	}
	next := a.cfg.History
	next.Enabled = false
	if err := a.updateHistorySettings(next, 0); err != nil {
		t.Fatal(err)
	}
	if saved, err := config.Load(a.cfgPath); err != nil || saved.History != next {
		t.Fatalf("history policy after retry: %+v, %v", saved.History, err)
	}
}

func TestHistoryBoundsFollowInvocationDisplay(t *testing.T) {
	saved := mygo.Rectangle{X: 380, Y: 240, Width: 720, Height: 460}
	primary := mygo.Rectangle{Width: 1920, Height: 1040}
	for _, tc := range []struct {
		name         string
		bounds, work mygo.Rectangle
		want         mygo.Rectangle
	}{
		{"keep a saved position on the same display", saved, primary, saved},
		{
			"move on reopen to the display left of and above the primary", saved,
			mygo.Rectangle{X: -1440, Y: -160, Width: 1440, Height: 860},
			mygo.Rectangle{X: -1080, Y: 40, Width: 720, Height: 460},
		},
		{
			"fit a display with a smaller work area", saved,
			mygo.Rectangle{X: 1920, Y: 30, Width: 540, Height: 380},
			mygo.Rectangle{X: 1920, Y: 30, Width: 540, Height: 380},
		},
		{
			"restore minimum size on a larger display after fitting a small one",
			mygo.Rectangle{X: 1920, Y: 30, Width: 540, Height: 350}, primary,
			mygo.Rectangle{X: 680, Y: 340, Width: 560, Height: 360},
		},
		{"retain bounds if no display is available", saved, mygo.Rectangle{}, saved},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := windowBoundsForWorkArea(tc.bounds, tc.work, historyMinWidth, historyMinHeight); got != tc.want {
				t.Fatalf("history bounds = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestRecordOnlyCopyableFinalTranslations(t *testing.T) {
	ctx := context.Background()
	store, err := history.Open(ctx, t.TempDir(), history.Policy{Enabled: true, Days: 30}, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	a := &application{history: store}
	order := []string{"deepl", "youdaoweb", "google"}
	req := a.beginHistory("  user_name\n", translate.Request{
		Text: "user name", SourceLang: "auto", TargetLang: "zh-HANS",
	}, order)
	defer req.Done()
	a.recordTranslation(ctx, req, translate.Result{Service: "deepl", Text: "part", Streaming: true})
	a.recordTranslation(ctx, req, translate.Result{Service: "google", Text: "partial", Err: "offline"})
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	a.recordTranslation(canceled, req, translate.Result{Service: "deepl", Text: "canceled"})
	if n, err := store.Count(ctx); n != 0 || err != nil {
		t.Fatalf("unsettled results entered history: %d, %v", n, err)
	}
	// Youdao's flat result text contains only one accent even when the entry has both.
	a.recordTranslation(ctx, req, translate.Result{
		Service: "youdaoweb", Text: "/ˈjuːzər/\n词义\n",
		Dictionary: &translate.DictionaryEntry{Word: "user", Meanings: "词义\n", UKPhonetic: "ˈjuːzə", USPhonetic: "ˈjuːzər"},
	})
	a.recordTranslation(ctx, req, translate.Result{Service: "deepl", Text: "complete\nresult"})
	rows, err := store.List(ctx, "", history.Cursor{}, 10)
	if err != nil || len(rows) != 1 || rows[0].Text != "user_name" || rows[0].SourceLang != "auto" {
		t.Fatalf("stored original: %+v, %v", rows, err)
	}
	got, err := store.Results(ctx, rows[0].ID)
	if err != nil || len(got) != 2 || got[0].Type != history.ResultTranslation || got[0].Text != "complete\nresult" ||
		got[1].Type != history.ResultDictionary || got[1].Text != "user\n英 /ˈjuːzə/  美 /ˈjuːzər/\n词义\n" {
		t.Fatalf("copyable results: %+v, %v", got, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	// Loss of history storage must leave the returned translation intact.
	final := translate.Result{Service: "google", Text: "still successful"}
	a.recordTranslation(ctx, req, final)
	if final.Err != "" || final.Text != "still successful" {
		t.Fatalf("recording changed translation: %+v", final)
	}
}

func TestHistoryBubbleDismissalAfterAcceptance(t *testing.T) {
	store, err := history.Open(t.Context(), t.TempDir(), history.Policy{Enabled: true}, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	a := &application{history: store}
	r := store.Begin(history.Key{Text: "hello", SourceLang: "auto", TargetLang: "ja"}, nil)
	defer r.Done()
	ctx, dismiss := context.WithCancel(t.Context())
	defer dismiss()
	// A stale provider callback is rejected, but closing immediately after accepting a result cannot undo it.
	var writes sync.WaitGroup
	emit := a.bubbleResultHandler(ctx, r, &writes, "run", func(msg bubble.TranslationUpdateMsg) bool {
		if msg.Service == "stale" {
			return false
		}
		dismiss()
		return true
	})
	emit(translate.Result{Service: "stale", Text: "obsolete"})
	emit(translate.Result{Service: "google", Text: "こんにちは"})
	emit(translate.Result{Service: "late", Text: "after dismissal"})
	writes.Wait()
	rows, err := store.List(t.Context(), "", history.Cursor{}, 1)
	if err != nil || len(rows) != 1 {
		t.Fatalf("history: %v, %v", rows, err)
	}
	results, err := store.Results(t.Context(), rows[0].ID)
	if err != nil || len(results) != 1 || results[0].Text != "こんにちは" {
		t.Fatalf("accepted result: %v, %v", results, err)
	}
}

func TestHistoryShortcutConflictPreservesConfiguration(t *testing.T) {
	a := &application{cfg: config.Default(), cfgPath: filepath.Join(t.TempDir(), "settings.json")}
	if err := a.updateHotkey("history", a.cfg.Hotkeys.Panel); err == nil {
		t.Fatal("history shortcut took the existing panel binding")
	}
	if a.cfg.Hotkeys != config.Default().Hotkeys {
		t.Fatalf("conflict changed settings: %+v", a.cfg.Hotkeys)
	}
}

func TestHistoryShortcutDoesNotTakeExistingBindingAtStartup(t *testing.T) {
	cfg := config.Default()
	cfg.Hotkeys.Panel = cfg.Hotkeys.History
	a := &application{cfg: cfg, shortcutBackend: failingShortcutBackend{}}
	if err := a.applyHotkeys(cfg.Hotkeys); err == nil {
		t.Fatal("accepted a history shortcut already owned by the panel")
	}
	if a.hotkeys["history"] != "" || a.hotkeys["panel"] == "" ||
		a.shortcutErrors()["history"] == "" {
		t.Fatalf("startup history conflict: %v, %v", a.hotkeys, a.shortcutErrors())
	}
}

func TestHistoryProviderMenuFollowsEnabledServiceOrder(t *testing.T) {
	cfg := config.ServiceSettings{
		Order:   []string{"deepl", "google", "removed-provider", "dictionary", "openai"},
		Enabled: []string{"openai", "dictionary", "removed-provider", "google"},
	}
	if got, want := enabledHistoryProviders(cfg), []string{"google", "dictionary", "openai"}; !slices.Equal(got, want) {
		t.Fatalf("enabled history providers=%v, want %v", got, want)
	}
}

func TestHistoryRetranslatesOnlyChosenProvider(t *testing.T) {
	var offline atomic.Bool
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var payload struct {
			Messages []map[string]string
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil ||
			len(payload.Messages) != 2 || payload.Messages[1]["content"] != "Привіт" ||
			!strings.Contains(payload.Messages[0]["content"], "into Japanese") || strings.Contains(payload.Messages[0]["content"], "from Russian") {
			t.Errorf("retranslation did not use the saved original and direction: %+v, %v", payload, err)
			http.Error(w, "bad payload", http.StatusBadRequest)
			return
		}
		if offline.Load() {
			http.Error(w, "offline", http.StatusServiceUnavailable)
			return
		}
		_, _ = io.WriteString(w, `{"choices":[{"finish_reason":"stop","message":{"content":"こんにちは"}}]}`)
	}))
	defer server.Close()
	ctx := t.Context()
	store, err := history.Open(ctx, t.TempDir(), history.Policy{Enabled: true, Days: 30}, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	cfg := config.Default()
	cfg.Services.Enabled = []string{"dictionary"}
	cfg.Services.OpenAI.BaseURL = server.URL
	a := &application{cfg: cfg, history: store, keys: &serviceTestKeyring{values: map[string]string{keys.OpenAIKey: "fixture"}}}
	// Cyrillic script alone must not turn automatic detection into a Russian source on retry.
	r := a.beginHistory("Привіт", translate.Request{Text: "Привіт", SourceLang: "auto", TargetLang: "ja"}, cfg.Services.Order)
	if ok, err := store.Record(ctx, r, "deepl", "以前の訳", history.ResultTranslation); err != nil || !ok {
		t.Fatalf("seed: %v, %v", ok, err)
	}
	r.Done()
	entries, err := store.List(ctx, "", history.Cursor{}, 10)
	if err != nil || len(entries) != 1 {
		t.Fatalf("seed: %+v, %v", entries, err)
	}
	entry := entries[0]
	if entry.SourceLang != "auto" {
		t.Fatalf("stored source = %q", entry.SourceLang)
	}
	if _, err := a.retranslateHistory(ctx, entry, "openai"); err == nil || calls.Load() != 0 {
		t.Fatalf("a disabled provider was dispatched: calls=%d, err=%v", calls.Load(), err)
	}
	cfg.Services.Enabled = []string{"openai", "dictionary"}
	a.cfg = cfg
	if result, err := a.retranslateHistory(ctx, entry, "openai"); err != nil || result.Text != "" {
		t.Fatalf("retranslation: %+v, %v", result, err)
	}
	before, err := store.Results(ctx, entry.ID)
	if err != nil || len(before) != 2 || before[0].Provider != "deepl" || before[1].Provider != "openai" ||
		before[1].Type != history.ResultTranslation || before[1].Text != "こんにちは" ||
		!slices.Equal(a.settings().Services.Enabled, cfg.Services.Enabled) {
		t.Fatalf("retranslation changed other results or defaults: %+v, %v", before, err)
	}
	offline.Store(true)
	if _, err := a.retranslateHistory(ctx, entry, "openai"); err == nil {
		t.Fatal("expected provider error")
	}
	after, err := store.Results(ctx, entry.ID)
	if err != nil || !slices.Equal(after, before) {
		t.Fatalf("offline retry changed history: %+v, %v", after, err)
	}
	if err := store.Delete(ctx, entry.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.retranslateHistory(ctx, entry, "openai"); !errors.Is(err, history.ErrMissing) || calls.Load() != 2 {
		t.Fatalf("stale retry contacted provider: %d, %v", calls.Load(), err)
	}
}

type historyTestKeyring struct {
	serviceTestKeyring
	started, release chan struct{}
}

func (k *historyTestKeyring) Snapshot(names []string) keys.Snapshot {
	close(k.started)
	<-k.release
	return k.serviceTestKeyring.Snapshot(names)
}

func TestHistoryMinimalRequestClearedWhileWaitingForCredentials(t *testing.T) {
	ctx := t.Context()
	store, err := history.Open(ctx, t.TempDir(), history.Policy{Enabled: true, Days: 30}, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	keyring := &historyTestKeyring{started: make(chan struct{}), release: make(chan struct{}),
		serviceTestKeyring: serviceTestKeyring{values: map[string]string{keys.OpenAIKey: "fixture"}}}
	cfg := config.Default()
	cfg.Services.Enabled = []string{"openai"}
	a := &application{cfg: cfg, history: store, keys: keyring}
	done := make(chan minimalRequest, 1)
	go func() {
		prepared, err := a.prepareMinimalRequest(ctx, "original")
		if err != nil {
			t.Error(err)
		}
		done <- prepared
	}()
	<-keyring.started
	err = store.Clear(ctx)
	close(keyring.release)
	if err != nil {
		t.Fatal(err)
	}
	prepared := <-done
	if prepared.history == nil {
		t.Fatal("missing request snapshot")
	}
	defer prepared.history.Done()
	a.recordTranslation(ctx, prepared.history, translate.Result{Service: "openai", Text: "late translation"})
	if n, err := store.Count(ctx); err != nil || n != 0 {
		t.Fatalf("minimal result revived cleared history: %d, %v", n, err)
	}
}

func TestHistoryShortcutTogglesOpenAndDismiss(t *testing.T) {
	store, err := history.Open(t.Context(), t.TempDir(), history.Policy{Enabled: true}, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	a := &application{history: store}
	shown, dismissed := 0, 0
	show := func() { shown++ }
	dismiss := func() { dismissed++ }

	a.toggleHistoryWindow(false, show, dismiss)
	a.toggleHistoryWindow(true, show, dismiss)
	a.quitting = true
	a.toggleHistoryWindow(false, show, dismiss)
	a.toggleHistoryWindow(true, show, dismiss)

	if shown != 1 || dismissed != 1 {
		t.Fatalf("toggle counts: shown=%d dismissed=%d, want 1, 1", shown, dismissed)
	}
}

func TestHistoryPreparedDictionaryEligibility(t *testing.T) {
	cfg := config.Default()
	cfg.Identifier.StripMarkdown = true
	for _, tc := range []struct {
		source, prepared string
		eligible         bool
	}{
		{"getUserInfo", "get User Info", false}, {"**hello**", "hello", true},
	} {
		entry := history.Entry{Key: history.Key{Text: tc.source, SourceLang: "auto", TargetLang: "zh-HANS"}}
		req, err := prepareHistoryRequest(entry, "dictionary", cfg)
		if (err == nil) != tc.eligible || tc.eligible && req.Text != tc.prepared {
			t.Errorf("%s: %+v, %v", tc.source, req, err)
		}
	}
}

func TestHistoryDictionaryRetranslationRejectsNonWordEntry(t *testing.T) {
	ctx := t.Context()
	store, err := history.Open(ctx, t.TempDir(), history.Policy{Enabled: true, Days: 30}, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	cfg := config.Default()
	cfg.Services.Enabled = []string{"deepl", "dictionary"}
	a := &application{cfg: cfg, history: store}

	r := store.Begin(history.Key{Text: "hello world", SourceLang: "en", TargetLang: "zh-HANS"}, cfg.Services.Order)
	if ok, err := store.Record(ctx, r, "deepl", "你好世界", history.ResultTranslation); err != nil || !ok {
		t.Fatalf("seed: %v, %v", ok, err)
	}
	r.Done()

	entries, err := store.List(ctx, "", history.Cursor{}, 10)
	if err != nil || len(entries) != 1 {
		t.Fatalf("list: %+v, %v", entries, err)
	}
	if _, err := a.retranslateHistory(ctx, entries[0], "dictionary"); err == nil || !strings.Contains(err.Error(), "单个英文单词") {
		t.Fatalf("dictionary retranslate on phrase = %v, want single-word error", err)
	}
}
