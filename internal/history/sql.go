package history

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/plugins/sqlite"
)

var schemaV1 = []sqlite.Statement{
	{SQL: `CREATE TABLE history_entry (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		source_text TEXT NOT NULL CHECK (source_text <> ''),
		source_lang TEXT NOT NULL CHECK (source_lang <> ''),
		target_lang TEXT NOT NULL CHECK (target_lang <> ''),
		last_translated_ms INTEGER NOT NULL,
		changed_revision INTEGER NOT NULL,
		UNIQUE (source_text, source_lang, target_lang)
	)`},
	{SQL: `CREATE INDEX history_entry_recent ON history_entry (last_translated_ms DESC, id DESC)`},
	{SQL: `CREATE INDEX history_entry_changed ON history_entry (changed_revision)`},
	{SQL: `CREATE TABLE history_result (
		entry_id INTEGER NOT NULL REFERENCES history_entry(id) ON DELETE CASCADE,
		provider_id TEXT NOT NULL CHECK (provider_id <> ''),
		result_type INTEGER NOT NULL CHECK (result_type IN (1, 2)),
		translated_text TEXT NOT NULL CHECK (translated_text <> ''),
		translated_ms INTEGER NOT NULL,
		display_rank INTEGER NOT NULL CHECK (display_rank >= 0),
		PRIMARY KEY (entry_id, provider_id)
	) WITHOUT ROWID`},
	{SQL: "PRAGMA user_version = 1"},
}

const insertEntry = `INSERT INTO history_entry (source_text, source_lang, target_lang, last_translated_ms, changed_revision)
SELECT ?1, ?2, ?3, ?4, ?5 WHERE ?6 = 0
ON CONFLICT (source_text, source_lang, target_lang) DO NOTHING`

const upsertResult = `INSERT INTO history_result (entry_id, provider_id, result_type, translated_text, translated_ms, display_rank)
SELECT id, ?4, ?5, ?6, ?7, ?8 FROM history_entry
WHERE source_text = ?1 AND source_lang = ?2 AND target_lang = ?3
AND (?9 = 0 OR id = ?9)
ON CONFLICT (entry_id, provider_id) DO UPDATE SET
result_type = excluded.result_type, translated_text = excluded.translated_text, translated_ms = excluded.translated_ms
WHERE excluded.translated_ms >= history_result.translated_ms`

// changes() refers to the immediately preceding result upsert in the same transaction.
const touchEntry = `UPDATE history_entry SET last_translated_ms = max(last_translated_ms, ?4), changed_revision = ?5
WHERE changes() > 0 AND source_text = ?1 AND source_lang = ?2 AND target_lang = ?3`

func migrate(ctx context.Context, db *sqlite.DB) error {
	if _, err := db.Execute(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		return fmt.Errorf("enable history foreign keys: %w", err)
	}
	fk, err := scalar(ctx, db, "PRAGMA foreign_keys")
	if err != nil {
		return fmt.Errorf("verify history foreign keys: %w", err)
	}
	if fk != 1 {
		return fmt.Errorf("history foreign keys disabled: %w", ErrUnavailable)
	}
	version, err := scalar(ctx, db, "PRAGMA user_version")
	if err != nil {
		return fmt.Errorf("read history schema version: %w", err)
	}
	switch version {
	case 0:
		if _, err := db.Transaction(ctx, schemaV1); err != nil {
			return fmt.Errorf("migrate history schema to v1: %w", err)
		}
	case 1:
	default:
		return fmt.Errorf("history schema version %d: %w", version, ErrNewerSchema)
	}
	return nil
}

func scalar(ctx context.Context, db *sqlite.DB, sql string, args ...any) (int64, error) {
	rows, err := db.Query(ctx, sql, args...)
	if err != nil {
		return 0, err
	}
	if len(rows.Values) != 1 || len(rows.Values[0]) != 1 {
		return 0, ErrUnavailable
	}
	n, ok := rows.Values[0][0].(int64)
	if !ok {
		return 0, ErrUnavailable
	}
	return n, nil
}

func cutoff(days int, now time.Time) int64 {
	return now.UnixMilli() - int64(days)*86_400_000
}

func prune(ctx context.Context, db *sqlite.DB, days int, now time.Time) (int64, error) {
	if days == 0 {
		return 0, nil
	}
	r, err := db.Execute(ctx, "DELETE FROM history_entry WHERE last_translated_ms < ?1", cutoff(days, now))
	if err != nil {
		return 0, fmt.Errorf("prune translation history: %w", err)
	}
	return r.Changes, nil
}

