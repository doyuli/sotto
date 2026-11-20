package translate

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"sotto/internal/config"
	"sotto/internal/usermessage"
)

const (
	youdaoWebName     = "有道查词与翻译"
	youdaoWebHome     = "https://fanyi.youdao.com"
	youdaoWebAPI      = "https://dict.youdao.com"
	youdaoWebClient   = "fanyideskweb"
	youdaoWebProduct  = "webfanyi"
	youdaoWebKeySeed  = "dsfdsfdfanknefjnjdsa"
	youdaoWebDictSeed = "uL1GrRjYUMbJTt36SotGGD33pUtqh6kM"
)

// YoudaoWeb uses the keyless web translator and the V4 web dictionary.
type YoudaoWeb struct {
	client   *http.Client
	homepage string
	endpoint string
	session  *youdaoWebSession
}

type youdaoWebSession struct {
	client *http.Client
	secret string
	aesKey string
	aesIV  string
}

func (YoudaoWeb) Name() string { return "youdaoweb" }

func (YoudaoWeb) Available(KeyStore, config.Settings) bool { return true }

func (y YoudaoWeb) forRun() Translator {
	// Chunks run sequentially and share this job's cookie jar and keys.
	y.session = &youdaoWebSession{}
	return y
}

func (y YoudaoWeb) Translate(ctx context.Context, req Request, _ KeyStore, _ config.Settings) (Result, error) {
	if y.client == nil {
		y.client = defaultClient
	}
	if y.endpoint == "" {
		y.endpoint = youdaoWebAPI
	}
	if y.homepage == "" {
		y.homepage = youdaoWebHome
	}
	if req.TargetLang == "zh-HANS" && req.allowsEnglishLookup() {
		return y.lookup(ctx, req.Text)
	}
	return y.translateText(ctx, req)
}

