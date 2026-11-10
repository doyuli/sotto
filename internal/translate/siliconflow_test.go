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

func TestSiliconFlowRequest(t *testing.T) {
	for _, tc := range []struct {
		source, target, prompt string
	}{
		{"auto", "zh-HANS", "把下面的文本翻译成中文（简体），不要额外解释。单词和语言名称也要直接翻译，不要对话或要求补充内容。"},
		{"zh-HANS", "en", "把下面的文本翻译成英语，不要额外解释。单词和语言名称也要直接翻译，不要对话或要求补充内容。"},
		{"fr", "de", "Translate the following segment into German, without additional explanation. Translate single words and language names directly; do not respond conversationally or ask for more text."},
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
					input.Messages[0].Role != "user" || input.Messages[0].Content != tc.prompt+"\n\nThe file is ready." {
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

func TestSiliconFlowUnsupportedTarget(t *testing.T) {
	_, err := (SiliconFlow{}).Translate(context.Background(), Request{Text: "hello", TargetLang: "invalid"},
		fakeKeys{keys.SiliconFlowKey: "own-key"}, config.Default())
	if err == nil || !strings.Contains(UserMessage(err), "不支持") {
		t.Errorf("invalid target: %v", err)
	}
}
