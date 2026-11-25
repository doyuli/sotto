// Package history stores final, copyable translations in the local history database.
package history

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo/plugins/sqlite"
)

const Filename = "sotto.db"

var (
	ErrUnavailable = errors.New("翻译历史不可用")
	ErrNewerSchema = errors.New("翻译历史由更新版本的 Sotto 创建")
	ErrMissing     = errors.New("这条翻译历史已不存在")
)

type Key struct {
	Text, SourceLang, TargetLang string
}

type Cursor struct {
	TranslatedMS, ID int64
}

type Entry struct {
	Key
	ID, TranslatedMS         int64
	Snippet, SnippetProvider string
}

func (e Entry) Cursor() Cursor { return Cursor{e.TranslatedMS, e.ID} }

type ResultType uint8

const (
	ResultTranslation ResultType = 1
	ResultDictionary  ResultType = 2
)

type Result struct {
	Provider, Text     string
	TranslatedMS, Rank int64
	Type               ResultType
}

type Policy struct {
	Enabled bool
	Days    int // 0 is unlimited
}

type Confirmation struct{ Count int64 }

func (e *Confirmation) Error() string {
	return fmt.Sprintf("缩短期限将删除 %d 条原文记录", e.Count)
}

// Store serializes writes and policy changes independently from cancellable reads.
// stateMu protects request bookkeeping; it is never held across database I/O.
type Store struct {
	writes   sync.Mutex
	db       atomic.Pointer[sqlite.DB]
	now      func() time.Time
	policy   Policy // guarded by writes
	closed   bool   // guarded by writes
	revision atomic.Uint64
	removals atomic.Uint64 // invalidates pending clipboard reads only when entries disappear
	changes  chan struct{}

	stateMu  sync.Mutex
	enabled  bool
	epoch    uint64
	sequence uint64
	active   map[Key]*inflight
}

type inflight struct {
	refs           int
	deletedThrough uint64
	latest         map[string]success
}

type success struct {
	ms       int64
	sequence uint64
}

type Request struct {
	store   *Store
	group   *inflight
	key     Key
	entryID int64 // zero for ordinary translations, which may create an entry
	at      int64
	seq     uint64
	epoch   uint64
	order   []string
	seen    map[string]bool // guarded by store.writes
	ended   bool            // guarded by store.stateMu
}

func New(policy Policy, now func() time.Time) *Store {
	return &Store{
		now: now, policy: policy, enabled: policy.Enabled,
		active: make(map[Key]*inflight), changes: make(chan struct{}, 1),
	}
}

func Open(ctx context.Context, dir string, policy Policy, now func() time.Time) (*Store, error) {
	s := New(policy, now)
	if err := s.Initialize(ctx, dir, policy); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) IsOpen() bool { return s.db.Load() != nil }

func (s *Store) Initialize(ctx context.Context, dir string, policy Policy) error {
	s.writes.Lock()
	defer s.writes.Unlock()
	if s.closed {
		return ErrUnavailable
	}
	if s.db.Load() != nil {
		return nil
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create history directory: %w", err)
	}
	path := filepath.Join(dir, Filename)
	if err := prepareHistoryFile(path); err != nil {
		return fmt.Errorf("secure history database: %w", err)
	}
	db, err := sqlite.Open(ctx, path, sqlite.OpenOptions{})
	if err != nil {
		return fmt.Errorf("open history database: %w", err)
	}
	if err := migrate(ctx, db); err != nil {
		_ = db.Close()
		return err
	}
	latest, err := scalar(ctx, db, "SELECT COALESCE(MAX(changed_revision), 0) FROM history_entry")
	if err != nil {
		_ = db.Close()
		return fmt.Errorf("read history revision: %w", err)
	}
	if _, err := prune(ctx, db, policy.Days, s.now()); err != nil {
		_ = db.Close()
		return err
	}
	s.policy = policy
	s.stateMu.Lock()
	s.enabled = policy.Enabled
	s.stateMu.Unlock()
	s.revision.Store(uint64(latest))
	s.db.Store(db)
	s.changed()
	return nil
}

func (s *Store) Close() error {
	s.writes.Lock()
	defer s.writes.Unlock()
	s.closed = true
	if db := s.db.Swap(nil); db != nil {
		return db.Close()
	}
	return nil
}

func (s *Store) Revision() uint64         { return s.revision.Load() }
func (s *Store) Changes() <-chan struct{} { return s.changes }

func (s *Store) RemovalRevision() uint64 { return s.removals.Load() }

func (s *Store) changed() {
	s.revision.Add(1)
	select {
	case s.changes <- struct{}{}:
	default:
	}
}

func (s *Store) Begin(key Key, order []string) *Request {
	key.Text = strings.TrimSpace(key.Text)
	if key.Text == "" || key.SourceLang == "" || key.TargetLang == "" {
		return nil
	}
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	if !s.enabled {
		return nil
	}
	s.sequence++
	g := s.active[key]
	if g == nil {
		g = &inflight{latest: make(map[string]success)}
		s.active[key] = g
	}
	g.refs++
	return &Request{store: s, group: g, key: key, at: s.now().UnixMilli(),
		seq: s.sequence, epoch: s.epoch, order: slices.Clone(order), seen: make(map[string]bool)}
}

