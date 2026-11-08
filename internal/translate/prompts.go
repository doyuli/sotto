package translate

import "fmt"

// Keep source text separate from instructions for general-purpose chat models.
func translationMessages(req Request, target string) []map[string]string {
	direction := "into " + target
	if source := englishName(req.SourceLang); source != "" {
		direction = "from " + source + " " + direction
	}
	return []map[string]string{
		{"role": "system", "content": fmt.Sprintf("You are a translation engine. Translate the user's text %s. Reply with the translation only, with no explanation.", direction)},
		{"role": "user", "content": req.Text},
	}
}
