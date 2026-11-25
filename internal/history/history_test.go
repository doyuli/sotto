package history

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/egoist/mygo/plugins/sqlite"
)

var ctx = context.Background()

func fixture(t *testing.T, days int) (*Store, *time.Time, string) {
	t.Helper()
	dir := t.TempDir()
	now := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	s, err := Open(ctx, dir, Policy{true, days}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, &now, dir
}

func record(t *testing.T, s *Store, r *Request, provider, value string, want bool) {
	t.Helper()
	if got, err := s.Record(ctx, r, provider, value, ResultTranslation); err != nil || got != want {
		t.Fatalf("record %s: %v, %v, want %v", provider, got, err, want)
	}
}

func page(t *testing.T, s *Store, query string, after Cursor, limit int) []Entry {
	t.Helper()
	rows, err := s.List(ctx, query, after, limit)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func results(t *testing.T, s *Store, id int64) []Result {
	t.Helper()
	rows, err := s.Results(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestHistorySchemaAndSearch(t *testing.T) {
	s, now, dir := fixture(t, 30)
	key := Key{"  Example\n100%  ", "en", "zh-HANS"}
	order := []string{"deepl", "youdaoweb", "youdao"}
	r := s.Begin(key, order)
	defer r.Done()
	// Provider callback order cannot affect the first result.
	record(t, s, r, "youdao", "中文片段 %_", true)
	record(t, s, r, "youdaoweb", "另一个中文片段", true)
	record(t, s, r, "deepl", " 第一条完整译文\n ", true)
	record(t, s, r, "deepl", "duplicate", false)
	entries := page(t, s, " 中文片段 ", Cursor{}, 10)
	if len(entries) != 1 || entries[0].Snippet != "另一个中文片段" || entries[0].Text != "Example\n100%" {
		t.Fatalf("matches: %+v", entries)
	}
	id := entries[0].ID
	if match := page(t, s, "%_", Cursor{}, 10); len(match) != 1 || match[0].Snippet != "中文片段 %_" {
		t.Fatalf("literal match: %+v", match)
	}
	if match := page(t, s, "eXaMpLE", Cursor{}, 10); len(match) != 1 || match[0].Snippet != "" {
		t.Fatalf("source match: %+v", match)
	}
	first, err := s.CopyText(ctx, id, "")
	if err != nil || first != " 第一条完整译文\n " {
		t.Fatalf("first: %q, %v", first, err)
	}
	before := results(t, s, id)
	*now = now.Add(24 * time.Hour)
	next := s.Begin(key, []string{"youdaoweb", "deepl", "youdao"})
	defer next.Done()
	// A provider can return either kind for the same entry. Update the type with the text.
	const nextText = "Example\n次日"
	if saved, err := s.Record(ctx, next, "youdaoweb", nextText, ResultDictionary); err != nil || !saved {
		t.Fatalf("record dictionary result: %v, %v", saved, err)
	}
	after := results(t, s, id)
	if len(after) != 3 || after[0] != before[0] || after[2] != before[2] ||
		before[1].Type != ResultTranslation || after[1].Type != ResultDictionary ||
		after[1].Text != nextText || after[1].Rank != before[1].Rank ||
		page(t, s, "", Cursor{}, 1)[0].TranslatedMS != now.UnixMilli() {
		t.Fatalf("next day: %+v", after)
	}
	key.TargetLang = "ja"
	other := s.Begin(key, order)
	defer other.Done()
	record(t, s, other, "deepl", "別の方向", true)
	all := page(t, s, "", Cursor{}, 10)
	if len(all) != 2 || all[0].ID == id || all[1].ID != id {
		t.Fatalf("directions: %+v", all)
	}
	if got := page(t, s, "example", all[0].Cursor(), 1); !reflect.DeepEqual(got, all[1:]) {
		t.Fatalf("second page: %+v", got)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(ctx, dir, Policy{true, 30}, func() time.Time { return *now })
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if got := results(t, reopened, id); !reflect.DeepEqual(got, after) {
		t.Fatalf("reopened results: %+v", got)
	}
}

func TestHistoryRefreshPageTracksSearchChanges(t *testing.T) {
	s, now, _ := fixture(t, 0)
	put := func(source, text string) {
		r := s.Begin(Key{source, "en", "zh-HANS"}, []string{"google"})
		defer r.Done()
		record(t, s, r, "google", text, true)
	}
	// A later provider response can change search membership or a snippet without changing sort time.
	for _, source := range []string{"unread", "promote", "keep", "drop", "update"} {
		put(source, "match")
	}
	const size = 3
	previous := page(t, s, "match", Cursor{}, size)
	since, more := s.Revision(), true
	refresh := func() {
		t.Helper()
		got, hasMore, err := s.RefreshPage(ctx, "match", previous, since, more, size)
		all := page(t, s, "match", Cursor{}, 100)
		if err != nil || len(got) > len(all) || !slices.Equal(got, all[:len(got)]) || hasMore != (len(got) < len(all)) || len(got) < min(size, len(all)) {
			t.Fatalf("refresh = %+v, more=%t, err=%v; fresh search = %+v", got, hasMore, err, all)
		}
		previous, since, more = got, s.Revision(), hasMore
	}
	put("update", "match changed in the same millisecond")
	put("drop", "lost")
	*now = now.Add(time.Second)
	put("promote", "new match")
	put("new", "match")
	refresh()
	for _, e := range previous {
		put(e.Text, "lost")
	}
	refresh()
	// An exhausted list must also see previously unmatched entries behind its former tail.
	*now = now.Add(-time.Hour)
	put("backdated", "match")
	refresh()
}

func TestHistoryFullPageLookahead(t *testing.T) {
	s, _, _ := fixture(t, 0)
	put := func(i int) {
		r := s.Begin(Key{strconv.Itoa(i), "en", "zh-HANS"}, nil)
		defer r.Done()
		record(t, s, r, "google", "result", true)
	}
	for i := range maxPageSize {
		put(i)
	}
	// A full last page and a full page with one unread entry must be distinguishable.
	for _, wantMore := range []bool{false, true} {
		if wantMore {
			put(maxPageSize)
		}
		entries, more, err := s.RefreshPage(ctx, "", nil, 0, false, maxPageSize)
		if err != nil || len(entries) != maxPageSize || more != wantMore {
			t.Fatalf("full page: len=%d, more=%v, err=%v; want more=%v", len(entries), more, err, wantMore)
		}
	}
}

func TestHistoryRejectedResultKeepsRevision(t *testing.T) {
	s, now, _ := fixture(t, 0)
	key := Key{"late", "en", "zh-HANS"}
	old := s.Begin(key, nil)
	defer old.Done()
	*now = now.Add(time.Second)
	newer := s.Begin(key, nil)
	defer newer.Done()
	record(t, s, newer, "google", "new", true)
	since := s.Revision()
	// A rejected result used to mark the entry as changed without notifying readers.
	record(t, s, old, "google", "old", false)
	rev, err := scalar(ctx, s.db.Load(), "SELECT changed_revision FROM history_entry")
	if err != nil || rev != int64(since) || s.Revision() != since {
		t.Fatalf("after rejection: stored=%d, published=%d, want=%d, err=%v", rev, s.Revision(), since, err)
	}
}

func TestHistoryLateResultsAndDeletion(t *testing.T) {
	s, now, _ := fixture(t, 30)
	key := Key{"A", "en", "zh-HANS"}
	order := []string{"google", "deepl"}
	old, newer := s.Begin(key, order), s.Begin(key, order)
	defer old.Done()
	defer newer.Done()
	record(t, s, newer, "google", "new", true)
	record(t, s, old, "google", "old same millisecond", false)
	record(t, s, old, "deepl", "older success, newer failed", true)
	*now = now.Add(time.Millisecond)
	later := s.Begin(key, order)
	defer later.Done()
	record(t, s, later, "google", "later", true)
	mustPolicy(t, s, Policy{false, 30}, 0)
	disabled := s.Begin(key, order)
	defer disabled.Done()
	mustPolicy(t, s, Policy{true, 30}, 0)
	record(t, s, disabled, "deepl", "disabled", false)
	record(t, s, later, "deepl", "before disable", false)
	entry := page(t, s, "", Cursor{}, 10)[0]
	delayed := s.Begin(key, order)
	defer delayed.Done()
	if err := s.Delete(ctx, entry.ID); err != nil {
		t.Fatal(err)
	}
	record(t, s, delayed, "google", "after deletion", false)
	assertChildCount(t, s, 0)
	beforeClear := s.Begin(key, order)
	defer beforeClear.Done()
	record(t, s, beforeClear, "google", "another A", true)
	a := page(t, s, "", Cursor{}, 10)[0].ID
	if err := s.Clear(ctx); err != nil {
		t.Fatal(err)
	}
	record(t, s, beforeClear, "deepl", "after clear", false)
	b := s.Begin(Key{"B", "en", "zh-HANS"}, order)
	defer b.Done()
	record(t, s, b, "google", "B result", true)
	if _, err := s.CopyText(ctx, a, ""); !errors.Is(err, ErrMissing) {
		t.Fatalf("stale copy: %v", err)
	}
	if err := s.Delete(ctx, a); !errors.Is(err, ErrMissing) {
		t.Fatalf("stale delete: %v", err)
	}
	if entries := page(t, s, "", Cursor{}, 10); len(entries) != 1 || entries[0].ID <= a || entries[0].Text != "B" {
		t.Fatalf("after clearing: %+v", entries)
	}
	assertChildCount(t, s, 1)
}

func TestHistoryRetranslationAfterPruning(t *testing.T) {
	for _, tc := range []struct {
		name     string
		recreate bool
	}{{"removed", false}, {"recreated", true}} {
		t.Run(tc.name, func(t *testing.T) {
			s, now, _ := fixture(t, 7)
			key := Key{"hello", "en", "zh-HANS"}
			seed := s.Begin(key, nil)
			record(t, s, seed, "google", "original", true)
			seed.Done()
			id := page(t, s, "", Cursor{}, 1)[0].ID

			*now = now.Add(8 * 24 * time.Hour)
			ordinary := s.Begin(key, nil)
			defer ordinary.Done()
			*now = now.Add(time.Second)
			_, retry, err := s.BeginForEntry(ctx, id, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer retry.Done()
			if err := s.Cleanup(ctx); err != nil {
				t.Fatal(err)
			}
			var replacementID int64
			if tc.recreate {
				record(t, s, ordinary, "google", "replacement", true)
				replacementID = page(t, s, "", Cursor{}, 1)[0].ID
				if replacementID == id {
					t.Fatal("replacement reused the expired entry")
				}
			}

			// Matching by text let the late retry recreate the expired row or overwrite its replacement.
			record(t, s, retry, "google", "late retry", false)
			if tc.recreate {
				if got, err := s.CopyText(ctx, replacementID, "google"); err != nil || got != "replacement" {
					t.Fatalf("replacement translation = %q, %v", got, err)
				}
			} else if entries := page(t, s, "", Cursor{}, 1); len(entries) != 0 {
				t.Fatalf("retry recreated the expired entry: %+v", entries)
			}
		})
	}
}

func TestHistoryRetention(t *testing.T) {
	s, now, dir := fixture(t, 30)
	start := *now
	early := s.Begin(Key{"old", "en", "zh-HANS"}, []string{"google", "deepl"})
	defer early.Done()
	record(t, s, early, "google", "old", true)
	record(t, s, early, "deepl", "also old", true)
	late := s.Begin(Key{"too late", "en", "zh-HANS"}, []string{"google"})
	defer late.Done()
	*now = start.Add(24 * time.Hour)
	boundary := s.Begin(Key{"boundary", "en", "zh-HANS"}, []string{"google"})
	defer boundary.Done()
	*now = start.Add(8 * 24 * time.Hour)
	n, err := s.Count(ctx)
	if err != nil || n != 1 {
		t.Fatalf("30 days: %d, %v", n, err)
	}
	persisted := false
	err = s.UpdatePolicy(ctx, Policy{true, 7}, 0, func() error { persisted = true; return nil })
	var confirmation *Confirmation
	if !errors.As(err, &confirmation) || confirmation.Count != 1 || persisted {
		t.Fatalf("confirmation: %v, persisted: %v", err, persisted)
	}
	assertChildCount(t, s, 2)
	mustPolicy(t, s, Policy{true, 7}, 1)
	assertChildCount(t, s, 0)
	record(t, s, late, "google", "expired", false)
	record(t, s, boundary, "google", "exact cutoff", true)
	mustPolicy(t, s, Policy{true, 0}, 0)
	*now = start.Add(365 * 24 * time.Hour)
	if err := s.Cleanup(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(ctx, dir, Policy{false, 0}, func() time.Time { return *now })
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	assertChildCount(t, reopened, 1)
	if err := reopened.Clear(ctx); err != nil {
		t.Fatal(err)
	}
	assertChildCount(t, reopened, 0)
}

func TestHistoryNewerSchemaAndWriteRollback(t *testing.T) {
	s, now, dir := fixture(t, 30)
	r := s.Begin(Key{"text", "en", "zh-HANS"}, []string{"google"})
	defer r.Done()
	if ok, err := s.Record(ctx, r, "google", "result", 3); err == nil || ok {
		t.Fatalf("unsupported result type: %v, %v", ok, err)
	}
	if _, err := s.db.Load().Execute(ctx, `CREATE TRIGGER reject_result BEFORE INSERT ON history_result
		BEGIN SELECT RAISE(ABORT, 'test failure'); END`); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.Record(ctx, r, "google", "result", ResultTranslation); err == nil || ok {
		t.Fatalf("rejected result: %v, %v", ok, err)
	}
	if entries := page(t, s, "", Cursor{}, 10); len(entries) != 0 {
		t.Fatalf("parent survived rollback: %+v", entries)
	}
	if _, err := s.db.Load().Execute(ctx, "PRAGMA user_version = 2"); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(ctx, dir, Policy{true, 30}, func() time.Time { return *now }); !errors.Is(err, ErrNewerSchema) {
		t.Fatalf("newer schema: %v", err)
	}
	db, err := sqlite.Open(ctx, filepath.Join(dir, Filename), sqlite.OpenOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if rows, err := db.Query(ctx, "PRAGMA user_version"); err != nil || rows.Values[0][0] != int64(2) {
		t.Fatalf("newer schema changed: %+v, %v", rows, err)
	}
}

func TestHistoryStartupAndDailyPruningWhileDisabled(t *testing.T) {
	s, now, dir := fixture(t, 0)
	order := []string{"google"}
	old := s.Begin(Key{"old", "en", "zh-HANS"}, order)
	record(t, s, old, "google", "old result", true)
	old.Done()
	*now = now.Add(8 * 24 * time.Hour)
	recent := s.Begin(Key{"recent", "en", "zh-HANS"}, order)
	record(t, s, recent, "google", "recent result", true)
	recent.Done()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(ctx, dir, Policy{false, 7}, func() time.Time { return *now })
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	assertChildCount(t, reopened, 1)
	if entries := page(t, reopened, "", Cursor{}, 10); len(entries) != 1 || entries[0].Text != "recent" {
		t.Fatalf("startup cleanup: %+v", entries)
	}
	*now = now.Add(8 * 24 * time.Hour)
	if err := reopened.Cleanup(ctx); err != nil {
		t.Fatal(err)
	}
	assertChildCount(t, reopened, 0)
}

func TestHistoryGroupCounts(t *testing.T) {
	s, now, _ := fixture(t, 0)
	put := func(text, translated string) {
		r := s.Begin(Key{text, "en", "zh-HANS"}, []string{"deepl"})
		record(t, s, r, "deepl", translated, true)
		r.Done()
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	recent := today.AddDate(0, 0, -2)

	*now = recent.Add(-time.Second)
	put("older alpha", "更早一条")
	*now = recent
	put("recent beta", "近三天一条")
	*now = today
	put("today alpha", "今天一条")
	*now = today.Add(time.Hour)
	put("today gamma", "今天二条")

	got, err := s.GroupCounts(ctx, "", today.UnixMilli(), recent.UnixMilli())
	if err != nil || got != [3]int64{2, 1, 1} {
		t.Fatalf("all group counts = %v, %v, want [2 1 1]", got, err)
	}
	got, err = s.GroupCounts(ctx, "alpha", today.UnixMilli(), recent.UnixMilli())
	if err != nil || got != [3]int64{1, 0, 1} {
		t.Fatalf("filtered group counts = %v, %v, want [1 0 1]", got, err)
	}
	got, err = s.GroupCounts(ctx, "missing", today.UnixMilli(), recent.UnixMilli())
	if err != nil || got != [3]int64{0, 0, 0} {
		t.Fatalf("empty group counts = %v, %v, want [0 0 0]", got, err)
	}
}

func mustPolicy(t *testing.T, s *Store, policy Policy, approved int64) {
	t.Helper()
	if err := s.UpdatePolicy(ctx, policy, approved, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
}

func assertChildCount(t *testing.T, s *Store, want int64) {
	t.Helper()
	if rows, err := s.db.Load().Query(ctx, "SELECT count(*) FROM history_result"); err != nil || rows.Values[0][0] != want {
		t.Fatalf("child count: %+v, %v, want %d", rows, err, want)
	}
}
