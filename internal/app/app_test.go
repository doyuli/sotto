package app

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"sotto/internal/config"
	"sotto/internal/selection"
	"sotto/internal/translate"
	"sotto/internal/ui/bubble"
	"sotto/internal/ui/settings"
)

func TestOpenAtLoginSurvivesQueuedSettingsSave(t *testing.T) {
	// Saving an unrelated form snapshot used to undo a newer tray login choice.
	cfg := config.Default()
	a := &application{cfg: cfg, cfgPath: filepath.Join(t.TempDir(), "settings.json"), bubbles: bubble.NewManager(nil, bubble.Actions{})}
	started, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	view := settings.NewView(settings.TabGeneral, cfg, settings.Actions{
		GetSettings: a.settings,
		SaveSettings: func(s config.Settings) error {
			close(started)
			<-release
			err := a.updateSettings(s)
			done <- err
			return err
		},
	})
	defer view.Dispose()
	a.settingsView = view
	tt := ui.NewTester(view.Render, 780, 600)
	if err := tt.Click("切换失焦自动隐藏"); err != nil {
		t.Fatal(err)
	}
	<-started
	var applied []bool
	apply := func(open bool) error { applied = append(applied, open); return nil }
	err := a.commitOpenAtLogin(true, apply)
	close(release)
	if saveErr := <-done; saveErr != nil || err != nil {
		t.Fatalf("tray save=%v, form save=%v", err, saveErr)
	}
	saved, err := config.Load(a.cfgPath)
	if err != nil || !saved.OpenAtLogin || saved.Bubble.AutoHide || !a.settings().OpenAtLogin {
		t.Fatalf("saved login=%v autoHide=%v active login=%v err=%v", saved.OpenAtLogin, saved.Bubble.AutoHide, a.settings().OpenAtLogin, err)
	}
	// Reassert the native choice even when the saved preference already matches.
	if err := a.commitOpenAtLogin(true, apply); err != nil || !slices.Equal(applied, []bool{true, true}) {
		t.Fatalf("login item updates=%v err=%v", applied, err)
	}
}

func TestPresentSettingsWindowFitsAfterRestore(t *testing.T) {
	// On Windows, fitting before Focus used to skip the minimized window's first reopen.
	saved := mygo.Rectangle{X: 100, Y: 160, Width: 780, Height: 600}
	work := mygo.Rectangle{X: -700, Y: 30, Width: 700, Height: 540}
	for _, tc := range []struct {
		name                             string
		minimized, maximized, fullScreen bool
		want                             mygo.Rectangle
	}{
		{"minimized on another display", true, false, false, work},
		{"minimized from maximized", true, true, false, saved},
		{"already maximized", false, true, false, saved},
		{"full screen", false, false, true, saved},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := &testSettingsWindow{bounds: saved, minimized: tc.minimized, maximized: tc.maximized, fullScreen: tc.fullScreen}
			presentSettingsWindow(w, work)
			if !w.focused || w.minimized || w.maximized != tc.maximized || w.fullScreen != tc.fullScreen || w.presented != tc.want {
				t.Fatalf("window state after open = %+v, want focused window with presented bounds %+v", w, tc.want)
			}
		})
	}
}

type testSettingsWindow struct {
	bounds, presented                mygo.Rectangle
	minimized, maximized, fullScreen bool
	focused                          bool
}

func (w *testSettingsWindow) IsMinimized() bool  { return w.minimized }
func (w *testSettingsWindow) IsMaximized() bool  { return w.maximized }
func (w *testSettingsWindow) IsFullScreen() bool { return w.fullScreen }
func (w *testSettingsWindow) Bounds() mygo.Rectangle {
	if w.minimized {
		return mygo.Rectangle{X: -32000, Y: -32000, Width: 160, Height: 24}
	}
	return w.bounds
}
func (*testSettingsWindow) SetMinimumSize(int, int)      {}
func (w *testSettingsWindow) SetBounds(r mygo.Rectangle) { w.bounds = r }
func (w *testSettingsWindow) Restore()                   { w.minimized = false }
func (w *testSettingsWindow) Show()                      { w.presented = w.Bounds() }
func (w *testSettingsWindow) Focus() {
	w.Restore()
	w.focused = true
}

func TestPrepareTranslationRequestCleansAndValidatesSource(t *testing.T) {
	// Cleanup can make the input empty or expand an identifier past the limit.
	cfg := config.Default()
	cfg.Identifier.StripMarkdown = true
	for _, tc := range []struct {
		name, text, want string
		wantErr          error
	}{
		{name: "trim and split", text: " \tgetUserInfo\n ", want: "get User Info"},
		{name: "empty after cleanup", text: " \n---\n ", wantErr: translate.ErrEmptySource},
		{name: "too long after splitting", text: strings.Repeat("aB", translate.MaxInputRunes/2), wantErr: translate.ErrInputLimit},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req, err := prepareTranslationRequest(tc.text, "", "", cfg)
			if !errors.Is(err, tc.wantErr) || req.Text != tc.want {
				t.Fatalf("request text length=%d, err=%v, want length=%d and err=%v", len(req.Text), err, len(tc.want), tc.wantErr)
			}
		})
	}
}

func TestExplicitTargetWins(t *testing.T) {
	cases := []struct{ script, requested, configured, want string }{
		{"latin", "en", "zh-HANS", "en"},
		{"zh", "zh-HANS", "en", "zh-HANS"},
		{"latin", "", "en", "en"},
		{"zh", "", "zh-HANS", "en"},
		{"zh", "auto", "zh-HANS", "en"},
		{"latin", "auto", "en", "zh-HANS"},
	}
	for _, c := range cases {
		if got := resolveTarget(c.script, c.requested, c.configured); got != c.want {
			t.Errorf("%+v: got %s", c, got)
		}
	}
}

