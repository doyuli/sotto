// Package historyview implements the native translation history window.
package historyview

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"sotto/internal/history"
)

const pageSize = 30
const searchDelay = 100 * time.Millisecond
const speechUnavailable = "朗读不可用"

type Actions struct {
	OpenStore          func(context.Context) error
	Run                func(func())
	Post               func(func())
	Copy               func(string) error
	Speak              func(text, lang string, onError func()) // onError runs on the UI thread
	Close              func()
	OpenServices       func()
	DictionaryEligible func(history.Entry) bool                 // runs on a detail worker after source preprocessing
	Options            func() (providers, copyFormats []string) // current settings, in configured order
	Retry              func(history.Entry, string) (RetryResult, error)
}

// RetryResult contains a completed translation that could not be added to the
// saved results. It is local to the current selection.
type RetryResult struct {
	Text, Notice string
	Type         history.ResultType
}

type retryState struct {
	provider       string
	pending        bool
	err            error
	lastErr        error
	result         RetryResult
	resultProvider string
}

// View owns UI state on the main thread. Jobs only retain snapshots and
// publish through Actions.Post; no widget or UI context crosses that boundary.
type View struct {
	store   *history.Store
	actions Actions
	now     func() time.Time

	life, queryCtx      context.Context
	cancel, cancelQuery context.CancelFunc
	cancelDetail        context.CancelFunc
	revision            uint64

	search, query, requestedQuery string
	searchDue                     time.Time
	buckets                       dayBuckets // shared by the committed page and its counts
	pageRevision, pageRemovals    uint64
	focus                         bool
	more, loading, ready, older   bool
	refreshing                    bool
	loadError, detailError        bool
	entries                       []history.Entry
	railRows                      []railRow
	selected, detailTarget        int64
	dictionaryEligible            bool
	entry                         history.Entry
	results                       []history.Result
	status                        string
	copySerial                    uint64
	speechSerial                  uint64
	clearOpen, deleting           bool
	providerOpen                  bool
	retries                       map[int64]retryState
	manualCollapse                map[string]bool
	sourceExpanded, sourceManual  bool
	detailMeasure                 detailMeasurement
	groupCounts                   [3]int64
	hasGroupCounts                bool
	pendingToast                  string
	clearToasts                   bool
	copiedKey                     string
	copiedUntil                   time.Time

	rail         ui.ListState
	detailScroll ui.ScrollState
	searchField  ui.Handle
}

type detailMeasurement struct {
	entryID           int64
	width             float32
	query             string
	resultCount       int
	sourceOverflow    bool
	sourceMatchHidden bool
	defaultCollapsed  map[string]bool
}

func New(store *history.Store, actions Actions, now func() time.Time) *View {
	if actions.Run == nil {
		actions.Run = func(work func()) { go work() }
	}
	return &View{store: store, actions: actions, now: now}
}

func (v *View) Open() {
	if v.life == nil {
		v.life, v.cancel = context.WithCancel(context.Background())
		v.search, v.query, v.status = "", "", ""
		v.searchDue, v.buckets = time.Time{}, dayBuckets{}
		v.older, v.clearOpen, v.deleting, v.providerOpen = false, false, false, false
		v.retries = make(map[int64]retryState)
		v.resetDetailDisclosure()
		v.groupCounts, v.hasGroupCounts = [3]int64{}, false
		v.pendingToast, v.clearToasts = "", true
		v.copiedKey, v.copiedUntil = "", time.Time{}
		v.ready, v.loadError = false, false
		v.entries, v.railRows = nil, nil
		v.commitDetail(0, history.Entry{}, nil, nil)
		v.reset(true)
	}
	v.focus = true
}

func (v *View) Dispose() {
	if v.life != nil {
		v.cancel()
		v.life = nil
	}
}

// Dismiss also cancels callbacks that haven't reached the clipboard yet.
func (v *View) Dismiss() {
	if v.life == nil {
		return
	}
	v.Dispose()
	v.actions.Close()
}

