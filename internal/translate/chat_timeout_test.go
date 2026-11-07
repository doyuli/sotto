package translate

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"sotto/internal/config"
	"sotto/internal/keys"
)

type chatRoundTripFunc func(*http.Request) (*http.Response, error)

func (f chatRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Pipes and timers stay inside synctest's fake clock. No network or real
// minute-long waits are needed to test the production timeout durations.
func timedChatClient(write func(context.Context, *io.PipeWriter)) *http.Client {
	return &http.Client{Timeout: defaultChatClient.Timeout, Transport: chatRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		reader, writer := io.Pipe()
		stop := context.AfterFunc(r.Context(), func() { _ = writer.CloseWithError(r.Context().Err()) })
		go func() {
			defer stop()
			defer writer.Close()
			write(r.Context(), writer)
		}()
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: reader}, nil
	})}
}

func TestChatStreamContinuesBeyondThirtySeconds(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		client := timedChatClient(func(ctx context.Context, w *io.PipeWriter) {
			// First-body latency can exceed 30 seconds, as can total duration.
			time.Sleep(45 * time.Second)
			for _, data := range []string{": heartbeat\n\n", "data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"thinking\"}}]}\n\n"} {
				if _, err := io.WriteString(w, data); err != nil {
					return
				}
				time.Sleep(20 * time.Second)
			}
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"你好\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
		})
		start := time.Now()
		choice, err := requestChatCompletion(context.Background(), client, "http://test.invalid", "", map[string]any{}, "test", true, nil)
		if err != nil || choice.Message.Content != "你好" || time.Since(start) != 85*time.Second {
			t.Fatalf("elapsed=%v choice=%+v err=%v", time.Since(start), choice, err)
		}
	})
}

func TestChatTimeoutPhasesAndCancellation(t *testing.T) {
	for _, name := range []string{"first body byte", "idle after text", "parent deadline", "parent deadline after text", "explicit cancellation"} {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				if strings.HasPrefix(name, "parent deadline") {
					var stop context.CancelFunc
					ctx, stop = context.WithTimeout(ctx, 12*time.Second)
					defer stop()
				}
				client := timedChatClient(func(ctx context.Context, w *io.PipeWriter) {
					if name != "first body byte" && name != "parent deadline" {
						fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"已有译文\"}}]}\n\n")
					}
					if name == "explicit cancellation" {
						time.Sleep(time.Second)
						cancel()
					}
					<-ctx.Done()
				})
				start := time.Now()
				choice, err := requestChatCompletion(ctx, client, "http://test.invalid", "", map[string]any{}, "test", true, nil)
				switch name {
				case "first body byte":
					if time.Since(start) != chatFirstByteTimeout || !errors.Is(err, ErrGenerationTimeout) {
						t.Fatalf("elapsed=%v err=%v", time.Since(start), err)
					}
				case "idle after text":
					if time.Since(start) != chatIdleTimeout || choice.Message.Content != "已有译文" || UserMessage(err) != "生成超时，译文未完成，请重试" {
						t.Fatalf("elapsed=%v choice=%+v err=%v", time.Since(start), choice, err)
					}
				case "parent deadline":
					if time.Since(start) != 12*time.Second || !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrGenerationTimeout) {
						t.Fatalf("elapsed=%v err=%v", time.Since(start), err)
					}
				case "parent deadline after text":
					if choice.Message.Content != "已有译文" || !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, ErrGenerationTimeout) {
						t.Fatalf("partial deadline: choice=%+v err=%v", choice, err)
					}
				case "explicit cancellation":
					if !errors.Is(err, context.Canceled) || errors.Is(err, ErrGenerationTimeout) {
						t.Fatalf("cancellation mapped to timeout: %v", err)
					}
				}
			})
		})
	}
}

func TestChatActivityCannotExpireAfterStopOrRecentBytes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, activity := newChatActivity(context.Background())
		activity.reset(chatIdleTimeout)
		time.Sleep(chatIdleTimeout - time.Second)
		activity.reset(chatIdleTimeout)
		activity.expire() // Simulate a callback already dispatched before Reset.
		time.Sleep(2 * time.Second)
		if ctx.Err() != nil {
			t.Fatalf("stale callback canceled active request: %v", context.Cause(ctx))
		}
		activity.stop()
		time.Sleep(chatFirstByteTimeout)
		if errors.Is(context.Cause(ctx), errChatIdleTimeout) {
			t.Fatal("timer expired after cleanup")
		}
	})
}

func TestConnectionProbeDoesNotRequireStreaming(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"stream":false`) || r.Header.Get("Accept") != "application/json" {
			t.Error("connection probe requested streaming")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":"你好世界"}}]}`)
	}))
	defer server.Close()
	cfg := config.Default()
	cfg.Services.OpenAI.BaseURL = server.URL
	if err := Test(context.Background(), "openai", cfg, fakeKeys{keys.OpenAIKey: "test"}); err != nil {
		t.Fatal(err)
	}
}

func TestChatEmptyResponsesAreReadable(t *testing.T) {
	for _, body := range []string{"", " \r\n\t  "} {
		t.Run(fmt.Sprintf("bytes=%d", len(body)), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
			defer server.Close()
			cfg := config.Default()
			cfg.Services.OpenAI.BaseURL = server.URL
			for _, stream := range []bool{false, true} {
				_, err := (OpenAI{Client: server.Client()}).translate(context.Background(), Request{Text: "hi", TargetLang: "en"}, fakeKeys{keys.OpenAIKey: "test"}, cfg, stream, nil)
				if UserMessage(err) != "OpenAI：未返回译文" {
					t.Fatalf("stream=%v err=%v", stream, err)
				}
			}
		})
	}
}

func TestChatResponseHeaderTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		server, connection := net.Pipe()
		defer server.Close()
		defer connection.Close()
		client := newChatClient()
		transport := client.Transport.(*http.Transport)
		defer transport.CloseIdleConnections()
		transport.DialContext = func(context.Context, string, string) (net.Conn, error) { return connection, nil }
		go func() {
			req, err := http.ReadRequest(bufio.NewReader(server))
			if err != nil {
				return
			}
			_, _ = io.Copy(io.Discard, req.Body)
			_ = req.Body.Close()
			// Read until the timed-out client closes; never send response headers.
			_, _ = io.Copy(io.Discard, server)
		}()
		start := time.Now()
		_, err := requestChatCompletion(context.Background(), client, "http://test.invalid", "", map[string]any{}, "test", true, nil)
		if time.Since(start) != 30*time.Second || UserMessage(err) != "连接超时，请稍后重试" {
			t.Fatalf("header wait = %v, err = %v", time.Since(start), err)
		}
	})
}
