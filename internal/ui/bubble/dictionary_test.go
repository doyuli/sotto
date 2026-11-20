package bubble

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"sotto/internal/config"
	"sotto/internal/translate"
)

func TestDictionaryCardWaitsForLookupResult(t *testing.T) {
	// A loading card could stay visible forever because skipped lookups emit no update.
	cfg := testBubbleConfig()
	cfg.Services.Enabled = []string{"google", "dictionary"}
	cfg.Services.Order = cfg.Services.Enabled
	v := newBubbleView(1, cfg, bubbleViewActions{})
	v.applyState(BubbleState{
		ID: 1, ShowID: 1, Generation: 1, SessionID: "sess-1", ReqID: "req-1",
		Text: "古典", Trigger: "selection", TargetLang: "en", Settings: cfg,
	}, false)
	tt := ui.NewTester(v.render, 480, 560)
	assertDictionaryVisible := func(visible bool, stage string) {
		t.Helper()
		tt.Frame()
		if _, found := tt.Find("折叠 词典"); found != visible {
			t.Fatalf("dictionary visibility after %s = %t, want %t", stage, found, visible)
		}
	}
	assertDictionaryVisible(false, "a pending lookup")
	if _, found := tt.Find("折叠 Google"); !found {
		t.Fatal("translation service disappeared while loading")
	}
	v.submitComposer("zzznodictionaryentry")
	v.applyMeta(RunMetaMsg{ReqID: v.reqID, Services: cfg.Services.Enabled})
	v.applyResult(TranslationUpdateMsg{ReqID: v.reqID, Service: "google", Text: "未收录"})
	assertDictionaryVisible(false, "an unanswered lookup alongside a completed translation")
	if !tt.HasText("未收录") {
		t.Fatalf("missing translation: %v", tt.Texts())
	}

	v.submitComposer("classical")
	entry := &translate.DictionaryEntry{Word: "classical", Phonetic: "ˈklæsikl", Meanings: "a. 古典的"}
	v.applyResult(TranslationUpdateMsg{ReqID: v.reqID, Service: "dictionary", Text: "/ˈklæsikl/\n" + entry.Meanings, Dictionary: entry})
	assertDictionaryVisible(true, "a successful lookup")
	v.StartRun("classical", "dictionary")
	assertDictionaryVisible(false, "retrying a lookup")
}

func TestDictionaryOnlyLeavesResultsEmptyWhenSkipped(t *testing.T) {
	// A skipped lookup used to show a dictionary card with a settings prompt.
	cfg := config.Default()
	cfg.Services.Enabled = []string{"dictionary"}
	for _, tc := range []struct {
		text       string
		singleWord bool
	}{
		{"古典", false},
		{"This is a sentence.", false},
		{"zzznodictionaryentry", true},
	} {
		t.Run(tc.text, func(t *testing.T) {
			v := newBubbleView(1, cfg, bubbleViewActions{})
			v.applyState(BubbleState{
				ID: 1, ShowID: 1, Generation: 1, SessionID: "sess-1", ReqID: "req-1",
				Text: tc.text, Trigger: "selection", Settings: cfg,
			}, false)
			v.applyMeta(RunMetaMsg{ReqID: "req-1", Services: cfg.Services.Enabled})
			var results []translate.Result
			translate.Run(t.Context(), cfg, nil, translate.Request{Text: tc.text, SingleWord: tc.singleWord}, func(res translate.Result) {
				results = append(results, res)
			})
			for _, res := range results {
				v.applyResult(TranslationUpdateMsg{ReqID: "req-1", Service: res.Service, Text: res.Text, Dictionary: res.Dictionary, Err: res.Err})
			}
			tt := ui.NewTester(v.render, 480, 480)
			tt.Frame()
			if _, found := tt.Find("折叠 词典"); found {
				t.Fatal("skipped lookup rendered a dictionary card")
			}
			if _, found := tt.Find("配置翻译服务"); found {
				t.Fatal("skipped lookup rendered a settings prompt")
			}
		})
	}
}

