// Package languages lists the languages sotto translates between. The
// codes are app-internal; each translation service maps them to its own.
package languages

// List holds the app-internal language codes, in menu order.
var List = []string{"zh-HANS", "en", "ja", "ko", "fr", "de", "ru", "es", "pt", "it"}

// Names maps a code to its Chinese display name.
var Names = map[string]string{
	"zh-HANS": "中文（简体）",
	"en":      "英语",
	"ja":      "日语",
	"ko":      "韩语",
	"fr":      "法语",
	"de":      "德语",
	"ru":      "俄语",
	"es":      "西班牙语",
	"pt":      "葡萄牙语",
	"it":      "意大利语",
}

// English maps a code to the name prompts use.
var English = map[string]string{
	"zh-HANS": "Simplified Chinese",
	"en":      "English",
	"ja":      "Japanese",
	"ko":      "Korean",
	"fr":      "French",
	"de":      "German",
	"ru":      "Russian",
	"es":      "Spanish",
	"pt":      "Portuguese",
	"it":      "Italian",
}

// Valid reports whether code is an app-internal language code.
func Valid(code string) bool {
	for _, c := range List {
		if c == code {
			return true
		}
	}
	return false
}
