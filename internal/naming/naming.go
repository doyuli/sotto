// Package naming splits programming identifiers into words and formats
// them in the common naming styles, so a translation can be copied as
// camelCase, snake_case and friends.
package naming

import (
	"strings"
	"unicode"
)

// The naming styles, as stored in settings and shown in the bubble.
const (
	LowerCamel = "lowerCamel"
	UpperCamel = "upperCamel"
	Snake      = "snake"
	Constant   = "constant"
	Kebab      = "kebab"
	None       = "none"
)

// Styles lists the valid styles in display order.
var Styles = []string{LowerCamel, UpperCamel, Snake, Constant, Kebab}

// Labels names the styles in Chinese for the UI.
var Labels = map[string]string{
	LowerCamel: "小驼峰",
	UpperCamel: "大驼峰",
	Snake:      "下划线",
	Constant:   "常量",
	Kebab:      "短横线",
}

// DetectStyle reports the identifier style of s: one of "camel", "snake",
// "kebab", "constant" or "none" for plain words, mixed or non-Latin text.
func DetectStyle(s string) string {
	if s == "" || strings.ContainsFunc(s, unicode.IsSpace) {
		return None
	}
	hasUpper, hasLower, hasLetter := false, false, false
	for _, r := range s {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '-' {
			return None
		}
		switch {
		case unicode.IsUpper(r):
			hasUpper = true
		case unicode.IsLower(r):
			hasLower = true
		}
		if unicode.IsLetter(r) {
			hasLetter = true
		}
	}
	if !hasLetter {
		return None
	}
	switch {
	case strings.ContainsRune(s, '_'):
		if hasLower {
			return Snake
		}
		return Constant
	case strings.ContainsRune(s, '-'):
		return Kebab
	case hasUpper && hasLower:
		return Camel
	}
	return None
}

// Camel is DetectStyle's value for mixed-case identifiers without
// separators, as in getUserInfo.
const Camel = "camel"

// SplitWords splits an identifier or phrase into its words: on '_', '-'
// and spaces, on camelCase boundaries (including acronyms, as HTTPServer
// splitting into HTTP and Server) and between letters and digits.
func SplitWords(s string) []string {
	var words []string
	var cur []rune
	runes := []rune(s)
	flush := func() {
		if len(cur) > 0 {
			words = append(words, string(cur))
			cur = nil
		}
	}
	for i, r := range runes {
		switch {
		case r == '_' || r == '-' || unicode.IsSpace(r):
			flush()
		case unicode.IsUpper(r):
			if len(cur) > 0 {
				prev := cur[len(cur)-1]
				var next rune
				if i+1 < len(runes) {
					next = runes[i+1]
				}
				// Split before an upper case letter that follows a lower
				// case letter or digit (getUser), or that starts a word
				// after an acronym (HTTPServer).
				if unicode.IsLower(prev) || unicode.IsDigit(prev) ||
					(unicode.IsUpper(prev) && unicode.IsLower(next)) {
					flush()
				}
			}
			cur = append(cur, r)
		case unicode.IsDigit(r):
			if len(cur) > 0 && unicode.IsLetter(cur[len(cur)-1]) {
				flush()
			}
			cur = append(cur, r)
		case unicode.IsLetter(r):
			if len(cur) > 0 && unicode.IsDigit(cur[len(cur)-1]) {
				flush()
			}
			cur = append(cur, r)
		default:
			// Punctuation and symbols separate words: they must never
			// survive into a camelCase/snake_case result.
			flush()
		}
	}
	flush()
	return words
}

// Format joins words in the given style. Letters after the first of each
// word are normalized ("user ID" becomes "userId", not "userID"). An
// unknown style formats as nothing.
func Format(words []string, style string) string {
	switch style {
	case LowerCamel:
		var b strings.Builder
		for i, w := range words {
			if i == 0 {
				b.WriteString(strings.ToLower(w))
			} else {
				b.WriteString(capitalize(w))
			}
		}
		return b.String()
	case UpperCamel:
		var b strings.Builder
		for _, w := range words {
			b.WriteString(capitalize(w))
		}
		return b.String()
	case Snake:
		return strings.ToLower(strings.Join(words, "_"))
	case Constant:
		return strings.ToUpper(strings.Join(words, "_"))
	case Kebab:
		return strings.ToLower(strings.Join(words, "-"))
	}
	return ""
}

func capitalize(w string) string {
	r := []rune(w)
	if len(r) == 0 {
		return ""
	}
	return string(unicode.ToUpper(r[0])) + strings.ToLower(string(r[1:]))
}
