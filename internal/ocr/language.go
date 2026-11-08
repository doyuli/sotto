package ocr

import "strings"

// localLanguage is bounded by PaddleOCR's published PP-OCRv6 small scope:
// Chinese, English, Japanese, and 46 Latin-script languages. Individual
// dictionary characters do not imply support for another language. Source:
// https://github.com/PaddlePaddle/PaddleOCR/blob/main/docs/version3.x/algorithm/PP-OCRv6/PP-OCRv6.en.md
func localLanguage(lang string) bool {
	lang = strings.ToLower(strings.TrimSpace(lang))
	if lang == "" {
		// Screenshots have no language hint; the existing confidence checks
		// can request fallback, but cannot detect every unsupported script.
		return true
	}
	parts := strings.Split(lang, "-")
	if len(parts) > 1 && len(parts[1]) == 3 && parts[1][0] >= 'a' && parts[1][0] <= 'z' {
		// BCP-47 allows an extlang before script and region. Its scope is not
		// implied by the primary language; leave these tags to system OCR.
		// Numeric three-digit regions (such as es-419) are not extlangs.
		return false
	}
	script := ""
	if len(parts) > 1 && len(parts[1]) == 4 && parts[1][0] >= 'a' && parts[1][0] <= 'z' {
		script = parts[1]
	}
	if script == "" && len(parts) > 1 {
		// These regional defaults use unsupported scripts even though the
		// base language defaults to Latin. An explicit script takes precedence.
		// Keep the exceptions limited to languages covered by this model.
		// https://github.com/unicode-org/cldr-json/blob/main/cldr-json/cldr-core/supplemental/likelySubtags.json
		switch parts[0] + "-" + parts[1] {
		case "az-iq", "az-ir", "az-ru", "uz-af", "uz-cn", "ms-cc",
			"ku-am", "ku-az", "ku-ge", "ku-iq", "ku-ir", "ku-lb", "ku-tm":
			return false
		}
	}
	switch parts[0] {
	case "zh":
		return script == "" || script == "hans" || script == "hant" || script == "hani"
	case "ja":
		return script == "" || script == "jpan" || script == "hrkt" || script == "hira" || script == "kana" || script == "hani"
	case "sr":
		// Only Latin-script Serbian is covered; its base tag is ambiguous.
		return script == "latn"
	case "en", "af", "az", "bs", "ca", "cs", "cy", "da", "de", "es", "et", "eu", "fi", "fr", "ga", "gl", "hr", "hu", "id", "is", "it", "ku", "la", "lb", "lt", "lv", "mi", "ms", "mt", "nl", "no", "oc", "pl", "pt", "qu", "rm", "ro", "sk", "sl", "sq", "sv", "sw", "tl", "tr", "uz", "vi":
		return script == "" || script == "latn"
	default:
		return false
	}
}
