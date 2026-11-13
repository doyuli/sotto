package ocr

import (
	"errors"
	"fmt"
	"strings"

	"sotto/internal/ocr/pipeline"
)

var errVisionLanguage = errors.New("macOS OCR language unavailable")

func visionLanguages(lang string, supported []string) ([]string, error) {
	match := func(want string) string {
		for _, have := range supported {
			if strings.EqualFold(have, want) {
				return have
			}
		}
		for _, have := range supported {
			if strings.EqualFold(strings.Split(have, "-")[0], want) {
				return have
			}
		}
		return ""
	}
	if lang != "" {
		want := strings.ReplaceAll(lang, "_", "-")
		switch strings.ToLower(want) {
		case "zh", "zh-cn", "zh-sg", "zh-hans":
			want = "zh-Hans"
		case "zh-tw", "zh-hk", "zh-mo", "zh-hant":
			want = "zh-Hant"
		}
		found := match(want)
		if found == "" && !strings.HasPrefix(strings.ToLower(want), "zh") {
			found = match(strings.Split(want, "-")[0])
		}
		if found == "" {
			return nil, fmt.Errorf("%w: %q", errVisionLanguage, lang)
		}
		return []string{found}, nil
	}
	var out []string
	for _, want := range []string{"zh-Hans", "zh-Hant", "en-US"} {
		if have := match(want); have != "" {
			out = append(out, have)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("macOS OCR has no Chinese or English recognition model")
	}
	return out, nil
}

// Vision observations have a lower-left normalized origin; layout consumes
// top-left pixel quads just like the Windows OCR backends.
func visionQuad(x, y, w, h float64, width, height int) pipeline.Quad {
	l, r := max(0, min(1, x))*float64(width), max(0, min(1, x+w))*float64(width)
	t, b := max(0, min(1, 1-y-h))*float64(height), max(0, min(1, 1-y))*float64(height)
	return pipeline.Quad{{X: l, Y: t}, {X: r, Y: t}, {X: r, Y: b}, {X: l, Y: b}}
}
