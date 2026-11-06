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

// fakeKeys is a KeyStore over a map.
type fakeKeys map[string]string

func (f fakeKeys) Get(name string) string { return f[name] }

func deeplServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, DeepL) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv, DeepL{Client: srv.Client(), Host: srv.URL}
}

func TestDeepLTranslate(t *testing.T) {
	srv, deepl := deeplServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/translate" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "DeepL-Auth-Key k" {
			t.Errorf("auth = %q", got)
		}
		var in struct {
			Text       []string `json:"text"`
			TargetLang string   `json:"target_lang"`
			SourceLang string   `json:"source_lang"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			t.Errorf("bad request: %v", err)
		}
		if len(in.Text) != 1 || in.Text[0] != "hello" {
			t.Errorf("text = %v", in.Text)
		}
		if in.TargetLang != "ZH-HANS" {
			t.Errorf("target = %q, want ZH-HANS", in.TargetLang)
		}
		if in.SourceLang != "" { // auto detection omits source_lang
			t.Errorf("source = %q, want omitted", in.SourceLang)
		}
		fmt.Fprint(w, `{"translations":[{"detected_source_language":"EN","text":"你好"}]}`)
	})
	_ = srv
	res, err := deepl.Translate(context.Background(), Request{Text: "hello", SourceLang: "auto", TargetLang: "zh-HANS"}, fakeKeys{keys.DeeplKey: "k"}, config.Settings{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "你好" || res.DetectedLang != "en" || res.Service != "deepl" {
		t.Errorf("res = %+v", res)
	}
}

func TestDeepLSourceLangMapped(t *testing.T) {
	// DeepL's source codes differ from its target codes: EN-US/PT-BR/
	// ZH-HANS are targets only, and sending them as source_lang is a 400.
	cases := []struct{ in, want string }{
		{"ja", "JA"},
		{"en", "EN"},
		{"zh-HANS", "ZH"},
		{"pt", "PT"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			_, deepl := deeplServer(t, func(w http.ResponseWriter, r *http.Request) {
				var in struct {
					SourceLang string `json:"source_lang"`
				}
				_ = json.NewDecoder(r.Body).Decode(&in)
				if in.SourceLang != tc.want {
					t.Errorf("source = %q, want %q", in.SourceLang, tc.want)
				}
				fmt.Fprint(w, `{"translations":[{"detected_source_language":"EN","text":"hello"}]}`)
			})
			_, err := deepl.Translate(context.Background(), Request{Text: "hello", SourceLang: tc.in, TargetLang: "en"}, fakeKeys{keys.DeeplKey: "k"}, config.Settings{})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestDeepLErrors(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		wantErr error
	}{
		{"quota", 456, "", ErrQuota},
		{"bad key", 401, "", ErrAuth},
		{"forbidden", 403, "", ErrAuth},
	}
	for _, tt := range tests {
		_, deepl := deeplServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tt.status)
			fmt.Fprint(w, tt.body)
		})
		_, err := deepl.Translate(context.Background(), Request{Text: "x", TargetLang: "en"}, fakeKeys{keys.DeeplKey: "k"}, config.Settings{})
		if !errors.Is(err, tt.wantErr) {
			t.Errorf("%s: err = %v, want %v", tt.name, err, tt.wantErr)
		}
	}
}

func TestDeepLRateLimitRetries(t *testing.T) {
	calls := 0
	_, deepl := deeplServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(429)
			return
		}
		fmt.Fprint(w, `{"translations":[{"detected_source_language":"EN","text":"你好"}]}`)
	})
	res, err := deepl.Translate(context.Background(), Request{Text: "hello", TargetLang: "zh-HANS"}, fakeKeys{keys.DeeplKey: "k"}, config.Settings{})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Errorf("calls = %d, want a retry", calls)
	}
	if res.Text != "你好" {
		t.Errorf("res = %+v", res)
	}
}

func TestDeepLNotConfigured(t *testing.T) {
	_, deepl := deeplServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("should not be called")
	})
	_, err := deepl.Translate(context.Background(), Request{Text: "x", TargetLang: "en"}, fakeKeys{}, config.Settings{})
	if !errors.Is(err, ErrNotConfigured) {
		t.Errorf("err = %v, want ErrNotConfigured", err)
	}
}

func TestDeepLUnsupportedTarget(t *testing.T) {
	_, deepl := deeplServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("should not be called")
	})
	_, err := deepl.Translate(context.Background(), Request{Text: "x", TargetLang: "klingon"}, fakeKeys{keys.DeeplKey: "k"}, config.Settings{})
	if err == nil || !strings.Contains(err.Error(), "klingon") {
		t.Errorf("err = %v, want unsupported klingon", err)
	}
}

func TestDeepLUsage(t *testing.T) {
	_, deepl := deeplServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/usage" {
			t.Errorf("path = %q", r.URL.Path)
		}
		fmt.Fprint(w, `{"character_count":42,"character_limit":500000}`)
	})
	used, limit, err := deepl.Usage(context.Background(), "k")
	if err != nil {
		t.Fatal(err)
	}
	if used != 42 || limit != 500000 {
		t.Errorf("usage = %d/%d", used, limit)
	}
	if _, _, err := deepl.Usage(context.Background(), ""); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("empty key: err = %v, want ErrNotConfigured", err)
	}
}
