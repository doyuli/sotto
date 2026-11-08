package ocr

import (
	"strings"
	"testing"
)

func TestLocalLanguageCoverage(t *testing.T) {
	// Keep coverage of the full published scope, including Serbian's script
	// restriction, rather than only the languages in the translation menu.
	supported := strings.Fields("zh-Hans zh-Hant en ja af az bs ca cs cy da de es et eu fi fr ga gl hr hu id is it ku la lb lt lv mi ms mt nl no oc pl pt qu rm ro sr-Latn sk sl sq sv sw tl tr uz vi")
	if len(supported) != 50 {
		t.Fatalf("expected 50 published language routes, got %d", len(supported))
	}
	for _, lang := range supported {
		t.Run(lang, func(t *testing.T) {
			if !localLanguage(lang) {
				t.Fatalf("supported language %q bypasses the local model", lang)
			}
		})
	}
}

func TestLocalLanguageTags(t *testing.T) {
	for _, tc := range []struct {
		lang string
		want bool
	}{
		{"", true}, {"  ", true}, {"zh", true}, {"ZH-hans-CN", true},
		{"zh-Hant-TW", true}, {"ja-JP", true}, {"ja-Jpan-JP", true},
		{" en-US ", true}, {"fr-CA", true}, {"de-1901", true},
		{"pt-BR", true}, {"vi-VN", true}, {"sr-Latn-RS", true},
		{"ko", false}, {"ko-KR", false}, {"ru-RU", false},
		{"ar", false}, {"th", false}, {"hi", false}, {"el", false},
		{"sr", false}, {"sr-Cyrl", false}, {"az-Cyrl", false},
		{"uz-Cyrl-UZ", false}, {"ku-Arab", false}, {"zh-Latn", false},
		{"english", false}, {"xx", false},
	} {
		t.Run(tc.lang, func(t *testing.T) {
			if got := localLanguage(tc.lang); got != tc.want {
				t.Fatalf("localLanguage(%q) = %t, want %t", tc.lang, got, tc.want)
			}
		})
	}
}
