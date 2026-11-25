package shared

import (
	"github.com/egoist/mygo/ui"

	"sotto/internal/languages"
)

var serviceDisplayNames = map[string]string{
	"deepl":           "DeepL",
	"openai":          "OpenAI",
	"siliconflow":     "硅基流动",
	"zhipu":           "智谱",
	"baidu":           "百度翻译",
	"volcengine":      "火山引擎",
	"tencent":         "腾讯云",
	"youdao":          "有道",
	"youdaoweb":       "有道查词与翻译",
	"azure":           "微软翻译",
	"google":          "Google",
	"index-translate": "Bilibili",
	"dictionary":      "词典",
}

func ServiceName(service string) string {
	if name, ok := serviceDisplayNames[service]; ok {
		return name
	}
	return service
}

func LanguageName(code string) string {
	if code == "auto" {
		return "自动检测"
	}
	if name, ok := languages.Names[code]; ok {
		return name
	}
	return code
}

var ChevronDown = ui.MustParseSVG([]byte(`<svg viewBox="0 0 24 24"><path d="m6 9 6 6 6-6" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>`))
var CloseIcon = ui.MustParseSVG([]byte(`<svg viewBox="0 0 24 24"><path d="M18 6 6 18M6 6l12 12" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>`))
var CopyIcon = ui.MustParseSVG([]byte(`<svg viewBox="0 0 24 24"><rect width="14" height="14" x="8" y="8" rx="2" ry="2" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/><path d="M4 16c-1.1 0-2-.9-2-2V4c0-1.1.9-2 2-2h10c1.1 0 2 .9 2 2" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>`))
var SpeakIcon = ui.MustParseSVG([]byte(`<svg viewBox="0 0 24 24"><path d="M11 5 6 9H2v6h4l5 4V5z" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/><path d="M15.54 8.46a5 5 0 0 1 0 7.07" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"/></svg>`))
