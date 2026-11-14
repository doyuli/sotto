//go:build darwin

package speech

import (
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo"

	"sotto/internal/macnative"
)

func runThread(fn func()) { fn() }

type macSpeaker struct {
	closed   atomic.Bool
	revision atomic.Uint64
	synth    objc.ID // main thread only
	post     func(func())
	play     func(string, string) error
	dispose  func()
}

func newEngine() (engine, error) {
	if _, err := macnative.Framework("AVFoundation"); err != nil {
		return nil, fmt.Errorf("load system speech: %w", err)
	}
	if macnative.Class("AVSpeechSynthesizer") == 0 {
		return nil, ErrUnavailable
	}
	e := &macSpeaker{post: macnative.PostMain}
	e.play = e.playNative
	e.dispose = e.release
	// Registered after the application's Speaker.Close listener. Clean native
	// objects on the main thread even if its async queue is no longer drained.
	mygo.App.OnQuit(func() { e.closed.Store(true); e.release() })
	return e, nil
}

func (e *macSpeaker) speak(text, lang string) error { e.speakAsync(text, lang, nil); return nil }

func (e *macSpeaker) speakAsync(text, lang string, onError func(error)) {
	revision := e.revision.Add(1)
	e.post(func() {
		if e.closed.Load() || e.revision.Load() != revision {
			return
		}
		if err := e.play(text, lang); err != nil && onError != nil {
			onError(err)
		}
	})
}

func (e *macSpeaker) playNative(text, lang string) error {
	if e.synth == 0 {
		e.synth = macnative.Class("AVSpeechSynthesizer").Send(macnative.Sel("alloc")).Send(macnative.Sel("init"))
	}
	if e.synth == 0 {
		return ErrUnavailable
	}
	utterance := macnative.Class("AVSpeechUtterance").Send(macnative.Sel("speechUtteranceWithString:"), macnative.String(text))
	language := strings.ReplaceAll(lang, "_", "-")
	switch strings.ToLower(language) {
	case "zh-hans", "zh":
		language = "zh-CN"
	case "zh-hant":
		language = "zh-TW"
	}
	if language != "" && language != "auto" {
		voice := macnative.Class("AVSpeechSynthesisVoice").Send(macnative.Sel("voiceWithLanguage:"), macnative.String(language))
		if voice == 0 {
			return ErrUnavailable
		}
		utterance.Send(macnative.Sel("setVoice:"), voice)
	}
	e.synth.Send(macnative.Sel("stopSpeakingAtBoundary:"), int64(0))
	e.synth.Send(macnative.Sel("speakUtterance:"), utterance)
	return nil
}

func (e *macSpeaker) release() {
	if e.synth != 0 {
		e.synth.Send(macnative.Sel("stopSpeakingAtBoundary:"), int64(0))
		e.synth.Send(macnative.Sel("release"))
		e.synth = 0
	}
}

func (e *macSpeaker) close() {
	e.revision.Add(1)
	if e.closed.Swap(true) {
		return
	}
	e.post(e.dispose)
}