const columns = "e.id, e.source_text, e.source_lang, e.target_lang, e.last_translated_ms"
const matches = `(instr(lower(e.source_text), lower(?1)) > 0
	OR EXISTS (SELECT 1 FROM history_result AS r WHERE r.entry_id = e.id
	AND instr(lower(r.translated_text), lower(?1)) > 0))`

func entryFromRow(r []any) Entry {
	return Entry{ID: r[0].(int64), Key: Key{r[1].(string), r[2].(string), r[3].(string)}, TranslatedMS: r[4].(int64)}
}

func (s *Store) query(ctx context.Context, sql string, args ...any) ([][]any, error) {
	db := s.db.Load()
	if db == nil {
		return nil, ErrUnavailable
	}
	rows, err := db.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("read translation history: %w", err)
	}
	return rows.Values, nil
}

func (s *Store) Entry(ctx context.Context, id int64) (Entry, error) {
	return s.Find(ctx, id, "")
}

// Find revalidates a selected entry even when it falls outside the refreshed page.
func (s *Store) Find(ctx context.Context, id int64, word string) (Entry, error) {
	sql := "SELECT " + columns + " FROM history_entry AS e WHERE "
	args := []any{id}
	if word = strings.TrimSpace(word); word != "" {
		sql += matches + " AND "
		args = []any{word, id}
	}
	rows, err := s.query(ctx, sql+"e.id = ?", args...)
	if err != nil {
		return Entry{}, err
	}
	if len(rows) == 0 {
		return Entry{}, ErrMissing
	}
	return entryFromRow(rows[0]), nil
}

const maxPageSize = 100

// List uses keyset pagination. Searching uses SQLite's ASCII lower and literal
// substring matching, identically for selection and the per-page snippets.
func (s *Store) List(ctx context.Context, word string, after Cursor, limit int) ([]Entry, error) {
	word = strings.TrimSpace(word)
	var args []any
	var conditions []string
	if word != "" {
		args = append(args, word)
		conditions = append(conditions, matches)
	}
	if after.ID != 0 {
		conditions = append(conditions, "(e.last_translated_ms, e.id) < (?, ?)")
		args = append(args, after.TranslatedMS, after.ID)
	}
	sql := "SELECT " + columns + " FROM history_entry AS e"
	if len(conditions) > 0 {
		sql += " WHERE " + strings.Join(conditions, " AND ")
	}
	sql += " ORDER BY e.last_translated_ms DESC, e.id DESC LIMIT ?"
	rows, err := s.query(ctx, sql, append(args, max(1, min(limit, maxPageSize+1)))...)
	if err != nil {
		return nil, err
	}
	entries := make([]Entry, len(rows))
	for i, row := range rows {
		entries[i] = entryFromRow(row)
	}
	if word != "" && len(entries) != 0 {
		if err := s.snippets(ctx, entries, word); err != nil {
			return nil, err
		}
	}
	return entries, nil
}

// RefreshPage reads the first page, or refreshes an already loaded prefix after writes.
// The caller starts a fresh list after removals. Translation timestamps can stay
// unchanged when another provider finishes, so use the write revision for snippets.
func (s *Store) RefreshPage(ctx context.Context, word string, previous []Entry, since uint64, more bool, pageSize int) ([]Entry, bool, error) {
	pageSize = max(1, min(pageSize, maxPageSize))
	if len(previous) == 0 {
		page, err := s.List(ctx, word, Cursor{}, pageSize+1)
		return page[:min(pageSize, len(page))], len(page) > pageSize, err
	}
	tail := previous[len(previous)-1].Cursor()
	word = strings.TrimSpace(word)
	match := "1"
	args := []any{}
	if word != "" {
		match = matches
		args = append(args, word)
	}
	sql := "SELECT " + columns + ", " + match + " FROM history_entry AS e WHERE e.changed_revision > ?"
	args = append(args, since)
	if more {
		sql += " AND (e.last_translated_ms, e.id) >= (?, ?)"
		args = append(args, tail.TranslatedMS, tail.ID)
	}
	rows, err := s.query(ctx, sql, args...)
	if err != nil {
		return nil, false, err
	}
	changed := make(map[int64]bool, len(rows))
	entries := make([]Entry, 0, len(previous)+len(rows))
	for _, row := range rows {
		changed[row[0].(int64)] = true
		if row[5].(int64) != 0 {
			entries = append(entries, entryFromRow(row))
		}
	}
	if word != "" {
		for batch := range slices.Chunk(entries, 100) {
			if err := s.snippets(ctx, batch, word); err != nil {
				return nil, false, err
			}
		}
	}
	for _, e := range previous {
		if !changed[e.ID] {
			entries = append(entries, e)
		}
	}
	slices.SortFunc(entries, func(a, b Entry) int {
		return cmp.Or(cmp.Compare(b.TranslatedMS, a.TranslatedMS), cmp.Compare(b.ID, a.ID))
	})
	if more {
		// Refills a prefix that lost search matches and checks whether unread rows remain.
		needed := max(0, pageSize-len(entries))
		next, err := s.List(ctx, word, tail, needed+1)
		if err != nil {
			return nil, false, err
		}
		more = len(next) > needed
		entries = append(entries, next[:min(needed, len(next))]...)
	}
	return entries, more, nil
}

