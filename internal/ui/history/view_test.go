package historyview

import (
	"context"
	"errors"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"sotto/internal/history"
	"sotto/internal/naming"
	"sotto/internal/translate"
)

type harness struct {
	t              *testing.T
	store          *history.Store
	view           *View
	ui             *ui.Tester
	now            time.Time
	work           []func()
	post           []func()
	copied         []string
	spoken         [][2]string
	speechFailures []func()
	closed         int
	copyErr        error
	onRetry        func(context.Context, history.Entry, string) error
	providers      []string
	openedServices int
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	dir := t.TempDir()
	h := newHarnessWithDirectory(t, func() (string, error) { return dir, nil })
	if err := h.view.actions.OpenStore(t.Context()); err != nil {
		t.Fatal(err)
	}
	return h
}

func newHarnessWithDirectory(t *testing.T, directory func() (string, error)) *harness {
	t.Helper()
	h := &harness{t: t, now: time.Date(2026, 11, 24, 10, 42, 0, 0, time.Local),
		providers: []string{"deepl", "google", "youdao"}}
	h.store = history.New(history.Policy{Enabled: true}, func() time.Time { return h.now })
	t.Cleanup(func() { _ = h.store.Close() })
	h.view = New(h.store, Actions{
		OpenStore: func(ctx context.Context) error {
			dir, err := directory()
			if err != nil {
				return err
			}
			return h.store.Initialize(ctx, dir, history.Policy{Enabled: true})
		},
		Run:  func(f func()) { h.work = append(h.work, f) },
		Post: func(f func()) { h.post = append(h.post, f) },
		Copy: func(text string) error {
			if h.copyErr != nil {
				return h.copyErr
			}
			h.copied = append(h.copied, text)
			return nil
		},
		Speak: func(text, lang string, onError func()) {
			h.spoken = append(h.spoken, [2]string{text, lang})
			h.speechFailures = append(h.speechFailures, onError)
		},
		Close:              func() { h.closed++ },
		OpenServices:       func() { h.openedServices++ },
		Options:            func() ([]string, []string) { return h.providers, naming.Styles },
		DictionaryEligible: func(e history.Entry) bool { return translate.AllowsEnglishLookup(e.Text, e.SourceLang) },
		Retry: func(e history.Entry, provider string) (RetryResult, error) {
			return RetryResult{}, h.onRetry(h.t.Context(), e, provider)
		},
	}, func() time.Time { return h.now })
	return h
}

func (h *harness) put(text string, translations ...string) {
	h.t.Helper()
	order := []string{"deepl", "google", "youdao"}
	r := h.store.Begin(history.Key{Text: text, SourceLang: "en", TargetLang: "zh-HANS"}, order)
	defer r.Done()
	for i, value := range translations {
		if ok, err := h.store.Record(h.t.Context(), r, order[i], value, history.ResultTranslation); err != nil || !ok {
			h.t.Fatalf("fixture write: %v, %v", ok, err)
		}
	}
}

func (h *harness) open() {
	h.view.Open()
	h.ui = ui.NewTester(h.view.Render, DefaultWidth, DefaultHeight)
	h.settle()
}

func (h *harness) one() {
	h.t.Helper()
	if len(h.work) == 0 {
		h.t.Fatal("no pending background work")
	}
	work := h.work[0]
	h.work = h.work[1:]
	work()
}

func (h *harness) settle() {
	h.t.Helper()
	if h.ui != nil && !h.view.searchDue.IsZero() {
		// Advance past MyGo's 1ms minimum timer clamp when 60Hz animations land just before searchDue.
		h.ui.Advance(max(searchDelay, h.view.searchDue.Sub(h.ui.Now())) + 2*time.Millisecond)
	}
	for i := 0; i < 20; i++ {
		if len(h.work) == 0 && len(h.post) == 0 {
			return
		}
		for len(h.work) > 0 {
			h.one()
		}
		posted := h.post
		h.post = nil
		for _, f := range posted {
			f()
		}
		h.ui.Frame()
	}
	h.t.Fatal("history view did not settle")
}

func (h *harness) click(label string) {
	h.t.Helper()
	if err := h.ui.Click(label); err != nil {
		h.t.Fatal(err)
	}
	h.settle()
}

func TestHistorySearchKeyboardCopyAndIME(t *testing.T) {
	h := newHarness(t)
	h.put("in retrospect", "回想起来", "回顾", "另一个关键片段")
	h.now = h.now.Add(time.Second)
	h.put("The margin of error is small.", "误差范围很小。\n第二行。", "误差幅度很小。", "关键片段：容错余地很小。")
	h.open()
	if !h.ui.Focused(searchLabel) {
		t.Fatal("opening history did not focus search")
	}
	h.ui.Type("关键片段")
	h.settle()
	if _, ok := h.ui.Find("in retrospect"); !ok {
		t.Fatal("translation-only match missing from the list")
	}
	h.ui.Compose("词", 1)
	h.ui.Key(0, ui.KeyDown)
	h.ui.Key(0, ui.KeyEnter)
	h.settle()
	if len(h.copied) != 0 || h.closed != 0 {
		t.Fatalf("IME input triggered history copy: %q, closed %d", h.copied, h.closed)
	}
	h.ui.Compose("", 0)
	h.ui.Key(0, ui.KeyDown)
	h.settle()
	if !h.ui.Focused(searchLabel) {
		t.Fatal("navigation moved focus out of search")
	}
	h.copyErr = errors.New("clipboard unavailable")
	h.ui.Key(0, ui.KeyEnter)
	h.settle()
	if h.closed != 0 || len(h.copied) != 0 {
		t.Fatal("clipboard failure closed the panel")
	}
	h.copyErr = nil
	h.ui.Key(0, ui.KeyEnter)
	h.settle()
	if h.closed != 1 || len(h.copied) != 1 || h.copied[0] != "回想起来" {
		t.Fatalf("Enter copied %q, closed %d", h.copied, h.closed)
	}
}

func TestHistoryBrowseWithArrowKeysAndCopyFirstTranslation(t *testing.T) {
	for _, focus := range []string{"empty search", "original row", "panel background"} {
		t.Run(focus, func(t *testing.T) {
			h := newHarness(t)
			h.put("older original", "首条译文\n第二行", "另一个源的译文")
			h.now = h.now.Add(time.Second)
			h.put("newer original", "最近的译文")
			h.open()

			// Navigation used to stop working once a click took focus out of search.
			switch focus {
			case "original row":
				h.click("newer original")
			case "panel background":
				h.ui.ClickAt(DefaultWidth*3/4, DefaultHeight*3/4)
			}
			if h.ui.Focused(searchLabel) != (focus == "empty search") {
				t.Fatal("unexpected search focus before keyboard navigation")
			}

			h.ui.Key(0, ui.KeyDown)
			h.settle()
			if !h.ui.HasText("另一个源的译文") {
				t.Fatalf("Down did not select the older original: %v", h.ui.Texts())
			}
			h.ui.Key(0, ui.KeyUp)
			h.settle()
			if !h.ui.HasText("最近的译文") {
				t.Fatalf("Up did not select the newer original: %v", h.ui.Texts())
			}
			h.ui.Key(0, ui.KeyDown)
			h.ui.Key(0, ui.KeyEnter)
			h.settle()
			if h.closed != 1 || !slices.Equal(h.copied, []string{"首条译文\n第二行"}) {
				t.Fatalf("Enter copied %q, closed %d windows", h.copied, h.closed)
			}
		})
	}
}

