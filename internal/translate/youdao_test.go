package translate

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"sotto/internal/config"
	"sotto/internal/keys"
)

// TestYoudaoInput pins the API's truncation rule: twenty characters and
// fewer go in whole, longer ones as first ten + length + last ten.
func TestYoudaoInput(t *testing.T) {
	tests := []struct {
		q, want string
	}{
		{"short", "short"},
		{"Hello, 世界", "Hello, 世界"},
		{"abcdefghijklmnopqrst", "abcdefghijklmnopqrst"},
		{"abcdefghijklmnopqrstu", "abcdefghij21lmnopqrstu"},
		{"中文输入也按字符计，而不是按字节计的长度啊", "中文输入也按字符计，21不是按字节计的长度啊"},
	}
	for _, tt := range tests {
		if got := youdaoInput(tt.q); got != tt.want {
			t.Errorf("youdaoInput(%q) = %q, want %q", tt.q, got, tt.want)
		}
	}
}

// TestYoudaoSign pins the digest to an independent sha256sum:
// sha256("appkey" + "apple" + "salt" + "1700000000" + "secret").
func TestYoudaoSign(t *testing.T) {
	got := youdaoSign("appkey", "apple", "salt", "1700000000", "secret")
	// echo -n 'appkeyapplesalt1700000000secret' | sha256sum
	want := "c81c3ace35bb74388d99d9be3af70a46ad173b75103c38216e0c42f5cf4862aa"
	if got != want {
		t.Errorf("sign = %s, want %s", got, want)
	}
}

func TestYoudaoTranslate(t *testing.T) {
	now := time.Unix(1700000000, 0)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("bad form: %v", err)
		}
		form := r.PostForm
		if got := form.Get("to"); got != "zh-CHS" {
			t.Errorf("to = %q, want zh-CHS", got)
		}
		if got := form.Get("from"); got != "auto" {
			t.Errorf("from = %q, want auto", got)
		}
		if got := form.Get("appKey"); got != "app-1" {
			t.Errorf("appKey = %q", got)
		}
		if got := form.Get("signType"); got != "v3" {
			t.Errorf("signType = %q", got)
		}
		if got := form.Get("curtime"); got != "1700000000" {
			t.Errorf("curtime = %q", got)
		}
		want := youdaoSign("app-1", form.Get("q"), form.Get("salt"), form.Get("curtime"), "secret-1")
		if got := form.Get("sign"); got != want {
			t.Errorf("sign = %q, want the v3 signature %q", got, want)
		}
		fmt.Fprint(w, `{"errorCode":"0","translation":["你好"],"basic":{"explains":["感叹词"]}}`)
	}))
	t.Cleanup(srv.Close)
	y := Youdao{Client: srv.Client(), Endpoint: srv.URL, Now: func() time.Time { return now }}
	res, err := y.Translate(context.Background(), Request{Text: "hello", TargetLang: "zh-HANS"},
		fakeKeys{keys.YoudaoAppKey: "app-1", keys.YoudaoSecret: "secret-1"}, config.Settings{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "你好" || res.Service != "youdao" {
		t.Errorf("res = %+v", res)
	}
}

func TestYoudaoFormEncoding(t *testing.T) {
	// The query travels as a form value, so a "+" in it must arrive as
	// "+" and not be read back as a space.
	now := time.Unix(1700000000, 0)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("bad form: %v", err)
		}
		if got := r.PostForm.Get("q"); got != "a+b c" {
			t.Errorf("q = %q, want \"a+b c\"", got)
		}
		fmt.Fprint(w, `{"errorCode":"0","translation":["x"]}`)
	}))
	t.Cleanup(srv.Close)
	y := Youdao{Client: srv.Client(), Endpoint: srv.URL, Now: func() time.Time { return now }}
	if _, err := y.Translate(context.Background(), Request{Text: "a+b c", TargetLang: "en"},
		fakeKeys{keys.YoudaoAppKey: "a", keys.YoudaoSecret: "s"}, config.Settings{}); err != nil {
		t.Fatal(err)
	}
}

func TestYoudaoErrors(t *testing.T) {
	tests := []struct {
		code    string
		wantErr error
	}{
		{"103", ErrAuth},
		{"108", ErrAuth},
		{"112", ErrQuota},
	}
	for _, tt := range tests {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(w, `{"errorCode":%q}`, tt.code)
		}))
		y := Youdao{Client: srv.Client(), Endpoint: srv.URL}
		_, err := y.Translate(context.Background(), Request{Text: "x", TargetLang: "en"},
			fakeKeys{keys.YoudaoAppKey: "a", keys.YoudaoSecret: "s"}, config.Settings{})
		if !errors.Is(err, tt.wantErr) {
			t.Errorf("code %s: err = %v, want %v", tt.code, err, tt.wantErr)
		}
		srv.Close()
	}
}
