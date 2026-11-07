package translate

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"sotto/internal/config"
	"sotto/internal/keys"
)

// OpenAI translates through any OpenAI-compatible chat completions API,
// which covers the many relays and local servers that speak the same
// schema: baseURL and model are the user's to choose.
type OpenAI struct {
	Client *http.Client // nil: a client with a 30s timeout
}

func (OpenAI) Name() string { return "openai" }

func (OpenAI) Available(k KeyStore, _ config.Settings) bool {
	return k.Get(keys.OpenAIKey) != ""
}

func (o OpenAI) Translate(ctx context.Context, req Request, k KeyStore, cfg config.Settings) (Result, error) {
	return o.Stream(ctx, req, k, cfg, nil)
}

func (o OpenAI) Stream(ctx context.Context, req Request, k KeyStore, cfg config.Settings, emit func(Result)) (Result, error) {
	key := k.Get(keys.OpenAIKey)
	if key == "" {
		return Result{}, ErrNotConfigured
	}
	target := englishName(req.TargetLang)
	if target == "" {
		return Result{}, unsupported("OpenAI", req.TargetLang)
	}
	base := strings.TrimSuffix(cfg.Services.OpenAI.BaseURL, "/")
	if base == "" {
		base = "https://api.openai.com/v1"
	}
	model := cfg.Services.OpenAI.Model
	if model == "" {
		model = "gpt-4o-mini"
	}
	// The model detects the source language itself, so the prompt only
	// names the target; the reply carries the translation and nothing else.
	system := fmt.Sprintf(
		"You are a translation engine. Translate the user's text into %s. "+
			"Reply with the translation only: no quotes, no explanations. "+
			"If the text is already in %s, reply with it unchanged.", target, target)
	payload := map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": req.Text},
		},
		"temperature": 0.1,
	}
	choice, err := requestChatCompletion(ctx, o.Client, base+"/chat/completions", key, payload, "OpenAI", func(text string) {
		if emit != nil {
			emit(Result{Service: o.Name(), Text: cleanOpenAIText(text), Streaming: true})
		}
	})
	// The reply may wrap the translation in quotes despite the prompt.
	return Result{Service: o.Name(), Text: cleanOpenAIText(choice.Message.Content)}, err
}

func cleanOpenAIText(text string) string {
	return strings.Trim(strings.TrimSpace(text), "\"'“”‘’")
}

type chatChoice struct {
	FinishReason string `json:"finish_reason"`
	Message      struct {
		Content string `json:"content"`
	} `json:"message"`
}

var defaultChatClient = &http.Client{Timeout: 30 * time.Second}

