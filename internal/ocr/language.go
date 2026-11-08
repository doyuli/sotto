package ocr

import "strings"

// localLanguage follows the bundled small model's language routes, not the
// presence of individual characters in its dictionary. Source:
// https://github.com/RapidAI/RapidOCR/blob/v3.10.0/python/rapidocr/default_models.yaml
func localLanguage(lang string) bool {
	lang = strings.ToLower(strings.TrimSpace(lang))
	if lang == "" {
		// Screenshots have no language hint; the existing confidence checks
		// can request fallback, but cannot detect every unsupported script.
		return true
	}
	parts := strings.Split(lang, "-")
	script := ""
	if len(parts) > 1 && len(parts[1]) == 4 && parts[1][0] >= 'a' && parts[1][0] <= 'z' {
		script = parts[1]
	}
	switch parts[0] {
	case "zh":
		return script == "" || script == "hans" || script == "hant" || script == "hani"
	case "ja":
		return script == "" || script == "jpan" || script == "hrkt" || script == "hira" || script == "kana" || script == "hani"
	case "sr":
		// The upstream rs_latin route covers only Latin-script Serbian.
		return script == "latn"
	case "en", "af", "az", "bs", "ca", "cs", "cy", "da", "de", "es", "et", "eu", "fi", "fr", "ga", "gl", "hr", "hu", "id", "is", "it", "ku", "la", "lb", "lt", "lv", "mi", "ms", "mt", "nl", "no", "oc", "pl", "pt", "qu", "rm", "ro", "sk", "sl", "sq", "sv", "sw", "tl", "tr", "uz", "vi":
		return script == "" || script == "latn"
	default:
		return false
	}
}
