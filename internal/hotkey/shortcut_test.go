package hotkey

import "testing"

func TestPlatformShortcutIdentity(t *testing.T) {
	for _, tc := range []struct{ input, platform, want string }{
		{"Alt+Cmd+a", "darwin", "Option+Cmd+A"}, {"Command+Option+A", "darwin", "Option+Cmd+A"},
		{"Ctrl+Alt+S", "darwin", "Ctrl+Option+S"}, {"Control+Alt+S", "windows", "Ctrl+Alt+S"},
		{"Super+Shift+K", "windows", "Shift+Win+K"}, {"CmdOrCtrl+Tab", "darwin", "Cmd+Tab"},
		{"CmdOrCtrl+Tab", "windows", "Ctrl+Tab"}, {"Option+;", "darwin", "Option+;"},
		{"CmdOrCtrl++", "darwin", "Shift+Cmd+="}, {"CmdOrCtrl++", "windows", "Ctrl+Shift+="},
	} {
		s, err := Parse(tc.input, tc.platform)
		if err != nil || s.String() != tc.want {
			t.Errorf("%s on %s: %s %v", tc.input, tc.platform, s.String(), err)
		}
	}
}
func TestShortcutRejectsUnsupportedOrIncompleteInput(t *testing.T) {
	for _, value := range []string{"", "A", "Ctrl+", "Ctrl+unknown", "Hyper+A", "Option+F24"} {
		if _, err := Parse(value, "darwin"); err == nil {
			t.Errorf("accepted %q", value)
		}
	}
}
