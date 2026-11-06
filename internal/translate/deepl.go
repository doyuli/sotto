package translate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"sotto/internal/config"
	"sotto/internal/keys"
)

// DeepL is the primary translation service. A free API key ends in ":fx"
// and switches the host on its own, so users cannot get it wrong.
type DeepL struct {
	Client *http.Client // nil: the shared client with a 10s timeout
	Host   string       // overrides the API host (tests)
}

func (DeepL) Name() string { return "deepl" }

func (DeepL) Available(k KeyStore, _ config.Settings) bool {
	return k.Get(keys.DeeplKey) != ""
}

func (d DeepL) Translate(ctx context.Context, req Request, k KeyStore, _ config.Settings) (Result, error) {
	key := k.Get(keys.DeeplKey)
	if key == "" {
		return Result{}, ErrNotConfigured
	}
	target, ok := mapLanguage("deepl", req.TargetLang)
	if !ok {
		return Result{}, unsupported("DeepL", req.TargetLang)
	}
	payload := map[string]any{
		"text":        []string{req.Text},
		"target_lang": target,
	}
	if req.SourceLang != "" && req.SourceLang != "auto" {
		if src, ok := mapSourceLanguage("deepl", req.SourceLang); ok {
			payload["source_lang"] = src
		}
	}
	// 429 and 529 ask for a pause; Retry-After counts the seconds. 456 is
	// the quota running out, which is a stop condition, not a retry one.
	for attempt := 0; ; attempt++ {
		status, header, body, err := d.do(ctx, http.MethodPost, d.host(key)+"/v2/translate", key, payload)
		if err != nil {
			return Result{}, err
		}
		switch {
		case status == http.StatusOK:
			return d.parse(body)
		case status == 429 || status == 529:
			if attempt >= 2 {
				return Result{}, fmt.Errorf("DeepL: 请求过于频繁")
			}
			wait := time.Second
			if s := header.Get("Retry-After"); s != "" {
				if n, err := strconv.Atoi(s); err == nil && n >= 0 && n <= 30 {
					wait = time.Duration(n) * time.Second
				}
			}
			select {
			case <-time.After(wait):
			case <-ctx.Done():
				return Result{}, ctx.Err()
			}
		case status == 456:
			return Result{}, ErrQuota
		case status == 401 || status == 403:
			return Result{}, ErrAuth
		default:
			return Result{}, fmt.Errorf("DeepL: HTTP %d: %s", status, strings.TrimSpace(string(body)))
		}
	}
}

// Usage reports this month's character count and limit.
func (d DeepL) Usage(ctx context.Context, key string) (used, limit int, err error) {
	if key == "" {
		return 0, 0, ErrNotConfigured
	}
	status, _, body, err := d.do(ctx, http.MethodGet, d.host(key)+"/v2/usage", key, nil)
	if err != nil {
		return 0, 0, err
	}
	if status != http.StatusOK {
		return 0, 0, fmt.Errorf("DeepL: HTTP %d: %s", status, strings.TrimSpace(string(body)))
	}
	var out struct {
		CharacterCount int `json:"character_count"`
		CharacterLimit int `json:"character_limit"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return 0, 0, fmt.Errorf("DeepL: %w", err)
	}
	return out.CharacterCount, out.CharacterLimit, nil
}

// host picks the API host: the free tier's key ends in ":fx" and only
// works there, the pro key only on the main host.
func (d DeepL) host(key string) string {
	if d.Host != "" {
		return d.Host
	}
	if strings.HasSuffix(key, ":fx") {
		return "https://api-free.deepl.com"
	}
	return "https://api.deepl.com"
}

func (d DeepL) parse(body []byte) (Result, error) {
	var out struct {
		Translations []struct {
			Text                   string `json:"text"`
			DetectedSourceLanguage string `json:"detected_source_language"`
		} `json:"translations"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return Result{}, fmt.Errorf("DeepL: %w", err)
	}
	if len(out.Translations) == 0 {
		return Result{}, fmt.Errorf("DeepL: 空响应")
	}
	t := out.Translations[0]
	return Result{
		Service:      "deepl",
		Text:         t.Text,
		DetectedLang: fromDetected("deepl", t.DetectedSourceLanguage),
	}, nil
}

func (d DeepL) do(ctx context.Context, method, url, key string, payload any) (int, http.Header, []byte, error) {
	var rd io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return 0, nil, nil, err
		}
		rd = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rd)
	if err != nil {
		return 0, nil, nil, err
	}
	req.Header.Set("Authorization", "DeepL-Auth-Key "+key)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := d.client().Do(req)
	if err != nil {
		return 0, nil, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, resp.Header, body, err
}

func (d DeepL) client() *http.Client {
	if d.Client != nil {
		return d.Client
	}
	return defaultClient
}

var defaultClient = &http.Client{Timeout: 10 * time.Second}
