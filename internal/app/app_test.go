package app

import "testing"

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
	old := g.begin()
	next := g.begin()
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