func (v *View) Refresh() {
	if v.life != nil && v.searchDue.IsZero() && v.revision != v.store.Revision() {
		v.reset(false)
	}
}

func (v *View) reset(clearSelection bool) {
	query := strings.TrimSpace(v.search)
	clearSelection = clearSelection || query != v.query
	id := v.navigationID()
	if clearSelection {
		id = 0
	}
	if v.cancelQuery != nil {
		v.cancelQuery()
	}
	v.cancelDetailRead()
	ctx, cancel := context.WithCancel(v.life)
	v.queryCtx, v.cancelQuery = ctx, cancel
	v.revision = v.store.Revision()
	v.requestedQuery = query
	v.refreshing, v.loading = true, false
	revision, older, buckets := v.revision, v.older, groupBounds(v.now())
	removals := v.store.RemovalRevision()
	previous, since, hadMore := v.entries, v.pageRevision, v.more
	if clearSelection || len(previous) <= pageSize || v.loadError || removals != v.pageRemovals {
		previous = nil
	}
	v.actions.Run(func() {
		var page []history.Entry
		var more bool
		var err error
		if !v.store.IsOpen() && v.actions.OpenStore != nil {
			err = v.actions.OpenStore(ctx)
		}
		if err == nil {
			page, more, err = v.store.RefreshPage(ctx, query, previous, since, hadMore, pageSize)
		}
		var entry history.Entry
		var results []history.Result
		var detailErr error
		if err == nil {
			entry, detailErr = v.firstPageEntry(ctx, page, id, query, older, buckets)
			// Retaining only the detail would leave arrow navigation anchored to a missing row.
			for more && entry.ID != 0 {
				tail := page[len(page)-1]
				if tail.TranslatedMS < entry.TranslatedMS || tail.TranslatedMS == entry.TranslatedMS && tail.ID <= entry.ID {
					break
				}
				next, readErr := v.store.List(ctx, query, tail.Cursor(), pageSize+1)
				if readErr != nil {
					err = readErr
					entry = history.Entry{}
					break
				}
				more = len(next) > pageSize
				page = append(page, next[:min(len(next), pageSize)]...)
			}
			if detailErr == nil && entry.ID != 0 {
				results, detailErr = v.store.Results(ctx, entry.ID)
			}
		}
		dictionaryEligible := v.canLookup(entry)
		v.actions.Post(func() {
			if !v.queryCurrent(ctx, revision) {
				return
			}
			if clearSelection {
				v.rail.ScrollTo(0, ui.Start)
				v.resetDetailDisclosure()
			}
			// Publish the list and first detail in the same frame so retained
			// results don't pass through an empty or partially updated view.
			v.query, v.entries, v.more = query, page, more
			v.buckets, v.pageRevision, v.pageRemovals = buckets, revision, removals
			v.hasGroupCounts = false
			v.rebuildRows()
			v.dictionaryEligible = dictionaryEligible
			v.refreshing, v.ready, v.loadError = false, true, err != nil
			v.commitDetail(entry.ID, entry, results, detailErr)
			if more && err == nil {
				v.loadCounts(ctx, revision, query, buckets.today, buckets.recent)
			}
		})
	})
}

func (v *View) loadCounts(ctx context.Context, revision uint64, query string, todayMS, recentMS int64) {
	v.actions.Run(func() {
		counts, err := v.store.GroupCounts(ctx, query, todayMS, recentMS)
		if err == nil {
			v.actions.Post(func() {
				if v.queryCurrent(ctx, revision) {
					v.groupCounts, v.hasGroupCounts = counts, true
					v.rebuildRows()
				}
			})
		}
	})
}

