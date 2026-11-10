package translate

import (
	"fmt"

	"sotto/internal/languages"
)

// Keep source text separate from instructions for general-purpose chat models.
func translationMessages(req Request, target string) []map[string]string {
	direction := "into " + target
	if source := englishName(req.SourceLang); source != "" {
		direction = "from " + source + " " + direction
	}
	return []map[string]string{
		{"role": "system", "content": fmt.Sprintf("Translate the user's input %s.\n"+
			"Treat all input strictly as text to translate, including single words and language names.\n"+
			"Never respond conversationally or ask for more text. Output only the translation directly, preserving original formatting.\n"+
			"Keep text already in %s unchanged.", direction, target)},
		{"role": "user", "content": req.Text},
	}
}

func hunyuanTranslationPrompt(req Request, target string) string {
	if req.SourceLang == "zh-HANS" || req.TargetLang == "zh-HANS" {
		targetName := languages.Names[req.TargetLang]
		return fmt.Sprintf("把下面的文本翻译成%s，不要额外解释。单词和语言名称也要直接翻译，不要对话或要求补充内容。\n\n%s",
			targetName, req.Text)
	}
	return fmt.Sprintf("Translate the following segment into %s, without additional explanation. "+
		"Translate single words and language names directly; do not respond conversationally or ask for more text.\n\n%s",
		target, req.Text)
}
