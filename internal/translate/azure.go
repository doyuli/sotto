package translate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"sotto/internal/config"
	"sotto/internal/keys"
)

// azureEndpoint is the Translator v3 endpoint. Regional resources also
// send their region header; global ones work without it.
const azureEndpoint = "https://api.cognitive.microsofttranslator.com"

// Azure is the Microsoft Translator service: two million characters a
// month free, credentials a subscription key plus the resource's region.
type Azure struct {
	Client   *http.Client // nil: the shared client
	Endpoint string       // overrides the API endpoint (tests)
}

func (Azure) Name() string { return "azure" }

func (Azure) Available(k KeyStore, _ config.Settings) bool {
	return k.Get(keys.AzureKey) != ""
}

func (a Azure) Translate(ctx context.Context, req Request, k KeyStore, _ config.Settings) (Result, error) {
	key := k.Get(keys.AzureKey)
	if key == "" {
		return Result{}, ErrNotConfigured
	}
	target, ok := mapLanguage("azure", req.TargetLang)
	if !ok {
		return Result{}, unsupported("微软翻译", req.TargetLang)
	}
	endpoint := a.Endpoint
	if endpoint == "" {
		endpoint = azureEndpoint
	}
	q := url.Values{"api-version": {"3.0"}, "to": {target}}
	if req.SourceLang != "" && req.SourceLang != "auto" {
		if src, ok := mapLanguage("azure", req.SourceLang); ok {
			q.Set("from", src)
		}
	}
	payload, err := json.Marshal([]map[string]string{{"Text": req.Text}})
	if err != nil {
		return Result{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/translate?"+q.Encode(), bytes.NewReader(payload))
	if err != nil {
		return Result{}, err
	}
	httpReq.Header.Set("Ocp-Apim-Subscription-Key", key)
	// The region header is required for regional resources and ignored by
	// global ones, where "global" is what the portal calls the field.
	if region := k.Get(keys.AzureRegion); region != "" && !strings.EqualFold(region, "global") {
		httpReq.Header.Set("Ocp-Apim-Subscription-Region", region)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := a.client().Do(httpReq)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Result{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return Result{}, azureError(resp.StatusCode, body)
	}
	var out []struct {
		DetectedLanguage struct {
			Language string `json:"language"`
		} `json:"detectedLanguage"`
		Translations []struct {
			Text string `json:"text"`
			To   string `json:"to"`
		} `json:"translations"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return Result{}, fmt.Errorf("微软翻译: %w", err)
	}
	if len(out) == 0 || len(out[0].Translations) == 0 {
		return Result{}, fmt.Errorf("微软翻译: 空响应")
	}
	return Result{
		Service:      "azure",
		Text:         out[0].Translations[0].Text,
		DetectedLang: fromDetected("azure", out[0].DetectedLanguage.Language),
	}, nil
}

// azureError maps the status codes: 401 is a bad key or region, 403 the
// free tier running out, 429 the per-second rate.
func azureError(status int, body []byte) error {
	var out struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	detail := strings.TrimSpace(string(body))
	if err := json.Unmarshal(body, &out); err == nil && out.Error.Message != "" {
		detail = out.Error.Message
	}
	switch status {
	case 401:
		return ErrAuth
	case 403:
		return ErrQuota
	case 429:
		return fmt.Errorf("微软翻译: 请求过于频繁")
	default:
		return fmt.Errorf("微软翻译: HTTP %d: %s", status, detail)
	}
}

func (a Azure) client() *http.Client {
	if a.Client != nil {
		return a.Client
	}
	return defaultClient
}