func TestHistoryProviderCopyDoubleClickAndEscape(t *testing.T) {
	h := newHarness(t)
	h.put("in retrospect", "回想起来", "回顾")
	h.open()
	h.click("复制 Google 译文")
	if len(h.copied) != 1 || h.copied[0] != "回顾" || h.closed != 0 {
		t.Fatalf("provider copy: %q, closed %d", h.copied, h.closed)
	}
	h.ui.Key(0, ui.KeyEnter)
	h.settle()
	if !slices.Equal(h.copied, []string{"回顾", "回顾"}) || h.closed != 0 {
		t.Fatalf("Enter overrode the focused provider action: %q, closed %d", h.copied, h.closed)
	}
	h.ui.Key(0, ui.KeyEscape)
	if h.closed != 1 || len(h.copied) != 2 {
		t.Fatalf("Escape wrote the clipboard: %q, closed %d", h.copied, h.closed)
	}
	h.view.Open()
	h.ui.Frame()
	h.settle()
	rect, ok := h.ui.Find("in retrospect")
	if !ok {
		t.Fatal("original row missing")
	}
	h.ui.ClickAt(rect.X+6, rect.Y+8)
	h.ui.ClickAt(rect.X+6, rect.Y+8)
	h.settle()
	if len(h.copied) != 3 || h.copied[2] != "回想起来" || h.closed != 1 {
		t.Fatalf("double-click copied %q, closed %d windows", h.copied, h.closed)
	}
}

func TestHistorySpeaksCurrentOriginalTranslationAndHeadword(t *testing.T) {
	h := newHarness(t)
	h.put("water", "水")
	request := h.store.Begin(history.Key{Text: "water", SourceLang: "en", TargetLang: "zh-HANS"}, []string{"youdaoweb"})
	if ok, err := h.store.Record(t.Context(), request, "youdaoweb", "water\n英 /ˈwɔːtə/  美 /ˈwɔːtər/\nn. 水", history.ResultDictionary); err != nil || !ok {
		t.Fatalf("dictionary fixture: %v, %v", ok, err)
	}
	request.Done()
	h.now = h.now.Add(time.Second)
	request = h.store.Begin(history.Key{Text: "こんにちは", SourceLang: "ja", TargetLang: "en"}, []string{"deepl"})
	if ok, err := h.store.Record(t.Context(), request, "deepl", "Hello", history.ResultTranslation); err != nil || !ok {
		t.Fatalf("translation fixture: %v, %v", ok, err)
	}
	request.Done()
	h.open()
	h.click("朗读原文")
	h.click("朗读译文 · DeepL")
	if err := h.ui.Click("water"); err != nil {
		t.Fatal(err)
	}
	// A click during the detail swap must not read the previously displayed result.
	if err := h.ui.Click("朗读译文 · DeepL"); err != nil {
		t.Fatal(err)
	}
	h.settle()
	h.click("朗读原文")
	h.click("朗读译文 · DeepL")
	h.click("朗读单词 · 有道查词与翻译")
	want := [][2]string{{"こんにちは", "ja"}, {"Hello", "en"}, {"water", "en"}, {"水", "zh-HANS"}, {"water", "en"}}
	if !slices.Equal(h.spoken, want) || h.closed != 0 || len(h.copied) != 0 {
		t.Fatalf("speech=%v, closed=%d, copied=%q", h.spoken, h.closed, h.copied)
	}
}

func TestHistoryIgnoresSpeechErrorsFromOldRequests(t *testing.T) {
	h := newHarness(t)
	h.put("first original", "首条译文")
	h.now = h.now.Add(time.Second)
	h.put("newer original", "另一条译文")
	h.open()
	h.click("朗读原文")
	h.click("first original")
	h.click("newer original")
	// An error from a previous selection must stay hidden even after returning to that row.
	h.speechFailures[0]()
	h.ui.Frame()
	if h.ui.HasText("朗读不可用") {
		t.Fatal("an old selection showed a speech error")
	}
	h.click("朗读原文")
	h.click("朗读译文 · DeepL")
	h.speechFailures[1]()
	h.ui.Frame()
	if h.ui.HasText("朗读不可用") {
		t.Fatal("a superseded request showed a speech error")
	}
	h.speechFailures[2]()
	h.ui.Frame()
	if !h.ui.HasText("朗读不可用") {
		t.Fatal("the current speech error is missing")
	}
	h.click("朗读原文")
	h.ui.Key(0, ui.KeyEscape)
	h.open()
	h.speechFailures[3]()
	h.ui.Frame()
	if h.ui.HasText("朗读不可用") {
		t.Fatal("a closed history window showed a speech error")
	}
}

func TestHistoryCloseButtonLeavesClipboardAlone(t *testing.T) {
	h := newHarness(t)
	h.put("in retrospect", "回想起来")
	h.open()
	h.ui.Type("retrospect")
	h.settle()
	h.click("关闭翻译历史")
	if h.closed != 1 || len(h.copied) != 0 {
		t.Fatalf("close button closed %d windows and copied %q", h.closed, h.copied)
	}
}

func TestHistoryDismissalCancelsPendingClipboardWrite(t *testing.T) {
	h := newHarness(t)
	h.put("in retrospect", "回想起来")
	h.open()
	h.ui.Key(0, ui.KeyEnter)
	h.one() // Prepare the copy, then lose focus before its clipboard callback.
	h.view.Dismiss()
	h.view.Dismiss()
	h.settle()
	if h.closed != 1 || len(h.copied) != 0 {
		t.Fatalf("dismissal closed %d windows and copied %q", h.closed, h.copied)
	}
}

func TestHistoryClearConfirmation(t *testing.T) {
	h := newHarness(t)
	h.put("one record", "一条记录")
	h.open()
	h.click("清空历史")
	if !h.ui.Focused("取消") {
		t.Fatal("opening destructive confirmation did not default focus to cancel")
	}
	h.ui.Key(0, ui.KeyEnter)
	h.settle()
	if n, err := h.store.Count(t.Context()); err != nil || n != 1 || h.closed != 0 {
		t.Fatalf("Enter on default focus changed data or closed history: n=%d closed=%d err=%v", n, h.closed, err)
	}
	h.click("清空历史")
	h.ui.Key(0, ui.KeyEscape)
	h.settle()
	if n, err := h.store.Count(t.Context()); err != nil || n != 1 || h.closed != 0 {
		t.Fatalf("Escape dismissed wrong layer or cleared data: n=%d closed=%d err=%v", n, h.closed, err)
	}
	if _, ok := h.ui.Find("确认清空"); ok {
		t.Fatal("Escape left confirmation modal open")
	}
	h.click("清空历史")
	h.click("取消")
	if n, err := h.store.Count(t.Context()); err != nil || n != 1 {
		t.Fatalf("cancel cleared records: %d, %v", n, err)
	}
	h.click("清空历史")
	h.click("确认清空")
	if n, err := h.store.Count(t.Context()); err != nil || n != 0 {
		t.Fatalf("confirmed deletion: %d, %v", n, err)
	}
	if _, ok := h.ui.Find("还没有翻译记录"); !ok {
		t.Fatalf("empty state missing: %v", h.ui.Texts())
	}
}

func TestHistoryDeletionDropsQueuedRetryFeedback(t *testing.T) {
	for _, clearAll := range []bool{false, true} {
		h := newHarness(t)
		h.put("old", "已有译文")
		h.open()
		id := h.view.selected
		h.onRetry = func(context.Context, history.Entry, string) error { return errors.New("离线") }
		h.click("换源翻译")
		if err := h.ui.Click("Google · 新增译文"); err != nil {
			t.Fatal(err)
		}
		h.one()
		late := h.post[0]
		h.post = nil
		if clearAll {
			h.click("清空历史")
			h.click("确认清空")
		} else {
			h.click("删除此条历史")
		}
		late()
		if _, exists := h.view.retries[id]; exists {
			t.Fatal("late callback restored deleted retry feedback")
		}
	}
}

