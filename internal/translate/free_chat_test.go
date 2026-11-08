package translate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"

	"sotto/internal/config"
	"sotto/internal/keys"
)

func TestFreeChatServicesCredentials(t *testing.T) {
	cfg := config.Default()
	cfg.Services.Enabled = []string{"siliconflow", "zhipu"}
	creds := fakeKeys{keys.OpenAIKey: "unrelated-openai-key"}
	if got := Available(cfg, creds); len(got) != 0 {
		t.Fatalf("OpenAI credentials enabled other services: %v", got)
	}
	for _, tc := range []struct {
		service Translator
		key     string
	}{
		{SiliconFlow{}, keys.SiliconFlowKey},
		{Zhipu{}, keys.ZhipuKey},
	} {
		_, err := tc.service.Translate(context.Background(), Request{Text: "hello", TargetLang: "en"}, creds, cfg)
		if !errors.Is(err, ErrNotConfigured) {
			t.Errorf("%s missing key: %v", tc.service.Name(), err)
		}
		creds[tc.key] = "own-key"
		if !tc.service.Available(creds, cfg) || !slices.Contains(Configured(cfg, creds), tc.service.Name()) {
			t.Errorf("%s not configured with its own key", tc.service.Name())
		}
		_, err = tc.service.Translate(context.Background(), Request{Text: "hello", TargetLang: "invalid"}, creds, cfg)
		if err == nil || !strings.Contains(UserMessage(err), "不支持") {
			t.Errorf("%s invalid target: %v", tc.service.Name(), err)
		}
	}
	if got := Available(cfg, creds); !reflect.DeepEqual(got, []string{"siliconflow", "zhipu"}) {
		t.Errorf("available = %v", got)
	}
	delete(creds, keys.SiliconFlowKey)
	if got := Available(cfg, creds); !reflect.DeepEqual(got, []string{"zhipu"}) {
		t.Errorf("removing SiliconFlow key affected Zhipu: %v", got)
	}
}

func TestSiliconFlowRequest(t *testing.T) {
	for _, tc := range []struct {
		source, target, prompt string
	}{
		{"auto", "zh-HANS", "把下面的文本翻译成中文（简体），不要额外解释。\n\nThe file is ready."},
		{"zh-HANS", "en", "把下面的文本翻译成英语，不要额外解释。\n\nThe file is ready."},
		{"fr", "de", "Translate the following segment into German, without additional explanation.\n\nThe file is ready."},
	} {
		t.Run(tc.source+"/"+tc.target, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer silicon-key" {
					t.Errorf("unexpected request: %s %s, auth = %q", r.Method, r.URL, r.Header.Get("Authorization"))
				}
				var input struct {
					Model    string
					Stream   bool
					Messages []struct{ Role, Content string }
				}
				if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
					t.Error(err)
				}
				if input.Model != "tencent/Hunyuan-MT-7B" || input.Stream || len(input.Messages) != 1 ||
					input.Messages[0].Role != "user" || input.Messages[0].Content != tc.prompt {
					t.Errorf("unexpected payload: %+v", input)
				}
				fmt.Fprint(w, `{"choices":[{"finish_reason":"stop","message":{"content":" 文件已就绪。 "}}]}`)
			}))
			defer server.Close()
			cfg := config.Default()
			cfg.Services.OpenAI = config.OpenAIOptions{BaseURL: "https://invalid.example", Model: "unrelated-model"}
			res, err := (SiliconFlow{Client: server.Client(), Endpoint: server.URL + "/v1/chat/completions"}).Translate(context.Background(),
				Request{Text: "The file is ready.", SourceLang: tc.source, TargetLang: tc.target},
				fakeKeys{keys.SiliconFlowKey: "silicon-key", keys.OpenAIKey: "unrelated-key", keys.ZhipuKey: "zhipu-key"}, cfg)
			if err != nil || res != (Result{Service: "siliconflow", Text: "文件已就绪。"}) {
				t.Errorf("result = %+v, err = %v", res, err)
			}
		})
	}
}

func TestZhipuRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/paas/v4/chat/completions" || r.Header.Get("Authorization") != "Bearer zhipu-key" {
			t.Errorf("unexpected request: %s %s, auth = %q", r.Method, r.URL, r.Header.Get("Authorization"))
		}
		var input struct {
			Model    string
			Stream   bool
			Messages []struct{ Role, Content string }
			Thinking struct{ Type string }
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Error(err)
		}
		if input.Model != "glm-4.5-flash" || input.Stream || input.Thinking.Type != "disabled" ||
			len(input.Messages) != 2 || input.Messages[0].Role != "system" ||
			!strings.Contains(input.Messages[0].Content, "from French into Simplified Chinese") ||
			input.Messages[1].Role != "user" || input.Messages[1].Content != "Bonjour !" {
			t.Errorf("unexpected payload: %+v", input)
		}
		fmt.Fprint(w, `{"choices":[{"finish_reason":"stop","message":{"content":" 你好！ "}}]}`)
	}))
	defer server.Close()
	cfg := config.Default()
	cfg.Services.OpenAI = config.OpenAIOptions{BaseURL: "https://invalid.example", Model: "unrelated-model"}
	res, err := (Zhipu{Client: server.Client(), Endpoint: server.URL + "/api/paas/v4/chat/completions"}).Translate(context.Background(),
		Request{Text: "Bonjour !", SourceLang: "fr", TargetLang: "zh-HANS"},
		fakeKeys{keys.ZhipuKey: "zhipu-key", keys.OpenAIKey: "unrelated-key", keys.SiliconFlowKey: "silicon-key"}, cfg)
	if err != nil || res != (Result{Service: "zhipu", Text: "你好！"}) {
		t.Errorf("result = %+v, err = %v", res, err)
	}
}

func TestFreeChatServicesErrors(t *testing.T) {
	for _, service := range []struct {
		name, label, key string
		translator       func(*httptest.Server) Translator
	}{
		{"siliconflow", "硅基流动", keys.SiliconFlowKey, func(s *httptest.Server) Translator { return SiliconFlow{Client: s.Client(), Endpoint: s.URL} }},
		{"zhipu", "智谱", keys.ZhipuKey, func(s *httptest.Server) Translator { return Zhipu{Client: s.Client(), Endpoint: s.URL} }},
	} {
		for _, tc := range []struct {
			name, body, want string
			status           int
		}{
			{"auth", `{"error":{"message":"private detail"}}`, "凭证无效，请检查设置", 401},
			{"rate limit", `{"error":{"message":"private detail"}}`, service.label + "：请求过于频繁，请稍后重试", 429},
			{"empty", `{"choices":[{"finish_reason":"stop","message":{"content":""}}]}`, service.label + "：未返回译文", 200},
			{"truncated", `{"choices":[{"finish_reason":"length","message":{"content":"已有译文"}}]}`, service.label + "：译文被截断，请缩短原文后重试", 200},
		} {
			t.Run(service.name+"/"+tc.name, func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(tc.status)
					fmt.Fprint(w, tc.body)
				}))
				defer server.Close()
				_, err := service.translator(server).Translate(context.Background(), Request{Text: "hello", TargetLang: "zh-HANS"}, fakeKeys{service.key: "own-key"}, config.Default())
				if got := UserMessage(err); got != tc.want {
					t.Errorf("error = %q, want %q", got, tc.want)
				}
			})
		}
	}
}
