package config

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"testing"
)

func TestDefault(t *testing.T) {
	s := Default()
	if s.TargetLanguage != "zh-HANS" {
		t.Errorf("default target = %q, want zh-HANS", s.TargetLanguage)
	}
	if s.Hotkeys != defaultHotkeys(runtime.GOOS) {
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

func TestPlatformHotkeyDefaults(t *testing.T) {
	mac := HotkeySettings{Panel: "Option+A", Ocr: "Option+S", Selection: "Option+D"}
	windows := HotkeySettings{Panel: "Ctrl+Alt+A", Ocr: "Ctrl+Alt+S", Selection: "Ctrl+Alt+D"}
	for _, tc := range []struct {
		platform string
		want     HotkeySettings
	}{{"darwin", mac}, {"windows", windows}} {
		if got := defaultHotkeys(tc.platform); got != tc.want {
			t.Fatalf("%s defaults=%+v", tc.platform, got)
		}
	}
}

func TestLoadPreservesHotkeysWithoutRewritingFile(t *testing.T) {
	for _, tc := range []struct {
		name, hotkeys string
		want          HotkeySettings
	}{
		{"control alt", `{"panel":"Ctrl+Alt+A","ocr":"Ctrl+Alt+S","selection":"Ctrl+Alt+D"}`, HotkeySettings{Panel: "Ctrl+Alt+A", Ocr: "Ctrl+Alt+S", Selection: "Ctrl+Alt+D"}},
		{"alt", `{"panel":"Alt+A","ocr":"Alt+S","selection":"Alt+D"}`, HotkeySettings{Panel: "Alt+A", Ocr: "Alt+S", Selection: "Alt+D"}},
		{"custom", `{"panel":"Cmd+Shift+K","ocr":"Ctrl+Alt+S","selection":"Ctrl+Alt+D"}`, HotkeySettings{Panel: "Cmd+Shift+K", Ocr: "Ctrl+Alt+S", Selection: "Ctrl+Alt+D"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "settings.json")
			original := `{"hotkeys":` + tc.hotkeys + `}`
			if err := os.WriteFile(path, []byte(original), 0600); err != nil {
				t.Fatal(err)
			}
			got, err := Load(path)
			if err != nil || got.Hotkeys != tc.want {
				t.Fatalf("loaded %+v: %v", got.Hotkeys, err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != original {
				t.Fatalf("loading rewrote settings: %s", data)
			}
		})
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
	if got.Hotkeys.Ocr != Default().Hotkeys.Ocr {
		t.Errorf("ocr hotkey = %q, want default", got.Hotkeys.Ocr)
	}
}

func TestNormalizePreservesExplicitHotkeys(t *testing.T) {
	for _, h := range []HotkeySettings{
		{Panel: "Alt+A", Ocr: "Alt+S", Selection: "Alt+D"},
		{Panel: "Ctrl+Alt+A", Ocr: "Ctrl+Alt+S", Selection: "Ctrl+Alt+D"},
		{Panel: "Ctrl+Shift+A", Ocr: "Ctrl+Shift+S", Selection: "Ctrl+Shift+D"},
	} {
		s := Default()
		s.Hotkeys = h
		for range 2 {
			s = Normalize(s)
			if s.Hotkeys != h {
				t.Errorf("explicit hotkeys were rewritten: got %+v, want %+v", s.Hotkeys, h)
			}
		}
	}
}

func TestSavePreservesExplicitHotkeysAcrossReloads(t *testing.T) {
	for _, h := range []HotkeySettings{
		{Panel: "Alt+A", Ocr: "Alt+S", Selection: "Alt+D"},
		{Panel: "Ctrl+Alt+A", Ocr: "Ctrl+Alt+S", Selection: "Ctrl+Alt+D"},
	} {
		path := filepath.Join(t.TempDir(), "settings.json")
		cfg := Default()
		cfg.Hotkeys = h
		for range 2 {
			if err := Save(path, cfg); err != nil {
				t.Fatal(err)
			}
			var err error
			cfg, err = Load(path)
			if err != nil || cfg.Hotkeys != h {
				t.Fatalf("explicit hotkeys changed after reload: got %+v, want %+v, err=%v", cfg.Hotkeys, h, err)
			}
		}
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
	if got.Hotkeys.Panel != Default().Hotkeys.Panel {
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
