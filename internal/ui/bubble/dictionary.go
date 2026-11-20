package bubble

import (
	"strings"

	"github.com/egoist/mygo/ui"

	"sotto/internal/translate"
)

func renderDictionary(c *ui.Context, p bubblePalette, entry *translate.DictionaryEntry) {
	ui.RichText(c,
		ui.Span{Text: entry.Word, Weight: 600},
		ui.Span{Text: "  /" + entry.Phonetic + "/", Size: 12, Color: p.muted},
	).MinHeight(24).FontSize(16).LineHeight(1.4).TextColor(p.fg).Selectable().SelectionColor(p.selection)
	ui.RichText(c, dictionaryMeaningSpans(entry.Meanings, p.muted)...).
		FontSize(15).LineHeight(1.65).TextColor(p.fg).Selectable().SelectionColor(p.selection)
}

func dictionaryMeaningSpans(text string, muted ui.Color) []ui.Span {
	var spans []ui.Span
	for line := range strings.SplitSeq(text, "\n") {
		if len(spans) != 0 {
			spans = append(spans, ui.Span{Text: "\n"})
		}
		label, definition, hasSpace := strings.Cut(line, " ")
		if hasSpace && dictionaryMeaningLabel(label) {
			spans = append(spans, ui.Span{Text: label + "  ", Size: 12, Color: muted}, ui.Span{Text: definition})
		} else {
			spans = append(spans, ui.Span{Text: line})
		}
	}
	return spans
}

func dictionaryMeaningLabel(label string) bool {
	switch label {
	case "n.", "a.", "adj.", "v.", "vt.", "vi.", "adv.", "pron.", "prep.", "conj.", "interj.", "num.", "art.", "det.", "aux.":
		return true
	}
	return strings.HasPrefix(label, "[") && strings.HasSuffix(label, "]")
}