func TestHistoryLateRetryStaysWithOriginal(t *testing.T) {
	h := newHarness(t)
	h.put("first original", "第一条")
	h.now = h.now.Add(time.Second)
	h.put("other original", "另一条")
	h.open()
	h.click("first original")
	h.click("换源翻译")
	h.onRetry = func(_ context.Context, e history.Entry, provider string) error {
		if e.Text != "first original" || provider != "google" {
			t.Fatalf("retry target: %+v, %s", e, provider)
		}
		return errors.New("离线测试")
	}
	if err := h.ui.Click("Google · 新增译文"); err != nil {
		t.Fatal(err)
	}
	h.one() // Finish the provider request but keep its UI callback queued.
	h.click("other original")
	if _, ok := h.ui.Find("离线测试"); ok {
		t.Fatal("late provider error appeared under the other original")
	}
	h.click("first original")
	if !h.ui.HasText("离线测试") {
		t.Fatal("the failure was lost after returning to the original entry")
	}
	h.click("复制 DeepL 译文")
	if len(h.copied) != 1 || h.copied[0] != "第一条" {
		t.Fatalf("old translation after offline retry: %q", h.copied)
	}
}

func TestHistoryMenuOffersOnlyEnabledProvidersAndLinksToSettings(t *testing.T) {
	h := newHarness(t)
	h.providers = []string{"google"}
	h.put("original", "保存的译文")
	h.open()
	h.click("换源翻译")
	if _, ok := h.ui.Find("Google · 新增译文"); !ok {
		t.Fatal("enabled provider is missing")
	}
	if _, ok := h.ui.Find("DeepL · 更新译文"); ok {
		t.Fatal("a disabled provider with saved history was offered")
	}
	if _, ok := h.ui.Find("重译 DeepL 译文"); ok {
		t.Fatal("a disabled provider can still be rerun")
	}
	h.ui.Key(0, ui.KeyEscape)
	if h.closed != 0 {
		t.Fatal("dismissing the menu closed history")
	}
	var chosen string
	h.onRetry = func(_ context.Context, _ history.Entry, provider string) error {
		chosen = provider
		return errors.New("离线测试")
	}
	h.click("换源翻译")
	h.ui.Key(0, ui.KeyDown)
	h.ui.Key(0, ui.KeyEnter)
	h.settle()
	if chosen != "google" {
		t.Fatalf("keyboard chose %q, want Google", chosen)
	}
	h.providers = nil
	h.ui.Frame()
	h.click("启用翻译源")
	if h.openedServices != 1 || !h.ui.HasText("保存的译文") {
		t.Fatalf("empty provider options did not offer the settings action: %v", h.ui.Texts())
	}
}

func TestHistoryRetryShowsProviderAndCanRetryFailure(t *testing.T) {
	h := newHarness(t)
	h.put("first original", "已有译文")
	h.open()
	attempts := 0
	h.onRetry = func(ctx context.Context, entry history.Entry, provider string) error {
		attempts++
		if provider != "google" {
			t.Fatalf("unexpected translation provider: %s", provider)
		}
		if attempts == 1 {
			return errors.New("网络暂时不可用")
		}
		request := h.store.Begin(entry.Key, []string{"deepl", "google"})
		defer request.Done()
		if saved, err := h.store.Record(ctx, request, provider, "新译文", history.ResultTranslation); err != nil || !saved {
			t.Fatalf("save fixture retry: %v, %v", saved, err)
		}
		return nil
	}
	h.click("换源翻译")
	if err := h.ui.Click("Google · 新增译文"); err != nil {
		t.Fatal(err)
	}
	if !h.ui.HasText("正在用 Google 翻译…") || !h.ui.HasText("已有译文") {
		t.Fatalf("provider progress or previous translation missing: %v", h.ui.Texts())
	}
	h.settle()
	if !h.ui.HasText("网络暂时不可用") || !h.ui.HasText("已有译文") {
		t.Fatalf("provider error or previous translation missing: %v", h.ui.Texts())
	}
	h.click("重试 Google 翻译")
	if attempts != 2 || !h.ui.HasText("新译文") || !h.ui.HasText("已有译文") {
		t.Fatalf("retry results: attempts=%d, %v", attempts, h.ui.Texts())
	}
	h.click("换源翻译")
	for _, label := range []string{"DeepL · 更新译文", "Google · 更新译文", "Google · 新增译文"} {
		if _, ok := h.ui.Find(label); ok {
			t.Fatalf("the menu still offers %q after saving translations", label)
		}
	}
	if _, ok := h.ui.Find("有道 · 新增译文"); !ok {
		t.Fatal("the remaining provider is missing from the menu")
	}
}

func TestHistoryRetranslatesOnlyChosenSavedProvider(t *testing.T) {
	h := newHarness(t)
	h.providers = []string{"deepl", "google"}
	h.put("original", "已有译文", "另一条译文")
	h.open()
	// Providers with existing results use per-result actions, even when the menu has no options.
	if _, ok := h.ui.Find("换源翻译"); ok {
		t.Fatal("the menu is still available when all enabled providers have translations")
	}
	h.onRetry = func(ctx context.Context, entry history.Entry, provider string) error {
		if entry.Text != "original" || provider != "deepl" {
			t.Fatalf("retranslation target: %+v, %s", entry, provider)
		}
		request := h.store.Begin(entry.Key, h.providers)
		defer request.Done()
		if saved, err := h.store.Record(ctx, request, provider, "更新后的译文", history.ResultTranslation); err != nil || !saved {
			t.Fatalf("save retranslation: %v, %v", saved, err)
		}
		return nil
	}
	if err := h.ui.Click("重译 DeepL 译文"); err != nil {
		t.Fatal(err)
	}
	if !h.ui.HasText("正在用 DeepL 翻译…") || !h.ui.HasText("已有译文") {
		t.Fatalf("pending retranslation hid the existing result: %v", h.ui.Texts())
	}
	h.settle()
	h.click("复制 DeepL 译文")
	h.click("复制 Google 译文")
	if !slices.Equal(h.copied, []string{"更新后的译文", "另一条译文"}) {
		t.Fatalf("retranslation affected the wrong results: %q", h.copied)
	}
}

func TestHistoryRetryFinishesAfterWindowDismissal(t *testing.T) {
	h := newHarness(t)
	h.put("old original", "已有译文")
	h.open()
	h.onRetry = func(ctx context.Context, entry history.Entry, provider string) error {
		if err := ctx.Err(); err != nil {
			t.Fatalf("closing the window canceled a dispatched translation: %v", err)
		}
		request := h.store.Begin(entry.Key, []string{"deepl", "google"})
		defer request.Done()
		if saved, err := h.store.Record(ctx, request, provider, "后台译文", history.ResultTranslation); err != nil || !saved {
			t.Fatalf("save after dismissal: %v, %v", saved, err)
		}
		return nil
	}
	h.click("换源翻译")
	if err := h.ui.Click("Google · 新增译文"); err != nil {
		t.Fatal(err)
	}
	h.view.Dismiss()
	h.settle()
	if h.closed != 1 || len(h.copied) != 0 {
		t.Fatalf("background completion changed the dismissed view: closed=%d, clipboard=%q", h.closed, h.copied)
	}
	h.open()
	if !h.ui.HasText("后台译文") {
		t.Fatalf("background result was missing after reopening: %v", h.ui.Texts())
	}
}

