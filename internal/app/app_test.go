package app

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

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
