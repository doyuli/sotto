package bubble

import (
	"crypto/rand"

	"sotto/internal/config"
)

// BubbleState is the native view snapshot when an unpinned bubble is reused
// for a new query or its recognition phase completes.
type BubbleState struct {
	MaxHeight           float64                `json:"maxHeight"`
	ClipboardRecoveryID string                 `json:"clipboardRecoveryID"`
	ShowID              int64                  `json:"showID"`
	ID                  int64                  `json:"id"`
	ReqID               string                 `json:"reqID"`
	ServiceRequests     map[string]string      `json:"serviceRequests"`
	Generation          int64                  `json:"generation"`
	SessionID           string                 `json:"sessionID"`
	Revision            int64                  `json:"revision"`
	Phase               string                 `json:"phase"`
	Message             string                 `json:"message"`
	Meta                *RunMetaMsg            `json:"meta"`
	Results             []TranslationUpdateMsg `json:"results"`
	Trigger             string                 `json:"trigger"`
	Text                string                 `json:"text"`
	SourceLang          string                 `json:"sourceLang"`
	TargetLang          string                 `json:"targetLang"`
	Pinned              bool                   `json:"pinned"`
	Settings            config.Settings        `json:"settings"`
}

// TranslationUpdateMsg carries cumulative text for one service and request.
// Streaming is true until completion; Err settles a failure without losing text.
type TranslationUpdateMsg struct {
	Streaming    bool   `json:"streaming"`
	ReqID        string `json:"reqID"`
	Service      string `json:"service"`
	Text         string `json:"text"`
	DetectedLang string `json:"detectedLang"`
	Err          string `json:"err"`
}

// RunMetaMsg tells the page the languages a request resolved to, so its
// badge and selectors can follow the automatic detection, and the
// services that will answer it.
type RunMetaMsg struct {
	ReqID      string   `json:"reqID"`
	SourceLang string   `json:"sourceLang"`
	TargetLang string   `json:"targetLang"`
	Services   []string `json:"services"`
}

type BubblePresentedMsg struct {
	ClipboardRecoveryID string `json:"clipboardRecoveryID"`
	SessionID           string `json:"sessionID"`
	ShowID              int64  `json:"showID"`
	Revision            int64  `json:"revision"`
	Focus               bool   `json:"focus"`
}

type RunStoppedMsg struct {
	ShowID    int64  `json:"showID"`
	SessionID string `json:"sessionID"`
	Revision  int64  `json:"revision"`
}

// RunReq is what a page sends to start a translation.
type RunReq struct {
	ReqID      string `json:"reqID"`
	SessionID  string `json:"sessionID"`
	Revision   int64  `json:"revision"`
	Service    string `json:"service"`
	Source     string `json:"source"` // "input" | "selection" | "ocr"
	Text       string `json:"text"`
	SourceLang string `json:"sourceLang"` // "" or "auto" detects
	TargetLang string `json:"targetLang"` // "" uses the default
}

// NewRequestID identifies one translation run or presentation.
func NewRequestID() string { return rand.Text() }