func TestHistoryRetryKeepsUnsavedTranslationAfterFailure(t *testing.T) {
	h := newHarness(t)
	h.put("old original", "已有译文")
	h.view.actions.Retry = func(_ history.Entry, provider string) (RetryResult, error) {
		if provider == "google" {
			return RetryResult{Text: "临时译文", Type: history.ResultTranslation, Notice: "本次译文未写入翻译历史"}, nil
		}
		return RetryResult{}, errors.New("服务暂时不可用")
	}
	h.open()
	h.click("换源翻译")
	h.click("Google · 新增译文")
	h.click("换源翻译")
	if _, ok := h.ui.Find("Google · 新增译文"); ok {
		t.Fatal("the menu still offers a provider with a visible unsaved translation")
	}
	h.click("有道 · 新增译文")
	h.click("朗读译文 · Google（未保存）")
	if !slices.Equal(h.spoken, [][2]string{{"临时译文", "zh-HANS"}}) {
		t.Fatalf("unsaved result speech: %v", h.spoken)
	}
	h.click("复制未保存的译文")
	if !slices.Equal(h.copied, []string{"临时译文"}) || !h.ui.HasText("Google · 本次译文（未保存）") {
		t.Fatalf("failed retry lost the previous unsaved translation: clipboard=%q, %v", h.copied, h.ui.Texts())
	}
}

func TestHistoryDropsInFlightPageAfterReorder(t *testing.T) {
	h := newHarness(t)
	for i := 0; i <= pageSize; i++ {
		h.put(fmt.Sprintf("original %02d", i), fmt.Sprintf("译文 %02d", i))
		h.now = h.now.Add(time.Second)
	}
	h.open()
	h.ui.Scroll(140, 305, 0, 3000)
	h.one() // The next page was read at the old revision, with its callback still queued.
	h.put("original 00", "刚刚重译")
	h.view.Refresh()
	h.settle()
	if len(h.view.entries) != pageSize || h.view.entries[0].Text != "original 00" {
		t.Fatalf("stale page survived refresh: %+v", h.view.entries)
	}
	h.ui.Key(0, ui.KeyEnter)
	h.settle()
	if len(h.copied) != 1 || h.copied[0] != fmt.Sprintf("译文 %02d", pageSize) {
		t.Fatalf("refresh copied a different ID: %q", h.copied)
	}
	h.view.Open()
	h.ui.Frame()
	h.settle()
	h.ui.Type("original 00")
	h.settle()
	h.ui.Key(0, ui.KeyEnter)
	h.settle()
	if len(h.copied) != 2 || h.copied[1] != "刚刚重译" {
		t.Fatalf("refreshed query copied %q", h.copied)
	}
}

func TestHistoryCopyDoesNotUseDeletedSelection(t *testing.T) {
	h := newHarness(t)
	h.put("old", "旧译文")
	h.open()
	h.ui.Key(0, ui.KeyEnter)
	h.one() // The old translation was read but has not reached the clipboard.
	if err := h.store.Clear(t.Context()); err != nil {
		t.Fatal(err)
	}
	h.put("new", "新译文")
	h.view.Refresh()
	h.settle()
	if h.closed != 0 || len(h.copied) != 0 {
		t.Fatalf("stale Enter copied %q and closed %d windows", h.copied, h.closed)
	}
	h.ui.Key(0, ui.KeyEnter)
	h.settle()
	if h.closed != 1 || len(h.copied) != 1 || h.copied[0] != "新译文" {
		t.Fatalf("fresh Enter copied %q and closed %d windows", h.copied, h.closed)
	}
}

func TestHistoryCopySurvivesUnrelatedInsert(t *testing.T) {
	h := newHarness(t)
	h.put("chosen", "first result")
	h.open()
	h.ui.Key(0, ui.KeyEnter)
	h.one()
	h.put("other", "unrelated result")
	// An insertion between the read and the clipboard callback must not force a second read.
	for _, post := range h.post {
		post()
	}
	if !slices.Equal(h.copied, []string{"first result"}) || len(h.work) != 0 {
		t.Fatalf("copied %v, pending jobs %d", h.copied, len(h.work))
	}
}

func TestHistoryDebounceSkipsIntermediateQueries(t *testing.T) {
	h := newHarness(t)
	h.put("apple", "苹果")
	h.open()
	h.ui.Type("a")
	h.ui.Advance(60 * time.Millisecond)
	h.ui.Type("p")
	h.ui.Advance(60 * time.Millisecond)
	h.ui.Key(0, ui.KeyEnter)
	if len(h.work) != 0 || h.closed != 0 {
		t.Fatal("an intermediate query or stale copy was dispatched")
	}
	h.ui.Advance(searchDelay)
	if len(h.work) != 1 {
		t.Fatalf("expected one debounced query, got %d", len(h.work))
	}
	h.settle()
	if h.view.query != "ap" || !h.ui.HasText("苹果") {
		t.Fatalf("last search not published: %v", h.ui.Texts())
	}
}

func TestHistorySameOriginalDifferentDirections(t *testing.T) {
	h := newHarness(t)
	h.put("apple", "苹果")
	r := h.store.Begin(history.Key{Text: "apple", SourceLang: "en", TargetLang: "ja"}, []string{"deepl"})
	defer r.Done()
	if ok, err := h.store.Record(t.Context(), r, "deepl", "りんご", history.ResultTranslation); err != nil || !ok {
		t.Fatal(err)
	}
	h.open()
	// Rows with identical labels must still navigate by entry ID.
	if !h.ui.HasText("りんご") {
		t.Fatal("newest translation missing")
	}
	h.ui.Key(0, ui.KeyDown)
	h.settle()
	if !h.ui.HasText("苹果") {
		t.Fatal("navigation skipped the other translation")
	}
}

func TestHistorySearchAcrossCollapsedDays(t *testing.T) {
	h := newHarness(t)
	h.now = h.now.AddDate(0, 0, -3)
	h.put("old original", "过往译文")
	h.now = h.now.AddDate(0, 0, 1)
	h.put("recent original", "近期译文")
	h.now = h.now.AddDate(0, 0, 2)
	h.open()
	if _, ok := h.ui.Find("old original"); ok {
		t.Fatal("older group starts expanded")
	}
	h.ui.Type("过往")
	h.settle()
	if _, ok := h.ui.Find("old original"); !ok {
		t.Fatal("query skipped the collapsed group")
	}
	h.ui.Key(ui.Cmd, ui.KeyA)
	h.ui.Type("no match")
	h.settle()
	h.ui.Key(0, ui.KeyEnter)
	h.settle()
	if h.closed != 0 || len(h.copied) != 0 || !h.ui.HasText("没找到匹配的记录") {
		t.Fatalf("no-match state: %v, clipboard %q, closed %d", h.ui.Texts(), h.copied, h.closed)
	}
}

func TestHistoryRetryOpensDatabaseAfterDirectoryRecovers(t *testing.T) {
	// A failed startup open used to leave Retry querying a nil connection until restart.
	dir := t.TempDir()
	available := false
	h := newHarnessWithDirectory(t, func() (string, error) {
		if !available {
			return "", os.ErrPermission
		}
		return dir, nil
	})
	if err := h.view.actions.OpenStore(t.Context()); err == nil {
		t.Fatal("expected startup to fail")
	}
	h.open()
	if !h.ui.HasText("无法读取翻译历史") {
		t.Fatal("missing load error")
	}
	h.click("重试")
	available = true
	h.click("重试")
	if !h.ui.HasText("还没有翻译记录") {
		t.Fatalf("retry did not reopen the database: %v", h.ui.Texts())
	}
	h.put("after recovery", "恢复后的译文")
	h.view.Refresh()
	h.settle()
	h.ui.Key(0, ui.KeyEnter)
	h.settle()
	if !slices.Equal(h.copied, []string{"恢复后的译文"}) {
		t.Fatalf("recording after recovery: %v", h.copied)
	}
}

