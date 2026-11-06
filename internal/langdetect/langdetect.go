// Package langdetect guesses the script-level language of a text from its
// Unicode script. Latin-script languages are deliberately not distinguished:
// the translation services detect them better than a local heuristic can.
package langdetect

import "unicode"

// Detect returns one of "zh", "ja", "ko", "ru", "ar", "th", "he", "el",
// "latin" (unspecified Latin script) or "unknown".
func Detect(text string) string {
	counts := make(map[string]int)
	latin := false
	for _, r := range text {
		if !unicode.IsLetter(r) {
			continue
		}
		switch {
		case unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r):
			counts["ja"]++
		case unicode.Is(unicode.Han, r):
			counts["zh"]++
		case unicode.Is(unicode.Hangul, r):
			counts["ko"]++
		default:
			if code, ok := scriptOf(r); ok {
				counts[code]++
			} else if unicode.Is(unicode.Latin, r) {
				latin = true
			}
		}
	}
	// A single kana character marks Japanese even in kanji-heavy text,
	// while Chinese never contains kana, so kana wins over Han.
	if counts["ja"] > 0 {
		return "ja"
	}
	best, bestN := "", 0
	for _, code := range []string{"zh", "ko", "ru", "ar", "th", "he", "el"} {
		if counts[code] > bestN {
			best, bestN = code, counts[code]
		}
	}
	if best != "" {
		return best
	}
	if latin {
		return "latin"
	}
	return "unknown"
}

// otherScripts are the single-script languages Detect can name, in the
// priority order ties are resolved by.
var otherScripts = []struct {
	name  string
	table *unicode.RangeTable
}{
	{"ru", unicode.Cyrillic},
	{"ar", unicode.Arabic},
	{"th", unicode.Thai},
	{"he", unicode.Hebrew},
	{"el", unicode.Greek},
}

func scriptOf(r rune) (string, bool) {
	for _, s := range otherScripts {
		if unicode.Is(s.table, r) {
			return s.name, true
		}
	}
	return "", false
}
