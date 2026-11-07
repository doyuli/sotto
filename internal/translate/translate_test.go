package translate

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"

	"sotto/internal/config"
)

// fakeTranslator reports a canned result, possibly after an error.
type fakeTranslator struct {
	name string
	res  Result
	err  error
}

func (f fakeTranslator) Name() string { return f.name }

func (f fakeTranslator) Available(k KeyStore, _ config.Settings) bool {
	return k.Get(f.name) != ""
}

func (f fakeTranslator) Translate(context.Context, Request, KeyStore, config.Settings) (Result, error) {
	if f.err != nil {
		return Result{}, f.err
	}
	return f.res, nil
}

func TestRunFanOut(t *testing.T) {
	all := []Translator{
		fakeTranslator{name: "a", res: Result{Service: "a", Text: "甲"}},
		fakeTranslator{name: "b", res: Result{Service: "b", Text: "乙"}},
		fakeTranslator{name: "c", res: Result{Service: "c"}, err: ErrQuota},
		fakeTranslator{name: "off", res: Result{Service: "off", Text: "不应出现"}},
	}
	cfg := config.Default()
	cfg.Services.Enabled = []string{"a", "b", "c"}
	cfg.Services.Order = []string{"b", "a", "c", "off"}
	k := fakeKeys{"a": "1", "b": "2", "c": "3"} // "off" has no key: unavailable

	var mu sync.Mutex
	var got []Result
	RunWith(context.Background(), all, cfg, k, Request{Text: "x", TargetLang: "en"}, func(res Result) {
		mu.Lock()
		got = append(got, res)
		mu.Unlock()
	})
	if len(got) != 3 {
		t.Fatalf("results = %v, want 3", got)
	}
	// Results arrive in completion order, so only the set is fixed here;
	// the card order itself is TestOrderTranslatorsAppendsUnnamed's.
	var names []string
	for _, r := range got {
		names = append(names, r.Service)
	}
	// The quota error becomes a user-facing message on its card.
	var c Result
	for _, r := range got {
		if r.Service == "c" {
			c = r
		}
	}
	if c.Err != "本月额度已用完" {
		t.Errorf("c = %+v, want quota message", c)
	}
}

func TestRunAllEnabled(t *testing.T) {
	all := []Translator{fakeTranslator{name: "a", res: Result{Service: "a", Text: "甲"}}}
	cfg := config.Default()
	cfg.Services.Enabled = []string{"a"}
	var count int
	RunWith(context.Background(), all, cfg, fakeKeys{"a": "1"}, Request{TargetLang: "en"}, func(Result) { count++ })
	if count != 1 {
		t.Errorf("count = %d, want 1", count)
	}
}

func TestOrderTranslatorsAppendsUnnamed(t *testing.T) {
	all := []Translator{
		fakeTranslator{name: "a"},
		fakeTranslator{name: "b"},
		fakeTranslator{name: "c"},
	}
	got := orderTranslators(all, []string{"c", "a"})
	var names []string
	for _, t := range got {
		names = append(names, t.Name())
	}
	if !reflect.DeepEqual(names, []string{"c", "a", "b"}) {
		t.Errorf("order = %v", names)
	}
}

// recordingTranslator keeps every request text it was handed.
type recordingTranslator struct {
	name  string
	texts []string
}

func (r *recordingTranslator) Name() string { return r.name }

func (r *recordingTranslator) Available(KeyStore, config.Settings) bool { return true }

func (r *recordingTranslator) Translate(_ context.Context, req Request, _ KeyStore, _ config.Settings) (Result, error) {
	r.texts = append(r.texts, req.Text)
	return Result{Service: r.name, Text: "[" + req.Text + "]"}, nil
}

func TestSplitLongText(t *testing.T) {
	text := strings.Repeat("这是第一句。", 500) // 3000 runes
	parts := splitLongText(text, maxChunkRunes)
	if len(parts) < 2 {
		t.Fatalf("split into %d parts, want more than one", len(parts))
	}
	if got := strings.Join(parts, ""); got != text {
		t.Error("split lost or reordered text")
	}
	for _, p := range parts {
		if n := len([]rune(p)); n > maxChunkRunes {
			t.Errorf("part has %d runes, over the %d limit", n, maxChunkRunes)
		}
	}
	if got := splitLongText("hello", maxChunkRunes); len(got) != 1 || got[0] != "hello" {
		t.Errorf("short text = %q, want it whole", got)
	}
}

func TestTranslateLongMergesChunks(t *testing.T) {
	text := strings.Repeat("这是一个很长的段落。", 300) // 3000 runes
	rec := &recordingTranslator{name: "x"}
	res, err := translateLong(context.Background(), rec,
		Request{Text: text, TargetLang: "en"}, fakeKeys{}, config.Default(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.texts) < 2 {
		t.Fatalf("sent %d requests, want the text split", len(rec.texts))
	}
	if got := strings.Join(rec.texts, ""); got != text {
		t.Error("chunks lost or reordered text")
	}
	if res.Service != "x" || !strings.Contains(res.Text, "这是一个很长的段落") {
		t.Errorf("merged result = %+v", res)
	}
}

func TestUserMessage(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
		{ErrQuota, "本月额度已用完"},
		{ErrAuth, "凭证无效，请检查设置"},
		{ErrNotConfigured, "未配置"},
		{fmt.Errorf("boom"), "boom"},
	}
	for _, tt := range tests {
		if got := UserMessage(tt.err); got != tt.want {
			t.Errorf("UserMessage(%v) = %q, want %q", tt.err, got, tt.want)
		}
	}
}

func TestLanguageMapping(t *testing.T) {
	for _, code := range []string{"zh-HANS", "en", "ja", "ko", "fr", "de", "ru", "es", "pt", "it"} {
		for _, service := range []string{"deepl", "baidu", "google"} {
			if _, ok := mapLanguage(service, code); !ok {
				t.Errorf("%s has no mapping for %s", service, code)
			}
		}
		if englishName(code) == "" {
			t.Errorf("no English name for %s", code)
		}
	}
	if v, _ := mapLanguage("deepl", "zh-HANS"); v != "ZH-HANS" {
		t.Errorf("deepl zh = %q", v)
	}
	if v, _ := mapLanguage("baidu", "ja"); v != "jp" {
		t.Errorf("baidu ja = %q", v)
	}
	if v, _ := mapLanguage("google", "zh-HANS"); v != "zh-CN" {
		t.Errorf("google zh = %q", v)
	}
	// Detected languages map back.
	for _, tt := range []struct{ service, in, want string }{
		{"deepl", "EN", "en"},
		{"deepl", "ZH", "zh-HANS"},
		{"baidu", "jp", "ja"},
		{"google", "zh-CN", "zh-HANS"},
	} {
		if got := fromDetected(tt.service, tt.in); got != tt.want {
			t.Errorf("fromDetected(%s, %s) = %q, want %q", tt.service, tt.in, got, tt.want)
		}
	}
	if got := fromDetected("deepl", "XX"); got != "" {
		t.Errorf("unknown detected language should map to empty, got %q", got)
	}
}
