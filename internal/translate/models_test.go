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

func TestConfiguredChatModels(t *testing.T) {
	for _, tc := range []struct {
		service, model, want     string
		hunyuan, disableThinking bool
	}{
		{"siliconflow", "", "tencent/Hunyuan-MT-7B", true, false},
		{"siliconflow", " \t", "tencent/Hunyuan-MT-7B", true, false},
		{"siliconflow", " Pro/tencent/Hunyuan-MT-7B ", "Pro/tencent/Hunyuan-MT-7B", true, false},
		{"siliconflow", " Qwen/Qwen3-8B ", "Qwen/Qwen3-8B", false, true},
		{"siliconflow", "Pro/Qwen/Qwen3-32B", "Pro/Qwen/Qwen3-32B", false, true},
		{"siliconflow", "Qwen/Qwen3-30B-A3B-Thinking-2507", "Qwen/Qwen3-30B-A3B-Thinking-2507", false, false},
		{"siliconflow", "Qwen/Qwen3-30B-A3B-Instruct-2507", "Qwen/Qwen3-30B-A3B-Instruct-2507", false, false},
		{"siliconflow", "custom-model", "custom-model", false, false},
		{"zhipu", "", "glm-4.7-flash", false, true},
		{"zhipu", " \n", "glm-4.7-flash", false, true},
		{"zhipu", " glm-4.6 ", "glm-4.6", false, true},
		{"zhipu", "glm-4-flash", "glm-4-flash", false, false},
		{"zhipu", "glm-4.6v", "glm-4.6v", false, false},
		{"zhipu", "custom-model", "custom-model", false, false},
	} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%q/stream=%t", tc.service, tc.model, stream), func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					var input struct {
						Model          string
						Stream         bool
						Messages       []struct{ Role, Content string }
						Thinking       *struct{ Type string }
						EnableThinking *bool `json:"enable_thinking"`
					}
					if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
						t.Error(err)
					}
					if input.Model != tc.want || input.Stream != stream {
						t.Errorf("model = %q, stream = %t", input.Model, input.Stream)
					}
					if tc.hunyuan {
						if len(input.Messages) != 1 || input.Messages[0].Role != "user" ||
							!strings.HasPrefix(input.Messages[0].Content, "将 <source> 内的原文翻译成中文（简体）。") ||
							!strings.HasSuffix(input.Messages[0].Content, "\n\n<source>\nBonjour !\n</source>") {
							t.Errorf("lost Hunyuan template: %+v", input.Messages)
						}
					} else if len(input.Messages) != 2 || input.Messages[0].Role != "system" ||
						!strings.Contains(input.Messages[0].Content, "from French into Simplified Chinese") ||
						input.Messages[1].Role != "user" || input.Messages[1].Content != "<source>\nBonjour !\n</source>" {
						t.Errorf("unexpected general chat prompt: %+v", input.Messages)
					}
					if tc.service == "zhipu" {
						if (input.Thinking != nil) != tc.disableThinking || (input.Thinking != nil && input.Thinking.Type != "disabled") || input.EnableThinking != nil {
							t.Errorf("unexpected Zhipu thinking options: %+v", input)
						}
					} else if (input.EnableThinking != nil) != tc.disableThinking || (input.EnableThinking != nil && *input.EnableThinking) || input.Thinking != nil {
						t.Errorf("unexpected SiliconFlow thinking options: %+v", input)
					}
					if stream {
						w.Header().Set("Content-Type", "text/event-stream")
						fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"hidden\"}}]}\n\ndata: {\"choices\":[{\"delta\":{\"content\":\"你好！\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
					} else {
						fmt.Fprint(w, `{"choices":[{"message":{"content":"你好！"},"finish_reason":"stop"}]}`)
					}
				}))
				defer server.Close()
				cfg := config.Default()
				cfg.Services.OpenAI.Model = "unrelated-openai-model"
				cfg.Services.SiliconFlow.Model = "unrelated-siliconflow-model"
				cfg.Services.Zhipu.Model = "unrelated-zhipu-model"
				var translator Translator
				if tc.service == "siliconflow" {
					cfg.Services.SiliconFlow.Model = tc.model
					translator = SiliconFlow{Client: server.Client(), Endpoint: server.URL}
				} else {
					cfg.Services.Zhipu.Model = tc.model
					translator = Zhipu{Client: server.Client(), Endpoint: server.URL}
				}
				creds := fakeKeys{keys.SiliconFlowKey: "silicon-key", keys.ZhipuKey: "zhipu-key"}
				req := Request{Text: "Bonjour !", SourceLang: "fr", TargetLang: "zh-HANS"}
				var res Result
				var err error
				if stream {
					var updates []Result
					res, err = translator.(StreamingTranslator).Stream(context.Background(), req, creds, cfg, func(r Result) { updates = append(updates, r) })
					if len(updates) != 1 || updates[0].Text != "你好！" {
						t.Errorf("streamed updates = %+v", updates)
					}
				} else {
					res, err = translator.Translate(context.Background(), req, creds, cfg)
				}
				if err != nil || res != (Result{Service: tc.service, Text: "你好！"}) {
					t.Errorf("result = %+v, err = %v", res, err)
				}
			})
		}
	}
}