func TestHistoryEmptyStatesKeepSearchUsable(t *testing.T) {
	h := newHarness(t)
	h.open()
	if !h.ui.HasText("还没有翻译记录") || !h.ui.Focused(searchLabel) {
		t.Fatalf("empty history did not show its message and focus search: %v", h.ui.Texts())
	}
	h.put("a working hypothesis", "一个工作假设")
	h.view.Refresh()
	h.settle()
	h.ui.Type("missing phrase")
	h.settle()
	if !h.ui.HasText("没找到匹配的记录") || !h.ui.Focused(searchLabel) {
		t.Fatalf("empty search did not show its message and retain focus: %v", h.ui.Texts())
	}
	h.ui.Key(ui.Cmd, ui.KeyA)
	h.ui.Type("hypothesis")
	h.settle()
	if !h.ui.HasText("一个工作假设") {
		t.Fatalf("editing the query did not recover results: %v", h.ui.Texts())
	}
}

func TestHistorySearchReplacesListAndDetailTogether(t *testing.T) {
	h := newHarness(t)
	h.put("needle beta", "第二个匹配的译文")
	h.put("needle alpha", "第一个匹配的译文")
	h.put("old original", "正在查看的译文")
	h.open()
	h.ui.Type("needle")
	if !h.ui.HasText("正在查看的译文") || h.ui.HasText("正在读取…") {
		t.Fatalf("typing replaced the current view before the query finished: %v", h.ui.Texts())
	}
	h.ui.Advance(searchDelay)
	h.one()
	h.ui.Type(" beta")
	publish := func() {
		posted := h.post
		h.post = nil
		for _, f := range posted {
			f()
		}
		h.ui.Frame()
	}
	publish() // The superseded search finished its reads before the next edit.
	if !h.ui.HasText("正在查看的译文") || h.ui.HasText("第一个匹配的译文") {
		t.Fatalf("the superseded query changed the view: %v", h.ui.Texts())
	}
	h.ui.Key(0, ui.KeyEnter)
	if len(h.work) != 0 || h.closed != 0 || len(h.copied) != 0 {
		t.Fatal("Enter started copying a stale search result during debounce")
	}
	h.ui.Advance(searchDelay)
	h.one()
	publish()
	if !h.ui.HasText("第二个匹配的译文") || h.ui.HasText("正在查看的译文") ||
		!h.ui.Focused(searchLabel) {
		t.Fatalf("the new list and detail did not appear together: %v", h.ui.Texts())
	}
}

func TestHistorySelectionKeepsDetailUntilReplacementIsReady(t *testing.T) {
	h := newHarness(t)
	h.put("older original", "旧的一条译文")
	h.put("middle original", "中间一条译文")
	h.put("newest original", "刚刚翻译的内容")
	h.open()
	h.ui.Key(0, ui.KeyDown)
	h.ui.Key(0, ui.KeyDown)
	if !h.ui.HasText("刚刚翻译的内容") {
		t.Fatalf("selection cleared the detail while it was loading: %v", h.ui.Texts())
	}
	h.settle()
	if !h.ui.HasText("旧的一条译文") || h.ui.HasText("刚刚翻译的内容") ||
		!h.ui.Focused(searchLabel) {
		t.Fatalf("rapid navigation did not finish on the last selection: %v", h.ui.Texts())
	}
	if err := h.ui.Click("newest original"); err != nil {
		t.Fatal(err)
	}
	if !h.ui.HasText("旧的一条译文") {
		t.Fatal("clicking a row blanked the preceding detail")
	}
	h.settle()
	if !h.ui.HasText("刚刚翻译的内容") {
		t.Fatal("clicking a row did not replace the detail")
	}
}

func TestHistoryResultTypeControlsFormattingWithoutChangingText(t *testing.T) {
	const text = "Memo\n/ˈmeməʊ/\nn. 备忘录\n示例：write a memo"
	p := palette{mark: ui.Hex("#d4e6fe")}
	// Identical multiline text can be a translation or a dictionary entry.
	for _, resultType := range []history.ResultType{history.ResultTranslation, history.ResultDictionary} {
		spans := resultSpans(resultType, text, "MEMO", p)
		var displayed strings.Builder
		var matches []string
		for _, span := range spans {
			displayed.WriteString(span.Text)
			if span.Background == p.mark {
				matches = append(matches, span.Text)
			}
		}
		if displayed.String() != text || !slices.Equal(matches, []string{"Memo", "memo"}) {
			t.Fatalf("type %d: display=%q, matches=%q", resultType, displayed.String(), matches)
		}
		if bold := spans[0].Weight == 600; bold != (resultType == history.ResultDictionary) {
			t.Fatalf("type %d: headword bold=%v", resultType, bold)
		}
	}
}

func TestHistoryFindsAndCopiesBothDictionaryAccents(t *testing.T) {
	h := newHarness(t)
	h.put("another entry", "其他条目")
	const provider = "youdaoweb"
	const british = "英 /ˈwɔːtə/"
	const definition = "water\n" + british + "  美 /ˈwɔːtər/\nn. 水"
	request := h.store.Begin(history.Key{Text: "water", SourceLang: "en", TargetLang: "zh-HANS"}, []string{provider})
	defer request.Done()
	if ok, err := h.store.Record(t.Context(), request, provider, definition, history.ResultDictionary); err != nil || !ok {
		t.Fatalf("dictionary fixture: %v, %v", ok, err)
	}
	h.open()
	// The British pronunciation was previously lost before the entry could be searched or copied.
	h.ui.Type(british)
	h.settle()
	if _, ok := h.ui.Find("another entry"); ok || !h.ui.HasText(definition) {
		t.Fatalf("search did not show the complete matching entry: %v", h.ui.Texts())
	}
	h.ui.Key(0, ui.KeyEnter)
	h.settle()
	if h.closed != 1 || !slices.Equal(h.copied, []string{definition}) {
		t.Fatalf("Enter copied %q and closed %d windows", h.copied, h.closed)
	}
}

func TestHistoryCopyOriginalNamingChipsAndToastFeedback(t *testing.T) {
	h := newHarness(t)
	r := h.store.Begin(history.Key{Text: "用户名称", SourceLang: "zh-HANS", TargetLang: "en"}, []string{"deepl", "google"})
	if ok, err := h.store.Record(t.Context(), r, "deepl", "user name", history.ResultTranslation); err != nil || !ok {
		t.Fatalf("seed deepl: %v, %v", ok, err)
	}
	if ok, err := h.store.Record(t.Context(), r, "google", "account name", history.ResultTranslation); err != nil || !ok {
		t.Fatalf("seed google: %v, %v", ok, err)
	}
	r.Done()

	h.open()
	h.click("复制原文")
	if !slices.Equal(h.copied, []string{"用户名称"}) || h.closed != 0 {
		t.Fatalf("copy original: copied=%q, closed=%d", h.copied, h.closed)
	}
	if !h.ui.HasText("已复制原文") || h.view.copiedKey != "source" {
		t.Fatalf("copy original feedback missing: key=%q, texts=%v", h.view.copiedKey, h.ui.Texts())
	}
	h.ui.Advance(1600 * time.Millisecond)
	h.ui.Frame()
	if h.view.copiedKey != "" {
		t.Fatalf("inline copy indicator did not expire: %q", h.view.copiedKey)
	}

	h.click("下划线：user_name")
	if !slices.Equal(h.copied, []string{"用户名称", "user_name"}) || !h.ui.HasText("已复制 下划线") {
		t.Fatalf("naming chip copy: copied=%q, texts=%v", h.copied, h.ui.Texts())
	}

	// Respect configured CopyFormats when provided.
	h.view.actions.Options = func() ([]string, []string) { return h.providers, []string{naming.Kebab} }
	h.ui.Frame()
	if _, ok := h.ui.Find("下划线：user_name"); ok {
		t.Fatal("disabled naming format chip is still visible")
	}
	if _, ok := h.ui.Find("短横线：user-name"); !ok {
		t.Fatal("configured naming format chip missing")
	}

	// Dismissing and reopening clears previous toast state.
	h.view.Dismiss()
	h.open()
	if h.ui.HasText("已复制 下划线") {
		t.Fatalf("stale toast persisted across reopen: %v", h.ui.Texts())
	}
}

