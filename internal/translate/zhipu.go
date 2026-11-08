package translate

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"sotto/internal/config"
	"sotto/internal/keys"
)

const (
	zhipuEndpoint = "https://open.bigmodel.cn/api/paas/v4/chat/completions"
	zhipuModel    = "glm-4.7-flash"
)

// Zhipu uses the free GLM model with thinking disabled for short translations.
type Zhipu struct {
	Client   *http.Client
	Endpoint string // overrides the endpoint (tests)
}

func (Zhipu) Name() string { return "zhipu" }

func (Zhipu) Available(k KeyStore, _ config.Settings) bool {
	return k.Get(keys.ZhipuKey) != ""
}

func (z Zhipu) Translate(ctx context.Context, req Request, k KeyStore, cfg config.Settings) (Result, error) {
	return z.translate(ctx, req, k, cfg, false, nil)
}

func (z Zhipu) Stream(ctx context.Context, req Request, k KeyStore, cfg config.Settings, emit func(Result)) (Result, error) {
	return z.translate(ctx, req, k, cfg, true, emit)
}

func (z Zhipu) translate(ctx context.Context, req Request, k KeyStore, _ config.Settings, stream bool, emit func(Result)) (Result, error) {
	const service = "智谱"
	key := k.Get(keys.ZhipuKey)
	if key == "" {
		return Result{}, ErrNotConfigured
	}
	target := englishName(req.TargetLang)
	if target == "" {
		return Result{}, unsupported(service, req.TargetLang)
	}
	direction := "into " + target
	if source := englishName(req.SourceLang); source != "" {
		direction = "from " + source + " " + direction
	}
	payload := map[string]any{
		"model": zhipuModel,
		"messages": []map[string]string{
			{"role": "system", "content": fmt.Sprintf("You are a translation engine. Translate the user's text %s. Reply with the translation only, with no explanation.", direction)},
			{"role": "user", "content": req.Text},
		},
		// Reasoning delays the first translation token and is not shown in the bubble.
		"thinking":    map[string]string{"type": "disabled"},
		"temperature": 0.1,
	}
	endpoint := z.Endpoint
	if endpoint == "" {
		endpoint = zhipuEndpoint
	}
	choice, err := requestChatCompletion(ctx, z.Client, endpoint, key, payload, service, stream, func(text string) {
		if emit != nil {
			emit(Result{Service: z.Name(), Text: strings.TrimSpace(text), Streaming: true})
		}
	})
	return Result{Service: z.Name(), Text: strings.TrimSpace(choice.Message.Content)}, err
}
