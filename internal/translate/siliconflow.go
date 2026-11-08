package translate

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"sotto/internal/config"
	"sotto/internal/keys"
	"sotto/internal/languages"
)

const (
	siliconFlowEndpoint = "https://api.siliconflow.cn/v1/chat/completions"
	siliconFlowModel    = "tencent/Hunyuan-MT-7B"
)

// SiliconFlow uses the free Hunyuan translation model with its own API key.
type SiliconFlow struct {
	Client   *http.Client
	Endpoint string // overrides the endpoint (tests)
}

func (SiliconFlow) Name() string { return "siliconflow" }

func (SiliconFlow) Available(k KeyStore, _ config.Settings) bool {
	return k.Get(keys.SiliconFlowKey) != ""
}

func (s SiliconFlow) Translate(ctx context.Context, req Request, k KeyStore, cfg config.Settings) (Result, error) {
	return s.translate(ctx, req, k, cfg, false, nil)
}

func (s SiliconFlow) Stream(ctx context.Context, req Request, k KeyStore, cfg config.Settings, emit func(Result)) (Result, error) {
	return s.translate(ctx, req, k, cfg, true, emit)
}

func (s SiliconFlow) translate(ctx context.Context, req Request, k KeyStore, _ config.Settings, stream bool, emit func(Result)) (Result, error) {
	const service = "硅基流动"
	key := k.Get(keys.SiliconFlowKey)
	if key == "" {
		return Result{}, ErrNotConfigured
	}
	target := englishName(req.TargetLang)
	if target == "" {
		return Result{}, unsupported(service, req.TargetLang)
	}
	// Hunyuan-MT publishes separate prompt templates for Chinese and other pairs.
	prompt := fmt.Sprintf("Translate the following segment into %s, without additional explanation.\n\n%s", target, req.Text)
	if req.SourceLang == "zh-HANS" || req.TargetLang == "zh-HANS" {
		prompt = fmt.Sprintf("把下面的文本翻译成%s，不要额外解释。\n\n%s", languages.Names[req.TargetLang], req.Text)
	}
	payload := map[string]any{
		"model":       siliconFlowModel,
		"messages":    []map[string]string{{"role": "user", "content": prompt}},
		"temperature": 0.1,
	}
	endpoint := s.Endpoint
	if endpoint == "" {
		endpoint = siliconFlowEndpoint
	}
	choice, err := requestChatCompletion(ctx, s.Client, endpoint, key, payload, service, stream, func(text string) {
		if emit != nil {
			emit(Result{Service: s.Name(), Text: strings.TrimSpace(text), Streaming: true})
		}
	})
	return Result{Service: s.Name(), Text: strings.TrimSpace(choice.Message.Content)}, err
}