// BeginForEntry binds a retranslation to the selected row even if its key is later reused.
func (s *Store) BeginForEntry(ctx context.Context, id int64, order []string) (Entry, *Request, error) {
	s.writes.Lock()
	defer s.writes.Unlock()
	entry, err := s.Entry(ctx, id)
	if err != nil {
		return Entry{}, nil, err
	}
	r := s.Begin(entry.Key, order)
	if r != nil {
		r.entryID = entry.ID
	}
	return entry, r, nil
}

// Done lets arbitration state expire once there are no outstanding requests
// for an entry. Older timestamps remain guarded by the database across restarts.
func (r *Request) Done() {
	if r == nil {
		return
	}
	s := r.store
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	if !r.ended {
		r.ended = true
		r.group.refs--
		if r.group.refs == 0 {
			delete(s.active, r.key)
		}
	}
}

func (s *Store) Record(ctx context.Context, r *Request, provider, text string, resultType ResultType) (bool, error) {
	if r == nil || r.store != s || provider == "" || strings.TrimSpace(text) == "" {
		return false, nil
	}
	s.writes.Lock()
	defer s.writes.Unlock()
	s.stateMu.Lock()
	valid := !r.ended && s.enabled && r.epoch == s.epoch && r.seq > r.group.deletedThrough
	s.stateMu.Unlock()
	if !valid || ctx.Err() != nil || r.seen[provider] {
		return false, nil
	}
	if s.policy.Days != 0 && r.at < cutoff(s.policy.Days, s.now()) {
		return false, nil
	}
	previous := r.group.latest[provider]
	if previous.ms == r.at && previous.sequence > r.seq {
		return false, nil
	}
	db := s.db.Load()
	if db == nil {
		return false, ErrUnavailable
	}
	rank := slices.Index(r.order, provider)
	if rank < 0 {
		rank = len(r.order)
	}
	key := r.key
	entryArgs := []any{key.Text, key.SourceLang, key.TargetLang, r.at, s.Revision() + 1}
	res, err := db.Transaction(ctx, []sqlite.Statement{
		{SQL: insertEntry, Args: append(entryArgs, r.entryID)},
		{SQL: upsertResult, Args: []any{key.Text, key.SourceLang, key.TargetLang, provider, int64(resultType), text, r.at, rank, r.entryID}},
		{SQL: touchEntry, Args: entryArgs},
	})
	if err != nil {
		return false, fmt.Errorf("write translation history: %w", err)
	}
	r.seen[provider] = true
	if res[1].Changes == 0 {
		return false, nil
	}
	r.group.latest[provider] = success{r.at, r.seq}
	s.changed()
	return true, nil
}

func (s *Store) Delete(ctx context.Context, id int64) error {
	s.writes.Lock()
	defer s.writes.Unlock()
	e, err := s.Entry(ctx, id)
	if err != nil {
		return err
	}
	if _, err := s.db.Load().Execute(ctx, "DELETE FROM history_entry WHERE id = ?1", id); err != nil {
		return fmt.Errorf("delete translation history: %w", err)
	}
	s.removals.Add(1)
	s.stateMu.Lock()
	if group := s.active[e.Key]; group != nil {
		group.deletedThrough = s.sequence
		clear(group.latest)
	}
	s.stateMu.Unlock()
	s.changed()
	return nil
}

func (s *Store) Clear(ctx context.Context) error {
	s.writes.Lock()
	defer s.writes.Unlock()
	db := s.db.Load()
	if db == nil {
		return ErrUnavailable
	}
	if _, err := db.Execute(ctx, "DELETE FROM history_entry"); err != nil {
		return fmt.Errorf("clear translation history: %w", err)
	}
	s.removals.Add(1)
	s.stateMu.Lock()
	s.epoch++
	s.stateMu.Unlock()
	s.changed()
	return nil
}

// UpdatePolicy checks the deletion count again under the writer lock. The caller
// can approve the new count if history grew since the confirmation was displayed.
func (s *Store) UpdatePolicy(ctx context.Context, policy Policy, approved int64, persist func() error) error {
	s.writes.Lock()
	defer s.writes.Unlock()
	db := s.db.Load()
	retentionChanged := s.policy.Days != policy.Days
	checkTime := s.now()
	if retentionChanged && policy.Days > 0 && (s.policy.Days == 0 || policy.Days < s.policy.Days) {
		if db == nil {
			return ErrUnavailable
		}
		count, err := scalar(ctx, db, "SELECT count(*) FROM history_entry WHERE last_translated_ms < ?1", cutoff(policy.Days, checkTime))
		if err != nil {
			return fmt.Errorf("count expiring translation history: %w", err)
		}
		if count > approved {
			return &Confirmation{count}
		}
	}
	if err := persist(); err != nil {
		return err
	}
	s.policy = policy
	s.stateMu.Lock()
	if s.enabled && !policy.Enabled {
		s.epoch++
	}
	s.enabled = policy.Enabled
	s.stateMu.Unlock()
	if retentionChanged && db != nil {
		// Use the confirmed cutoff even if saving preferences crossed a time boundary.
		return s.prune(ctx, db, checkTime)
	}
	return nil
}

func (s *Store) Cleanup(ctx context.Context) error {
	s.writes.Lock()
	defer s.writes.Unlock()
	if db := s.db.Load(); db != nil {
		return s.prune(ctx, db, s.now())
	}
	return ErrUnavailable
}

func (s *Store) prune(ctx context.Context, db *sqlite.DB, now time.Time) error {
	n, err := prune(ctx, db, s.policy.Days, now)
	if n != 0 {
		s.removals.Add(1)
		s.changed()
	}
	return err
}
