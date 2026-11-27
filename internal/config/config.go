// Package config stores the app's settings in a JSON file under the user
// data directory. Service credentials are not part of it: they live in the
// system keyring (see internal/keys).
package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"sotto/internal/languages"
	"sotto/internal/naming"
)

// ServiceSettings configures which translation services run, and the order
// the bubble shows their cards in.
type ServiceSettings struct {
	Enabled     []string      `json:"enabled"`
	Order       []string      `json:"order"`
	OpenAI      OpenAIOptions `json:"openai"`
	Magpie      MagpieOptions `json:"magpie"`
	SiliconFlow ModelOptions  `json:"siliconflow"`
	Zhipu       ModelOptions  `json:"zhipu"`
}

// Model returns the editable model, or nil for services with a fixed model.
func (s *ServiceSettings) Model(service string) *string {
	switch service {
	case "openai":
		return &s.OpenAI.Model
	case "magpie":
		return &s.Magpie.Model
	case "siliconflow":
		return &s.SiliconFlow.Model
	case "zhipu":
		return &s.Zhipu.Model
	}
	return nil
}

const (
	DefaultSiliconFlowModel = "tencent/Hunyuan-MT-7B"
	DefaultZhipuModel       = "glm-4.7-flash"
)

// ModelOptions configure a service whose API endpoint is fixed.
// Credentials remain in the keyring.
type ModelOptions struct {
	Model string `json:"model"`
}

// OpenAIOptions configure the OpenAI-compatible service. Its API key is
// stored in the keyring, not here.
type OpenAIOptions struct {
	BaseURL string `json:"baseURL"`
	Model   string `json:"model"`
}

// MagpieOptions use zero for automatic port discovery and an empty model for
// automatic selection when the gateway lists a single chat model.
type MagpieOptions struct {
	Port  int    `json:"port,omitempty"`
	Model string `json:"model,omitempty"`
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

// HotkeySettings use platform modifier names in accelerator syntax.
type HotkeySettings struct {
	Panel              string `json:"panel"`
	Ocr                string `json:"ocr"`
	Selection          string `json:"selection"`
	MinimalTranslation string `json:"minimalTranslation"`
	History            string `json:"history"`
}

// HistorySettings controls local recording and retention. Zero days is unlimited.
type HistorySettings struct {
	Enabled       bool `json:"enabled"`
	RetentionDays int  `json:"retentionDays"`
}

// BubbleSettings control bubble behavior.
type BubbleSettings struct {
	// AutoHide hides an unpinned bubble when it loses focus.
	AutoHide bool `json:"autoHide"`
}

const (
	TrayActionPanel    = "panel"
	TrayActionOcr      = "ocr"
	TrayActionHistory  = "history"
	TrayActionMenu     = "menu"
	TrayActionSettings = "settings"
)

// TrayClickActions lists the supported primary-click actions in display order.
var TrayClickActions = []string{
	TrayActionPanel,
	TrayActionOcr,
	TrayActionHistory,
	TrayActionMenu,
	TrayActionSettings,
}

// Settings contains the app's persisted preferences.
type Settings struct {
	Services           ServiceSettings    `json:"services"`
	TargetLanguage     string             `json:"targetLanguage"`
	Identifier         IdentifierSettings `json:"identifier"`
	CopyFormats        []string           `json:"copyFormats"`
	Hotkeys            HotkeySettings     `json:"hotkeys"`
	History            HistorySettings    `json:"history"`
	Bubble             BubbleSettings     `json:"bubble"`
	TrayClickAction    string             `json:"trayClickAction"`
	MinimalTranslation bool               `json:"minimalTranslation"`
	OpenAtLogin        bool               `json:"openAtLogin"`
	AutoUpdate         bool               `json:"autoUpdate"`
}

// Default returns the settings a fresh install starts with.
func Default() Settings {
	return Settings{
		Services: ServiceSettings{
			Enabled:     []string{"index-translate", "youdaoweb"},
			Order:       []string{"index-translate", "google", "siliconflow", "zhipu", "baidu", "volcengine", "deepl", "openai", "tencent", "youdao", "youdaoweb", "azure", "dictionary", "magpie"},
			OpenAI:      OpenAIOptions{BaseURL: "https://api.openai.com/v1", Model: "gpt-4o-mini"},
			SiliconFlow: ModelOptions{Model: DefaultSiliconFlowModel},
			Zhipu:       ModelOptions{Model: DefaultZhipuModel},
		},
		TargetLanguage:  "zh-HANS",
		Identifier:      IdentifierSettings{AutoSplit: true},
		CopyFormats:     slices.Clone(naming.Styles),
		Hotkeys:         defaultHotkeys(runtime.GOOS),
		History:         HistorySettings{Enabled: true, RetentionDays: 30},
		Bubble:          BubbleSettings{AutoHide: true},
		TrayClickAction: TrayActionPanel,
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
	switch s.History.RetentionDays {
	case 0, 7, 14, 30:
	default:
		s.History.RetentionDays = 30
	}
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
	s.Services.OpenAI.BaseURL = strings.TrimSpace(s.Services.OpenAI.BaseURL)
	if s.Services.OpenAI.BaseURL == "" {
		s.Services.OpenAI.BaseURL = d.Services.OpenAI.BaseURL
	}
	s.Services.OpenAI.Model = strings.TrimSpace(s.Services.OpenAI.Model)
	if s.Services.OpenAI.Model == "" {
		s.Services.OpenAI.Model = d.Services.OpenAI.Model
	}
	s.Services.Magpie.Model = strings.TrimSpace(s.Services.Magpie.Model)
	if s.Services.Magpie.Port < 0 || s.Services.Magpie.Port > 65535 {
		s.Services.Magpie.Port = 0
	}
	s.Services.SiliconFlow.Model = strings.TrimSpace(s.Services.SiliconFlow.Model)
	if s.Services.SiliconFlow.Model == "" {
		s.Services.SiliconFlow.Model = d.Services.SiliconFlow.Model
	}
	s.Services.Zhipu.Model = strings.TrimSpace(s.Services.Zhipu.Model)
	if s.Services.Zhipu.Model == "" {
		s.Services.Zhipu.Model = d.Services.Zhipu.Model
	}
	if s.TargetLanguage != "auto" && !languages.Valid(s.TargetLanguage) {
		s.TargetLanguage = d.TargetLanguage
	}
	if !slices.Contains(TrayClickActions, s.TrayClickAction) {
		s.TrayClickAction = d.TrayClickAction
	}
	s.CopyFormats = validFormats(s.CopyFormats)
	if s.CopyFormats == nil {
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
	if s.Hotkeys.MinimalTranslation == "" {
		s.Hotkeys.MinimalTranslation = d.Hotkeys.MinimalTranslation
	}
	if s.Hotkeys.History == "" {
		s.Hotkeys.History = d.Hotkeys.History
	}
	return s
}

func defaultHotkeys(goos string) HotkeySettings {
	if goos == "darwin" {
		return HotkeySettings{Panel: "Option+A", Ocr: "Option+S", Selection: "Option+D", MinimalTranslation: "Option+F", History: "Option+H"}
	}
	return HotkeySettings{Panel: "Ctrl+Alt+A", Ocr: "Ctrl+Alt+S", Selection: "Ctrl+Alt+D", MinimalTranslation: "Ctrl+Alt+F", History: "Ctrl+Alt+H"}
}

func validFormats(formats []string) []string {
	// An explicit empty list disables all naming formats.
	if len(formats) == 0 {
		return formats
	}
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
