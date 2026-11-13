package speech

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestPrimaryLanguage(t *testing.T) {
	for code, want := range map[string]uint16{
		"zh-HANS": 4, "zh": 4, "zh-CN": 4, "en": 9, "EN_us": 9,
		"ja": 17, "ko": 18, "fr": 12, "de": 7, "ru": 25,
		"es": 10, "pt": 22, "it": 16, "": 0, "auto": 0, "latin": 0, "unknown": 0,
	} {
		if got := primaryLanguage(code); got != want {
			t.Errorf("primaryLanguage(%q)=%d, want %d", code, got, want)
		}
	}
}

func TestVoiceIndex(t *testing.T) {
	for _, tc := range []struct {
		name, code string
		voices     []string
		want       int
	}{
		{"simplified after traditional", "zh-HANS", []string{"404", "c04", "804"}, 2},
		{"mainland tag", "zh-CN", []string{"404", "804"}, 1},
		{"normalized simplified tag", "ZH_hans", []string{"c04", "804"}, 1},
		{"multiple languages on token", "zh-HANS", []string{"404", "409; 0804"}, 1},
		{"first exact match", "zh-HANS", []string{"804", "804"}, 0},
		{"primary fallback", "zh-HANS", []string{"409", "c04", "404"}, 1},
		{"no Chinese voice", "zh-HANS", []string{"409", "411"}, -1},
		{"invalid attribute", "zh-HANS", []string{"invalid;10000", "404", "804"}, 2},
		{"other languages unchanged", "en", []string{"804", "809", "409"}, 1},
		{"script code unchanged", "zh", []string{"404", "804"}, 0},
		{"system default", "auto", []string{"804", "409"}, -1},
		{"no voices", "zh-HANS", nil, -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := voiceIndex(tc.code, tc.voices); got != tc.want {
				t.Fatalf("voiceIndex(%q, %v)=%d, want %d", tc.code, tc.voices, got, tc.want)
			}
		})
	}
}

type utterance struct{ text, lang string }
type fakeEngine struct {
	spoken chan utterance
	closed chan struct{}
	err    error
}

type fakeAsyncEngine struct {
	fakeEngine
	failure error
}

func (e *fakeAsyncEngine) speak(string, string) error { panic("async speech used the blocking path") }
func (e *fakeAsyncEngine) speakAsync(text, lang string, onError func(error)) {
	e.spoken <- utterance{text, lang}
	if onError != nil {
		onError(e.failure)
	}
}

func TestAsyncSpeakerReportsNativeErrorsAndClosesWithoutMainDispatch(t *testing.T) {
	want := errors.New("native voice unavailable")
	e := &fakeAsyncEngine{fakeEngine: fakeEngine{spoken: make(chan utterance, 1), closed: make(chan struct{})}, failure: want}
	s := newSpeaker(func() (engine, error) { return e, nil })
	errs := make(chan error, 1)
	s.Speak("test", "zh-HANS", func(err error) { errs <- err })
	if got := receive(t, e.spoken); got != (utterance{"test", "zh-HANS"}) {
		t.Fatal(got)
	}
	if err := receive(t, errs); !errors.Is(err, want) {
		t.Fatal(err)
	}
	s.Close()
	receive(t, e.closed)
}

func (e *fakeEngine) speak(text, lang string) error { e.spoken <- utterance{text, lang}; return e.err }
func (e *fakeEngine) close()                        { close(e.closed) }

func receive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not respond")
		var zero T
		return zero
	}
}

func TestSpeakerLazyReplaceAndClose(t *testing.T) {
	e := &fakeEngine{spoken: make(chan utterance, 8), closed: make(chan struct{})}
	started, release := make(chan struct{}), make(chan struct{})
	s := newSpeaker(func() (engine, error) { close(started); <-release; return e, nil })
	s.Speak("  ", "en", nil)
	if s.wake != nil {
		t.Fatal("empty request initialized worker")
	}
	s.Speak("obsolete", "en", nil)
	receive(t, started)
	s.Speak("intermediate", "ja", nil)
	s.Speak("latest", "zh-HANS", nil)
	close(release)
	if got := receive(t, e.spoken); got != (utterance{"latest", "zh-HANS"}) {
		t.Fatalf("spoken=%+v", got)
	}
	s.Speak("latest", "zh-HANS", nil)
	if got := receive(t, e.spoken); got.text != "latest" {
		t.Fatalf("repeat=%+v", got)
	}
	s.Close()
	receive(t, e.closed)
	s.Close()
	s.Speak("after close", "en", nil)
	select {
	case got := <-e.spoken:
		t.Fatalf("unexpected playback=%+v", got)
	default:
	}
}

func TestSpeakerRetriesInitializationAndReportsFailure(t *testing.T) {
	want := errors.New("no voice")
	errs := make(chan error, 2)
	e := &fakeEngine{spoken: make(chan utterance, 2), closed: make(chan struct{}), err: want}
	var calls int
	s := newSpeaker(func() (engine, error) {
		calls++
		if calls == 1 {
			return nil, want
		}
		return e, nil
	})
	defer s.Close()
	s.Speak("first", "en", func(err error) { errs <- err })
	if got := receive(t, errs); !errors.Is(got, want) {
		t.Fatal(got)
	}
	s.Speak("retry", "en", func(err error) { errs <- err })
	if got := receive(t, errs); !errors.Is(got, want) {
		t.Fatal(got)
	}
	if got := receive(t, e.spoken); got.text != "retry" {
		t.Fatal(got)
	}
}

func TestCloseDuringInitializationDoesNotSpeak(t *testing.T) {
	e := &fakeEngine{spoken: make(chan utterance, 1), closed: make(chan struct{})}
	started, release := make(chan struct{}), make(chan struct{})
	s := newSpeaker(func() (engine, error) { close(started); <-release; return e, nil })
	s.Speak("obsolete", "en", nil)
	receive(t, started)
	// Publish shutdown before releasing initialization without depending on timers.
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	close(release)
	s.Close()
	receive(t, e.closed)
	select {
	case got := <-e.spoken:
		t.Fatalf("spoke during shutdown: %+v", got)
	default:
	}
}

func TestSpeakerConcurrentShutdown(t *testing.T) {
	s := newSpeaker(func() (engine, error) {
		return &fakeEngine{spoken: make(chan utterance, 100), closed: make(chan struct{})}, nil
	})
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); s.Speak("hello", "en", nil); s.Close() }()
	}
	wg.Wait()
}
