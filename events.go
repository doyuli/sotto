package main

import (
	"github.com/egoist/mygo"

	"sotto/internal/config"
)

// BubbleState is the whole state of one bubble: what Ready returns when
// its page mounts, and what BubbleReset carries when an unpinned bubble
// is reused for a new query.
type BubbleState struct {
	ID         int64           `json:"id"`
	ReqID      string          `json:"reqID"`
	Trigger    string          `json:"trigger"`
	Text       string          `json:"text"`
	SourceLang string          `json:"sourceLang"`
	TargetLang string          `json:"targetLang"`
	Pinned     bool            `json:"pinned"`
	Settings   config.Settings `json:"settings"`
}

// TranslationUpdateMsg reports one service's result of one request. Err
// holds the user-facing message when the service failed.
type TranslationUpdateMsg struct {
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

var (
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
