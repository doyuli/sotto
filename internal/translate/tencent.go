package translate

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
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

// The Tencent Cloud Machine Translation (TMT) endpoint. Every request
// needs the TC3-HMAC-SHA256 signature, which covers the action and the
// payload, so each one is signed fresh.
const (
	tencentEndpoint = "https://tmt.tencentcloudapi.com"
	tencentService  = "tmt"
	tencentVersion  = "2018-03-21"
	tencentRegion   = "ap-guangzhou"
	tencentAction   = "TextTranslate"
)

// Tencent is the Tencent Cloud translation service: five million
// characters a month free, credentials are a SecretId/SecretKey pair
// from the access-management console.
type Tencent struct {
	Client   *http.Client     // nil: the shared client
	Endpoint string           // overrides the API endpoint (tests)
	Now      func() time.Time // overrides the clock (tests)
}

func (Tencent) Name() string { return "tencent" }

func (Tencent) Available(k KeyStore, _ config.Settings) bool {
	return k.Get(keys.TencentSecretID) != "" && k.Get(keys.TencentSecretKey) != ""
}

func (t Tencent) Translate(ctx context.Context, req Request, k KeyStore, _ config.Settings) (Result, error) {
	id, secret := k.Get(keys.TencentSecretID), k.Get(keys.TencentSecretKey)
	if id == "" || secret == "" {
		return Result{}, ErrNotConfigured
	}
	target, ok := mapLanguage("tencent", req.TargetLang)
	if !ok {
		return Result{}, unsupported("腾讯云", req.TargetLang)
	}
	from := "auto"
	if req.SourceLang != "" && req.SourceLang != "auto" {
		if src, ok := mapLanguage("tencent", req.SourceLang); ok {
			from = src
		}
	}
	payload, err := json.Marshal(map[string]any{
		"Source":    from,
		"Target":    target,
		"ProjectId": 0,
		"Text":      req.Text,
	})
	if err != nil {
		return Result{}, err
	}
	endpoint := t.Endpoint
	if endpoint == "" {
		endpoint = tencentEndpoint
	}
	now := t.now()
	auth := tc3Authorization(id, secret, tencentService, tencentHost(endpoint), tencentAction, payload, now)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return Result{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json; charset=utf-8")
	httpReq.Header.Set("X-TC-Action", tencentAction)
	httpReq.Header.Set("X-TC-Version", tencentVersion)
	httpReq.Header.Set("X-TC-Region", tencentRegion)
	httpReq.Header.Set("X-TC-Timestamp", strconv.FormatInt(now.Unix(), 10))
	httpReq.Header.Set("Authorization", auth)
	resp, err := t.client().Do(httpReq)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Result{}, err
	}
	var out struct {
		Response struct {
			Translation string `json:"Translation"`
			Source      string `json:"Source"`
			Target      string `json:"Target"`
			Error       *struct {
				Code    string `json:"Code"`
				Message string `json:"Message"`
			} `json:"Error"`
		} `json:"Response"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return Result{}, fmt.Errorf("腾讯云: %w", err)
	}
	if e := out.Response.Error; e != nil {
		return Result{}, tencentError(e.Code, e.Message)
	}
	if out.Response.Translation == "" {
		return Result{}, fmt.Errorf("腾讯云: 空响应")
	}
	return Result{
		Service:      "tencent",
		Text:         out.Response.Translation,
		DetectedLang: fromDetected("tencent", out.Response.Source),
	}, nil
}

// tencentError maps the API's error codes; the family prefixes carry the
// part users can act on.
func tencentError(code, msg string) error {
	switch {
	case strings.HasPrefix(code, "AuthFailure"), strings.HasPrefix(code, "UnauthorizedOperation"):
		return ErrAuth
	case strings.HasPrefix(code, "LimitExceeded"), strings.HasPrefix(code, "RequestLimitExceeded"):
		return fmt.Errorf("腾讯云: 请求过于频繁")
	case strings.HasPrefix(code, "FailedOperation"):
		return fmt.Errorf("腾讯云: %s %s", code, msg)
	default:
		return fmt.Errorf("腾讯云: %s %s", code, msg)
	}
}

// tc3Authorization builds the TC3-HMAC-SHA256 Authorization header for a
// POST of payload to action at host, per Tencent Cloud's signature v3.
func tc3Authorization(secretID, secretKey, service, host, action string, payload []byte, now time.Time) string {
	date := now.UTC().Format("2006-01-02")
	timestamp := strconv.FormatInt(now.Unix(), 10)
	credentialScope := date + "/" + service + "/tc3_request"
	stringToSign := tc3StringToSign(timestamp, credentialScope,
		sha256hex([]byte(tc3CanonicalRequest(host, action, payload))))
	// The key derives date → service → the request string, key first and
	// message second in every HMAC.
	kDate := hmacSHA256([]byte("TC3"+secretKey), []byte(date))
	kService := hmacSHA256(kDate, []byte(service))
	kSigning := hmacSHA256(kService, []byte("tc3_request"))
	signature := hex.EncodeToString(hmacSHA256(kSigning, []byte(stringToSign)))
	return "TC3-HMAC-SHA256 Credential=" + secretID + "/" + credentialScope +
		", SignedHeaders=content-type;host;x-tc-action" +
		", Signature=" + signature
}

// tc3CanonicalRequest is the canonical request for a POST of payload to
// action at host: the method, the path, an empty query, the three signed
// headers in ASCII order, and the payload's hash.
func tc3CanonicalRequest(host, action string, payload []byte) string {
	return strings.Join([]string{
		"POST",
		"/",
		"",
		"content-type:application/json; charset=utf-8",
		"host:" + host,
		"x-tc-action:" + strings.ToLower(action),
		"",
		"content-type;host;x-tc-action",
		sha256hex(payload),
	}, "\n")
}

// tc3StringToSign is the string the derived signing key signs.
func tc3StringToSign(timestamp, credentialScope, hashedCanonical string) string {
	return strings.Join([]string{"TC3-HMAC-SHA256", timestamp, credentialScope, hashedCanonical}, "\n")
}

func sha256hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func hmacSHA256(key, data []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	return mac.Sum(nil)
}

// tencentHost extracts the Host header's value from the endpoint URL.
func tencentHost(endpoint string) string {
	return strings.TrimPrefix(strings.TrimPrefix(endpoint, "https://"), "http://")
}

func (t Tencent) now() time.Time {
	if t.Now != nil {
		return t.Now()
	}
	return time.Now()
}

func (t Tencent) client() *http.Client {
	if t.Client != nil {
		return t.Client
	}
	return defaultClient
}