func TestEntryOwnership(t *testing.T) {
	var g entryGate
	old := g.begin(false)
	next := g.begin(false)
	if old.Valid() || old.Context().Err() == nil || !next.Valid() {
		t.Fatal("new entry must revoke old acquisition")
	}
	old.Cancel()
	if !next.Valid() {
		t.Fatal("old cancellation revoked the new entry")
	}
	next.Cancel()
	if next.Valid() {
		t.Fatal("canceled entry still owns the UI")
	}
}

func TestSelectionPlacementUsesCapturedSource(t *testing.T) {
	// A slow clipboard grab used to place the full bubble at the new cursor.
	task := entryTask{cursor: mygo.Point{X: -320, Y: 400}}
	if got := task.selectionSource(selection.Selection{Via: "clipboard"}); got != (mygo.Rectangle{X: -320, Y: 400}) {
		t.Fatalf("clipboard selection moved away from the shortcut: %+v", got)
	}
	sel := selection.Selection{HasRect: true, Rect: selection.Rect{X: 10, Y: 12, W: 40, H: 18}}
	if got := task.selectionSource(sel); got != (mygo.Rectangle{X: 10, Y: 12, Width: 40, Height: 18}) {
		t.Fatalf("selection bounds lost: %+v", got)
	}
}

func TestTrayAutoDismissSuppression(t *testing.T) {
	// MouseDown on the tray icon auto-dismisses the panel/history right before MouseUp fires OnClick.
	trayBounds := mygo.Rectangle{X: 1180, Y: 0, Width: 28, Height: 24}
	inside := mygo.Point{X: 1192, Y: 12}
	edgeSlack := mygo.Point{X: 1179, Y: 25}
	outside := mygo.Point{X: 500, Y: 500}
	t0 := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)

	boundsCalls := 0
	queryBounds := func() mygo.Rectangle {
		boundsCalls++
		return trayBounds
	}
	emptyBounds := func() mygo.Rectangle {
		boundsCalls++
		return mygo.Rectangle{}
	}

	a := &application{}

	// Empty state must not query tray bounds.
	if a.consumeTrayAutoDismissAt(config.TrayActionPanel, t0, queryBounds) || boundsCalls != 0 {
		t.Fatalf("empty auto-dismiss queried tray bounds (%d calls)", boundsCalls)
	}

	// Same kind inside tray bounds suppresses reopening once, then clears state.
	a.recordTrayAutoDismissAt(config.TrayActionHistory, t0, inside)
	if !a.consumeTrayAutoDismissAt(config.TrayActionHistory, t0.Add(150*time.Millisecond), queryBounds) || boundsCalls != 1 {
		t.Fatalf("expected tray press auto-dismiss to suppress immediate reopen (boundsCalls=%d)", boundsCalls)
	}
	if a.consumeTrayAutoDismissAt(config.TrayActionHistory, t0.Add(200*time.Millisecond), queryBounds) || boundsCalls != 1 {
		t.Fatalf("second click should not stay suppressed or query bounds (boundsCalls=%d)", boundsCalls)
	}

	// Edge within slack suppresses reopen.
	a.recordTrayAutoDismissAt(config.TrayActionPanel, t0, edgeSlack)
	if !a.consumeTrayAutoDismissAt(config.TrayActionPanel, t0.Add(50*time.Millisecond), queryBounds) {
		t.Fatal("expected edge-slack tray press to suppress reopen")
	}

	// Outside click followed by quick tray click must reopen normally.
	a.recordTrayAutoDismissAt(config.TrayActionPanel, t0, outside)
	if a.consumeTrayAutoDismissAt(config.TrayActionPanel, t0.Add(150*time.Millisecond), queryBounds) {
		t.Fatal("outside dismissal should not suppress clicking tray to reopen")
	}

	// Different action kind must not suppress opening the configured target or query bounds.
	prevCalls := boundsCalls
	a.recordTrayAutoDismissAt(config.TrayActionHistory, t0, inside)
	if a.consumeTrayAutoDismissAt(config.TrayActionPanel, t0.Add(50*time.Millisecond), queryBounds) || boundsCalls != prevCalls {
		t.Fatal("dismissing history should not suppress opening panel or query bounds")
	}

	// Expired auto-dismiss must not suppress reopen or query bounds.
	a.recordTrayAutoDismissAt(config.TrayActionPanel, t0, inside)
	if a.consumeTrayAutoDismissAt(config.TrayActionPanel, t0.Add(trayAutoDismissWindow+time.Millisecond), queryBounds) || boundsCalls != prevCalls {
		t.Fatal("expired dismissal should not suppress reopen or query bounds")
	}

	// Right-click menu popup must clear any auto-dismiss triggered by right-mouse-down.
	a.recordTrayAutoDismissAt(config.TrayActionPanel, t0, inside)
	a.popUpTrayMenu()
	if a.consumeTrayAutoDismissAt(config.TrayActionPanel, t0.Add(100*time.Millisecond), queryBounds) || boundsCalls != prevCalls {
		t.Fatal("popUpTrayMenu should clear right-mouse-down auto-dismiss state")
	}

	// Empty tray bounds (e.g. Windows overflow) must not suppress reopen.
	a.recordTrayAutoDismissAt(config.TrayActionPanel, t0, inside)
	if a.consumeTrayAutoDismissAt(config.TrayActionPanel, t0.Add(50*time.Millisecond), emptyBounds) {
		t.Fatal("empty tray bounds should not suppress reopen")
	}
}
