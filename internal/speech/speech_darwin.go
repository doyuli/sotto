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
}

func newEngine() (engine, error) {
	if _, err := macnative.Framework("AVFoundation"); err != nil {
		return nil, fmt.Errorf("load system speech: %w", err)
	}
	if macnative.Class("AVSpeechSynthesizer") == 0 {
		return nil, ErrUnavailable
	}
	e := &macSpeaker{}
	// Registered after the application's Speaker.Close listener. Clean native
	// objects on the main thread even if its async queue is no longer drained.
	mygo.App.OnQuit(func() { e.closed.Store(true); e.release() })
	return e, nil
}

func (e *macSpeaker) speak(text, lang string) error { e.speakAsync(text, lang, nil); return nil }

func (e *macSpeaker) speakAsync(text, lang string, onError func(error)) {
	revision := e.revision.Add(1)
	macnative.PostMain(func() {
		if e.closed.Load() || e.revision.Load() != revision {
			return
		}
		if e.synth == 0 {
			e.synth = macnative.Class("AVSpeechSynthesizer").Send(macnative.Sel("alloc")).Send(macnative.Sel("init"))
		}
		if e.synth == 0 {
			if onError != nil {
				onError(ErrUnavailable)
			}
			return
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
				if onError != nil {
					onError(ErrUnavailable)
				}
				return
			}
			utterance.Send(macnative.Sel("setVoice:"), voice)
		}
		e.synth.Send(macnative.Sel("stopSpeakingAtBoundary:"), int64(0))
		e.synth.Send(macnative.Sel("speakUtterance:"), utterance)
	})
}

func (e *macSpeaker) release() {
	if e.synth != 0 {
		e.synth.Send(macnative.Sel("stopSpeakingAtBoundary:"), int64(0))
		e.synth.Send(macnative.Sel("release"))
		e.synth = 0
	}
}

func (e *macSpeaker) close() {
	e.closed.Store(true)
	e.revision.Add(1)
	macnative.PostMain(e.release)
}
