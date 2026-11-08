package translate

import (
	"context"
	"net/http"
	"strings"

	"sotto/internal/config"
	"sotto/internal/keys"
)

const zhipuEndpoint = "https://open.bigmodel.cn/api/paas/v4/chat/completions"

// Zhipu uses BigModel's chat API with an independently configured model.
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

func (z Zhipu) translate(ctx context.Context, req Request, k KeyStore, cfg config.Settings, stream bool, emit func(Result)) (Result, error) {
	const service = "智谱"
	key := k.Get(keys.ZhipuKey)
	if key == "" {
		return Result{}, ErrNotConfigured
	}
	target := englishName(req.TargetLang)
	if target == "" {
		return Result{}, unsupported(service, req.TargetLang)
	}
	model := strings.TrimSpace(cfg.Services.Zhipu.Model)
	if model == "" {
		model = config.DefaultZhipuModel
	}
	payload := map[string]any{
		"model":       model,
		"messages":    translationMessages(req, target),
		"temperature": 0.1,
	}
	// Disable reasoning only for known hybrid text models. Other models keep
	// their API defaults instead of receiving an unsupported parameter.
	switch model {
	case "glm-4.5", "glm-4.5-air", "glm-4.5-airx", "glm-4.5-flash", "glm-4.5-x",
		"glm-4.6", "glm-4.7", "glm-4.7-flash", "glm-4.7-flashx":
		payload["thinking"] = map[string]string{"type": "disabled"}
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
