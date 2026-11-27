package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"sotto/internal/config"
	"sotto/internal/keys"
	"sotto/internal/translate"
	"sotto/internal/ui/bubble"
)

func TestMinimalJobLoadingOnlyWhileWaiting(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// A quick translation must not leave a delayed placeholder behind.
		updates, complete := make(chan string, 2), make(chan translate.Result, 1)
		show := func(text string) { updates <- text }
		done := func(result translate.Result) { complete <- result }
		runMinimalJob(t.Context(), func(context.Context, func(string)) translate.Result {
			return translate.Result{Text: "快速译文"}
		}, show, done)
		time.Sleep(minimalLoadingDelay)
		synctest.Wait()
		if len(updates) != 0 || (<-complete).Text != "快速译文" {
			t.Fatal("fast result displayed a loading state")
		}

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go runMinimalJob(ctx, func(ctx context.Context, _ func(string)) translate.Result {
			<-ctx.Done()
			return translate.Result{}
		}, show, done)
		synctest.Wait()
		time.Sleep(minimalLoadingDelay - time.Millisecond)
		if len(updates) != 0 {
			t.Fatal("loading appeared before the delay")
		}
		time.Sleep(time.Millisecond)
		synctest.Wait()
		if len(updates) != 1 || <-updates != "" || len(complete) != 0 {
			t.Fatal("pending job did not display loading")
		}
		cancel()
		if (<-complete).Text != "" {
			t.Fatal("canceled job returned a translation")
		}
	})
}

func TestMinimalJobFirstChunkSkipsTheLoadingState(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		updates := make(chan string, 2)
		done := make(chan translate.Result, 1)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go runMinimalJob(ctx, func(ctx context.Context, progress func(string)) translate.Result {
			progress("第一段")
			<-ctx.Done()
			progress("已经取消")
			return translate.Result{}
		}, func(text string) { updates <- text }, func(result translate.Result) { done <- result })
		if got := <-updates; got != "第一段" {
			t.Fatalf("first update = %q", got)
		}
		time.Sleep(2 * minimalLoadingDelay)
		synctest.Wait()
		cancel()
		<-done
		if len(updates) != 0 {
			t.Fatal("loading or stale text arrived after the first chunk")
		}
	})
}

func TestMinimalJobsOutliveTheirEntries(t *testing.T) {
	// Reusing the acquisition context would discard the first translation.
	a := &application{}
	first := a.entry.begin(true)
	firstCtx, ok := a.beginMinimalJob(first)
	if !ok {
		t.Fatal("first job rejected")
	}
	second := a.entry.begin(true)
	secondCtx, ok := a.beginMinimalJob(second)
	if !ok || first.Valid() || firstCtx.Err() != nil {
		t.Fatal("second acquisition canceled the accepted first job")
	}
	if !a.finishMinimalJob(second.ID()) || secondCtx.Err() == nil || firstCtx.Err() != nil {
		t.Fatal("finishing the second job affected the first")
	}
	if !a.finishMinimalJob(first.ID()) || a.finishMinimalJob(first.ID()) {
		t.Fatal("first job must finish successfully exactly once")
	}
}

func TestMinimalJobsSurviveModeSwitch(t *testing.T) {
	// Switching while acquiring input or streaming used to discard both translations.
	cfg := config.Default()
	cfg.MinimalTranslation = true
	a := &application{cfg: cfg, cfgPath: filepath.Join(t.TempDir(), "settings.json"), bubbles: bubble.NewManager(nil, bubble.Actions{})}
	streaming := a.entry.begin(true)
	ctx, ok := a.beginMinimalJob(streaming)
	if !ok {
		t.Fatal("streaming job rejected")
	}
	acquiring := a.entry.begin(true)
	if err := a.setMinimalTranslation(false); err != nil {
		t.Fatal(err)
	}
	if ctx.Err() != nil || !a.finishMinimalJob(streaming.ID()) {
		t.Fatal("switching modes discarded the streaming translation")
	}
	if _, ok := a.beginMinimalJob(acquiring); !ok || !a.finishMinimalJob(acquiring.ID()) {
		t.Fatal("input acquired after switching could not complete in its original mode")
	}
}

