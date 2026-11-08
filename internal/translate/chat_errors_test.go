package translate

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestChatRequestErrors(t *testing.T) {
	for _, tc := range []struct {
		name, service, body, want string
		status                    int
	}{
		{"Zhipu balance overrides rate limit", "智谱", `{"error":{"code":"1113","message":"private detail"}}`, "智谱：账户余额不足，请前往控制台充值", 429},
		{"Zhipu numeric model code", "智谱", `{"error":{"code":1211}}`, "智谱：模型不存在，请检查模型名称", 400},
		{"Zhipu access", "智谱", `{"error":{"code":"1220"}}`, "智谱：无调用权限，请检查服务配置", 403},
		{"Zhipu quota", "智谱", `{"error":{"code":"1310"}}`, "智谱：调用额度已用完，请检查服务商控制台", 429},
		{"Zhipu overload", "智谱", `{"error":{"code":"1305"}}`, "智谱：服务暂时不可用，请稍后重试", 429},
		{"Zhipu input too long", "智谱", `{"error":{"code":"1261"}}`, "智谱：原文过长，请缩短后重试", 400},
		{"Zhipu content blocked", "智谱", `{"error":{"code":"1301"}}`, "智谱：内容被服务拦截，请修改原文后重试", 400},
		{"SiliconFlow numeric model code", "硅基流动", `{"code":20012,"message":"private detail","data":null}`, "硅基流动：模型不存在，请检查模型名称", 400},
		{"SiliconFlow string model code", "硅基流动", `{"code":"20012"}`, "硅基流动：模型不存在，请检查模型名称", 400},
		{"SiliconFlow balance", "硅基流动", `{"code":30001}`, "硅基流动：账户余额不足，请前往控制台充值", 402},
		{"SiliconFlow access", "硅基流动", `{"message":"private detail"}`, "硅基流动：无调用权限，请检查服务配置", 403},
		{"OpenAI model access", "OpenAI", `{"error":{"code":"model_not_found"}}`, "OpenAI：模型不可用，请检查模型名称与调用权限", 404},
		{"OpenAI balance before quota type", "OpenAI", `{"error":{"code":"credit_balance_exhausted","type":"insufficient_quota"}}`, "OpenAI：账户余额不足，请前往控制台充值", 429},
		{"OpenAI quota code", "OpenAI", `{"error":{"code":"insufficient_quota"}}`, "OpenAI：调用额度已用完，请检查服务商控制台", 429},
		{"OpenAI quota type", "OpenAI", `{"error":{"code":null,"type":"insufficient_quota"}}`, "OpenAI：调用额度已用完，请检查服务商控制台", 429},
		{"OpenAI spend limit", "OpenAI", `{"error":{"code":"project_spend_limit_exceeded"}}`, "OpenAI：调用额度已用完，请检查服务商控制台", 429},
		{"OpenAI slow down", "OpenAI", `{"error":{"code":"slow_down"}}`, "OpenAI：请求过于频繁，请稍后重试", 429},
		{"bad credentials", "OpenAI", `{"error":{"message":"private key"}}`, "凭证无效，请检查设置", 401},
		{"unknown model is not inferred from status", "OpenAI", `{"error":{"code":"unknown","message":"private detail"}}`, "OpenAI：请求失败（HTTP 404）", 404},
		{"numeric codes are provider specific", "智谱", `{"code":20012}`, "智谱：请求失败（HTTP 400）", 400},
		{"numeric codes do not cross providers", "硅基流动", `{"code":"1113"}`, "硅基流动：请求过于频繁，请稍后重试", 429},
		{"non JSON gateway error", "OpenAI", `<html>private gateway</html>`, "OpenAI：服务暂时不可用，请稍后重试", 503},
		{"malformed JSON", "OpenAI", `{"error":`, "OpenAI：请求失败（HTTP 400）", 400},
		{"unexpected error shape", "OpenAI", `{"error":"private key"}`, "OpenAI：请求失败（HTTP 400）", 400},
		{"JSON failure with successful status", "智谱", `{"error":{"code":"1113"}}`, "智谱：账户余额不足，请前往控制台充值", 200},
		{"flat JSON failure with successful status", "硅基流动", `{"code":20012,"message":"private detail"}`, "硅基流动：模型不存在，请检查模型名称", 200},
		{"unknown successful status error", "OpenAI", `{"error":{"message":"private detail"}}`, "OpenAI：生成中断，请重试", 200},
		{"anonymous billing details ignored", "B站翻译", `{"error":{"code":"credit_balance_exhausted"}}`, "B站翻译：请求失败（HTTP 402）", 402},
		{"anonymous JSON error ignored", "B站翻译", `{"error":{"code":"invalid_api_key"}}`, "B站翻译：生成中断，请重试", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			key := "test-key"
			if tc.service == "B站翻译" {
				key = ""
			}
			_, err := requestChatCompletion(context.Background(), server.Client(), server.URL, key, map[string]any{}, tc.service, false, nil)
			if got := UserMessage(err); got != tc.want {
				t.Errorf("message = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestChatStreamErrorMessagesKeepPartialText(t *testing.T) {
	for _, tc := range []struct{ service, error, want string }{
		{"智谱", `{"error":{"code":"1113","message":"private"}}`, "智谱：账户余额不足，请前往控制台充值"},
		{"硅基流动", `{"error":{"code":20012,"message":"private"}}`, "硅基流动：模型不存在，请检查模型名称"},
		{"OpenAI", `{"error":{"type":"insufficient_quota","message":"private"}}`, "OpenAI：调用额度已用完，请检查服务商控制台"},
		{"B站翻译", `{"error":{"code":"invalid_api_key","message":"private"}}`, "B站翻译：生成中断，请重试"},
	} {
		t.Run(tc.service, func(t *testing.T) {
			body := "data: {\"choices\":[{\"delta\":{\"content\":\"已有译文\"}}]}\n\ndata: " + tc.error + "\n\n"
			var progress []string
			choice, err := readChatStream(strings.NewReader(body), tc.service, func(text string) { progress = append(progress, text) })
			if choice.Message.Content != "已有译文" || len(progress) != 1 || progress[0] != "已有译文" || UserMessage(err) != tc.want {
				t.Fatalf("choice = %+v, progress = %v, err = %v", choice, progress, err)
			}
		})
	}
}

func TestChatErrorBodyLimit(t *testing.T) {
	// A model code after the limit must neither be read nor affect classification.
	body := `{"message":"` + strings.Repeat("private", maxChatErrorBytes) + `","code":20012}`
	reader := strings.NewReader(body)
	client := &http.Client{Transport: chatRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 400, Header: make(http.Header), Body: io.NopCloser(reader)}, nil
	})}
	_, err := requestChatCompletion(context.Background(), client, "https://example.test", "key", map[string]any{}, "硅基流动", false, nil)
	if read := len(body) - reader.Len(); read != maxChatErrorBytes {
		t.Errorf("read %d error bytes, want %d", read, maxChatErrorBytes)
	}
	if got := UserMessage(err); got != "硅基流动：请求失败（HTTP 400）" {
		t.Errorf("limited message = %q", got)
	}
	if got := UserMessage(chatResponseError("硅基流动", 200, []byte(body))); got != "硅基流动：生成中断，请重试" {
		t.Errorf("oversized JSON/SSE message = %q", got)
	}
}