func (v *View) searchChanged(now time.Time) {
	next := strings.TrimSpace(v.search)
	if next == v.requestedQuery {
		return
	}
	v.copySerial++
	v.status = ""
	v.requestedQuery = next
	v.cancelQuery()
	v.cancelDetailRead()
	v.refreshing = true
	v.searchDue = now.Add(searchDelay)
	if next == "" {
		v.searchDue = time.Time{}
		v.reset(true)
	}
}

// The UI clock coalesces edits without timers or goroutines surviving the window.
func (v *View) updateQuery(c *ui.Context) {
	if !v.searchDue.IsZero() {
		if remaining := v.searchDue.Sub(c.Now()); remaining > 0 {
			c.After(remaining)
		} else {
			v.searchDue = time.Time{}
			v.reset(true)
		}
	} else if v.ready && !v.refreshing && groupBounds(v.now()) != v.buckets {
		v.reset(false)
	}
}

func (v *View) firstPageEntry(ctx context.Context, page []history.Entry, id int64, query string, older bool, buckets dayBuckets) (history.Entry, error) {
	visible := func(e history.Entry) bool { return older || query != "" || buckets.group(e) != 2 }
	if id != 0 {
		if i := slices.IndexFunc(page, func(e history.Entry) bool { return e.ID == id }); i >= 0 {
			if visible(page[i]) {
				return page[i], nil
			}
		} else {
			entry, err := v.store.Find(ctx, id, query)
			if err != nil && !errors.Is(err, history.ErrMissing) {
				return history.Entry{}, err
			}
			if err == nil && visible(entry) {
				return entry, nil
			}
		}
	}
	for _, entry := range page {
		if visible(entry) {
			return entry, nil
		}
	}
	return history.Entry{}, nil
}

// queryCurrent rejects late results before they can change the committed view.
func (v *View) queryCurrent(ctx context.Context, revision uint64) bool {
	if ctx.Err() != nil {
		return false
	}
	if v.store.Revision() != revision {
		v.reset(false)
		return false
	}
	return true
}

func (v *View) pageUsable() bool {
	return v.life != nil && !v.refreshing && strings.TrimSpace(v.search) == v.query
}

func (v *View) detailUsable() bool {
	return v.pageUsable() && v.detailTarget == 0
}

func (v *View) navigationID() int64 {
	if v.detailTarget != 0 {
		return v.detailTarget
	}
	return v.selected
}

// loadPage is called by keyboard navigation and the virtual list's post-layout
// near-end callback. Neither layout nor painting performs database work.
func (v *View) loadPage() {
	if !v.pageUsable() || v.loading || !v.more || v.loadError || len(v.entries) == 0 {
		return
	}
	after := v.entries[len(v.entries)-1].Cursor()
	v.loading = true
	ctx, query, revision := v.queryCtx, v.query, v.revision
	v.actions.Run(func() {
		entries, err := v.store.List(ctx, query, after, pageSize+1)
		v.actions.Post(func() {
			if !v.queryCurrent(ctx, revision) {
				return
			}
			v.loading, v.ready = false, true
			if err != nil {
				v.loadError = true
				return
			}
			v.more = len(entries) > pageSize
			v.entries = append(v.entries, entries[:min(len(entries), pageSize)]...)
			v.rebuildRows()
			if v.navigationID() == 0 {
				v.selectFirst()
			}
		})
	})
}

func (v *View) selectFirst() {
	for _, e := range v.entries {
		if v.visible(e) {
			v.selectEntry(e.ID)
			return
		}
	}
}

func (v *View) selectEntry(id int64) {
	if !v.pageUsable() || v.navigationID() == id {
		return
	}
	if id == v.selected {
		v.cancelDetailRead()
	} else if id == 0 {
		v.cancelDetailRead()
		v.commitDetail(0, history.Entry{}, nil, nil)
	} else {
		v.loadDetail(id)
	}
}

func (v *View) cancelDetailRead() {
	if v.cancelDetail != nil {
		v.cancelDetail()
		v.cancelDetail = nil
	}
	v.detailTarget = 0
}