func TestHistoryCollapseCardsAndClearSearch(t *testing.T) {
	h := newHarness(t)
	h.put("older phrase", "旧译文")
	h.now = h.now.Add(time.Second)
	h.put("in retrospect", "回想起来", "回顾")
	h.open()

	h.click("折叠 DeepL")
	if h.ui.HasText("回想起来") || !h.ui.HasText("回顾") {
		t.Fatalf("collapsing DeepL did not hide only DeepL body: %v", h.ui.Texts())
	}
	h.click("折叠 DeepL")
	if !h.ui.HasText("回想起来") {
		t.Fatalf("expanding DeepL did not restore body: %v", h.ui.Texts())
	}

	// Search clear button clears query, restores entries, and keeps focus in search.
	h.click(searchLabel)
	h.ui.Type("retrospect")
	h.settle()
	if _, ok := h.ui.Find("older phrase"); ok {
		t.Fatal("filter did not hide non-matching entry")
	}
	h.click("清空搜索")
	if _, ok := h.ui.Find("older phrase"); !ok || !h.ui.Focused(searchLabel) {
		t.Fatalf("clearing search did not restore list and focus: focused=%v, texts=%v", h.ui.Focused(searchLabel), h.ui.Texts())
	}
}

func TestHistoryNavigateAfterSecondPageRefresh(t *testing.T) {
	h := newHarness(t)
	for i := range pageSize + 10 {
		h.put(fmt.Sprintf("row %02d", i), fmt.Sprint(i))
		h.now = h.now.Add(time.Second)
	}
	h.open()
	for range pageSize + 2 {
		h.ui.Key(0, ui.KeyDown)
		h.settle()
	}
	// A background result used to drop the selected row but keep its detail.
	h.put("new", "new")
	h.view.Refresh()
	h.settle()
	h.ui.Key(0, ui.KeyDown)
	h.settle()
	h.ui.Key(0, ui.KeyEnter)
	h.settle()
	if !slices.Equal(h.copied, []string{"6"}) {
		t.Fatalf("next row after refresh: %v", h.copied)
	}
}

func TestHistoryCountDoesNotBlockOrOverwriteSearch(t *testing.T) {
	h := newHarness(t)
	for i := range pageSize + 1 {
		h.put(fmt.Sprintf("row %02d", i), "result")
	}
	h.view.Open()
	h.ui = ui.NewTester(h.view.Render, DefaultWidth, DefaultHeight)
	h.one()
	for _, post := range h.post {
		post()
	}
	h.post = nil
	h.ui.Frame()
	if !h.ui.HasText("已加载 30+ 条") {
		t.Fatalf("first page waited for counts: %v", h.ui.Texts())
	}
	h.one() // Hold the completed count callback while the search changes.
	h.ui.Type("row 01")
	h.settle()
	if !h.ui.HasText("1 条记录") || h.view.hasGroupCounts {
		t.Fatalf("obsolete count survived search: %v", h.ui.Texts())
	}
}

func TestHistoryPagedGroupCountsStayAccurate(t *testing.T) {
	h := newHarness(t)
	h.now = h.now.AddDate(0, 0, -5)
	for i := range 4 {
		h.put(fmt.Sprintf("older %02d", i), fmt.Sprintf("更早译文 %02d", i))
		h.now = h.now.Add(time.Second)
	}
	h.now = h.now.AddDate(0, 0, 5)
	for i := range pageSize {
		h.put(fmt.Sprintf("today %02d", i), fmt.Sprintf("今天译文 %02d", i))
		h.now = h.now.Add(time.Second)
	}
	h.open()
	total := pageSize + 4
	wantFooter := fmt.Sprintf("已加载 %d / 共 %d 条", pageSize, total)
	if !h.ui.HasText(wantFooter) {
		t.Fatalf("footer count missing %q: %v", wantFooter, h.ui.Texts())
	}
	if !h.ui.HasText(fmt.Sprint(pageSize)) {
		t.Fatalf("today group count %d missing: %v", pageSize, h.ui.Texts())
	}
}

func TestHistoryDateRolloverRecountsPage(t *testing.T) {
	h := newHarness(t)
	for i := range pageSize + 2 {
		h.put(fmt.Sprintf("today %d", i), "result")
	}
	h.open()
	if !h.ui.HasText("今天") {
		t.Fatal("missing today's entries")
	}
	h.now = h.now.AddDate(0, 0, 1)
	h.ui.Frame()
	h.settle()
	if h.ui.HasText("今天") || !h.ui.HasText("近 3 天") || !h.ui.HasText(fmt.Sprint(pageSize+2)) {
		t.Fatalf("stale counts after midnight: %v", h.ui.Texts())
	}
}

func TestHistoryDictionaryProviderRequiresSingleEnglishWord(t *testing.T) {
	h := newHarness(t)
	h.providers = []string{"google", "dictionary"}
	h.put("hello world", "已有译文", "你好世界")
	h.now = h.now.Add(time.Second)
	for _, item := range []struct {
		text, source, target, translated string
	}{
		{"用户名称", "zh-HANS", "en", "user name"},
		{"chat", "fr", "zh-HANS", "猫"},
		{"memo", "en", "zh-HANS", "备忘录"},
	} {
		r := h.store.Begin(history.Key{Text: item.text, SourceLang: item.source, TargetLang: item.target}, []string{"deepl"})
		if ok, err := h.store.Record(t.Context(), r, "deepl", item.translated, history.ResultTranslation); err != nil || !ok {
			t.Fatalf("seed %s: %v, %v", item.text, ok, err)
		}
		r.Done()
		h.now = h.now.Add(time.Second)
	}
	h.open()

	// Single English word offers the dictionary option.
	h.click("换源翻译")
	if _, ok := h.ui.Find("词典 · 新增译文"); !ok {
		t.Fatal("dictionary option missing for single English word")
	}
	h.ui.Key(0, ui.KeyEscape)

	// French single word and Chinese text omit dictionary while keeping Google.
	for _, label := range []string{"chat", "用户名称"} {
		h.click(label)
		h.click("换源翻译")
		if _, ok := h.ui.Find("词典 · 新增译文"); ok {
			t.Fatalf("dictionary option offered for %q", label)
		}
		if _, ok := h.ui.Find("Google · 新增译文"); !ok {
			t.Fatalf("Google option missing for %q", label)
		}
		h.ui.Key(0, ui.KeyEscape)
	}

	// Phrase with Google already saved hides the menu entirely instead of offering only dictionary.
	h.click("hello world")
	if _, ok := h.ui.Find("换源翻译"); ok {
		t.Fatal("menu remained visible when only ineligible dictionary was left")
	}
}

