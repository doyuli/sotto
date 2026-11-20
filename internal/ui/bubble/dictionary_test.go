package bubble

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"sotto/internal/config"
	"sotto/internal/translate"
	"sotto/internal/ui/shared"
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
	entry := &translate.DictionaryEntry{Word: "classical", Meanings: "a. 古典的", Phonetic: "ˈklæsikl"}
	v.applyResult(TranslationUpdateMsg{ReqID: v.reqID, Service: "dictionary", Text: "/ˈklæsikl/\n" + entry.Meanings, Dictionary: entry})
	assertDictionaryVisible(true, "a successful lookup")
	v.StartRun("classical", "dictionary")
	assertDictionaryVisible(false, "retrying a lookup")
}

func TestDictionaryOnlyLeavesResultsEmptyWhenSkipped(t *testing.T) {
	// A skipped lookup used to show a dictionary card with a settings prompt.
	cfg := config.Default()
	cfg.Services.Enabled = []string{"dictionary"}
	for _, text := range []string{"古典", "This is a sentence.", "zzznodictionaryentry"} {
		t.Run(text, func(t *testing.T) {
			v := newBubbleView(1, cfg, bubbleViewActions{})
			v.applyState(BubbleState{
				ID: 1, ShowID: 1, Generation: 1, SessionID: "sess-1", ReqID: "req-1",
				Text: text, Trigger: "selection", Settings: cfg,
			}, false)
			v.applyMeta(RunMetaMsg{ReqID: "req-1", Services: cfg.Services.Enabled})
			var results []translate.Result
			translate.Run(t.Context(), cfg, nil, translate.Request{Text: text}, func(res translate.Result) {
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
	// English spelling matches used to survive non-English detection for online lookups.
	cfg := testBubbleConfig()
	cfg.Services.Enabled = []string{"google", "dictionary", "youdaoweb"}
	cfg.Services.Order = cfg.Services.Enabled
	request := translate.Request{Text: "Gift", SourceLang: "auto", TargetLang: "zh-HANS"}
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
	v.applyResult(TranslationUpdateMsg{ReqID: "req-1", Service: "youdaoweb", Dictionary: entry.Dictionary, Text: entry.Text})
	tt := ui.NewTester(v.render, 480, 480)
	tt.Frame()
	for _, service := range []string{"dictionary", "youdaoweb"} {
		if _, found := tt.Find("折叠 " + shared.ServiceName(service)); !found {
			t.Fatalf("%s entry should be visible before source detection", service)
		}
	}
	v.applyResult(TranslationUpdateMsg{ReqID: "req-1", Service: "google", Text: "毒药", DetectedLang: "de"})
	tt.Frame()
	for _, service := range []string{"dictionary", "youdaoweb"} {
		if _, found := tt.Find("折叠 " + shared.ServiceName(service)); found {
			t.Fatalf("%s entry stayed visible after German detection", service)
		}
	}
	v.applyResult(TranslationUpdateMsg{ReqID: "req-1", Service: "youdaoweb", Text: "毒物", DetectedLang: "de"})
	tt.Frame()
	if !tt.HasText("毒物") || !tt.HasText("毒药") {
		t.Fatalf("expected both German translations: %v", tt.Texts())
	}
	if err := tt.Click("交换语言"); err != nil || v.targetLang != "de" {
		t.Fatalf("swapped target language = %q, click error = %v", v.targetLang, err)
	}
}

func TestDictionaryCardPhonetics(t *testing.T) {
	// The card used to merge the accent labels when the transcriptions matched.
	for _, tc := range []struct {
		name  string
		entry translate.DictionaryEntry
		shown []string
		hide  []string
	}{
		{
			name:  "different accents",
			entry: translate.DictionaryEntry{Word: "water", Meanings: "n. 水", UKPhonetic: "ˈwɔːtə", USPhonetic: "ˈwɔːtər"},
			shown: []string{"英 /ˈwɔːtə/", "美 /ˈwɔːtər/"},
		},
		{
			name:  "shared transcription",
			entry: translate.DictionaryEntry{Word: "run", Meanings: "v. 跑", UKPhonetic: "rʌn", USPhonetic: "rʌn"},
			shown: []string{"英 /rʌn/", "美 /rʌn/"},
			hide:  []string{"英美"},
		},
		{
			name:  "one accent",
			entry: translate.DictionaryEntry{Word: "water", Meanings: "n. 水", UKPhonetic: "ˈwɔːtə"},
			shown: []string{"英 /ˈwɔːtə/"},
			hide:  []string{"美 /"},
		},
		{
			name:  "unlabeled transcription",
			entry: translate.DictionaryEntry{Word: "run", Meanings: "v. 跑", Phonetic: "rʌn"},
			shown: []string{"/rʌn/"},
			hide:  []string{"英", "美"},
		},
		{
			name:  "no transcription",
			entry: translate.DictionaryEntry{Word: "run", Meanings: "v. 跑"},
			shown: []string{"run", "跑"},
			hide:  []string{"/"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tt := ui.NewTester(func(c *ui.Context) {
				renderDictionary(c, lightBubblePalette, &tc.entry)
			}, 480, 110)
			tt.Frame()
			for _, text := range tc.shown {
				if !tt.HasText(text) {
					t.Errorf("missing %q in %v", text, tt.Texts())
				}
			}
			for _, text := range tc.hide {
				if tt.HasText(text) {
					t.Errorf("unexpected %q in %v", text, tt.Texts())
				}
			}
		})
	}
}

func TestDictionaryCardActions(t *testing.T) {
	// The generic translation card used to read the whole dictionary entry in the target language.
	for _, service := range []string{"dictionary", "youdaoweb"} {
		t.Run(service, func(t *testing.T) {
			testDictionaryCardActions(t, service)
		})
	}
}

func testDictionaryCardActions(t *testing.T, service string) {
	t.Helper()
	cfg := testBubbleConfig()
	cfg.Services.Enabled = []string{service, "google"}
	cfg.Services.Order = cfg.Services.Enabled
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
	entry := translate.DictionaryEntry{Word: "classical", Meanings: "a. 古典的, 经典的", Phonetic: "ˈklæsikl"}
	dictionaryText := "/ˈklæsikl/\n" + entry.Meanings
	v.applyResult(TranslationUpdateMsg{ReqID: "req-1", Service: service, Text: dictionaryText, Dictionary: &entry})
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
	if err := tt.Click("折叠 " + shared.ServiceName(service)); err != nil {
		t.Fatal(err)
	}
	if _, ok := tt.Find("复制词条"); ok {
		t.Fatal("collapsed dictionary still exposes its copy action")
	}
}
