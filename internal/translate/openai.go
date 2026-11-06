package translate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"sotto/internal/config"
	"sotto/internal/keys"
)

// OpenAI translates through any OpenAI-compatible chat completions API,
// which covers the many relays and local servers that speak the same
// schema: baseURL and model are the user's to choose.
type OpenAI struct {
	Client *http.Client // nil: a client with a 30s timeout
}

func (OpenAI) Name() string { return "openai" }

func (OpenAI) Available(k KeyStore, _ config.Settings) bool {
	return k.Get(keys.OpenAIKey) != ""
}

func (o OpenAI) Translate(ctx context.Context, req Request, k KeyStore, cfg config.Settings) (Result, error) {
	key := k.Get(keys.OpenAIKey)
	if key == "" {
		return Result{}, ErrNotConfigured
	}
	target := englishName(req.TargetLang)
	if target == "" {
		return Result{}, unsupported("OpenAI", req.TargetLang)
	}
	base := strings.TrimSuffix(cfg.Services.OpenAI.BaseURL, "/")
	if base == "" {
		base = "https://api.openai.com/v1"
	}
	model := cfg.Services.OpenAI.Model
	if model == "" {
		model = "gpt-4o-mini"
	}
	// The model detects the source language itself, so the prompt only
	// names the target; the reply carries the translation and nothing else.
	system := fmt.Sprintf(
		"You are a translation engine. Translate the user's text into %s. "+
			"Reply with the translation only: no quotes, no explanations. "+
			"If the text is already in %s, reply with it unchanged.", target, target)
	payload := map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": req.Text},
		},
		"temperature": 0.1,
	}
	choice, err := requestChatCompletion(ctx, o.Client, base+"/chat/completions", key, payload, "OpenAI")
	if err != nil {
		return Result{}, err
	}
	// The reply may wrap the translation in quotes despite the prompt.
	text := strings.TrimSpace(choice.Message.Content)
	text = strings.Trim(text, "\"'“”‘’")
	return Result{Service: o.Name(), Text: text}, nil
}

type chatChoice struct {
	FinishReason string `json:"finish_reason"`
	Message      struct {
		Content string `json:"content"`
	} `json:"message"`
}

var defaultChatClient = &http.Client{Timeout: 30 * time.Second}

// requestChatCompletion shares the wire protocol between independent services.
func requestChatCompletion(ctx context.Context, client *http.Client, endpoint, key string, payload map[string]any, service string) (chatChoice, error) {
	if client == nil {
		client = defaultChatClient
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return chatChoice{}, fmt.Errorf("%s：构造请求：%w", service, err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return chatChoice{}, fmt.Errorf("%s：构造请求：%w", service, err)
	}
	if key != "" {
		httpReq.Header.Set("Authorization", "Bearer "+key)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(httpReq)
	if err != nil {
		return chatChoice{}, fmt.Errorf("%s：%w", service, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		return chatChoice{}, fmt.Errorf("%s：请求过于频繁，请稍后重试", service)
	}
	// Public gateways may return HTML or echo request data. Keep anonymous
	// service errors short and avoid suggesting credentials they cannot set.
	if status := resp.StatusCode; status != http.StatusOK && key == "" {
		switch {
		case status == http.StatusUnauthorized || status == http.StatusForbidden:
			return chatChoice{}, fmt.Errorf("%s：暂时无法匿名访问，请稍后重试或切换翻译服务", service)
		case status >= http.StatusInternalServerError:
			return chatChoice{}, fmt.Errorf("%s：服务暂时不可用，请稍后重试", service)
		default:
			return chatChoice{}, fmt.Errorf("%s：请求失败（HTTP %d）", service, status)
		}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return chatChoice{}, fmt.Errorf("%s：读取响应：%w", service, err)
	}
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == 401 {
			return chatChoice{}, ErrAuth
		}
		return chatChoice{}, fmt.Errorf("%s: HTTP %d: %s", service, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out struct {
		Choices []chatChoice `json:"choices"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return chatChoice{}, fmt.Errorf("%s：解析响应：%w", service, err)
	}
	if len(out.Choices) == 0 {
		return chatChoice{}, fmt.Errorf("%s：空响应", service)
	}
	return out.Choices[0], nil
}
