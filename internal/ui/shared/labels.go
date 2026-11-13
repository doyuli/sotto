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
