package ocr

import (
	"strings"
	"unicode"
)

// CleanText is the post-processing pass for text from any source — a
// selection grab or an OCR engine — so a caller outside the package gets
// the same control-character and CJK-spacing cleanup.
func CleanText(s string) string { return cleanText(s) }

// cleanText is the single post-processing step every engine's output goes
// through: it removes the characters no consumer should see — control and
// zero-width code points, BOM, replacement characters, private-use glyphs
// and Unicode noncharacters — normalizes line endings, joins the spaces
// OCR puts inside CJK runs, and trims the ragged whitespace screenshots
// produce. Ordinary punctuation is left alone.
func cleanText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")

	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\n':
			b.WriteByte('\n')
		case r == '\t':
			b.WriteByte(' ')
		case r == '\u3000': // ideographic space: a word-space here, not a glyph
			b.WriteByte(' ')
		case unicode.IsControl(r):
			// C0/C1 controls and DEL carry no text.
		case unicode.Is(unicode.Cf, r):
			// Zero-width spaces/joiners, BOM, soft hyphen, bidi overrides.
		case r == '\uFFFD' || r == '\uFFFC':
			// Replacement character and object-replacement character.
		case r >= 0xE000 && r <= 0xF8FF:
			// Private use: OCR's stand-in for glyphs it cannot map.
		case r >= 0xFDD0 && r <= 0xFDEF, r&0xFFFE == 0xFFFE:
			// Unicode noncharacters.
		default:
			b.WriteRune(r)
		}
	}

	// Trim each line's trailing spaces, drop blank lines at the ends and
	// collapse runs of blank lines to one: OCR often leaves an empty line
	// for the gaps between paragraphs, not a paragraph break.
	lines := strings.Split(cjkText(b.String()), "\n")
	out := make([]string, 0, len(lines))
	blank := false
	for _, line := range lines {
		line = strings.TrimRight(line, " ")
		if strings.TrimSpace(line) == "" {
			if len(out) > 0 {
				blank = true
			}
			continue
		}
		if blank {
			out = append(out, "")
			blank = false
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// cjkText joins the single spaces Windows' OCR inserts between Han and
// kana glyphs — it does not know these scripts run without word spaces —
// while keeping the spaces around Latin words. A CJK line that reaches
// the translator as "划 词 翻 译" translates poorly.
func cjkText(s string) string {
	runes := []rune(s)
	out := make([]rune, 0, len(runes))
	for i, r := range runes {
		if r == ' ' {
			j := i - 1
			for j >= 0 && runes[j] == ' ' {
				j--
			}
			k := i + 1
			for k < len(runes) && runes[k] == ' ' {
				k++
			}
			if j >= 0 && k < len(runes) && cjkSpaceSide(runes[j]) && cjkSpaceSide(runes[k]) {
				continue
			}
		}
		out = append(out, r)
	}
	return string(out)
}

// isCJK reports whether a rune belongs to a script written without
// spaces between words. Hangul is left out: Korean does use spaces.
func isCJK(r rune) bool {
	return unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r)
}

// cjkSpaceSide reports whether text on this side of a space belongs to
// such a script, its punctuation included — "文字 。" must lose the space
// just as "划 词" does.
func cjkSpaceSide(r rune) bool {
	if isCJK(r) {
		return true
	}
	return (r >= 0x3000 && r <= 0x303F) || // CJK symbols and punctuation
		(r >= 0xFE30 && r <= 0xFE4F) || // CJK compatibility forms
		(r >= 0xFF01 && r <= 0xFF20) || // fullwidth punctuation
		(r >= 0xFF3B && r <= 0xFF40) ||
		(r >= 0xFF5B && r <= 0xFF65)
}