func (s *Store) snippets(ctx context.Context, entries []Entry, word string) error {
	args := []any{word}
	for _, entry := range entries {
		args = append(args, entry.ID)
	}
	rows, err := s.query(ctx, `SELECT e.id, r.provider_id, r.translated_text, instr(lower(r.translated_text), lower(?1))
		FROM history_entry AS e JOIN history_result AS r ON r.entry_id = e.id
		WHERE e.id IN (`+strings.TrimSuffix(strings.Repeat("?,", len(entries)), ",")+`)
		AND instr(lower(e.source_text), lower(?1)) = 0 AND instr(lower(r.translated_text), lower(?1)) > 0
		AND r.provider_id = (SELECT p.provider_id FROM history_result AS p WHERE p.entry_id = e.id
			AND instr(lower(p.translated_text), lower(?1)) > 0 ORDER BY p.display_rank, p.provider_id LIMIT 1)`, args...)
	if err != nil {
		return err
	}
	type match struct{ provider, text string }
	byID := make(map[int64]match, len(rows))
	for _, row := range rows {
		byID[row[0].(int64)] = match{row[1].(string), snippet(row[2].(string), int(row[3].(int64))-1, len([]rune(word)))}
	}
	for i := range entries {
		match := byID[entries[i].ID]
		entries[i].Snippet, entries[i].SnippetProvider = match.text, match.provider
	}
	return nil
}

func snippet(text string, start, length int) string {
	runes := []rune(text)
	left, right := max(0, start-12), min(len(runes), start+length+28)
	value := string(runes[left:right])
	if left > 0 {
		value = "…" + value
	}
	if right < len(runes) {
		value += "…"
	}
	return value
}

func (s *Store) Results(ctx context.Context, id int64) ([]Result, error) {
	rows, err := s.query(ctx, `SELECT provider_id, result_type, translated_text, translated_ms, display_rank
		FROM history_result WHERE entry_id = ?1 ORDER BY display_rank, provider_id`, id)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, ErrMissing
	}
	results := make([]Result, len(rows))
	for i, row := range rows {
		results[i] = Result{
			Provider: row[0].(string), Type: ResultType(row[1].(int64)), Text: row[2].(string),
			TranslatedMS: row[3].(int64), Rank: row[4].(int64),
		}
	}
	return results, nil
}

func (s *Store) CopyText(ctx context.Context, id int64, provider string) (string, error) {
	sql := "SELECT translated_text FROM history_result WHERE entry_id = ?1"
	args := []any{id}
	if provider != "" {
		sql += " AND provider_id = ?2"
		args = append(args, provider)
	}
	rows, err := s.query(ctx, sql+" ORDER BY display_rank, provider_id LIMIT 1", args...)
	if err != nil {
		return "", err
	}
	if len(rows) == 0 {
		return "", ErrMissing
	}
	return rows[0][0].(string), nil
}

func (s *Store) Count(ctx context.Context) (int64, error) {
	db := s.db.Load()
	if db == nil {
		return 0, ErrUnavailable
	}
	return scalar(ctx, db, "SELECT count(*) FROM history_entry")
}

// GroupCounts returns matching entry counts for today (>= todayMS), recent
// (< todayMS and >= recentMS), and older (< recentMS) buckets.
func (s *Store) GroupCounts(ctx context.Context, word string, todayMS, recentMS int64) ([3]int64, error) {
	var counts [3]int64
	sql := `SELECT
		COALESCE(SUM(CASE WHEN e.last_translated_ms >= ?1 THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN e.last_translated_ms < ?1 AND e.last_translated_ms >= ?2 THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN e.last_translated_ms < ?2 THEN 1 ELSE 0 END), 0)
		FROM history_entry AS e`
	args := []any{todayMS, recentMS}
	if word = strings.TrimSpace(word); word != "" {
		sql += " WHERE " + strings.ReplaceAll(matches, "?1", "?3")
		args = append(args, word)
	}
	rows, err := s.query(ctx, sql, args...)
	if err != nil {
		return counts, err
	}
	if len(rows) != 1 || len(rows[0]) != 3 {
		return counts, ErrUnavailable
	}
	for i := range 3 {
		n, ok := rows[0][i].(int64)
		if !ok {
			return counts, ErrUnavailable
		}
		counts[i] = n
	}
	return counts, nil
}
