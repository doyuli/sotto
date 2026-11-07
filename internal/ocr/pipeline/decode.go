package pipeline

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"
)

// ParseDictionary preserves order and whitespace labels. Index zero is blank;
// the appended space occupies Classes-1.
func ParseDictionary(data []byte) ([]string, error) {
	if len(data) > 1<<20 {
		return nil, fmt.Errorf("%w: dictionary", ErrLimit)
	}
	if !utf8.Valid(data) {
		return nil, fmt.Errorf("ocr dictionary: invalid UTF-8")
	}
	s := strings.ReplaceAll(string(data), "\r\n", "\n")
	s = strings.TrimSuffix(s, "\n")
	labels := strings.Split(s, "\n")
	if len(labels) != DictionaryEntries {
		return nil, fmt.Errorf("ocr dictionary: expected %d entries, got %d", DictionaryEntries, len(labels))
	}
	for i, label := range labels {
		if label == "" || strings.ContainsRune(label, '\r') {
			return nil, fmt.Errorf("ocr dictionary: invalid entry %d", i)
		}
	}
	return append(append([]string{""}, labels...), " "), nil
}

type decoded struct {
	text   string
	score  float64
	tokens []Token
}

func decode(ctx context.Context, t Tensor, labels []string, limit int) ([]decoded, error) {
	if err := validateTensor(ctx, t, 3, limit); err != nil {
		return nil, err
	}
	if int(t.Shape[2]) != len(labels) {
		return nil, fmt.Errorf("CTC classes %d do not match dictionary %d", t.Shape[2], len(labels))
	}
	n, steps, classes := int(t.Shape[0]), int(t.Shape[1]), int(t.Shape[2])
	out := make([]decoded, n)
	for b := 0; b < n; b++ {
		var text strings.Builder
		previous := -1
		for step := 0; step < steps; step++ {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			row := t.Data[(b*steps+step)*classes : (b*steps+step+1)*classes]
			best := 0
			for i := 1; i < classes; i++ {
				if row[i] > row[best] {
					best = i
				}
			}
			if best != 0 && best != previous {
				p := min(1, max(0, float64(row[best])))
				text.WriteString(labels[best])
				out[b].tokens = append(out[b].tokens, Token{Text: labels[best], Score: p, Step: step})
				out[b].score += p
			}
			previous = best
		}
		out[b].text = text.String()
		if len(out[b].tokens) > 0 {
			out[b].score /= float64(len(out[b].tokens))
		}
	}
	return out, nil
}
