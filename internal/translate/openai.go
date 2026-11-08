package translate

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
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
	Client *http.Client // nil: the shared client with connection/header timeouts
}

func (OpenAI) Name() string { return "openai" }

func (OpenAI) Available(k KeyStore, _ config.Settings) bool {
	return k.Get(keys.OpenAIKey) != ""
}

func (o OpenAI) Translate(ctx context.Context, req Request, k KeyStore, cfg config.Settings) (Result, error) {
	return o.translate(ctx, req, k, cfg, false, nil)
}

func (o OpenAI) Stream(ctx context.Context, req Request, k KeyStore, cfg config.Settings, emit func(Result)) (Result, error) {
	return o.translate(ctx, req, k, cfg, true, emit)
}

func (o OpenAI) translate(ctx context.Context, req Request, k KeyStore, cfg config.Settings, stream bool, emit func(Result)) (Result, error) {
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
	choice, err := requestChatCompletion(ctx, o.Client, base+"/chat/completions", key, payload, "OpenAI", stream, func(text string) {
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

var defaultChatClient = newChatClient()

func newChatClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	transport.TLSHandshakeTimeout = 10 * time.Second
	transport.ResponseHeaderTimeout = 30 * time.Second
	return &http.Client{Transport: transport}
}

const (
	maxChatJSONBytes  = 1 << 20
	maxChatEventBytes = 1 << 20
	maxChatTextBytes  = 1 << 20
)

// requestChatCompletion shares the wire protocol between independent services.
func requestChatCompletion(ctx context.Context, client *http.Client, endpoint, key string, payload map[string]any, service string, stream bool, emit func(string)) (choice chatChoice, resultErr error) {
	ctx, activity := newChatActivity(ctx)
	defer activity.stop()
	defer func() {
		if resultErr == nil {
			return
		}
		// Cancellation can race a clean body EOF; keep the request's actual
		// cause instead of misreporting that race as a broken stream.
		if ctx.Err() != nil && !errors.Is(context.Cause(ctx), errChatIdleTimeout) {
			resultErr = fmt.Errorf("%s：%w", service, ctx.Err())
		}
		var timeout net.Error
		if errors.Is(context.Cause(ctx), errChatIdleTimeout) ||
			(choice.Message.Content != "" && (errors.Is(resultErr, context.DeadlineExceeded) ||
				(errors.As(resultErr, &timeout) && timeout.Timeout()))) {
			resultErr = fmt.Errorf("%w: %w", ErrGenerationTimeout, resultErr)
		}
	}()
	if client == nil {
		client = defaultChatClient
	}
	payload["stream"] = stream
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
	if stream {
		httpReq.Header.Set("Accept", "text/event-stream")
	} else {
		httpReq.Header.Set("Accept", "application/json")
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return chatChoice{}, fmt.Errorf("%s：%w", service, err)
	}
	defer resp.Body.Close()
	// Receiving headers is not the first body byte: reasoning models may
	// acknowledge the request long before they start sending any events.
	activity.reset(chatFirstByteTimeout)
	if resp.StatusCode != http.StatusOK {
		var body []byte
		if key != "" {
			body, err = io.ReadAll(io.LimitReader(chatActivityReader{Reader: resp.Body, activity: activity}, maxChatErrorBytes))
			if err != nil {
				return chatChoice{}, fmt.Errorf("%s：读取错误响应：%w", service, err)
			}
		}
		return chatChoice{}, chatResponseError(service, resp.StatusCode, body)
	}
	// Some compatible endpoints ignore stream and return a regular JSON body.
	// Sniff the body since relays may omit or mislabel the content type.
	// Bound retained events and text separately: repeated SSE metadata and
	// discarded reasoning must not consume the translation's size budget.
	reader := bufio.NewReader(chatActivityReader{Reader: resp.Body, activity: activity})
	for {
		b, err := reader.ReadByte()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return chatChoice{}, fmt.Errorf("%s：未返回译文", service)
			}
			return chatChoice{}, fmt.Errorf("%s：读取响应：%w", service, err)
		}
		if b != ' ' && b != '\n' && b != '\r' && b != '\t' {
			_ = reader.UnreadByte()
			break
		}
	}
	first, err := reader.Peek(1)
	if len(first) == 0 {
		if err == nil || errors.Is(err, io.EOF) {
			return chatChoice{}, fmt.Errorf("%s：未返回译文", service)
		}
		return chatChoice{}, fmt.Errorf("%s：读取响应：%w", service, err)
	}
	if first[0] == '{' {
		var out struct {
			Choices []chatChoice    `json:"choices"`
			Error   json.RawMessage `json:"error"`
			Code    json.RawMessage `json:"code"`
		}
		body, err := io.ReadAll(io.LimitReader(reader, maxChatJSONBytes+1))
		if err != nil {
			return chatChoice{}, fmt.Errorf("%s：读取响应：%w", service, err)
		}
		if len(body) > maxChatJSONBytes {
			return chatChoice{}, fmt.Errorf("%s：响应过大，请缩短原文后重试", service)
		}
		if err := json.Unmarshal(body, &out); err != nil {
			return chatChoice{}, fmt.Errorf("%s：解析响应：%w", service, err)
		}
		if len(out.Error) > 0 && string(out.Error) != "null" {
			return chatChoice{}, chatResponseError(service, http.StatusOK, body)
		}
		if len(out.Choices) == 0 {
			if len(out.Code) > 0 && string(out.Code) != "null" {
				return chatChoice{}, chatResponseError(service, http.StatusOK, body)
			}
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
	var data strings.Builder
	hasData := false
	consume := func() (bool, error) {
		if !hasData {
			return false, nil
		}
		payload := data.String()
		data.Reset()
		hasData = false
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
			return false, chatResponseError(service, http.StatusOK, []byte(payload))
		}
		for _, part := range chunk.Choices {
			if part.Index != 0 {
				continue
			}
			if part.Delta.Content != "" {
				if len(part.Delta.Content) > maxChatTextBytes-text.Len() {
					return false, fmt.Errorf("%s：译文过长，请缩短原文后重试", service)
				}
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
	// The scanner bounds individual lines; the builder also bounds multiline
	// events, including separators and empty data fields.
	scanner.Buffer(make([]byte, 4096), maxChatEventBytes+16)
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
			value := strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " ")
			separator := 0
			if hasData {
				separator = 1
			}
			if len(value) > maxChatEventBytes-data.Len()-separator {
				return choice, fmt.Errorf("%s：单条响应过大，请缩短原文后重试", service)
			}
			if hasData {
				data.WriteByte('\n')
			}
			data.WriteString(value)
			hasData = true
		}
	}
	if err := scanner.Err(); err != nil {
		if err == bufio.ErrTooLong {
			return choice, fmt.Errorf("%s：单条响应过大，请缩短原文后重试", service)
		}
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
