package translate

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"sotto/internal/config"
	"sotto/internal/keys"
)

func TestChatStreamArrivesBeforeCompletion(t *testing.T) {
	for _, name := range []string{"openai", "index-translate"} {
		t.Run(name, func(t *testing.T) {
			first := make(chan Result, 3)
			release := make(chan struct{})
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var input struct{ Stream bool }
				if err := json.NewDecoder(r.Body).Decode(&input); err != nil || !input.Stream {
					t.Errorf("stream not requested: %+v, %v", input, err)
				}
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, ": heartbeat\r\n\r\ndata: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"}}]}\r\n\r\n")
				fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"你好\"}}]}\r\n\r\n")
				fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"，世界\"}}]}\n\n")
				w.(http.Flusher).Flush()
				select {
				case <-release:
				case <-r.Context().Done():
					return
				}
				fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: {\"choices\":[],\"usage\":{}}\n\ndata: [DONE]\n\n")
			}))
			defer server.Close()
			cfg := config.Default()
			cfg.Services.Enabled = []string{name}
			cfg.Services.OpenAI.BaseURL = server.URL
			var translator Translator = OpenAI{Client: server.Client()}
			if name == "index-translate" {
				translator = IndexTranslate{Client: server.Client(), Endpoint: server.URL}
			}
			var updates []Result
			done := make(chan struct{})
			go func() {
				defer close(done)
				RunWith(ctx, []Translator{translator}, cfg, fakeKeys{keys.OpenAIKey: "test"}, Request{Text: "hello", TargetLang: "zh-HANS"}, func(res Result) {
					updates = append(updates, res)
					if res.Streaming {
						first <- res
					}
				})
			}()
			select {
			case res := <-first:
				if res.Text != "你好" || !res.Streaming || res.Err != "" {
					t.Errorf("first = %+v", res)
				}
			case <-ctx.Done():
				t.Error("no text before the response finished")
			}
			select {
			case res := <-first:
				if res.Text != "你好，世界" || !res.Streaming {
					t.Errorf("paused burst = %+v", res)
				}
			case <-ctx.Done():
				t.Error("latest burst was not flushed while the server paused")
			}
			close(release)
			<-done
			if len(updates) < 2 {
				t.Fatalf("updates = %+v", updates)
			}
			last := updates[len(updates)-1]
			if last.Text != "你好，世界" || last.Streaming || last.Err != "" {
				t.Errorf("terminal = %+v", last)
			}
		})
	}
}

func TestChatStreamFailuresKeepPartialText(t *testing.T) {
	const prefix = "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"已有译文\"}}]}\n\n"
	for _, tc := range []struct{ name, tail, want string }{
		{"disconnect", "", "连接中断"},
		{"malformed", "data: {oops}\n\n", "解析流式响应"},
		{"provider error", "event: error\ndata: {\"error\":{\"message\":\"private gateway details\"}}\n\n", "生成中断"},
		{"length", "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"length\"}]}\n\ndata: [DONE]\n\n", "译文被截断"},
		{"filter", "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"content_filter\"}]}\n\ndata: [DONE]\n\n", "译文被服务拦截"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, prefix+tc.tail) }))
			defer server.Close()
			cfg := config.Default()
			cfg.Services.Enabled = []string{"index-translate"}
			var got []Result
			RunWith(context.Background(), []Translator{IndexTranslate{Client: server.Client(), Endpoint: server.URL}}, cfg, nil, Request{Text: "hello", TargetLang: "en"}, func(res Result) { got = append(got, res) })
			last := got[len(got)-1]
			if last.Text != "已有译文" || last.Streaming || !strings.Contains(last.Err, tc.want) || strings.Contains(last.Err, "private gateway") {
				t.Errorf("terminal = %+v", last)
			}
		})
	}
}

func TestChatStreamFraming(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
		fail             bool
	}{
		{"multiline and final event", "data: {\"choices\":\ndata: [{\"index\":0,\"delta\":{\"content\":\"yes\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]", "yes", false},
		{"other choices ignored", "data: {\"choices\":[{\"index\":1,\"delta\":{\"content\":\"wrong\"}},{\"index\":0,\"delta\":{\"content\":\"yes\"},\"finish_reason\":\"stop\"}]}\n\n", "yes", false},
		{"empty", "data: [DONE]\n\n", "", true},
		{"reasoning ignored", "data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"private\"},\"finish_reason\":\"stop\"}]}\n\n", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := readChatStream(strings.NewReader(tc.body), "test", nil)
			if res.Message.Content != tc.want || (err != nil) != tc.fail {
				t.Errorf("choice = %+v, err = %v", res, err)
			}
		})
	}
}

func TestChatStreamCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	closed := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(closed)
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	cfg := config.Default()
	cfg.Services.Enabled = []string{"index-translate"}
	var got []Result
	RunWith(ctx, []Translator{IndexTranslate{Client: server.Client(), Endpoint: server.URL}}, cfg, nil, Request{Text: "hi", TargetLang: "en"}, func(res Result) { got = append(got, res); cancel() })
	if len(got) != 1 || !got[0].Streaming {
		t.Errorf("cancellation emitted stale completion: %+v", got)
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Error("HTTP request was not canceled")
	}
}

type streamChunks struct {
	calls int
	fail  bool
}

