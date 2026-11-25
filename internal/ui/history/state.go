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
	"sotto/internal/naming"
)

const pageSize = 30
const speechUnavailable = "朗读不可用"

type Actions struct {
	Run                func(func())
	Post               func(func())
	Copy               func(string) error
	Speak              func(text, lang string, onError func()) // onError runs on the UI thread
	Close              func()
	OpenServices       func()
	DictionaryEligible func(history.Entry) bool // runs on a detail worker after source preprocessing
	Providers          func() []string          // enabled providers in configured order
	CopyFormats        func() []string          // enabled naming styles in configured order
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
	focus                         bool
	more, loading, ready, older   bool
	refreshing                    bool
	loadError, detailError        bool
	entries                       []history.Entry
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
	collapsed                     map[string]bool
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
		v.older, v.clearOpen, v.deleting, v.providerOpen = false, false, false, false
		v.retries = make(map[int64]retryState)
		v.collapsed = make(map[string]bool)
		v.groupCounts, v.hasGroupCounts = [3]int64{}, false
		v.pendingToast, v.clearToasts = "", true
		v.copiedKey, v.copiedUntil = "", time.Time{}
		v.ready, v.loadError = false, false
		v.entries = nil
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
	if v.life != nil && v.revision != v.store.Revision() {
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
	revision, older, now := v.revision, v.older, v.now()
	todayMS, recentMS := groupBounds(now)
	v.actions.Run(func() {
		entries, err := v.store.List(ctx, query, history.Cursor{}, pageSize+1)
		more := len(entries) > pageSize
		page := entries[:min(len(entries), pageSize)]
		var entry history.Entry
		var results []history.Result
		var detailErr error
		if err == nil {
			entry, detailErr = v.firstPageEntry(ctx, page, id, query, older, now)
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
			}
			// Publish the list and first detail in the same frame so retained
			// results don't pass through an empty or partially updated view.
			v.query, v.entries, v.more = query, page, more
			v.hasGroupCounts = false
			v.dictionaryEligible = dictionaryEligible
			v.refreshing, v.ready, v.loadError = false, true, err != nil
			v.commitDetail(entry.ID, entry, results, detailErr)
			if more && err == nil {
				v.loadCounts(ctx, revision, query, todayMS, recentMS)
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
				}
			})
		}
	})
}

func (v *View) searchChanged() {
	next := strings.TrimSpace(v.search)
	if next != v.requestedQuery {
		v.copySerial++
		v.status = ""
		v.reset(true)
	}
}

func (v *View) firstPageEntry(ctx context.Context, page []history.Entry, id int64, query string, older bool, now time.Time) (history.Entry, error) {
	visible := func(e history.Entry) bool { return older || query != "" || groupOf(now, e) != 2 }
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

func (v *View) commitDetail(id int64, entry history.Entry, results []history.Result, err error) {
	if v.selected != id {
		v.speechSerial++
		v.providerOpen, v.status = false, ""
		v.collapsed = make(map[string]bool)
		v.copiedKey, v.copiedUntil = "", time.Time{}
		v.detailScroll.ScrollTo(0, 0, ui.Animation{})
	}
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

func (v *View) copySelected() {
	v.copy(v.navigationID(), "", true)
}

func (v *View) copySource() {
	if !v.detailUsable() || v.entry.ID == 0 {
		return
	}
	v.copyCustom(v.entry.ID, "", v.entry.Text, "已复制原文", "source", false)
}

func (v *View) copy(id int64, provider string, closeAfter bool) {
	v.copyCustom(id, provider, "", "已复制译文", "provider:"+provider, closeAfter)
}

func (v *View) copyCustom(id int64, provider, overrideText, toastMsg, feedbackKey string, closeAfter bool) {
	if !v.pageUsable() || id == 0 {
		return
	}
	v.copySerial++
	v.copyAtRevision(id, provider, overrideText, toastMsg, feedbackKey, closeAfter, v.copySerial)
}

func (v *View) copyAtRevision(id int64, provider, overrideText, toastMsg, feedbackKey string, closeAfter bool, serial uint64) {
	ctx := v.life
	v.actions.Run(func() {
		revision := v.store.Revision()
		text, err := v.store.CopyText(ctx, id, provider)
		if overrideText != "" && err == nil {
			text = overrideText
		}
		v.actions.Post(func() {
			if ctx.Err() != nil || serial != v.copySerial {
				return
			}
			if v.store.Revision() != revision {
				v.Refresh()
				v.copyAtRevision(id, provider, overrideText, toastMsg, feedbackKey, closeAfter, serial)
				return
			}
			if errors.Is(err, history.ErrMissing) {
				v.reset(false)
				return
			}
			if err != nil {
				v.status = "无法读取译文，请重试。"
				return
			}
			var copyErr error
			if !v.store.AtRevision(revision, func() { copyErr = v.actions.Copy(text) }) {
				v.copyAtRevision(id, provider, overrideText, toastMsg, feedbackKey, closeAfter, serial)
				return
			}
			if copyErr != nil {
				v.status = "无法写入剪贴板，请重试。"
				return
			}
			if closeAfter {
				v.Dismiss()
			} else {
				v.status = ""
				v.pendingToast = toastMsg
				v.copiedKey = feedbackKey
				v.copiedUntil = time.Time{}
			}
		})
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
				v.reset(false)
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
	if !v.detailUsable() || v.entry.ID != entry.ID || state.pending ||
		v.actions.Retry == nil || !slices.Contains(v.actions.Providers(), provider) ||
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
			if life.Err() != nil {
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

func groupBounds(now time.Time) (int64, int64) {
	year, month, day := now.Date()
	today := time.Date(year, month, day, 0, 0, 0, 0, now.Location())
	return today.UnixMilli(), today.AddDate(0, 0, -2).UnixMilli()
}

func groupOf(now time.Time, e history.Entry) int {
	todayMS, recentMS := groupBounds(now)
	switch {
	case e.TranslatedMS >= todayMS:
		return 0
	case e.TranslatedMS >= recentMS:
		return 1
	default:
		return 2
	}
}

func (v *View) visible(e history.Entry) bool {
	return v.older || v.query != "" || groupOf(v.now(), e) != 2
}

type railRow struct {
	header bool
	group  int
	count  int
	entry  history.Entry
}

func (r railRow) key() int64 {
	if r.header {
		return -int64(r.group) - 1
	}
	return r.entry.ID
}

func (v *View) rows() []railRow {
	now, previous := v.now(), -1
	rows := make([]railRow, 0, len(v.entries)+3)
	header := 0
	for _, e := range v.entries {
		group := groupOf(now, e)
		if group != previous {
			header = len(rows)
			rows = append(rows, railRow{header: true, group: group})
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
	return rows
}

func (v *View) totalCount() int {
	if v.hasGroupCounts {
		return int(v.groupCounts[0] + v.groupCounts[1] + v.groupCounts[2])
	}
	return len(v.entries)
}

func (v *View) copyFormats() []string {
	if v.actions.CopyFormats != nil {
		return v.actions.CopyFormats()
	}
	return naming.Styles
}

func (v *View) move(delta int) {
	if !v.pageUsable() {
		return
	}
	rows := v.rows()
	id := v.navigationID()
	i := slices.IndexFunc(rows, func(r railRow) bool { return !r.header && r.entry.ID == id })
	if i < 0 && delta < 0 {
		i = len(rows)
	}
	for i += delta; i >= 0 && i < len(rows); i += delta {
		if !rows[i].header {
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
