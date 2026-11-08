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

func TestZhipuUnsupportedTarget(t *testing.T) {
	_, err := (Zhipu{}).Translate(context.Background(), Request{Text: "hello", TargetLang: "invalid"},
		fakeKeys{keys.ZhipuKey: "own-key"}, config.Default())
	if err == nil || !strings.Contains(UserMessage(err), "不支持") {
		t.Errorf("invalid target: %v", err)
	}
}