// requestChatCompletion shares the wire protocol between independent services.
func requestChatCompletion(ctx context.Context, client *http.Client, endpoint, key string, payload map[string]any, service string, emit func(string)) (chatChoice, error) {
	if client == nil {
		client = defaultChatClient
	}
	payload["stream"] = true
	data, err := json.Marshal(payload)
	if err != nil {
		return chatChoice{}, fmt.Errorf("%s：构造请求：%w", service, err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return chatChoice{}, fmt.Errorf("%s：构造请求：%w", service, err)
	}
	if key != "" {
		httpReq.Header.Set("Authorization", "Bearer "+key)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	resp, err := client.Do(httpReq)
	if err != nil {
		return chatChoice{}, fmt.Errorf("%s：%w", service, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		return chatChoice{}, fmt.Errorf("%s：请求过于频繁，请稍后重试", service)
	}
	// Public gateways may return HTML or echo request data. Keep anonymous
	// service errors short and avoid suggesting credentials they cannot set.
	if status := resp.StatusCode; status != http.StatusOK && key == "" {
		switch {
		case status == http.StatusUnauthorized || status == http.StatusForbidden:
			return chatChoice{}, fmt.Errorf("%s：暂时无法匿名访问，请稍后重试或切换翻译服务", service)
		case status >= http.StatusInternalServerError:
			return chatChoice{}, fmt.Errorf("%s：服务暂时不可用，请稍后重试", service)
		default:
			return chatChoice{}, fmt.Errorf("%s：请求失败（HTTP %d）", service, status)
		}
	}
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusUnauthorized {
			return chatChoice{}, ErrAuth
		}
		return chatChoice{}, fmt.Errorf("%s：请求失败（HTTP %d）", service, resp.StatusCode)
	}
	// Some compatible endpoints ignore stream and return a regular JSON body.
	// Sniff the body since relays may omit or mislabel the content type.
	reader := bufio.NewReader(io.LimitReader(resp.Body, (1<<20)+1))
	for {
		b, err := reader.ReadByte()
		if err != nil {
			return chatChoice{}, fmt.Errorf("%s：读取响应：%w", service, err)
		}
		if b != ' ' && b != '\n' && b != '\r' && b != '\t' {
			_ = reader.UnreadByte()
			break
		}
	}
	first, _ := reader.Peek(1)
	if first[0] == '{' {
		var out struct {
			Choices []chatChoice `json:"choices"`
		}
		if err := json.NewDecoder(reader).Decode(&out); err != nil {
			return chatChoice{}, fmt.Errorf("%s：解析响应：%w", service, err)
		}
		if len(out.Choices) == 0 {
			return chatChoice{}, fmt.Errorf("%s：空响应", service)
		}
		return out.Choices[0], chatCompletionError(out.Choices[0], service)
	}
	return readChatStream(reader, service, emit)
}

// Each callback is a cumulative snapshot, so Ready can restore a stream without
// replaying tokens. Only content from the first choice is translation text.
func readChatStream(reader io.Reader, service string, emit func(string)) (chatChoice, error) {
	var choice chatChoice
	var text strings.Builder
	var data []string
	consume := func() (bool, error) {
		if len(data) == 0 {
			return false, nil
		}
		payload := strings.Join(data, "\n")
		data = nil
		if payload == "[DONE]" {
			return true, nil
		}
		var chunk struct {
			Choices []struct {
				Index int `json:"index"`
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
			Error json.RawMessage `json:"error"`
		}
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			return false, fmt.Errorf("%s：解析流式响应：%w", service, err)
		}
		if len(chunk.Error) > 0 && string(chunk.Error) != "null" {
			return false, fmt.Errorf("%s：生成中断，请重试", service)
		}
		for _, part := range chunk.Choices {
			if part.Index != 0 {
				continue
			}
			if part.Delta.Content != "" {
				text.WriteString(part.Delta.Content)
				choice.Message.Content = text.String()
				if emit != nil {
					emit(choice.Message.Content)
				}
			}
			if part.FinishReason != "" {
				choice.FinishReason = part.FinishReason
			}
		}
		return false, nil
	}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			done, err := consume()
			if err != nil {
				return choice, err
			}
			if done {
				return choice, chatCompletionError(choice, service)
			}
		} else if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	if err := scanner.Err(); err != nil {
		return choice, fmt.Errorf("%s：读取流式响应：%w", service, err)
	}
	// Accept a final event without a trailing blank line, but never silently
	// treat an interrupted stream as a complete translation.
	done, err := consume()
	if err != nil {
		return choice, err
	}
	if !done && choice.FinishReason == "" {
		return choice, fmt.Errorf("%s：连接中断，译文未完成，请重试", service)
	}
	return choice, chatCompletionError(choice, service)
}

func chatCompletionError(choice chatChoice, service string) error {
	switch choice.FinishReason {
	case "length":
		return fmt.Errorf("%s：译文被截断，请缩短原文后重试", service)
	case "content_filter":
		return fmt.Errorf("%s：译文被服务拦截，请修改原文后重试", service)
	case "", "stop":
	default:
		return fmt.Errorf("%s：未完成翻译，请重试", service)
	}
	if strings.TrimSpace(choice.Message.Content) == "" {
		return fmt.Errorf("%s：未返回译文", service)
	}
	return nil
}