func (*streamChunks) Name() string                             { return "chunks" }
func (*streamChunks) Available(KeyStore, config.Settings) bool { return true }
func (*streamChunks) Translate(context.Context, Request, KeyStore, config.Settings) (Result, error) {
	panic("stream expected")
}
func (s *streamChunks) Stream(_ context.Context, _ Request, _ KeyStore, _ config.Settings, emit func(Result)) (Result, error) {
	s.calls++
	text := fmt.Sprintf("译文%d", s.calls)
	emit(Result{Text: text})
	res := Result{Text: text}
	if s.fail && s.calls == 2 {
		return res, fmt.Errorf("offline")
	}
	return res, nil
}

func TestStreamingLongTextMergesProgressAndErrors(t *testing.T) {
	for _, fail := range []bool{false, true} {
		s := &streamChunks{fail: fail}
		var got []string
		res, err := translateLong(context.Background(), s, Request{Text: strings.Repeat("字", maxChunkRunes+1)}, nil, config.Default(), func(r Result) {
			if !r.Streaming {
				t.Error("progress must be streaming")
			}
			got = append(got, r.Text)
		})
		if !reflect.DeepEqual(got, []string{"译文1", "译文1\n译文2"}) || res.Text != "译文1\n译文2" || (err != nil) != fail {
			t.Errorf("progress=%v result=%+v err=%v", got, res, err)
		}
	}
}

func TestStreamProgressStopsScheduledUpdates(t *testing.T) {
	updates := make(chan Result, 3)
	progress, stop := streamProgress(context.Background(), func(r Result) { updates <- r })
	progress(Result{Text: "first", Streaming: true})
	progress(Result{Text: "pending", Streaming: true})
	stop()
	// Discard updates delivered before stop; none may follow a terminal result.
	for len(updates) > 0 {
		<-updates
	}
	select {
	case result := <-updates:
		t.Fatalf("scheduled partial arrived after stop: %+v", result)
	case <-time.After(80 * time.Millisecond):
	}
}

func TestChatStreamAllowsLargeReasoningEnvelope(t *testing.T) {
	// Repeated SSE metadata can exceed the JSON response limit even when the
	// combined reasoning and translation are small. Reasoning is not output.
	reasoning := `data: {"id":"` + strings.Repeat("a", 100) + `","object":"chat.completion.chunk","model":"compatible-reasoning-model","choices":[{"index":0,"delta":{"reasoning_content":"thinking"},"finish_reason":null}]}` + "\n\n"
	body := strings.Repeat(reasoning, 4000) + "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"你好\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"
	if len(body) <= 1<<20 {
		t.Fatal("fixture must exceed the former whole-response limit")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, body)
	}))
	defer server.Close()
	cfg := config.Default()
	cfg.Services.OpenAI.BaseURL = server.URL
	var progress []string
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	res, err := (OpenAI{Client: server.Client()}).Stream(ctx,
		Request{Text: strings.Repeat("字", maxChunkRunes), TargetLang: "zh-HANS"},
		fakeKeys{keys.OpenAIKey: "test"}, cfg, func(r Result) { progress = append(progress, r.Text) })
	if err != nil || res.Text != "你好" || !reflect.DeepEqual(progress, []string{"你好"}) {
		t.Fatalf("large reasoning envelope: result = %+v, progress = %v, err = %v", res, progress, err)
	}
}

func TestChatStreamSizeLimitsKeepPartialTranslation(t *testing.T) {
	const translated = "已有译文"
	prefix := "data: {\"choices\":[{\"delta\":{\"content\":\"" + translated + "\"}}]}\n\n"
	part := strings.Repeat("界", maxChatTextBytes/6)
	contentEvent := "data: {\"choices\":[{\"delta\":{\"content\":\"" + part + "\"}}]}\n\n"
	for _, tc := range []struct{ name, tail, wantText, wantError string }{
		{
			name:     "oversized single line event",
			tail:     "data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"" + strings.Repeat("x", maxChatEventBytes) + "\"}}]}\n\n",
			wantText: translated, wantError: "单条响应过大",
		},
		{
			name: "oversized multiline event",
			tail: strings.Repeat("data: "+strings.Repeat(" ", 1024)+"\n", maxChatEventBytes/1024+1) +
				"data: {\"choices\":[{\"delta\":{\"content\":\"not accepted\"}}]}\n\n",
			wantText: translated, wantError: "单条响应过大",
		},
		{
			name:     "cumulative translation across bounded events",
			tail:     contentEvent + contentEvent + "data: [DONE]\n\n",
			wantText: translated + part, wantError: "译文过长",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := readChatStream(strings.NewReader(prefix+tc.tail), "test", nil)
			if res.Message.Content != tc.wantText || err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("retained bytes = %d, want %d; err = %v, want %q", len(res.Message.Content), len(tc.wantText), err, tc.wantError)
			}
		})
	}
}

func TestChatJSONFallbackRemainsBounded(t *testing.T) {
	body := `{"choices":[{"message":{"content":"` + strings.Repeat("x", maxChatJSONBytes) + `"}}]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, body)
	}))
	defer server.Close()
	cfg := config.Default()
	cfg.Services.OpenAI.BaseURL = server.URL
	res, err := (OpenAI{Client: server.Client()}).Translate(context.Background(),
		Request{Text: "hi", TargetLang: "zh-HANS"}, fakeKeys{keys.OpenAIKey: "test"}, cfg)
	if res.Text != "" || err == nil || !strings.Contains(err.Error(), "响应过大") {
		t.Fatalf("oversized JSON: text bytes = %d, err = %v", len(res.Text), err)
	}
}