func TestDictionaryDoesNotOverrideDetectedLanguage(t *testing.T) {
	// A local English spelling match used to win auto-detection over a slower translation provider.
	cfg := testBubbleConfig()
	cfg.Services.Enabled = []string{"google", "dictionary"}
	cfg.Services.Order = cfg.Services.Enabled
	request := translate.Request{Text: "Gift", SourceLang: "auto", TargetLang: "zh-HANS", SingleWord: true}
	entry, err := (translate.Dictionary{}).Translate(t.Context(), request, nil, cfg)
	if err != nil || entry.Dictionary == nil {
		t.Fatalf("dictionary lookup = %+v, %v", entry, err)
	}
	v := newBubbleView(1, cfg, bubbleViewActions{})
	v.applyState(BubbleState{
		ID: 1, ShowID: 1, Generation: 1, SessionID: "sess-1", ReqID: "req-1",
		Text: request.Text, Trigger: "selection", TargetLang: request.TargetLang, Settings: cfg,
	}, false)
	v.applyResult(TranslationUpdateMsg{ReqID: "req-1", Service: entry.Service, Dictionary: entry.Dictionary, Text: entry.Text, DetectedLang: entry.DetectedLang})
	tt := ui.NewTester(v.render, 480, 480)
	tt.Frame()
	if _, found := tt.Find("折叠 词典"); !found {
		t.Fatal("dictionary entry should be visible before source detection")
	}
	v.applyResult(TranslationUpdateMsg{ReqID: "req-1", Service: "google", Text: "毒药", DetectedLang: "de"})
	tt.Frame()
	if _, found := tt.Find("折叠 词典"); found || !tt.HasText("毒药") {
		t.Fatalf("expected only the German translation: %v", tt.Texts())
	}
	if err := tt.Click("交换语言"); err != nil || v.targetLang != "de" {
		t.Fatalf("swapped target language = %q, click error = %v", v.targetLang, err)
	}
}

func TestDictionaryCardActions(t *testing.T) {
	// The generic translation card used to read the whole dictionary entry in the target language.
	cfg := testBubbleConfig()
	cfg.Services.Enabled = []string{"dictionary", "google"}
	cfg.Services.Order = []string{"dictionary", "google"}
	var spokenText, spokenLanguage string
	v := newBubbleView(1, cfg, bubbleViewActions{
		speakText: func(text, lang string, _ func()) {
			spokenText, spokenLanguage = text, lang
		},
	})
	v.applyState(BubbleState{
		ID: 1, ShowID: 1, Generation: 1, SessionID: "sess-1", ReqID: "req-1",
		Text: "CLASSICAL", Trigger: "selection", TargetLang: "zh-HANS", Settings: cfg,
	}, false)
	entry := translate.DictionaryEntry{Word: "classical", Phonetic: "ˈklæsikl", Meanings: "a. 古典的, 经典的"}
	dictionaryText := "/ˈklæsikl/\n" + entry.Meanings
	v.applyResult(TranslationUpdateMsg{ReqID: "req-1", Service: "dictionary", Text: dictionaryText, Dictionary: &entry})
	v.applyResult(TranslationUpdateMsg{ReqID: "req-1", Service: "google", Text: "古典的"})
	tt := ui.NewTester(v.render, 480, 480)
	if err := tt.Click("朗读单词"); err != nil {
		t.Fatal(err)
	}
	if spokenText != entry.Word || spokenLanguage != "en" {
		t.Fatalf("dictionary spoke %q in %q, want the English word", spokenText, spokenLanguage)
	}
	if err := tt.Click("复制词条"); err != nil {
		t.Fatal(err)
	}
	if got := tt.Clipboard(); got != entry.Word+"\n"+dictionaryText {
		t.Fatalf("dictionary clipboard = %q", got)
	}
	if err := tt.Click("复制 Google"); err != nil {
		t.Fatal(err)
	}
	if got := tt.Clipboard(); got != "古典的" {
		t.Fatalf("translation clipboard = %q", got)
	}
	if err := tt.Click("折叠 词典"); err != nil {
		t.Fatal(err)
	}
	if _, ok := tt.Find("复制词条"); ok {
		t.Fatal("collapsed dictionary still exposes its copy action")
	}
}
