package translate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"sotto/internal/config"
	"sotto/internal/keys"
)

func TestOpenAITranslate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer k" {
			t.Errorf("auth = %q", got)
		}
		var in struct {
			Model    string `json:"model"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			t.Errorf("bad request: %v", err)
		}
		if in.Model != "m" {
			t.Errorf("model = %q, want m", in.Model)
		}
		if len(in.Messages) != 2 || in.Messages[0].Role != "system" || in.Messages[1].Content != "hello" {
			t.Errorf("messages = %+v", in.Messages)
		}
		if !strings.Contains(in.Messages[0].Content, "Simplified Chinese") {
			t.Errorf("system prompt = %q, want the target language in English", in.Messages[0].Content)
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":"\u4f60\u597d"}}]}`) // "你好"
	}))
	t.Cleanup(srv.Close)
	cfg := config.Default()
	cfg.Services.OpenAI.BaseURL = srv.URL + "/v1"
	cfg.Services.OpenAI.Model = "m"
	o := OpenAI{Client: srv.Client()}
	res, err := o.Translate(context.Background(), Request{Text: "hello", TargetLang: "zh-HANS"}, fakeKeys{keys.OpenAIKey: "k"}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "你好" || res.Service != "openai" {
		t.Errorf("res = %+v", res)
	}
}

func TestOpenAIQuotesStripped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"choices":[{"message":{"content":"\"hello\""}}]}`)
	}))
	t.Cleanup(srv.Close)
	cfg := config.Default()
	cfg.Services.OpenAI.BaseURL = srv.URL
	o := OpenAI{Client: srv.Client()}
	res, err := o.Translate(context.Background(), Request{Text: "你好", TargetLang: "en"}, fakeKeys{keys.OpenAIKey: "k"}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "hello" {
		t.Errorf("text = %q, want quotes stripped", res.Text)
	}
}

func TestOpenAIAuthError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
	}))
	t.Cleanup(srv.Close)
	cfg := config.Default()
	cfg.Services.OpenAI.BaseURL = srv.URL
	o := OpenAI{Client: srv.Client()}
	_, err := o.Translate(context.Background(), Request{Text: "x", TargetLang: "en"}, fakeKeys{keys.OpenAIKey: "k"}, cfg)
	if !errors.Is(err, ErrAuth) {
		t.Errorf("err = %v, want ErrAuth", err)
	}
}

func TestBaiduSign(t *testing.T) {
	// sign = md5(appid + q + salt + secret), lower case hex; the digest is
	// the input's, verified against an independent md5sum.
	got := baiduSign("2015083000000001", "apple", "1435660288", "secret")
	want := "bc83860fcf143d9109ee1e0296d03e5d"
	if got != want {
		t.Errorf("sign = %q, want %q", got, want)
	}
}

func TestBaiduTranslate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("bad form: %v", err)
		}
		if got := r.PostForm.Get("to"); got != "zh" {
			t.Errorf("to = %q, want zh", got)
		}
		if got := r.PostForm.Get("from"); got != "auto" {
			t.Errorf("from = %q, want auto", got)
		}
		if got := r.PostForm.Get("appid"); got != "appid-1" {
			t.Errorf("appid = %q", got)
		}
		if got := r.PostForm.Get("sign"); got != baiduSign("appid-1", r.PostForm.Get("q"), r.PostForm.Get("salt"), "secret-1") {
			t.Errorf("sign = %q, want the MD5 signature", got)
		}
		fmt.Fprint(w, `{"from":"en","to":"zh","trans_result":[{"src":"hello","dst":"你好"},{"src":"world","dst":"世界"}]}`)
	}))
	t.Cleanup(srv.Close)
	b := Baidu{Client: srv.Client(), Endpoint: srv.URL}
	res, err := b.Translate(context.Background(), Request{Text: "hello\nworld", TargetLang: "zh-HANS"}, fakeKeys{keys.BaiduAppID: "appid-1", keys.BaiduKey: "secret-1"}, config.Settings{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "你好\n世界" {
		t.Errorf("text = %q", res.Text)
	}
	if res.DetectedLang != "en" {
		t.Errorf("detected = %q, want en", res.DetectedLang)
	}
}

func TestBaiduErrors(t *testing.T) {
	tests := []struct {
		code    string
		wantErr error
	}{
		{"52001", context.DeadlineExceeded},
		{"54001", ErrAuth},
		{"58002", ErrAccess},
	}
	for _, tt := range tests {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(w, `{"error_code":"%s","error_msg":"x"}`, tt.code)
		}))
		b := Baidu{Client: srv.Client(), Endpoint: srv.URL}
		_, err := b.Translate(context.Background(), Request{Text: "x", TargetLang: "en"}, fakeKeys{keys.BaiduAppID: "a", keys.BaiduKey: "s"}, config.Settings{})
		if !errors.Is(err, tt.wantErr) {
			t.Errorf("code %s: err = %v, want %v", tt.code, err, tt.wantErr)
		}
		srv.Close()
	}
}

func TestGoogleTranslate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if got := q.Get("client"); got != "gtx" {
			t.Errorf("client = %q", got)
		}
		if got := q.Get("tl"); got != "zh-CN" {
			t.Errorf("tl = %q, want zh-CN", got)
		}
		if got := q.Get("sl"); got != "auto" {
			t.Errorf("sl = %q, want auto", got)
		}
		if got := q.Get("q"); got != "hello world" {
			t.Errorf("q = %q", got)
		}
		fmt.Fprint(w, `[[["你好","hello",null,null,10],["世界","world",null,null,10]],null,"en",null,null,null,null]`)
	}))
	t.Cleanup(srv.Close)
	g := Google{Client: srv.Client(), Endpoint: srv.URL + "/translate_a/single"}
	res, err := g.Translate(context.Background(), Request{Text: "hello world", TargetLang: "zh-HANS"}, fakeKeys{}, config.Settings{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "你好世界" {
		t.Errorf("text = %q", res.Text)
	}
	if res.DetectedLang != "en" {
		t.Errorf("detected = %q", res.DetectedLang)
	}
}

func TestGoogleAvailableWithoutKey(t *testing.T) {
	if !(Google{}).Available(fakeKeys{}, config.Settings{}) {
		t.Error("google should always be available")
	}
}
