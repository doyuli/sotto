package translate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"sotto/internal/config"
	"sotto/internal/keys"
)

// youdaoEndpoint is the text translation API.
const youdaoEndpoint = "https://openapi.youdao.com/api"

// Youdao is the Youdao Zhiyun translation service. Its v3 signature is
// sha256 over appKey + a truncated query + salt + curtime + secret, and
// the truncation rule is the API's own: a query longer than 20 characters
// stands in as its first ten, its length, and its last ten.
type Youdao struct {
	Client   *http.Client     // nil: the shared client
	Endpoint string           // overrides the API endpoint (tests)
	Now      func() time.Time // overrides the clock (tests)
}

func (Youdao) Name() string { return "youdao" }

func (Youdao) Available(k KeyStore, _ config.Settings) bool {
	return k.Get(keys.YoudaoAppKey) != "" && k.Get(keys.YoudaoSecret) != ""
}

func (y Youdao) Translate(ctx context.Context, req Request, k KeyStore, _ config.Settings) (Result, error) {
	appKey, secret := k.Get(keys.YoudaoAppKey), k.Get(keys.YoudaoSecret)
	if appKey == "" || secret == "" {
		return Result{}, ErrNotConfigured
	}
	target, ok := mapLanguage("youdao", req.TargetLang)
	if !ok {
		return Result{}, unsupported("有道", req.TargetLang)
	}
	from := "auto"
	if req.SourceLang != "" && req.SourceLang != "auto" {
		if src, ok := mapLanguage("youdao", req.SourceLang); ok {
			from = src
		}
	}
	now := y.now()
	salt := strconv.FormatInt(now.UnixNano(), 36)
	curtime := strconv.FormatInt(now.Unix(), 10)
	sign := youdaoSign(appKey, req.Text, salt, curtime, secret)
	form := url.Values{
		"q":        {req.Text},
		"from":     {from},
		"to":       {target},
		"appKey":   {appKey},
		"salt":     {salt},
		"sign":     {sign},
		"signType": {"v3"},
		"curtime":  {curtime},
	}
	endpoint := y.Endpoint
	if endpoint == "" {
		endpoint = youdaoEndpoint
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return Result{}, err
	}
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := y.client().Do(httpReq)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Result{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return Result{}, fmt.Errorf("有道: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out struct {
		ErrorCode   string   `json:"errorCode"`
		Translation []string `json:"translation"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return Result{}, fmt.Errorf("有道: %w", err)
	}
	if out.ErrorCode != "" && out.ErrorCode != "0" {
		return Result{}, youdaoError(out.ErrorCode)
	}
	if len(out.Translation) == 0 {
		return Result{}, fmt.Errorf("有道: 空响应")
	}
	return Result{
		Service: "youdao",
		Text:    strings.Join(out.Translation, "\n"),
	}, nil
}

// youdaoInput is the truncated form of the query inside the signature:
// at most twenty characters go in whole, longer ones as first ten +
// length + last ten, counted in characters, not bytes.
func youdaoInput(q string) string {
	runes := []rune(q)
	if len(runes) <= 20 {
		return q
	}
	return string(runes[:10]) + strconv.Itoa(len(runes)) + string(runes[len(runes)-10:])
}

// youdaoSign is the v3 signature: sha256(appKey + input + salt + curtime
// + secret), lower case hex.
func youdaoSign(appKey, q, salt, curtime, secret string) string {
	sum := sha256.Sum256([]byte(appKey + youdaoInput(q) + salt + curtime + secret))
	return hex.EncodeToString(sum[:])
}

// youdaoError maps the API's error codes; only the ones users can act on
// get their own error.
func youdaoError(code string) error {
	switch code {
	case "103", "108", "110", "111":
		return ErrAuth
	case "112":
		return ErrQuota
	case "411", "412":
		return fmt.Errorf("有道: 请求过于频繁")
	default:
		return fmt.Errorf("有道: 错误码 %s", code)
	}
}

func (y Youdao) now() time.Time {
	if y.Now != nil {
		return y.Now()
	}
	return time.Now()
}

func (y Youdao) client() *http.Client {
	if y.Client != nil {
		return y.Client
	}
	return defaultClient
}
