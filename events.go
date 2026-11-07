package main

import (
	"github.com/egoist/mygo"

	"sotto/internal/config"
)

// BubbleState is the whole state of one bubble: what Ready returns when
// its page mounts, and what BubbleReset carries when an unpinned bubble
// is reused for a new query.
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

var (
	// BubbleLayoutChanged updates the reading budget after a display or zoom change.
	BubbleLayoutChanged = mygo.NewEvent[float64]("bubble-layout-changed")
	RunStopped          = mygo.NewEvent[RunStoppedMsg]("run-stopped")
	BubblePresented     = mygo.NewEvent[BubblePresentedMsg]("bubble-presented")
	// BubbleReset re-initializes a reused bubble's page.
	BubbleReset = mygo.NewEvent[BubbleState]("bubble-reset")
	// TranslationUpdate reports one service result as it arrives.
	TranslationUpdate = mygo.NewEvent[TranslationUpdateMsg]("translation-update")
	// RunMeta reports a request's resolved languages before its results.
	RunMeta = mygo.NewEvent[RunMetaMsg]("run-meta")
	// SettingsChanged tells every page the new settings.
	SettingsChanged = mygo.NewEvent[config.Settings]("settings-changed")
	// OpenTranslationSettings selects the services tab in an existing window.
	OpenTranslationSettings = mygo.NewEvent[bool]("open-translation-settings")
)