func TestStoppedMinimalJobsRejectLateResults(t *testing.T) {
	a := &application{}
	first := a.entry.begin(true)
	ctx, ok := a.beginMinimalJob(first)
	if !ok {
		t.Fatal("job rejected")
	}
	a.stopMinimalJobs()
	if ctx.Err() == nil || a.finishMinimalJob(first.ID()) {
		t.Fatal("stopped job could still present a late result")
	}
}

func TestMinimalTranslationOnlyReadsFirstEnabledProvider(t *testing.T) {
	cfg := config.Default()
	cfg.Services.Enabled = []string{"azure", "openai", "dictionary"}
	cfg.Services.Order = []string{"dictionary", "google", "openai", "azure"}
	keyring := &minimalTestKeyring{serviceTestKeyring: serviceTestKeyring{
		values: map[string]string{keys.OpenAIKey: "openai", keys.AzureKey: "azure", keys.AzureRegion: "region"},
	}}
	a := &application{cfg: cfg, keys: keyring}
	got, err := a.prepareMinimalRequest(t.Context(), "测试文本")
	if err != nil || !slices.Equal(got.settings.Services.Enabled, []string{"openai"}) ||
		!slices.Equal(keyring.read, []string{keys.OpenAIKey}) || got.request.TargetLang != "en" {
		t.Fatalf("enabled=%v target=%s credentials=%v err=%v", got.settings.Services.Enabled, got.request.TargetLang, keyring.read, err)
	}
	a.cfg.Services.Order = []string{"azure", "openai"}
	a.cfg.TargetLanguage = "ja"
	keyring.read = nil
	got, err = a.prepareMinimalRequest(t.Context(), "测试文本")
	if err != nil || !slices.Equal(got.settings.Services.Enabled, []string{"azure"}) ||
		!slices.Equal(keyring.read, []string{keys.AzureKey, keys.AzureRegion}) || got.request.TargetLang != "ja" {
		t.Fatalf("after settings change: enabled=%v target=%s credentials=%v err=%v", got.settings.Services.Enabled, got.request.TargetLang, keyring.read, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	keyring.read = nil
	_, err = a.prepareMinimalRequest(ctx, "测试文本")
	if len(keyring.read) != 0 || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled job: credentials=%v err=%v", keyring.read, err)
	}
}

func TestMinimalTranslationUsesUpdatedEnabledServices(t *testing.T) {
	// The input snapshot used to exclude a newly enabled service after a settings write.
	original := config.Default()
	original.Services.Enabled = []string{"openai", "google"}
	original.Services.Order = []string{"openai", "google", "youdaoweb"}
	current := original
	current.Services.Enabled = []string{"youdaoweb"}
	a := &application{cfg: current, keys: &unusedKeyring{t: t}}

	provider, _, err := a.pickMinimalProvider(t.Context(), &original)
	if err != nil || provider == nil || provider.Name() != "youdaoweb" {
		t.Fatalf("provider = %v, err = %v", provider, err)
	}
}

func TestMinimalTranslationSkipsUnavailableMagpie(t *testing.T) {
	// A failed Magpie probe used to re-read credentials for providers it had
	// already checked. The settings lock must also be free during the probe.
	probing, release := make(chan struct{}, 2), make(chan struct{})
	var released sync.Once
	defer released.Do(func() { close(release) })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		probing <- struct{}{}
		<-release
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)
	cfg := config.Default()
	cfg.Services.Magpie.Port = server.Listener.Addr().(*net.TCPAddr).Port
	cfg.Services.Enabled = []string{"openai", "magpie", "google"}
	cfg.Services.Order = []string{"openai", "magpie", "azure", "google"}
	keyring := &minimalTestKeyring{serviceTestKeyring: serviceTestKeyring{
		values: map[string]string{keys.AzureKey: "azure"},
	}}
	a := &application{cfg: cfg, keys: keyring}
	done := make(chan error, 1)
	go func() {
		got, err := a.prepareMinimalRequest(t.Context(), "Hello world")
		if err == nil && (got.provider == nil || got.provider.Name() != "azure") {
			err = errors.New("the enabled fallback was not selected")
		}
		done <- err
	}()
	select {
	case <-probing:
	case <-time.After(time.Second):
		t.Fatal("Magpie was not probed")
	}
	if !a.configWriteMu.TryLock() {
		t.Fatal("Magpie probe blocked settings writes")
	}
	// Azure was enabled while discovery was in flight and was absent from the original candidates.
	a.mu.Lock()
	a.cfg.Services.Enabled = []string{"openai", "magpie", "azure"}
	a.mu.Unlock()
	a.configWriteMu.Unlock()
	released.Do(func() { close(release) })
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(keyring.read, []string{keys.OpenAIKey, keys.AzureKey, keys.AzureRegion}) {
		t.Fatalf("unexpected credential reads during fallback: %v", keyring.read)
	}
	a.cfg.Services.Enabled = []string{"magpie"}
	if _, err := a.prepareMinimalRequest(t.Context(), "Hello world"); !strings.Contains(translate.UserMessage(err), "未发现 Magpie") {
		t.Fatalf("offline Magpie without a fallback: %v", err)
	}
}

