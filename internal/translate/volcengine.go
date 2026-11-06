package translate

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"sotto/internal/config"
	"sotto/internal/keys"
)

const (
	volcEndpoint = "https://translate.volcengineapi.com/?Action=TranslateText&Version=2020-06-01"
	volcRegion   = "cn-north-1"
	volcService  = "translate"
)

// Volcengine calls the TranslateText API using an AK/SK signature.
type Volcengine struct {
	Client   *http.Client
	Endpoint string // overrides the endpoint (tests)
}

func (Volcengine) Name() string { return "volcengine" }

func (Volcengine) Available(k KeyStore, _ config.Settings) bool {
	return k.Get(keys.VolcAccessKey) != "" && k.Get(keys.VolcSecretKey) != ""
}

func (v Volcengine) Translate(ctx context.Context, req Request, k KeyStore, _ config.Settings) (Result, error) {
	access, secret := k.Get(keys.VolcAccessKey), k.Get(keys.VolcSecretKey)
	if access == "" || secret == "" {
		return Result{}, ErrNotConfigured
	}
	target, ok := mapLanguage(v.Name(), req.TargetLang)
	if !ok {
		return Result{}, unsupported("火山引擎", req.TargetLang)
	}
	source, _ := mapLanguage(v.Name(), req.SourceLang)
	body, err := json.Marshal(struct {
		SourceLanguage string `json:"SourceLanguage,omitempty"`
		TargetLanguage string
		TextList       []string
	}{source, target, []string{req.Text}})
	if err != nil {
		return Result{}, fmt.Errorf("火山引擎：构造请求：%w", err)
	}
	endpoint := v.Endpoint
	if endpoint == "" {
		endpoint = volcEndpoint
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return Result{}, fmt.Errorf("火山引擎：构造请求：%w", err)
	}
	volcSign(httpReq, body, access, secret, time.Now())
	client := v.Client
	if client == nil {
		client = defaultClient
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return Result{}, fmt.Errorf("火山引擎：%w", err)
	}
	defer resp.Body.Close()

	var out struct {
		ResponseMetadata struct {
			Error *struct {
				Code  string
				CodeN int
			}
		}
		TranslationList []struct {
			Translation            string
			DetectedSourceLanguage string
		}
	}
	decodeErr := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out)
	if apiErr := out.ResponseMetadata.Error; decodeErr == nil && apiErr != nil {
		return Result{}, volcError(resp.StatusCode, apiErr.Code, apiErr.CodeN)
	}
	if resp.StatusCode != http.StatusOK {
		return Result{}, volcError(resp.StatusCode, "", 0)
	}
	if decodeErr != nil {
		return Result{}, fmt.Errorf("火山引擎：解析响应：%w", decodeErr)
	}
	if len(out.TranslationList) != 1 || strings.TrimSpace(out.TranslationList[0].Translation) == "" {
		return Result{}, fmt.Errorf("火山引擎：未返回译文")
	}
	tr := out.TranslationList[0]
	return Result{
		Service:      v.Name(),
		Text:         tr.Translation,
		DetectedLang: fromDetected(v.Name(), tr.DetectedSourceLanguage),
	}, nil
}

func volcSign(req *http.Request, body []byte, access, secret string, now time.Time) {
	const algorithm = "HMAC-SHA256"
	const signedHeaders = "content-type;host;x-content-sha256;x-date"
	const contentType = "application/json"

	date := now.UTC().Format("20060102T150405Z")
	bodyHash := sha256hex(body)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("X-Date", date)
	req.Header.Set("X-Content-Sha256", bodyHash)

	// Canonical headers end in a newline before the signed header names.
	headers := fmt.Sprintf("content-type:%s\nhost:%s\nx-content-sha256:%s\nx-date:%s\n",
		contentType, req.Host, bodyHash, date)
	query := strings.ReplaceAll(req.URL.Query().Encode(), "+", "%20")
	canonical := strings.Join([]string{req.Method, req.URL.EscapedPath(), query, headers, signedHeaders, bodyHash}, "\n")
	scope := strings.Join([]string{date[:8], volcRegion, volcService, "request"}, "/")
	toSign := strings.Join([]string{algorithm, date, scope, sha256hex([]byte(canonical))}, "\n")

	key := []byte(secret)
	for _, part := range []string{date[:8], volcRegion, volcService, "request"} {
		key = hmacSHA256(key, []byte(part))
	}
	signature := hex.EncodeToString(hmacSHA256(key, []byte(toSign)))
	req.Header.Set("Authorization", fmt.Sprintf("%s Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		algorithm, access, scope, signedHeaders, signature))
}

func volcError(status int, code string, number int) error {
	switch code {
	case "InvalidAccessKey", "SignatureDoesNotMatch", "InvalidCredential":
		return ErrAuth
	case "AccessDenied":
		return ErrAccess
	case "InvalidTimestamp":
		return fmt.Errorf("火山引擎：请检查系统时间后重试")
	case "-415":
		return fmt.Errorf("火山引擎：不支持当前语种组合")
	case "-429", "FlowLimitExceeded":
		return fmt.Errorf("火山引擎：请求过于频繁，请稍后重试")
	}
	switch {
	case number == -415:
		return fmt.Errorf("火山引擎：不支持当前语种组合")
	case status == http.StatusUnauthorized:
		return ErrAuth
	case status == http.StatusForbidden:
		return ErrAccess
	case status == http.StatusPaymentRequired:
		return fmt.Errorf("火山引擎：请检查账户余额")
	case status == http.StatusTooManyRequests || number == -429:
		return fmt.Errorf("火山引擎：请求过于频繁，请稍后重试")
	case code != "":
		return fmt.Errorf("火山引擎：翻译失败（%s）", code)
	default:
		return fmt.Errorf("火山引擎：翻译失败（HTTP %d）", status)
	}
}
