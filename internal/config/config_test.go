package config

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
)

func TestDefault(t *testing.T) {
	s := Default()
	if s.TargetLanguage != "zh-HANS" {
		t.Errorf("default target = %q, want zh-HANS", s.TargetLanguage)
	}
	if s.Hotkeys.Panel != "Ctrl+Alt+A" || s.Hotkeys.Ocr != "Ctrl+Alt+S" || s.Hotkeys.Selection != "Ctrl+Alt+D" {
		t.Errorf("default hotkeys = %+v", s.Hotkeys)
	}
	if !s.Identifier.AutoSplit || !s.Bubble.AutoHide {
		t.Errorf("autoSplit/autoHide should default to on")
	}
	if s.Identifier.StripCodeComments || s.Identifier.FlattenNewlines || s.Identifier.StripMarkdown {
		t.Error("source cleanup should default to off")
	}
	if s.AutoUpdate {
		t.Error("automatic updates should default to off")
	}
	if len(s.CopyFormats) == 0 {
		t.Error("copy formats must default to a list")
	}
	if !reflect.DeepEqual(s.Services.Enabled, []string{"index-translate"}) {
		t.Errorf("enabled = %v, want only Index-Translate", s.Services.Enabled)
	}
}

func TestLoadMissingCreatesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s, Default()) {
		t.Errorf("missing file should give defaults, got %+v", s)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("defaults were not written: %v", err)
	}
}

func TestRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	s := Default()
	s.TargetLanguage = "en"
	s.AutoUpdate = true
	s.CopyFormats = []string{"snake", "kebab"}
	s.Services.Enabled = []string{"deepl", "google"}
	s.Services.Order = []string{"deepl", "google"}
	s.Identifier = IdentifierSettings{StripCodeComments: true, FlattenNewlines: true, StripMarkdown: true}
	if err := Save(path, s); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.TargetLanguage != "en" {
		t.Errorf("target = %q, want en", got.TargetLanguage)
	}
	if !got.AutoUpdate {
		t.Error("automatic update preference was not saved")
	}
	if !reflect.DeepEqual(got.CopyFormats, []string{"snake", "kebab"}) {
		t.Errorf("copy formats = %v", got.CopyFormats)
	}
	if !reflect.DeepEqual(got.Services.Enabled, []string{"deepl", "google"}) {
		t.Errorf("enabled = %v", got.Services.Enabled)
	}
	if !slices.Contains(got.Services.Order, "index-translate") {
		t.Errorf("Index-Translate should be available to enable in existing settings: %v", got.Services.Order)
	}
	if got.Identifier != s.Identifier {
		t.Errorf("identifier settings = %+v, want %+v", got.Identifier, s.Identifier)
	}
}

func TestAllServicesDisabledRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	s := Default()
	s.Services.Enabled = []string{}
	if err := Save(path, Normalize(s)); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Services.Enabled, []string{}) {
		t.Errorf("enabled = %#v, want an empty list", got.Services.Enabled)
	}
}

func TestLoadDamagedKeepsBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s, Default()) {
		t.Errorf("damaged file should give defaults, got %+v", s)
	}
	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Errorf("damaged file was not kept as .bak: %v", err)
	}
	// And the file now holds valid defaults.
	if _, err := Load(path); err != nil {
		t.Errorf("reload after repair: %v", err)
	}
}

func TestNormalizeFillsAndDrops(t *testing.T) {
	s := Default()
	s.TargetLanguage = "klingon" // invalid → default
	s.CopyFormats = []string{"snake", "bogus", "snake", ""}
	s.Services.Enabled = []string{"deepl", "deepl", "potato"}
	s.Services.Order = []string{"deepl"}
	s.Hotkeys.Ocr = ""
	got := Normalize(s)
	if got.TargetLanguage != "zh-HANS" {
		t.Errorf("target = %q, want default", got.TargetLanguage)
	}
	if !reflect.DeepEqual(got.CopyFormats, []string{"snake"}) {
		t.Errorf("copy formats = %v, want [snake]", got.CopyFormats)
	}
	// Unknown service names stay (the registry ignores them) but the
	// duplicates go and every enabled service gets a card position.
	if !reflect.DeepEqual(got.Services.Enabled, []string{"deepl", "potato"}) {
		t.Errorf("enabled = %v", got.Services.Enabled)
	}
	if !reflect.DeepEqual(got.Services.Order, []string{"deepl", "potato", "index-translate", "google", "siliconflow", "zhipu", "baidu", "volcengine", "openai", "tencent", "youdao", "azure"}) {
		t.Errorf("order = %v, want enabled and built-in services appended", got.Services.Order)
	}
	if got.Hotkeys.Ocr != "Ctrl+Alt+S" {
		t.Errorf("ocr hotkey = %q, want default", got.Hotkeys.Ocr)
	}
}

func TestNormalizeMigratesFirstHotkeys(t *testing.T) {
	s := Default()
	s.Hotkeys = HotkeySettings{Panel: "Alt+A", Ocr: "Alt+S", Selection: "Alt+D"}
	if got := Normalize(s); got.Hotkeys != Default().Hotkeys {
		t.Errorf("old defaults were not migrated: %+v", got.Hotkeys)
	}
	// A hand-picked combination survives.
	s.Hotkeys = HotkeySettings{Panel: "Ctrl+Shift+A", Ocr: "Ctrl+Shift+S", Selection: "Ctrl+Shift+D"}
	if got := Normalize(s); got.Hotkeys != s.Hotkeys {
		t.Errorf("custom hotkeys were rewritten: %+v", got.Hotkeys)
	}
}