func (v *View) resetDetailDisclosure() {
	v.manualCollapse = make(map[string]bool)
	v.sourceExpanded, v.sourceManual = false, false
}

func (v *View) commitDetail(id int64, entry history.Entry, results []history.Result, err error) {
	if v.selected != id {
		v.speechSerial++
		v.providerOpen, v.status = false, ""
		v.resetDetailDisclosure()
		v.copiedKey, v.copiedUntil = "", time.Time{}
		v.detailScroll.ScrollTo(0, 0, ui.Animation{})
	}
	v.detailMeasure = detailMeasurement{}
	v.selected, v.entry, v.results, v.detailError = id, entry, results, err != nil
}

func (v *View) loadDetail(id int64) {
	if !v.pageUsable() || id == 0 {
		return
	}
	v.cancelDetailRead()
	ctx, cancel := context.WithCancel(v.queryCtx)
	v.cancelDetail = cancel
	v.detailTarget = id
	query, revision := v.query, v.revision
	v.actions.Run(func() {
		entry, err := v.store.Find(ctx, id, query)
		var results []history.Result
		if err == nil {
			results, err = v.store.Results(ctx, id)
		}
		dictionaryEligible := v.canLookup(entry)
		v.actions.Post(func() {
			if !v.queryCurrent(ctx, revision) {
				return
			}
			v.cancelDetailRead()
			v.dictionaryEligible = dictionaryEligible
			v.commitDetail(id, entry, results, err)
		})
	})
}

func (v *View) speak(id int64, text, lang string) {
	text = strings.TrimSpace(text)
	if !v.detailUsable() || id != v.selected || v.actions.Speak == nil || text == "" {
		return
	}
	if v.status == speechUnavailable {
		v.status = ""
	}
	v.speechSerial++
	life, serial := v.life, v.speechSerial
	v.actions.Speak(text, lang, func() {
		if life.Err() == nil && serial == v.speechSerial && v.detailUsable() {
			v.status = speechUnavailable
		}
	})
}

func (v *View) delete(id int64) {
	if v.deleting || id != 0 && !v.detailUsable() {
		return
	}
	v.deleting, v.status = true, ""
	ctx := v.life
	v.actions.Run(func() {
		// An explicitly confirmed mutation keeps running after the window closes.
		writeCtx := context.WithoutCancel(ctx)
		var err error
		if id == 0 {
			err = v.store.Clear(writeCtx)
		} else {
			err = v.store.Delete(writeCtx, id)
		}
		v.actions.Post(func() {
			if ctx.Err() != nil {
				return
			}
			v.deleting = false
			switch {
			case err == nil || errors.Is(err, history.ErrMissing):
				if id == 0 {
					clear(v.retries)
				} else {
					delete(v.retries, id)
				}
				v.Refresh()
			default:
				v.status = "无法删除翻译历史，请重试。"
			}
		})
	})
}

func (v *View) hasTranslation(entryID int64, provider string) bool {
	retry := v.retries[entryID]
	return retry.result.Text != "" && retry.resultProvider == provider ||
		v.entry.ID == entryID && slices.ContainsFunc(v.results, func(result history.Result) bool {
			return result.Provider == provider
		})
}

func (v *View) canLookup(entry history.Entry) bool {
	return entry.ID != 0 && v.actions.DictionaryEligible != nil && v.actions.DictionaryEligible(entry)
}

func (v *View) providerEligible(provider string) bool {
	return provider != "dictionary" || v.dictionaryEligible
}