func TestMinimalTranslationUsesYoudaoWebAsTranslator(t *testing.T) {
	// Minimal results show translated text, while the full bubble displays dictionary entries.
	cfg := config.Default()
	cfg.Services.Enabled = []string{"dictionary", "youdaoweb"}
	cfg.Services.Order = cfg.Services.Enabled
	a := &application{cfg: cfg, keys: &unusedKeyring{t: t}}
	minimal, err := a.prepareMinimalRequest(t.Context(), "classical")
	if err != nil || !minimal.request.TranslationOnly || minimal.request.Text != "classical" ||
		!slices.Equal(minimal.settings.Services.Enabled, []string{"youdaoweb"}) {
		t.Fatalf("minimal request = %+v, services = %v, err = %v", minimal.request, minimal.settings.Services.Enabled, err)
	}
}

func TestMinimalTranslationSkipsMissingCredentialsButReportsReadFailures(t *testing.T) {
	// A key removed outside Settings used to block its visible first service.
	cfg := config.Default()
	cfg.Services.Enabled = []string{"openai", "baidu", "google", "deepl"}
	cfg.Services.Order = slices.Clone(cfg.Services.Enabled)
	keyring := &minimalTestKeyring{serviceTestKeyring: serviceTestKeyring{
		values: map[string]string{keys.BaiduAppID: "baidu"},
	}}
	a := &application{cfg: cfg, keys: keyring}
	got, err := a.prepareMinimalRequest(t.Context(), "Test input")
	if err != nil || !slices.Equal(got.settings.Services.Enabled, []string{"google"}) ||
		!slices.Equal(keyring.read, []string{keys.OpenAIKey, keys.BaiduAppID, keys.BaiduKey}) {
		t.Fatalf("enabled=%v credentials=%v err=%v", got.settings.Services.Enabled, keyring.read, err)
	}
	keyring.failRead = keys.OpenAIKey
	keyring.read = nil
	_, err = a.prepareMinimalRequest(t.Context(), "Test input")
	if !errors.Is(err, keys.ErrUnavailable) || !slices.Equal(keyring.read, []string{keys.OpenAIKey}) {
		t.Fatalf("unavailable keyring: credentials=%v err=%v", keyring.read, err)
	}
}

func TestMinimalTranslationRejectsEmptyPreparedSourceBeforeReadingCredentials(t *testing.T) {
	// Cleaning the source can reject a request before the keyring prompts the user.
	cfg := config.Default()
	cfg.Identifier.StripMarkdown = true
	cfg.Services.Enabled = []string{"openai"}
	a := &application{cfg: cfg, keys: &unusedKeyring{t: t}}
	got := a.minimalTranslation(t.Context(), "---", nil)
	if got.Err != translate.UserMessage(translate.ErrEmptySource) {
		t.Fatalf("empty source result: %+v", got)
	}
}

type minimalTestKeyring struct {
	serviceTestKeyring
	read []string
}

