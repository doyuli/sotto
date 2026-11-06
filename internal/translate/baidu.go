package translate

import (
	"context"
	"crypto/md5"
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

	"golang.org/x/time/rate"
)

// baiduEndpoint is the general translation API.
const baiduEndpoint = "https://fanyi-api.baidu.com/api/trans/vip/translate"

// Share the free tier's one-query-per-second budget across bubbles and
// connection tests. A fresh limiter per run would allow concurrent bursts.
var baiduLimiter = rate.NewLimiter(rate.Limit(1), 1)

// Baidu is the friendliest keyed service for users in China: the standard
// tier is free and only signs its requests.
type Baidu struct {
	Client   *http.Client  // nil: the shared client
	Limiter  *rate.Limiter // nil: unlimited (tests)
	Endpoint string        // overrides the API endpoint (tests)
}

func (Baidu) Name() string { return "baidu" }

func (Baidu) Available(k KeyStore, _ config.Settings) bool {
	return k.Get(keys.BaiduAppID) != "" && k.Get(keys.BaiduKey) != ""
}

func (b Baidu) Translate(ctx context.Context, req Request, k KeyStore, _ config.Settings) (Result, error) {
	appid, secret := k.Get(keys.BaiduAppID), k.Get(keys.BaiduKey)
	if appid == "" || secret == "" {
		return Result{}, ErrNotConfigured
	}
	target, ok := mapLanguage("baidu", req.TargetLang)
	if !ok {
		return Result{}, unsupported("百度", req.TargetLang)
	}
	from := "auto"
	if req.SourceLang != "" && req.SourceLang != "auto" {
		if src, ok := mapLanguage("baidu", req.SourceLang); ok {
			from = src
		}
	}
	if b.Limiter != nil {
		if err := b.Limiter.Wait(ctx); err != nil {
			return Result{}, err
		}
	}
	salt := strconv.FormatInt(time.Now().UnixNano(), 36)
	form := url.Values{
		"q":     {req.Text},
		"from":  {from},
		"to":    {target},
		"appid": {appid},
		"salt":  {salt},
		"sign":  {baiduSign(appid, req.Text, salt, secret)},
	}
	endpoint := b.Endpoint
	if endpoint == "" {
		endpoint = baiduEndpoint
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return Result{}, err
	}
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := b.client().Do(httpReq)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Result{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return Result{}, fmt.Errorf("百度: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out struct {
		From        string `json:"from"`
		To          string `json:"to"`
		TransResult []struct {
			Src string `json:"src"`
			Dst string `json:"dst"`
		} `json:"trans_result"`
		ErrorCode string `json:"error_code"`
		ErrorMsg  string `json:"error_msg"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return Result{}, fmt.Errorf("百度: %w", err)
	}
	if out.ErrorCode != "" {
		return Result{}, baiduError(out.ErrorCode, out.ErrorMsg)
	}
	var lines []string
	for _, tr := range out.TransResult {
		lines = append(lines, tr.Dst)
	}
	if len(lines) == 0 {
		return Result{}, fmt.Errorf("百度: 空响应")
	}
	return Result{
		Service:      "baidu",
		Text:         strings.Join(lines, "\n"),
		DetectedLang: fromDetected("baidu", out.From),
	}, nil
}

// baiduSign is the API's MD5 signature: appid + query + salt + secret.
func baiduSign(appid, query, salt, secret string) string {
	sum := md5.Sum([]byte(appid + query + salt + secret))
	return hex.EncodeToString(sum[:])
}

// baiduError maps the API's error codes; only the ones users can act on
// get their own error.
func baiduError(code, msg string) error {
	switch code {
	case "52001":
		return context.DeadlineExceeded
	case "52002":
		return fmt.Errorf("百度翻译暂时不可用，请稍后重试")
	case "52003":
		return fmt.Errorf("请检查百度翻译的 APP ID，并确认已开通通用文本翻译")
	case "54001":
		return ErrAuth
	case "54003":
		return fmt.Errorf("百度: 请求过于频繁")
	case "54004":
		return fmt.Errorf("百度翻译账户余额不足，请前往控制台检查")
	case "58002":
		return ErrAccess
	case "58003":
		return fmt.Errorf("当前 IP 无法调用百度翻译，请检查控制台")
	default:
		return fmt.Errorf("百度: %s %s", code, msg)
	}
}

func (b Baidu) client() *http.Client {
	if b.Client != nil {
		return b.Client
	}
	return defaultClient
}