func (v *View) retry(entry history.Entry, provider string) {
	state := v.retries[entry.ID]
	providers, _ := v.actions.Options()
	if !v.detailUsable() || v.entry.ID != entry.ID || state.pending ||
		v.actions.Retry == nil || !slices.Contains(providers, provider) ||
		!v.providerEligible(provider) {
		return
	}
	if state.provider == provider && state.err != nil {
		state.lastErr = state.err
	} else {
		state.lastErr = nil
	}
	state.provider, state.pending, state.err = provider, true, nil
	v.retries[entry.ID] = state
	life := v.life
	v.actions.Run(func() {
		result, err := v.actions.Retry(entry, provider)
		v.actions.Post(func() {
			if life.Err() != nil || !v.retries[entry.ID].pending {
				return
			}
			if errors.Is(err, history.ErrMissing) {
				delete(v.retries, entry.ID)
			} else {
				state.pending, state.err, state.lastErr = false, err, nil
				if err == nil {
					state.result, state.resultProvider = result, provider
				}
				v.retries[entry.ID] = state
			}
			v.Refresh()
		})
	})
}

type dayBuckets struct{ today, recent int64 }

func groupBounds(now time.Time) dayBuckets {
	year, month, day := now.Date()
	today := time.Date(year, month, day, 0, 0, 0, 0, now.Location())
	return dayBuckets{today.UnixMilli(), today.AddDate(0, 0, -2).UnixMilli()}
}

func (b dayBuckets) group(e history.Entry) int {
	switch {
	case e.TranslatedMS >= b.today:
		return 0
	case e.TranslatedMS >= b.recent:
		return 1
	default:
		return 2
	}
}

func (v *View) visible(e history.Entry) bool {
	return v.older || v.query != "" || v.buckets.group(e) != 2
}

type railRow struct {
	group int
	count int
	entry *history.Entry // nil for a group header
}

func (r railRow) key() int64 {
	if r.entry == nil {
		return -int64(r.group) - 1
	}
	return r.entry.ID
}

func (v *View) rebuildRows() {
	previous := -1
	rows := make([]railRow, 0, len(v.entries)+3)
	header := 0
	for i := range v.entries {
		e := &v.entries[i]
		group := v.buckets.group(*e)
		if group != previous {
			header = len(rows)
			rows = append(rows, railRow{group: group})
			previous = group
		}
		if v.hasGroupCounts {
			rows[header].count = int(v.groupCounts[group])
		} else {
			rows[header].count++
		}
		if group != 2 || v.older || v.query != "" {
			rows = append(rows, railRow{entry: e})
		}
	}
	if v.more && !v.hasGroupCounts && len(rows) != 0 {
		rows[header].count = 0
	}
	v.railRows = rows
}

func (v *View) totalCount() int {
	if v.hasGroupCounts {
		return int(v.groupCounts[0] + v.groupCounts[1] + v.groupCounts[2])
	}
	return len(v.entries)
}

func (v *View) move(delta int) {
	if !v.pageUsable() {
		return
	}
	rows := v.railRows
	id := v.navigationID()
	i := slices.IndexFunc(rows, func(r railRow) bool { return r.entry != nil && r.entry.ID == id })
	if i < 0 && delta < 0 {
		i = len(rows)
	}
	for i += delta; i >= 0 && i < len(rows); i += delta {
		if rows[i].entry != nil {
			v.selectEntry(rows[i].entry.ID)
			v.rail.ScrollIntoView(i)
			if delta > 0 && i >= len(rows)-3 && v.canExtend() {
				v.loadPage()
			}
			return
		}
	}
	if delta > 0 && v.canExtend() {
		v.loadPage()
	}
}

func (v *View) canExtend() bool {
	// Once the loaded tail is in a collapsed section, more pages in the
	// same section would not expose any new rows.
	return len(v.entries) != 0 && v.visible(v.entries[len(v.entries)-1])
}

func (v *View) toggleOlder() {
	if !v.pageUsable() {
		return
	}
	v.cancelDetailRead()
	v.older = !v.older
	v.rebuildRows()
	if v.older && v.selected == 0 {
		v.selectFirst()
	}
	if !v.older && v.entry.ID != 0 && !v.visible(v.entry) {
		v.selectFirst()
		if v.detailTarget == 0 {
			v.selectEntry(0)
		}
	}
}