func TestHistoryRetryCardKeepsLayoutStableWhileRetrying(t *testing.T) {
	h := newHarness(t)
	h.put("first original", "已有译文")
	h.open()
	h.onRetry = func(context.Context, history.Entry, string) error {
		return errors.New("网络暂时不可用，请检查当前网络或代理设置后重试")
	}
	h.click("换源翻译")
	h.click("Google · 新增译文")
	beforeRect, ok := h.ui.Find("已有译文")
	if !ok {
		t.Fatal("existing translation missing")
	}
	if err := h.ui.Click("重试 Google 翻译"); err != nil {
		t.Fatal(err)
	}
	duringRect, ok := h.ui.Find("已有译文")
	if !ok || !h.ui.HasText("Google 重试中…") || duringRect.Y != beforeRect.Y {
		t.Fatalf("retrying jumped layout: before=%+v during=%+v texts=%v", beforeRect, duringRect, h.ui.Texts())
	}
	// Clicking the disabled retry button while pending must not queue another attempt.
	if err := h.ui.Click("重试 Google 翻译"); err != nil {
		t.Fatal(err)
	}
	if len(h.work) != 1 {
		t.Fatalf("pending retry queued %d jobs, want 1", len(h.work))
	}
	h.settle()
	afterRect, ok := h.ui.Find("已有译文")
	if !ok || afterRect.Y != beforeRect.Y {
		t.Fatalf("failed retry shifted layout: before=%+v after=%+v", beforeRect, afterRect)
	}
}

func TestHistoryLongSourceCollapsesAndAutoExpandsOnHiddenSearchMatch(t *testing.T) {
	h := newHarness(t)
	h.put("short source", "短译文")
	h.now = h.now.Add(time.Second)

	var lines []string
	lines = append(lines, "earlyNeedle on first line")
	for i := 2; i <= 9; i++ {
		lines = append(lines, fmt.Sprintf("middle filler line %d with enough words to stay readable", i))
	}
	lines = append(lines, "deepNeedle hidden past preview")
	longSource := strings.Join(lines, "\n")
	h.put(longSource, "长原文的译文")
	h.open()

	// Long source starts collapsed while copy still writes the full text.
	if _, ok := h.ui.Find("展开全文"); !ok {
		t.Fatalf("long source did not show expand control: %v", h.ui.Texts())
	}
	transBefore, ok := h.ui.Find("译文")
	if !ok {
		t.Fatal("translation heading missing")
	}
	h.click("复制原文")
	if !slices.Equal(h.copied, []string{longSource}) {
		t.Fatalf("copy original while collapsed = %q, want full source", h.copied)
	}

	// The label changes on expand, but the same control must keep keyboard focus.
	for i := 0; i < 20 && !h.ui.Focused("展开全文"); i++ {
		h.ui.Key(0, ui.KeyTab)
	}
	if !h.ui.Focused("展开全文") {
		t.Fatal("could not reach the expand control with Tab")
	}
	h.ui.Key(0, ui.KeyEnter)
	if !h.ui.Focused("收起原文") {
		t.Fatal("expanding long source lost focus on the toggle")
	}
	transExpanded, ok := h.ui.Find("译文")
	if !ok || transExpanded.Y <= transBefore.Y {
		t.Fatalf("expanding long source did not push translation down: before=%+v expanded=%+v", transBefore, transExpanded)
	}
	h.ui.Key(0, ui.KeyEnter)
	if !h.ui.Focused("展开全文") {
		t.Fatal("pressing Enter again did not collapse the source and retain focus")
	}
	transRestored, ok := h.ui.Find("译文")
	if !ok || transRestored.Y != transBefore.Y {
		t.Fatalf("collapsing long source did not restore layout: before=%+v restored=%+v", transBefore, transRestored)
	}

	// Short source omits the expand/collapse toggle.
	h.click("short source")
	if _, ok := h.ui.Find("展开全文"); ok {
		t.Fatal("short source unexpectedly showed expand control")
	}
	if _, ok := h.ui.Find("收起原文"); ok {
		t.Fatal("short source unexpectedly showed collapse control")
	}

	// Search match within the visible preview keeps source collapsed.
	h.click(searchLabel)
	h.ui.Type("earlyNeedle")
	h.settle()
	if _, ok := h.ui.Find("展开全文"); !ok {
		t.Fatal("visible match unnecessarily expanded long source")
	}

	// Search match past the preview auto-expands source, while manual collapse stays respected.
	h.click("清空搜索")
	h.ui.Type("deepNeedle")
	h.settle()
	if _, ok := h.ui.Find("收起原文"); !ok {
		t.Fatal("hidden search match did not auto-expand long source")
	}
	h.click("收起原文")
	if _, ok := h.ui.Find("展开全文"); !ok {
		t.Fatal("manual collapse after search auto-expand was overridden")
	}
}

func TestHistorySourceDisclosureUpdatesOnResize(t *testing.T) {
	h := newHarness(t)
	const wideWidth = 960
	h.put(strings.TrimSpace(strings.Repeat("one two three four five six seven eight nine ten ", 8)), "译文")
	h.open()
	h.ui.SetSize(wideWidth, DefaultHeight)
	if h.ui.HasText("展开全文") {
		t.Fatal("source should fit at the wider width")
	}

	// Measuring the previous frame's width left the disclosure stale until another redraw.
	h.ui.SetSize(560, DefaultHeight)
	if !h.ui.HasText("展开全文") {
		t.Fatal("narrowing the window did not immediately reveal the disclosure")
	}
	h.ui.SetSize(wideWidth, DefaultHeight)
	if h.ui.HasText("展开全文") {
		t.Fatal("restoring the window width did not immediately hide the disclosure")
	}
}

func TestHistoryLongResultCollapsesSecondaryProviders(t *testing.T) {
	h := newHarness(t)
	longFirst := strings.Repeat("这是第一条非常长的译文内容，用来测试单源超长时其他翻译源的默认折叠行为。\n", 12) + "firstTail"
	longSecond := strings.Repeat("这是第二条也很长的译文内容。\n", 12) + "googleMatch"
	shortThird := "第三条简短译文"
	h.put("long result entry", longFirst, longSecond, shortThird)
	h.open()

	// A long result keeps only the first provider expanded by default.
	if !h.ui.HasText(longFirst) {
		t.Fatalf("first long translation was hidden: %v", h.ui.Texts())
	}
	if h.ui.HasText(longSecond) || h.ui.HasText(shortThird) {
		t.Fatalf("secondary providers were not collapsed by default: %v", h.ui.Texts())
	}
	h.ui.Scroll(500, 250, 0, 800)
	h.click("复制 Google 译文")
	if !slices.Equal(h.copied, []string{longSecond}) {
		t.Fatalf("copying collapsed secondary provider copied %q", h.copied)
	}
	h.click("折叠 Google")
	if !h.ui.HasText(longSecond) {
		t.Fatal("clicking collapsed secondary provider did not expand it")
	}

	// Searching a term in a secondary provider keeps that matching provider expanded.
	h.click(searchLabel)
	h.ui.Type("googleMatch")
	h.settle()
	if !h.ui.HasText(longSecond) || h.ui.HasText(shortThird) {
		t.Fatalf("search did not expand matching secondary provider only: %v", h.ui.Texts())
	}
}

