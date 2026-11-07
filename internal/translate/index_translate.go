package translate

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"sotto/internal/config"
)

const (
	indexEndpoint = "https://index-translate.bilibili.com/v1/chat/completions"
	indexModel    = "Index-Translate-35B-A3B"
)

// IndexTranslate uses Bilibili's public, unauthenticated translation API.
type IndexTranslate struct {
	Client   *http.Client
	Endpoint string // overrides the endpoint (tests)
}

func (IndexTranslate) Name() string { return "index-translate" }

func (IndexTranslate) Available(KeyStore, config.Settings) bool { return true }

func (t IndexTranslate) Translate(ctx context.Context, req Request, k KeyStore, cfg config.Settings) (Result, error) {
	return t.Stream(ctx, req, k, cfg, nil)
}

func (t IndexTranslate) Stream(ctx context.Context, req Request, _ KeyStore, _ config.Settings, emit func(Result)) (Result, error) {
	const service = "B站翻译"
	target := englishName(req.TargetLang)
	if target == "" {
		return Result{}, unsupported(service, req.TargetLang)
	}
	direction := "into " + target
	if source := englishName(req.SourceLang); source != "" {
		direction = "from " + source + " " + direction
	}
	prompt := fmt.Sprintf("Translate the following text %s. Output only the translation, with no explanation.\n\n%s", direction, req.Text)
	payload := map[string]any{
		"model":       indexModel,
		"messages":    []map[string]string{{"role": "user", "content": prompt}},
		"temperature": 0,
		// The public API's reasoning output can otherwise replace the translation.
		"chat_template_kwargs": map[string]bool{"enable_thinking": false},
	}
	endpoint := t.Endpoint
	if endpoint == "" {
		endpoint = indexEndpoint
	}
	choice, err := requestChatCompletion(ctx, t.Client, endpoint, "", payload, service, func(text string) {
		if emit != nil {
			emit(Result{Service: t.Name(), Text: strings.TrimSpace(text), Streaming: true})
		}
	})
	return Result{Service: t.Name(), Text: strings.TrimSpace(choice.Message.Content)}, err
}
