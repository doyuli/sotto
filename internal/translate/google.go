package translate

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"sotto/internal/config"
)

// googleEndpoint is the free, keyless translate endpoint browsers use. It
// rate limits and some networks cannot reach it, so Google is strictly
// best effort: no retries, a short timeout, and the bubble shows whatever
// failure came.
const googleEndpoint = "https://translate.googleapis.com/translate_a/single"

// Google translates through the keyless endpoint.
type Google struct {
	Client   *http.Client // nil: the shared client
	Endpoint string       // overrides the endpoint (tests)
}

func (Google) Name() string { return "google" }

// Available is always true: no key, no configuration.
func (Google) Available(KeyStore, config.Settings) bool { return true }

func (g Google) Translate(ctx context.Context, req Request, _ KeyStore, _ config.Settings) (Result, error) {
	target, ok := mapLanguage("google", req.TargetLang)
	if !ok {
		return Result{}, unsupported("Google", req.TargetLang)
	}
	source := "auto"
	if req.SourceLang != "" && req.SourceLang != "auto" {
		if src, ok := mapLanguage("google", req.SourceLang); ok {
			source = src
		}
	}
	endpoint := g.Endpoint
	if endpoint == "" {
		endpoint = googleEndpoint
	}
	// Best effort: a hard 5s ceiling, no retry.
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	u := endpoint + "?client=gtx&dt=t" +
		"&sl=" + url.QueryEscape(source) +
		"&tl=" + url.QueryEscape(target) +
		"&q=" + url.QueryEscape(req.Text)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return Result{}, err
	}
	resp, err := g.client().Do(httpReq)
	if err != nil {
		return Result{}, fmt.Errorf("Google: 网络不可达（可能需要代理）")
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Result{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return Result{}, fmt.Errorf("Google: HTTP %d", resp.StatusCode)
	}
	// The reply is an untyped nested array: [[["你好","Hello",...]],...,"en",...]
	var data []any
	if err := json.Unmarshal(body, &data); err != nil {
		return Result{}, fmt.Errorf("Google: %w", err)
	}
	if len(data) == 0 {
		return Result{}, fmt.Errorf("Google: 空响应")
	}
	var b strings.Builder
	if segments, ok := data[0].([]any); ok {
		for _, seg := range segments {
			parts, ok := seg.([]any)
			if !ok || len(parts) == 0 {
				continue
			}
			if text, ok := parts[0].(string); ok {
				b.WriteString(text)
			}
		}
	}
	text := b.String()
	if text == "" {
		return Result{}, fmt.Errorf("Google: 空响应")
	}
	detected := ""
	if len(data) > 2 {
		if code, ok := data[2].(string); ok {
			detected = fromDetected("google", code)
		}
	}
	return Result{Service: "google", Text: text, DetectedLang: detected}, nil
}

func (g Google) client() *http.Client {
	if g.Client != nil {
		return g.Client
	}
	return defaultClient
}
