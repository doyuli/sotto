package translate

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"sotto/internal/config"
)

func TestMagpieDiscoversConfiguredPortAndStreams(t *testing.T) {
	// A custom gateway port must be discovered without taking over OpenAI settings.
	var hello, catalogs, requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer magpie-sotto" {
			t.Error("missing Magpie client identity")
		}
		switch r.URL.Path {
		case "/api/hello":
			hello.Add(1)
			io.WriteString(w, `{"name":"magpie","version":"1.0.0"}`)
		case "/v1/models":
			if catalogs.Add(1) == 1 {
				io.WriteString(w, `{"data":[{"id":"group/translation","reasoning_levels":["none","high"]}]}`)
			} else {
				io.WriteString(w, `{"data":[{"id":"group/translation","reasoning_levels":["none","high"]},{"id":"other/chat","reasoning_levels":["low","high"]}]}`)
			}
		case "/v1/chat/completions":
			request := requests.Add(1)
			var payload struct {
				Model           string
				Stream          bool
				Messages        []struct{ Role, Content string }
				ReasoningEffort json.RawMessage `json:"reasoning_effort"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Errorf("decode request: %v", err)
			}
			// Sending "none" to a model that only offers low/high can reject the entire request.
			wantModel, wantEffort := "group/translation", `"none"`
			if request == 3 {
				wantModel, wantEffort = "other/chat", ""
			}
			if !payload.Stream || payload.Model != wantModel || string(payload.ReasoningEffort) != wantEffort ||
				!slices.ContainsFunc(payload.Messages, func(m struct{ Role, Content string }) bool {
					return m.Role == "user" && strings.HasPrefix(strings.ToLower(m.Content), "hello")
				}) {
				t.Errorf("unexpected translation request: %+v", payload)
			}
			w.Header().Set("Content-Type", "text/event-stream")
			io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"你好\"}}]}\n\ndata: [DONE]\n\n")
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if err := os.Mkdir(filepath.Join(dir, "magpie"), 0700); err != nil {
		t.Fatal(err)
	}
	port := server.Listener.Addr().(*net.TCPAddr).Port
	if err := os.WriteFile(filepath.Join(dir, "magpie", "settings.json"), fmt.Appendf(nil, `{"port":%d}`, port), 0600); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	cfg.Services.Enabled = []string{"magpie"}
	var final Result
	parts := strings.Repeat("hello\n", maxChunkRunes/6+1)
	Run(t.Context(), cfg, nil, Request{Text: parts, TargetLang: "zh-HANS"}, func(result Result) {
		if !result.Streaming {
			final = result
		}
	})
	if final.Service != "magpie" || final.Text != "你好\n你好" || final.Err != "" {
		t.Fatalf("translation = %+v", final)
	}
	if hello.Load() != 1 || catalogs.Load() != 1 || requests.Load() != 2 {
		t.Fatalf("hello=%d catalogs=%d translations=%d", hello.Load(), catalogs.Load(), requests.Load())
	}
	cfg.Services.Magpie.Model = "other/chat"
	if err := Test(t.Context(), "magpie", cfg, nil); err != nil {
		t.Fatalf("connection test failed: %v", err)
	}
}

func TestMagpieVerifiesGatewayBeforeSendingText(t *testing.T) {
	// A different HTTP service can be listening on the configured port.
	var called atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called.Add(1)
		io.WriteString(w, `{"name":"other-service"}`)
	}))
	t.Cleanup(server.Close)
	cfg := config.Default()
	cfg.Services.Magpie.Port = server.Listener.Addr().(*net.TCPAddr).Port
	_, err := (Magpie{}).Stream(t.Context(), Request{Text: "hello", TargetLang: "zh-HANS"}, nil, cfg, nil)
	if err == nil || called.Load() != 1 || !strings.Contains(UserMessage(err), "未发现 Magpie") {
		t.Fatalf("discovery err=%v calls=%d", err, called.Load())
	}
}

func TestMagpieDoesNotSilentlyChangeSelectedModel(t *testing.T) {
	gateway := MagpieGateway{Models: []string{"group/translation", "workbuddy/auto"}}
	if selected, err := gateway.SelectModel(""); selected != "" || err == nil {
		t.Fatalf("ambiguous catalog selected %q, err=%v", selected, err)
	}
	if selected, err := gateway.SelectModel("group/translation"); selected != "group/translation" || err != nil {
		t.Fatalf("explicit selection = %q, err=%v", selected, err)
	}
	gateway.Models = []string{"workbuddy/auto"}
	if selected, err := gateway.SelectModel("group/translation"); selected != "" || err == nil {
		t.Fatalf("removed model replaced with %q, err=%v", selected, err)
	}
}

func TestMagpieDoesNotFollowRedirects(t *testing.T) {
	var redirected atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirected.Add(1)
	}))
	t.Cleanup(destination.Close)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/hello":
			io.WriteString(w, `{"name":"magpie"}`)
		case "/v1/models":
			io.WriteString(w, `{"data":[{"id":"group/translation"}]}`)
		default:
			http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
		}
	}))
	t.Cleanup(server.Close)

	cfg := config.Default()
	cfg.Services.Magpie.Port = server.Listener.Addr().(*net.TCPAddr).Port
	_, err := (Magpie{}).Stream(t.Context(), Request{Text: "hello", TargetLang: "zh-HANS"}, nil, cfg, nil)
	if err == nil || redirected.Load() != 0 {
		t.Fatalf("redirect err=%v redirected=%d", err, redirected.Load())
	}
}
