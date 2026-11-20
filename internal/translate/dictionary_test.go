package translate

import (
	"errors"
	"strings"
	"testing"

	"sotto/internal/config"
)

func TestDictionaryWord(t *testing.T) {
	cfg := config.Default()
	cfg.Services.Enabled = []string{"dictionary"}
	for _, tc := range []struct {
		word   string
		target string
		want   []string
	}{
		// The old service gave English definitions for English selections.
		{"CLASSICAL", "zh-HANS", []string{"/ˈklæsikl/\n", "古典的", "经典的", "\n[医]"}},
		// Common inflections keep their own pronunciation and dictionary gloss.
		{"went", "en", []string{"/went/\n", "go的过去式"}},
	} {
		t.Run(tc.word, func(t *testing.T) {
			var results []Result
			Run(t.Context(), cfg, nil, Request{Text: tc.word, TargetLang: tc.target, SingleWord: true}, func(r Result) {
				results = append(results, r)
			})
			if len(results) != 1 || results[0].Service != "dictionary" || results[0].DetectedLang != "" || results[0].Err != "" {
				t.Fatalf("dictionary results = %+v", results)
			}
			entry := results[0].Dictionary
			if entry == nil || entry.Word != strings.ToLower(tc.word) || entry.Phonetic == "" || entry.Meanings == "" {
				t.Fatalf("incomplete dictionary entry after translation fan-out: %+v", entry)
			}
			for _, want := range tc.want {
				if !strings.Contains(results[0].Text, want) {
					t.Errorf("dictionary text %q missing %q", results[0].Text, want)
				}
			}
			if tc.word == "went" && entry.Meanings != "go的过去式" {
				t.Errorf("went gloss = %q, want the original dictionary gloss", entry.Meanings)
			}
		})
	}
}

func TestDictionarySkipsUnsupportedSelections(t *testing.T) {
	for _, req := range []Request{
		{Text: "hello world", SourceLang: "en"},
		{Text: "古典", TargetLang: "en"},
		{Text: "chat", SourceLang: "fr", SingleWord: true},
	} {
		res, err := (Dictionary{}).Translate(t.Context(), req, nil, config.Settings{})
		if !errors.Is(err, ErrSkip) || res.Text != "" || res.Dictionary != nil {
			t.Errorf("lookup %q = %+v, %v, want a skipped lookup", req.Text, res, err)
		}
	}
}

func TestRunWithOmitsDictionaryMissAlongsideTranslation(t *testing.T) {
	// A missing dictionary word should emit no card while other providers complete.
	all := []Translator{
		fakeTranslator{name: "a", res: Result{Service: "a", Text: "甲"}},
		Dictionary{},
	}
	cfg := config.Default()
	cfg.Services.Enabled = []string{"a", "dictionary"}
	results := make(chan Result, len(all))
	RunWith(t.Context(), all, cfg, fakeKeys{"a": "1"}, Request{Text: "zzznodictionaryentry", SingleWord: true}, func(r Result) {
		results <- r
	})
	close(results)
	got := make(map[string]Result)
	for res := range results {
		got[res.Service] = res
	}
	if len(got) != 1 || got["a"].Text != "甲" {
		t.Fatalf("expected only the translation, got %+v", got)
	}
}

func TestDictionaryServiceProbe(t *testing.T) {
	// The default translation probe is a phrase, which the dictionary rejects.
	if err := Test(t.Context(), "dictionary", config.Default(), nil); err != nil {
		t.Fatal(err)
	}
}
