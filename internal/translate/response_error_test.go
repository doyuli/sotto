package translate

import (
	"context"
	"io"
	"net/http"
	"testing"

	"sotto/internal/config"
	"sotto/internal/keys"
)

type unreadableResponseBody struct {
	read bool
}

func (b *unreadableResponseBody) Read([]byte) (int, error) {
	b.read = true
	return 0, io.ErrUnexpectedEOF
}

func TestHTTPStatusOnlyFailuresSkipBody(t *testing.T) {
	// A failed body read used to replace the error already known from the headers.
	var body unreadableResponseBody
	client := &http.Client{Transport: chatRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusBadGateway, Body: io.NopCloser(&body)}, nil
	})}
	credentials := fakeKeys{
		keys.AzureKey: "fixture", keys.BaiduAppID: "fixture", keys.BaiduKey: "fixture", keys.DeeplKey: "fixture",
		keys.YoudaoAppKey: "fixture", keys.YoudaoSecret: "fixture",
	}
	for _, tc := range []struct {
		service string
		engine  Translator
	}{
		{"微软翻译", Azure{Client: client}},
		{"百度", Baidu{Client: client}},
		{"DeepL", DeepL{Client: client}},
		{"Google", Google{Client: client}},
		{"有道", Youdao{Client: client}},
	} {
		t.Run(tc.service, func(t *testing.T) {
			body.read = false
			_, err := tc.engine.Translate(context.Background(), Request{Text: "hello", TargetLang: "zh-HANS"}, credentials, config.Settings{})
			want := tc.service + "：服务暂时不可用，请稍后重试"
			if got := UserMessage(err); body.read || got != want {
				t.Fatalf("body read = %t, message = %q, want %q", body.read, got, want)
			}
		})
	}
}
