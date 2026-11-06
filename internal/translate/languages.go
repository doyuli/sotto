package translate

import "sotto/internal/languages"

// languageMaps maps an app-internal language code to each service's own
// code. The OpenAI-compatible service is absent: its prompts name the
// language in English instead.
var languageMaps = map[string]map[string]string{
	"deepl": {
		"zh-HANS": "ZH-HANS",
		"en":      "EN-US",
		"ja":      "JA",
		"ko":      "KO",
		"fr":      "FR",
		"de":      "DE",
		"ru":      "RU",
		"es":      "ES",
		"pt":      "PT-BR",
		"it":      "IT",
	},
	"baidu": {
		"zh-HANS": "zh",
		"en":      "en",
		"ja":      "jp",
		"ko":      "kor",
		"fr":      "fra",
		"de":      "de",
		"ru":      "ru",
		"es":      "spa",
		"pt":      "pt",
		"it":      "it",
	},
	"tencent": {
		"zh-HANS": "zh",
		"en":      "en",
		"ja":      "ja",
		"ko":      "ko",
		"fr":      "fr",
		"de":      "de",
		"ru":      "ru",
		"es":      "es",
		"pt":      "pt",
		"it":      "it",
	},
	"youdao": {
		"zh-HANS": "zh-CHS",
		"en":      "en",
		"ja":      "ja",
		"ko":      "ko",
		"fr":      "fr",
		"de":      "de",
		"ru":      "ru",
		"es":      "es",
		"pt":      "pt",
		"it":      "it",
	},
	"azure": {
		"zh-HANS": "zh-Hans",
		"en":      "en",
		"ja":      "ja",
		"ko":      "ko",
		"fr":      "fr",
		"de":      "de",
		"ru":      "ru",
		"es":      "es",
		"pt":      "pt",
		"it":      "it",
	},
	"google": {
		"zh-HANS": "zh-CN",
		"en":      "en",
		"ja":      "ja",
		"ko":      "ko",
		"fr":      "fr",
		"de":      "de",
		"ru":      "ru",
		"es":      "es",
		"pt":      "pt",
		"it":      "it",
	},
}

// detectedMaps maps a service-reported source language back to an
// app-internal code.
var detectedMaps = map[string]map[string]string{
	"deepl": {
		"EN": "en", "EN-US": "en", "EN-GB": "en",
		"ZH": "zh-HANS", "ZH-HANS": "zh-HANS",
		"JA": "ja", "KO": "ko", "FR": "fr", "DE": "de",
		"RU": "ru", "ES": "es", "PT": "pt", "IT": "it",
	},
	"baidu": {
		"zh": "zh-HANS", "en": "en", "jp": "ja", "kor": "ko",
		"fra": "fr", "de": "de", "ru": "ru", "spa": "es",
		"pt": "pt", "it": "it",
	},
	"tencent": {
		"zh": "zh-HANS", "zh-TW": "zh-HANS", "en": "en", "ja": "ja",
		"ko": "ko", "fr": "fr", "de": "de", "ru": "ru",
		"es": "es", "pt": "pt", "it": "it",
	},
	"youdao": {
		"zh-CHS": "zh-HANS", "zh": "zh-HANS", "en": "en", "ja": "ja",
		"ko": "ko", "fr": "fr", "de": "de", "ru": "ru",
		"es": "es", "pt": "pt", "it": "it",
	},
	"azure": {
		"zh-Hans": "zh-HANS", "zh-Hant": "zh-HANS", "en": "en",
		"ja": "ja", "ko": "ko", "fr": "fr", "de": "de",
		"ru": "ru", "es": "es", "pt": "pt", "it": "it",
	},
	"google": {
		"zh-CN": "zh-HANS", "en": "en", "ja": "ja", "ko": "ko",
		"fr": "fr", "de": "de", "ru": "ru", "es": "es",
		"pt": "pt", "it": "it",
	},
}

// mapLanguage maps an app-internal code to the service's code.
func mapLanguage(service, code string) (string, bool) {
	m, ok := languageMaps[service]
	if !ok {
		return "", false
	}
	v, ok := m[code]
	return v, ok
}

// fromDetected maps a service-reported language back to an app-internal
// code; "" when it does not map.
func fromDetected(service, code string) string {
	return detectedMaps[service][code]
}

// englishName is the language's name for prompts.
func englishName(code string) string {
	return languages.English[code]
}