func (k *minimalTestKeyring) Snapshot(names []string) keys.Snapshot {
	k.read = append(k.read, names...)
	return k.serviceTestKeyring.Snapshot(names)
}

func TestMinimalTranslationUsesBubbleLanguageRules(t *testing.T) {
	// The overlay used to infer the source language and override the saved target.
	for _, tc := range []struct{ name, text, configured, target string }{
		{"Chinese with default target", "请审查这次改动。", "zh-HANS", "English"},
		{"English with Greek symbol", "\t The angle θ is measured in radians. \n", "zh-HANS", "Simplified Chinese"},
		{"configured Japanese target", "Review the change.", "ja", "Japanese"},
		{"French with English target", "Bonjour, comment allez-vous ?", "en", "English"},
		{"automatic target", "今日は晴れです。", "auto", "Simplified Chinese"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var payload struct {
					Messages []struct{ Role, Content string }
				}
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Errorf("decode translation request: %v", err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				if !slices.ContainsFunc(payload.Messages, func(m struct{ Role, Content string }) bool {
					return m.Role == "system" && strings.Contains(m.Content, "Translate the user's input into "+tc.target+".")
				}) {
					t.Errorf("expected auto source and target %q, messages=%+v", tc.target, payload.Messages)
				}
				if !slices.ContainsFunc(payload.Messages, func(m struct{ Role, Content string }) bool {
					return m.Role == "user" && m.Content == strings.TrimSpace(tc.text)
				}) {
					t.Errorf("expected trimmed source %q, messages=%+v", strings.TrimSpace(tc.text), payload.Messages)
				}
				io.WriteString(w, `{"choices":[{"message":{"content":"translated"}}]}`)
			}))
			defer server.Close()
			cfg := config.Default()
			cfg.TargetLanguage = tc.configured
			cfg.Services.Enabled = []string{"openai"}
			cfg.Services.OpenAI.BaseURL = server.URL
			a := &application{cfg: cfg, keys: &serviceTestKeyring{values: map[string]string{keys.OpenAIKey: "test"}}}
			if got := a.minimalTranslation(t.Context(), tc.text, func(string) {}); got.Text != "translated" || got.Err != "" {
				t.Fatalf("expected provider result, got %+v", got)
			}
		})
	}
}

func TestMinimalTranslationPublishesTextBeforeFirstProviderFinishes(t *testing.T) {
	// The first chunk must reach the overlay while the provider is still waiting.
	var calls atomic.Int32
	release := make(chan struct{})
	finishResponse := sync.OnceFunc(func() { close(release) })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var request struct {
			Stream   bool
			Messages []struct{ Role, Content string }
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil || !request.Stream {
			t.Errorf("expected a supported streaming request: %+v, %v", request, err)
		}
		if !slices.ContainsFunc(request.Messages, func(message struct{ Role, Content string }) bool {
			return message.Role == "user" && strings.Contains(message.Content, "review")
		}) {
			t.Error("request missing the selected word")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"审\"}}]}\n\n")
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"查\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	defer finishResponse()
	cfg := config.Default()
	cfg.Services.Enabled = []string{"deepl", "openai", "dictionary"}
	cfg.Services.Order = []string{"openai", "deepl"}
	cfg.Services.OpenAI.BaseURL = server.URL
	a := &application{cfg: cfg, keys: &serviceTestKeyring{values: map[string]string{keys.OpenAIKey: "test"}}}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	updates, done := make(chan string, 2), make(chan translate.Result, 1)
	go func() {
		done <- a.minimalTranslation(ctx, "review", func(text string) { updates <- text })
	}()
	select {
	case first := <-updates:
		if first != "审" {
			t.Fatalf("first chunk = %q", first)
		}
	case <-ctx.Done():
		t.Fatal("no streamed text before the service finished")
	}
	finishResponse()
	select {
	case got := <-done:
		if got.Text != "审查" || got.Err != "" || calls.Load() != 1 {
			t.Fatalf("result=%+v, calls=%d", got, calls.Load())
		}
	case <-ctx.Done():
		t.Fatal("no final translation")
	}
}
