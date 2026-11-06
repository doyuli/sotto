package translate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"sotto/internal/config"
	"sotto/internal/keys"
)

func TestAzureTranslate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if got := q.Get("api-version"); got != "3.0" {
			t.Errorf("api-version = %q", got)
		}
		if got := q.Get("to"); got != "zh-Hans" {
			t.Errorf("to = %q, want zh-Hans", got)
		}
		if got := q.Get("from"); got != "" {
			t.Errorf("from = %q, want absent for auto", got)
		}
		if got := r.Header.Get("Ocp-Apim-Subscription-Key"); got != "k" {
			t.Errorf("key header = %q", got)
		}
		if got := r.Header.Get("Ocp-Apim-Subscription-Region"); got != "chinaeast2" {
			t.Errorf("region header = %q", got)
		}
		var in []map[string]string
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			t.Errorf("bad body: %v", err)
		}
		if len(in) != 1 || in[0]["Text"] != "hello" {
			t.Errorf("body = %+v", in)
		}
		fmt.Fprint(w, `[{"detectedLanguage":{"language":"en","score":1.0},"translations":[{"text":"你好","to":"zh-Hans"}]}]`)
	}))
	t.Cleanup(srv.Close)
	a := Azure{Client: srv.Client(), Endpoint: srv.URL}
	res, err := a.Translate(context.Background(), Request{Text: "hello", TargetLang: "zh-HANS"},
		fakeKeys{keys.AzureKey: "k", keys.AzureRegion: "chinaeast2"}, config.Settings{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "你好" || res.Service != "azure" || res.DetectedLang != "en" {
		t.Errorf("res = %+v", res)
	}
}

func TestAzureGlobalResourceOmitsRegion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Ocp-Apim-Subscription-Region"); got != "" {
			t.Errorf("region header = %q, want absent for a global resource", got)
		}
		fmt.Fprint(w, `[{"translations":[{"text":"hi","to":"en"}]}]`)
	}))
	t.Cleanup(srv.Close)
	a := Azure{Client: srv.Client(), Endpoint: srv.URL}
	if _, err := a.Translate(context.Background(), Request{Text: "你好", TargetLang: "en"},
		fakeKeys{keys.AzureKey: "k", keys.AzureRegion: "global"}, config.Settings{}); err != nil {
		t.Fatal(err)
	}
}

func TestAzureExplicitSource(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("from"); got != "en" {
			t.Errorf("from = %q, want en", got)
		}
		fmt.Fprint(w, `[{"translations":[{"text":"你好","to":"zh-Hans"}]}]`)
	}))
	t.Cleanup(srv.Close)
	a := Azure{Client: srv.Client(), Endpoint: srv.URL}
	res, err := a.Translate(context.Background(), Request{Text: "hello", SourceLang: "en", TargetLang: "zh-HANS"},
		fakeKeys{keys.AzureKey: "k"}, config.Settings{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "你好" {
		t.Errorf("res = %+v", res)
	}
}

func TestAzureErrors(t *testing.T) {
	tests := []struct {
		status  int
		wantErr error
	}{
		{401, ErrAuth},
		{403, ErrQuota},
	}
	for _, tt := range tests {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tt.status)
			fmt.Fprint(w, `{"error":{"code":400000,"message":"x"}}`)
		}))
		a := Azure{Client: srv.Client(), Endpoint: srv.URL}
		_, err := a.Translate(context.Background(), Request{Text: "x", TargetLang: "en"},
			fakeKeys{keys.AzureKey: "k"}, config.Settings{})
		if !errors.Is(err, tt.wantErr) {
			t.Errorf("status %d: err = %v, want %v", tt.status, err, tt.wantErr)
		}
		srv.Close()
	}
}