func TestHistoryResultCollapseDefaultsFollowRefresh(t *testing.T) {
	h := newHarness(t)
	const source = "original text"
	const secondary = "secondary translation"
	h.put(source, "short translation", secondary)
	h.open()
	if !h.ui.HasText(secondary) {
		t.Fatal("secondary translation should start expanded when all results are short")
	}

	refresh := func(first string) {
		h.now = h.now.Add(time.Second)
		h.put(source, first, secondary)
		h.view.Refresh()
		h.settle()
	}
	longFirst := strings.Repeat("a longer translation\n", 11)
	// A refresh of the same entry used to keep the defaults from its initial render.
	refresh(longFirst + "first ending")
	if h.ui.HasText(secondary) {
		t.Fatal("secondary translation stayed expanded after the first result became long")
	}
	h.ui.Scroll(500, 250, 0, 800)
	h.click("折叠 Google")
	if !h.ui.HasText(secondary) {
		t.Fatal("could not manually expand the secondary translation")
	}
	refresh(longFirst + "new ending")
	if !h.ui.HasText(secondary) {
		t.Fatal("refresh overrode the manually expanded secondary translation")
	}
}

func TestHistoryNamingChipsSkipLongOrMultilineTranslations(t *testing.T) {
	for name, text := range map[string]string{
		"multiline":  "user\nname",
		"byte limit": strings.Repeat("é", 61),
		"word limit": "one two three four five six seven eight nine",
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			req := h.store.Begin(history.Key{Text: "中文输入", SourceLang: "zh-HANS", TargetLang: "en"}, []string{"deepl"})
			defer req.Done()
			if ok, err := h.store.Record(t.Context(), req, "deepl", text, history.ResultTranslation); err != nil || !ok {
				t.Fatalf("seed English translation: %v, %v", ok, err)
			}
			h.open()
			if !h.ui.HasText(text) {
				t.Fatal("fixture translation is not displayed")
			}
			// Each limit independently suppresses naming chips for unsuitable translations.
			if h.ui.HasText("下划线：") {
				t.Fatalf("unexpected naming chip for %q", text)
			}
		})
	}
}

func TestHistoryProviderMenuAlignmentAndToggle(t *testing.T) {
	for _, tc := range []struct {
		name          string
		width, height int
		source        string
	}{
		{name: "default", width: DefaultWidth, height: DefaultHeight, source: "hello world"},
		{name: "narrow", width: 560, height: DefaultHeight, source: "hello world"},
		{name: "compact-long-source", width: 560, height: 360, source: strings.Repeat("A long source sentence before the provider menu. ", 15)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.providers = []string{"deepl", "google", "youdaoweb", "openai", "siliconflow",
				"zhipu", "baidu", "volcengine", "tencent", "youdao", "azure", "index-translate"}
			h.put(tc.source, "你好世界")
			h.open()
			h.ui.SetSize(tc.width, tc.height)

			// The provider popup previously used a wider fixed width that overflowed and shifted out of alignment with the trigger.
			trigger, ok := h.ui.Find("换源翻译")
			if !ok {
				t.Fatal("missing provider menu trigger")
			}
			h.click("换源翻译")
			popup, ok := h.ui.Find("换源翻译选项")
			if !ok {
				t.Fatal("missing provider menu popup")
			}
			if popup.X != trigger.X || popup.W != trigger.W {
				t.Fatalf("popup does not align with trigger: trigger=%+v popup=%+v", trigger, popup)
			}
			if popup.X < 0 || popup.Y < 0 || popup.X+popup.W > float32(tc.width) || popup.Y+popup.H > float32(tc.height) {
				t.Fatalf("popup overflows %dx%d window: %+v", tc.width, tc.height, popup)
			}
			if popup.Y < trigger.Y+trigger.H && popup.Y+popup.H > trigger.Y {
				t.Fatalf("popup overlaps trigger: trigger=%+v popup=%+v", trigger, popup)
			}
			if _, ok := h.ui.Find("有道查词与翻译 · 新增译文"); !ok {
				t.Fatal("missing youdaoweb option in provider menu")
			}
			h.ui.ClickAt(trigger.X+trigger.W/2, trigger.Y+trigger.H/2)
			if _, ok := h.ui.Find("换源翻译选项"); ok {
				t.Fatal("second click on trigger did not close provider menu")
			}
		})
	}
}

func TestHistoryPreview(t *testing.T) {
	dir := os.Getenv("SOTTO_HISTORY_PREVIEW")
	if dir == "" {
		t.Skip("set SOTTO_HISTORY_PREVIEW to export the headless history window")
	}
	h := newHarness(t)
	h.open()
	save := func(name string) {
		t.Helper()
		f, err := os.Create(filepath.Join(dir, "history-"+name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		if err := errors.Join(png.Encode(f, h.ui.Image()), f.Close()); err != nil {
			t.Fatal(err)
		}
	}
	save("empty")
	h.now = h.now.AddDate(0, 0, -5)
	h.put("a working hypothesis", "一个工作假设")
	h.now = h.now.AddDate(0, 0, 4)
	h.put("Make room for uncertainty.", "为不确定性留出空间。")
	h.now = h.now.AddDate(0, 0, 1)
	h.put("in retrospect", "回想起来", "回顾")
	h.now = h.now.Add(time.Second)
	h.put("The margin of error is small.", "误差范围很小。", "误差幅度很小。", "容错余地很小。")
	h.view.Refresh()
	h.settle()
	save("light")
	h.ui.SetDark(true)
	save("dark")
	h.ui.SetSize(560, 360)
	h.ui.SetDark(false)
	save("compact")
	h.put("in retrospect", "回想起来", "读完这段说明后，可以作出更完整的判断。", "不确定性出现时仍然需要留有余地。")
	h.view.Refresh()
	h.settle()
	h.ui.Type("完整的判断")
	h.settle()
	save("compact-match")
	h.ui.Key(ui.Cmd, ui.KeyA)
	h.ui.Type("no matching result")
	save("search-pending")
	h.settle()
	h.ui.SetDark(true)
	save("compact-no-match")

	h.now = h.now.Add(time.Second)
	r := h.store.Begin(history.Key{Text: "memo", SourceLang: "en", TargetLang: "zh-HANS"}, []string{"youdaoweb"})
	defer r.Done()
	if ok, err := h.store.Record(t.Context(), r, "youdaoweb", "memo\n英 /ˈmeməʊ/  美 /ˈmemoʊ/\nn. 备忘录\n示例：write a memo", history.ResultDictionary); err != nil || !ok {
		t.Fatalf("dictionary fixture: %v, %v", ok, err)
	}
	h.view.Refresh()
	h.settle()
	h.ui.SetSize(DefaultWidth, DefaultHeight)
	h.ui.SetDark(false)
	h.ui.Key(ui.Cmd, ui.KeyA)
	h.ui.Type("memo")
	h.settle()
	save("dictionary")
	h.providers = []string{"google", "youdaoweb"}
	h.ui.Frame()
	h.click("换源翻译")
	save("providers")
	h.onRetry = func(context.Context, history.Entry, string) error {
		return errors.New("网络暂时不可用，请稍后重试")
	}
	if err := h.ui.Click("Google · 新增译文"); err != nil {
		t.Fatal(err)
	}
	save("provider-pending")
	h.settle()
	save("provider-failure")

	h.now = h.now.Add(time.Second)
	enReq := h.store.Begin(history.Key{Text: "用户名称", SourceLang: "zh-HANS", TargetLang: "en"}, []string{"deepl", "google"})
	defer enReq.Done()
	_, _ = h.store.Record(t.Context(), enReq, "deepl", "user name", history.ResultTranslation)
	_, _ = h.store.Record(t.Context(), enReq, "google", "account name", history.ResultTranslation)
	h.click("清空搜索")
	h.click("折叠 Google")
	h.click("清空历史")
	save("confirm-clear")
	h.ui.SetDark(true)
	save("confirm-clear-dark")
	h.click("取消")
	h.ui.SetDark(false)
	h.click("下划线：user_name")
	h.ui.Advance(200 * time.Millisecond)
	save("naming-and-toast")
}
