package translate

import (
	"errors"
	"unicode/utf8"
)

// A desktop selection may contain a whole document. Bound the entire job,
// independently of provider-specific per-request limits and service fan-out.
const MaxInputRunes = 18000
const maxChunks = 32
const maxResultBytes = 256 << 10

var ErrInputLimit = errors.New("translation input exceeds budget")
var ErrChunkLimit = errors.New("translation chunk count exceeds budget")
var ErrResultLimit = errors.New("translation result exceeds budget")

func ValidateInput(text string) error {
	if len(text) > MaxInputRunes*utf8.UTFMax || utf8.RuneCountInString(text) > MaxInputRunes {
		return ErrInputLimit
	}
	return nil
}
func prepareParts(text string) ([]string, error) {
	if err := ValidateInput(text); err != nil {
		return nil, err
	}
	parts := splitLongText(text, maxChunkRunes)
	if len(parts) > maxChunks {
		return nil, ErrChunkLimit
	}
	return parts, nil
}
