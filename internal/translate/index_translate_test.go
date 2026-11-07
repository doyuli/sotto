package translate

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"sotto/internal/config"
	"sotto/internal/keys"
)

func TestIndexTranslate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "" {
			t.Error("Index-Translate must not send OpenAI credentials")
		}
		var input struct {
			Stream   bool
			Model    string
			Messages []struct{ Role, Content string }
			Options  map[string]bool `json:"chat_template_kwargs"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if input.Stream {
			t.Error("Translate must use a non-streaming request")
		}
		if input.Model != "Index-Translate-35B-A3B" {
			t.Errorf("unexpected model: %s", input.Model)
		}
		if thinking, ok := input.Options["enable_thinking"]; !ok || thinking {
			t.Error("translation should explicitly disable thinking")
		}
		if len(input.Messages) != 1 || input.Messages[0].Role != "user" ||
			!strings.Contains(input.Messages[0].Content, "from English into Simplified Chinese") ||
			!strings.HasSuffix(input.Messages[0].Content, "The file is ready.") {
			t.Errorf("unexpected messages: %+v", input.Messages)
		}
		fmt.Fprint(w, `{"choices":[{"finish_reason":"stop","message":{"content":" 文件已就绪。 "}}]}`)
	}))
	defer server.Close()

	cfg := config.Default()
	cfg.Services.OpenAI = config.OpenAIOptions{BaseURL: "https://invalid.example/v1", Model: "other-model"}
	creds := fakeKeys{keys.OpenAIKey: "unrelated-key"}
	translator := IndexTranslate{Endpoint: server.URL + "/v1/chat/completions", Client: server.Client()}
	res, err := translator.Translate(context.Background(), Request{
		Text: "The file is ready.", SourceLang: "en", TargetLang: "zh-HANS",
	}, creds, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if res.Service != "index-translate" || res.Text != "文件已就绪。" {
		t.Errorf("unexpected result: %+v", res)
	}
}

func TestIndexTranslateUserErrors(t *testing.T) {
	for _, tt := range []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"truncated", 200, `{"choices":[{"finish_reason":"length","message":{"content":"译文开头"}}]}`, "B站翻译：译文被截断，请缩短原文后重试"},
		{"empty", 200, `{"choices":[{"finish_reason":"stop","message":{"content":""}}]}`, "B站翻译：未返回译文"},
		{"limited", 429, "", "B站翻译：请求过于频繁，请稍后重试"},
		{"gateway HTML", 503, "<html>" + strings.Repeat("Service unavailable ", 4096) + "</html>", "B站翻译：服务暂时不可用，请稍后重试"},
		{"anonymous denied", 401, `{"error":"Unauthorized"}`, "B站翻译：暂时无法匿名访问，请稍后重试或切换翻译服务"},
		{"forbidden", 403, "<html>Access denied</html>", "B站翻译：暂时无法匿名访问，请稍后重试或切换翻译服务"},
		{"bad request", 400, `{"error":"invalid request"}`, "B站翻译：请求失败（HTTP 400）"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				fmt.Fprint(w, tt.body)
			}))
			defer server.Close()
			translator := IndexTranslate{Endpoint: server.URL, Client: server.Client()}
			_, err := translator.Translate(context.Background(), Request{Text: "Hi", TargetLang: "zh-HANS"}, nil, config.Default())
			if got := UserMessage(err); got != tt.want {
				t.Errorf("user message = %.160q, want %q", got, tt.want)
			}
		})
	}
}