func (y YoudaoWeb) lookup(ctx context.Context, word string) (Result, error) {
	const keyFrom = "webdict"
	salted := word + keyFrom
	length := strconv.Itoa(utf8.RuneCountInString(salted) % 10)
	seed := reverseASCII(youdaoWebDictSeed)
	form := url.Values{
		"q":       {word},
		"le":      {"en"},
		"client":  {"web"},
		"t":       {length},
		"sign":    {youdaoWebMD5("web" + word + length + seed + youdaoWebMD5(salted))},
		"keyfrom": {keyFrom},
	}
	body, err := youdaoWebRequest(ctx, y.client, http.MethodPost, y.endpoint+"/jsonapi_s?doctype=json&jsonversion=4", form)
	if err != nil {
		return Result{}, usermessage.Explain(err, "有道词典查询失败，请稍后重试")
	}
	var response struct {
		EC struct {
			Word struct {
				USPhone string `json:"usphone"`
				UKPhone string `json:"ukphone"`
				Entries []struct {
					Part        string `json:"pos"`
					Translation string `json:"tran"`
				} `json:"trs"`
			} `json:"word"`
		} `json:"ec"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return Result{}, usermessage.Explain(fmt.Errorf("decode Youdao dictionary: %w", err), "有道词典查询失败，请稍后重试")
	}
	var meanings []string
	for _, definition := range response.EC.Word.Entries {
		if value := strings.TrimSpace(definition.Translation); value != "" {
			meanings = append(meanings, strings.TrimSpace(definition.Part+" "+value))
		}
	}
	if len(meanings) == 0 {
		return Result{}, usermessage.New("有道词典未找到该词条")
	}
	entry := &DictionaryEntry{
		Word: word, Meanings: strings.Join(meanings, "\n"),
		UKPhonetic: strings.TrimSpace(response.EC.Word.UKPhone),
		USPhonetic: strings.TrimSpace(response.EC.Word.USPhone),
	}
	return Result{Service: y.Name(), Dictionary: entry, Text: entry.text()}, nil
}

func (y YoudaoWeb) translateText(ctx context.Context, req Request) (Result, error) {
	// Both Youdao APIs use the same codes for Sotto's supported languages.
	to, ok := mapLanguage("youdao", req.TargetLang)
	if !ok {
		return Result{}, unsupported(youdaoWebName, req.TargetLang)
	}
	from := "auto"
	if code, ok := mapLanguage("youdao", req.SourceLang); ok {
		from = code
	}

	session := y.session
	if session == nil {
		session = &youdaoWebSession{}
	}
	if session.client == nil {
		opened, err := y.openSession(ctx)
		if err != nil {
			return Result{}, err
		}
		*session = opened
	}
	form := youdaoWebParams(youdaoWebProduct, session.secret)
	form.Set("i", req.Text)
	form.Set("from", from)
	form.Set("to", to)
	form.Set("dictResult", "false")
	body, err := youdaoWebRequest(ctx, session.client, http.MethodPost, y.endpoint+"/webtranslate", form)
	if err != nil {
		return Result{}, err
	}
	return y.decodeTranslation(body, session.aesKey, session.aesIV)
}

func (y YoudaoWeb) openSession(ctx context.Context) (youdaoWebSession, error) {
	// The normal web session supplies its own cookie. Keep it scoped to this
	// client so requests never reuse another provider's session.
	client := *y.client
	jar, err := cookiejar.New(nil)
	if err != nil {
		return youdaoWebSession{}, fmt.Errorf("create Youdao web session: %w", err)
	}
	client.Jar = jar
	if _, err := youdaoWebRequest(ctx, &client, http.MethodGet, y.homepage, nil); err != nil {
		return youdaoWebSession{}, fmt.Errorf("open Youdao web session: %w", err)
	}
	keyParams := youdaoWebParams("webfanyi-key-getter", reverseASCII(youdaoWebKeySeed))
	keyBody, err := youdaoWebRequest(ctx, &client, http.MethodGet, y.endpoint+"/webtranslate/key", keyParams)
	if err != nil {
		return youdaoWebSession{}, fmt.Errorf("get Youdao web session key: %w", err)
	}
	var keys struct {
		Code int `json:"code"`
		Data struct {
			Secret string `json:"secretKey"`
			AESKey string `json:"aesKey"`
			AESIV  string `json:"aesIv"`
		} `json:"data"`
	}
	if err := json.Unmarshal(keyBody, &keys); err != nil {
		return youdaoWebSession{}, fmt.Errorf("decode Youdao web session key: %w", err)
	}
	if keys.Code != 0 {
		return youdaoWebSession{}, responseCodeError(youdaoWebName, strconv.Itoa(keys.Code))
	}
	if keys.Data.Secret == "" || keys.Data.AESKey == "" || keys.Data.AESIV == "" {
		return youdaoWebSession{}, usermessage.New(youdaoWebName + "未返回完整的会话信息，请稍后重试")
	}
	return youdaoWebSession{client: &client, secret: keys.Data.Secret, aesKey: keys.Data.AESKey, aesIV: keys.Data.AESIV}, nil
}

func (y YoudaoWeb) decodeTranslation(body []byte, key, iv string) (Result, error) {
	encoded := strings.TrimRight(strings.TrimSpace(string(body)), "=")
	data, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return Result{}, usermessage.Explain(err, youdaoWebName+"返回的数据格式异常，请稍后重试")
	}
	if len(data) == 0 || len(data)%aes.BlockSize != 0 {
		return Result{}, usermessage.New(youdaoWebName + "返回的数据长度异常，请稍后重试")
	}
	keyHash, ivHash := md5.Sum([]byte(key)), md5.Sum([]byte(iv))
	block, err := aes.NewCipher(keyHash[:])
	if err != nil {
		return Result{}, fmt.Errorf("create Youdao web cipher: %w", err)
	}
	cipher.NewCBCDecrypter(block, ivHash[:]).CryptBlocks(data, data)
	padding := int(data[len(data)-1])
	if padding == 0 || padding > aes.BlockSize {
		return Result{}, usermessage.New(youdaoWebName + "返回的数据格式异常，请稍后重试")
	}
	for _, b := range data[len(data)-padding:] {
		if int(b) != padding {
			return Result{}, usermessage.New(youdaoWebName + "返回的数据格式异常，请稍后重试")
		}
	}
	var response struct {
		Code   int    `json:"code"`
		Type   string `json:"type"`
		Groups [][]struct {
			Target string `json:"tgt"`
		} `json:"translateResult"`
	}
	if err := json.Unmarshal(data[:len(data)-padding], &response); err != nil {
		return Result{}, fmt.Errorf("decode Youdao web translation: %w", err)
	}
	if response.Code != 0 {
		return Result{}, responseCodeError(youdaoWebName, strconv.Itoa(response.Code))
	}
	var text strings.Builder
	for _, group := range response.Groups {
		for _, part := range group {
			text.WriteString(part.Target)
		}
	}
	if strings.TrimSpace(text.String()) == "" {
		return Result{}, usermessage.New(youdaoWebName + "未返回译文，请稍后重试")
	}
	source, _, _ := strings.Cut(response.Type, "2")
	return Result{Service: y.Name(), Text: text.String(), DetectedLang: fromDetected("youdao", source)}, nil
}

func youdaoWebParams(keyID, key string) url.Values {
	timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
	sign := youdaoWebMD5("client=" + youdaoWebClient + "&mysticTime=" + timestamp + "&product=" + youdaoWebProduct + "&key=" + key)
	return url.Values{
		"client":     {youdaoWebClient},
		"product":    {youdaoWebProduct},
		"appVersion": {"1.0.0"},
		"vendor":     {"web"},
		"pointParam": {"client,mysticTime,product"},
		"keyfrom":    {"fanyi.web"},
		"keyid":      {keyID},
		"mysticTime": {timestamp},
		"sign":       {sign},
	}
}

func youdaoWebMD5(text string) string {
	sum := md5.Sum([]byte(text))
	return hex.EncodeToString(sum[:])
}

func reverseASCII(s string) string {
	b := []byte(s)
	slices.Reverse(b)
	return string(b)
}

func youdaoWebRequest(ctx context.Context, client *http.Client, method, endpoint string, params url.Values) ([]byte, error) {
	var body io.Reader
	if method == http.MethodPost {
		body = strings.NewReader(params.Encode())
	} else if len(params) != 0 {
		endpoint += "?" + params.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, fmt.Errorf("create Youdao web request: %w", err)
	}
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.Header.Set("Referer", youdaoWebHome)
	req.Header.Set("User-Agent", "Mozilla/5.0")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send Youdao web request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, responseStatusError(youdaoWebName, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, fmt.Errorf("read Youdao web response: %w", err)
	}
	return data, nil
}
