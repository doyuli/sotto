// Package config stores the app's settings in a JSON file under the user
// data directory. Service credentials are not part of it: they live in the
// system keyring (see internal/keys).
package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"

	"sotto/internal/languages"
	"sotto/internal/naming"
)

// ServiceSettings configures which translation services run, and the order
// the bubble shows their cards in.
type ServiceSettings struct {
	Enabled []string      `json:"enabled"`
	Order   []string      `json:"order"`
	OpenAI  OpenAIOptions `json:"openai"`
}

// OpenAIOptions configure the OpenAI-compatible service. Its API key is
// stored in the keyring, not here.
type OpenAIOptions struct {
	BaseURL string `json:"baseURL"`
	Model   string `json:"model"`
}

// IdentifierSettings control how source text is prepared for translation.
type IdentifierSettings struct {
	// AutoSplit recognizes camelCase/snake_case/kebab-case input and
	// splits it into words before translating.
	AutoSplit bool `json:"autoSplit"`
	// StripCodeComments removes delimiters while preserving the comment text.
	// Keep the JSON key compatible with previously saved settings.
	StripCodeComments bool `json:"stripCodeComments"`
	FlattenNewlines   bool `json:"flattenNewlines"`
	StripMarkdown     bool `json:"stripMarkdown"`
}

// HotkeySettings are the global shortcuts, in mygo accelerator syntax.
type HotkeySettings struct {
	Panel     string `json:"panel"`
	Ocr       string `json:"ocr"`
	Selection string `json:"selection"`
}

// BubbleSettings control bubble behavior.
type BubbleSettings struct {
	// AutoHide hides an unpinned bubble when it loses focus.
	AutoHide bool `json:"autoHide"`
}

// Settings is everything the user can configure.
type Settings struct {
	Services       ServiceSettings    `json:"services"`
	TargetLanguage string             `json:"targetLanguage"`
	Identifier     IdentifierSettings `json:"identifier"`
	CopyFormats    []string           `json:"copyFormats"`
	Hotkeys        HotkeySettings     `json:"hotkeys"`
	Bubble         BubbleSettings     `json:"bubble"`
	OpenAtLogin    bool               `json:"openAtLogin"`
}

// Default returns the settings a fresh install starts with.
func Default() Settings {
	return Settings{
		Services: ServiceSettings{
			Enabled: []string{"index-translate", "google"},
			Order:   []string{"index-translate", "google", "baidu", "volcengine", "deepl", "openai", "tencent", "youdao", "azure"},
			OpenAI:  OpenAIOptions{BaseURL: "https://api.openai.com/v1", Model: "gpt-4o-mini"},
		},
		TargetLanguage: "zh-HANS",
		Identifier:     IdentifierSettings{AutoSplit: true},
		CopyFormats:    slices.Clone(naming.Styles),
		Hotkeys: HotkeySettings{
			Panel:     "Ctrl+Alt+A",
			Ocr:       "Ctrl+Alt+S",
			Selection: "Ctrl+Alt+D",
		},
		Bubble: BubbleSettings{AutoHide: true},
	}
}

// Load returns the settings stored at path, or the defaults when the file
// is missing or damaged. A damaged file is kept beside it as .bak and
// replaced with the defaults.
func Load(path string) (Settings, error) {
	s := Default()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, Save(path, s)
	}
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(data, &s); err != nil {
		_ = os.WriteFile(path+".bak", data, 0o600)
		s = Default()
		return s, Save(path, s)
	}
	return Normalize(s), nil
}

// Save atomically writes s to path, through a temporary file.
func Save(path string, s Settings) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "settings-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

// Normalize returns s with missing or invalid values replaced by defaults:
// unknown copy formats dropped, duplicate service entries removed, every
// enabled service given a position in the card order.
func Normalize(s Settings) Settings {
	d := Default()
	s.Services.Enabled = dedupe(s.Services.Enabled)
	s.Services.Order = dedupe(s.Services.Order)
	for _, name := range s.Services.Enabled {
		if !slices.Contains(s.Services.Order, name) {
			s.Services.Order = append(s.Services.Order, name)
		}
	}
	for _, name := range d.Services.Order {
		if !slices.Contains(s.Services.Order, name) {
			s.Services.Order = append(s.Services.Order, name)
		}
	}
	if s.Services.OpenAI.BaseURL == "" {
		s.Services.OpenAI.BaseURL = d.Services.OpenAI.BaseURL
	}
	if s.Services.OpenAI.Model == "" {
		s.Services.OpenAI.Model = d.Services.OpenAI.Model
	}
	if s.TargetLanguage != "auto" && !languages.Valid(s.TargetLanguage) {
		s.TargetLanguage = d.TargetLanguage
	}
	s.CopyFormats = validFormats(s.CopyFormats)
	if len(s.CopyFormats) == 0 {
		s.CopyFormats = d.CopyFormats
	}
	if s.Hotkeys.Panel == "" {
		s.Hotkeys.Panel = d.Hotkeys.Panel
	}
	if s.Hotkeys.Ocr == "" {
		s.Hotkeys.Ocr = d.Hotkeys.Ocr
	}
	if s.Hotkeys.Selection == "" {
		s.Hotkeys.Selection = d.Hotkeys.Selection
	}
	// A config still holding the first defaults exactly moves to the
	// current Ctrl+Alt set; any hand-picked combination is kept.
	if s.Hotkeys.Panel == "Alt+A" && s.Hotkeys.Ocr == "Alt+S" && s.Hotkeys.Selection == "Alt+D" {
		s.Hotkeys = d.Hotkeys
	}
	return s
}

func validFormats(formats []string) []string {
	var out []string
	for _, f := range formats {
		if slices.Contains(naming.Styles, f) && !slices.Contains(out, f) {
			out = append(out, f)
		}
	}
	return out
}

func dedupe(xs []string) []string {
	out := make([]string, 0, len(xs))
	for _, x := range xs {
		if !slices.Contains(out, x) {
			out = append(out, x)
		}
	}
	return out
}
