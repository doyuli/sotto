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
	data, err := json.Marshal(payload)
	if err != nil {
		return Result{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/chat/completions", bytes.NewReader(data))
	if err != nil {
		return Result{}, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+key)
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := o.client().Do(httpReq)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Result{}, err
	}
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == 401 {
			return Result{}, ErrAuth
		}
		return Result{}, fmt.Errorf("OpenAI: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return Result{}, fmt.Errorf("OpenAI: %w", err)
	}
	if len(out.Choices) == 0 {
		return Result{}, fmt.Errorf("OpenAI: 空响应")
	}
	// The reply may wrap the translation in quotes despite the prompt.
	text := strings.TrimSpace(out.Choices[0].Message.Content)
	text = strings.Trim(text, "\"'“”‘’")
	return Result{Service: "openai", Text: text}, nil
}

func (o OpenAI) client() *http.Client {
	if o.Client != nil {
		return o.Client
	}
	return &http.Client{Timeout: 30 * time.Second}
}
