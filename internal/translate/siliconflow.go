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

const siliconFlowEndpoint = "https://api.siliconflow.cn/v1/chat/completions"

// SiliconFlow uses its own API key and model, with Hunyuan-MT as the default.
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

func (s SiliconFlow) translate(ctx context.Context, req Request, k KeyStore, cfg config.Settings, stream bool, emit func(Result)) (Result, error) {
	const service = "硅基流动"
	key := k.Get(keys.SiliconFlowKey)
	if key == "" {
		return Result{}, ErrNotConfigured
	}
	target := englishName(req.TargetLang)
	if target == "" {
		return Result{}, unsupported(service, req.TargetLang)
	}
	model := strings.TrimSpace(cfg.Services.SiliconFlow.Model)
	if model == "" {
		model = config.DefaultSiliconFlowModel
	}
	messages := translationMessages(req, target)
	if strings.TrimPrefix(model, "Pro/") == config.DefaultSiliconFlowModel {
		// Keep Hunyuan-MT's single-message template, but disambiguate short input.
		prompt := fmt.Sprintf("Translate the following segment into %s, without additional explanation. "+
			"Translate even single words and language names; do not respond conversationally or ask for more text.\n\n%s", target, req.Text)
		if req.SourceLang == "zh-HANS" || req.TargetLang == "zh-HANS" {
			prompt = fmt.Sprintf("把下面的文本翻译成%s，不要额外解释。"+
				"单词和语言名称也要翻译，不要对话或要求补充原文。\n\n%s", languages.Names[req.TargetLang], req.Text)
		}
		messages = []map[string]string{{"role": "user", "content": prompt}}
	}
	payload := map[string]any{
		"model":       model,
		"messages":    messages,
		"temperature": 0.1,
	}
	// These original Qwen3 models support switching off reasoning. Do not
	// infer that capability for thinking-only variants or unknown models.
	// Keep this list in sync when SiliconFlow adds hybrid-reasoning models.
	switch strings.TrimPrefix(model, "Pro/") {
	case "Qwen/Qwen3-0.6B", "Qwen/Qwen3-1.7B", "Qwen/Qwen3-4B", "Qwen/Qwen3-8B",
		"Qwen/Qwen3-14B", "Qwen/Qwen3-32B", "Qwen/Qwen3-30B-A3B", "Qwen/Qwen3-235B-A22B":
		payload["enable_thinking"] = false
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