func TestNormalizePartialFile(t *testing.T) {
	// A settings file written by an older version only has some fields;
	// the rest keep their defaults instead of going zero.
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"targetLanguage":"ja","identifier":{"autoSplit":false}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.TargetLanguage != "ja" {
		t.Errorf("target = %q, want ja", got.TargetLanguage)
	}
	if got.Hotkeys.Panel != "Ctrl+Alt+A" {
		t.Errorf("unrelated fields lost their defaults: %+v", got)
	}
	if got.Identifier != (IdentifierSettings{}) {
		t.Errorf("legacy identifier settings = %+v, want all switches off", got.Identifier)
	}
	if got.AutoUpdate {
		t.Error("legacy settings should keep automatic updates off")
	}
	if got.Services.SiliconFlow.Model != DefaultSiliconFlowModel || got.Services.Zhipu.Model != DefaultZhipuModel {
		t.Errorf("legacy settings lost model defaults: %+v", got.Services)
	}
}

func TestServiceModelsNormalizeAndRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		name, siliconFlow, zhipu, wantSiliconFlow, wantZhipu string
	}{
		{"missing", "", "", DefaultSiliconFlowModel, DefaultZhipuModel},
		{"whitespace", " \t", "\n ", DefaultSiliconFlowModel, DefaultZhipuModel},
		{"custom", " Qwen/Qwen3-8B ", " glm-4.6 ", "Qwen/Qwen3-8B", "glm-4.6"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Default()
			cfg.Services.SiliconFlow.Model = tc.siliconFlow
			cfg.Services.Zhipu.Model = tc.zhipu
			cfg = Normalize(cfg)
			if cfg.Services.SiliconFlow.Model != tc.wantSiliconFlow || cfg.Services.Zhipu.Model != tc.wantZhipu {
				t.Fatalf("normalized models = %+v", cfg.Services)
			}
			path := filepath.Join(t.TempDir(), "settings.json")
			if err := Save(path, cfg); err != nil {
				t.Fatal(err)
			}
			got, err := Load(path)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, cfg) {
				t.Errorf("round trip = %+v, want %+v", got, cfg)
			}
		})
	}
}

func TestOpenAIOptionsNormalizeWhitespace(t *testing.T) {
	cfg := Default()
	cfg.Services.OpenAI = OpenAIOptions{BaseURL: " https://example.com/v1 ", Model: " custom-model "}
	got := Normalize(cfg)
	if got.Services.OpenAI != (OpenAIOptions{BaseURL: "https://example.com/v1", Model: "custom-model"}) {
		t.Errorf("OpenAI options = %+v, want trimmed values", got.Services.OpenAI)
	}

	cfg.Services.OpenAI = OpenAIOptions{BaseURL: " \t", Model: "\n "}
	got = Normalize(cfg)
	if got.Services.OpenAI != Default().Services.OpenAI {
		t.Errorf("blank OpenAI options = %+v, want defaults", got.Services.OpenAI)
	}
}

func TestCustomServiceNormalization(t *testing.T) {
	cfg := Default()
	cfg.Services.Custom = []CustomService{
		{ID: " custom:1 ", Name: " DeepSeek ", BaseURL: " https://api.deepseek.com/v1/ ", Model: " deepseek-chat "},
		{ID: "invalid-id", Name: "Bad", BaseURL: "http://bad", Model: "bad"},
		{ID: "custom:1", Name: "Duplicate", BaseURL: "http://dup", Model: "dup"},
		{ID: "custom:2", Name: "Ollama", BaseURL: "http://localhost:11434/v1", Model: "qwen2.5:7b"},
	}
	cfg.Services.Enabled = []string{"custom:1", "custom:nonexistent", "google"}
	got := Normalize(cfg)
	if len(got.Services.Custom) != 2 {
		t.Fatalf("custom count = %d, want 2", len(got.Services.Custom))
	}
	if got.Services.Custom[0] != (CustomService{ID: "custom:1", Name: "DeepSeek", BaseURL: "https://api.deepseek.com/v1/", Model: "deepseek-chat"}) {
		t.Errorf("custom 0 = %+v", got.Services.Custom[0])
	}
	if slices.Contains(got.Services.Enabled, "custom:nonexistent") {
		t.Errorf("orphaned custom service was kept in enabled: %v", got.Services.Enabled)
	}
	if !slices.Contains(got.Services.Order, "custom:1") || !slices.Contains(got.Services.Order, "custom:2") {
		t.Errorf("custom services missing in order: %v", got.Services.Order)
	}
}

func TestCustomServiceRequiredFields(t *testing.T) {
	for _, tc := range []struct {
		name    string
		service CustomService
		want    bool
	}{
		{"draft without ID", CustomService{Name: "Local", BaseURL: "http://localhost/v1", Model: "m"}, true},
		{"surrounding whitespace", CustomService{Name: " Local ", BaseURL: " http://localhost/v1 ", Model: " m "}, true},
		{"blank name", CustomService{Name: " \t", BaseURL: "http://localhost/v1", Model: "m"}, false},
		{"blank URL", CustomService{Name: "Local", BaseURL: " \n", Model: "m"}, false},
		{"missing model", CustomService{Name: "Local", BaseURL: "http://localhost/v1"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.service.IsConfigured(); got != tc.want {
				t.Errorf("IsConfigured() = %v, want %v", got, tc.want)
			}
		})
	}
}
