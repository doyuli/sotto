// Package speech reads text aloud using the system's installed voices.
package speech

import (
	"errors"
	"strconv"
	"strings"
	"sync"
)

var ErrUnavailable = errors.New("speech: system speech unavailable")

type engine interface {
	speak(text, lang string) error
	close()
}

type request struct {
	text, lang string
	onError    func(error)
}

// Speaker serializes OS calls on one worker. Pending clicks replace each other
// rather than building a playback queue while the engine starts.
type Speaker struct {
	mu      sync.Mutex
	pending *request
	wake    chan struct{}
	done    chan struct{}
	closed  bool
	open    func() (engine, error)
}

// New does not load COM or any voices until the first nonempty request.
func New() *Speaker { return newSpeaker(newEngine) }

func newSpeaker(open func() (engine, error)) *Speaker {
	return &Speaker{open: open}
}

// Speak returns immediately. Errors are reported on the worker, never the UI
// thread. Each processed request replaces the currently playing utterance.
func (s *Speaker) Speak(text, lang string, onError func(error)) {
	if strings.TrimSpace(text) == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	if s.wake == nil {
		s.wake = make(chan struct{}, 1)
		s.done = make(chan struct{})
		go s.run()
	}
	s.pending = &request{text: text, lang: lang, onError: onError}
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// Close stops playback and releases the engine on its owning thread.
func (s *Speaker) Close() {
	s.mu.Lock()
	s.closed = true
	s.pending = nil
	done := s.done
	if s.wake != nil {
		select {
		case s.wake <- struct{}{}:
		default:
		}
	}
	s.mu.Unlock()
	if done != nil {
		<-done
	}
}

func (s *Speaker) run() {
	defer close(s.done)
	// The platform wrapper keeps engine creation, use and release on one
	// initialized OS thread; no COM pointers escape this callback.
	runThread(func() {
		var e engine
		defer func() {
			if e != nil {
				e.close()
			}
		}()
		for range s.wake {
			s.mu.Lock()
			r, closed := s.pending, s.closed
			s.pending = nil
			s.mu.Unlock()
			if closed {
				return
			}
			if r == nil {
				continue
			}
			var err error
			if e == nil {
				e, err = s.open()
			}
			// A click or shutdown during initialization supersedes this request.
			s.mu.Lock()
			stale := s.closed || s.pending != nil
			s.mu.Unlock()
			if stale {
				continue
			}
			if err == nil {
				err = e.speak(r.text, r.lang)
			}
			if err != nil && r.onError != nil {
				r.onError(err)
			}
		}
	})
}

// voiceIndex prefers mainland Chinese for the former UI's zh-HANS -> zh-CN
// mapping before falling back to the first voice with the same primary language.
// SAPI Language attributes contain semicolon-separated hexadecimal LANGIDs.
func voiceIndex(code string, attributes []string) int {
	primary := primaryLanguage(code)
	if primary == 0 {
		return -1
	}
	var preferred uint16
	switch strings.ToLower(strings.ReplaceAll(code, "_", "-")) {
	case "zh-hans", "zh-cn":
		preferred = 0x0804
	}
	fallback := -1
	for i, attribute := range attributes {
		for _, id := range strings.Split(attribute, ";") {
			value, err := strconv.ParseUint(strings.TrimSpace(id), 16, 16)
			if err != nil {
				continue
			}
			lang := uint16(value)
			if preferred != 0 && lang == preferred {
				return i
			}
			if fallback == -1 && lang&0x3ff == primary {
				fallback = i
			}
		}
	}
	return fallback
}

// primaryLanguage maps app/script codes to Windows primary language IDs.
// Auto/unknown use the system default, as the former browser implementation did.
func primaryLanguage(code string) uint16 {
	switch strings.ToLower(strings.Split(strings.ReplaceAll(code, "_", "-"), "-")[0]) {
	case "zh":
		return 0x04
	case "en":
		return 0x09
	case "ja":
		return 0x11
	case "ko":
		return 0x12
	case "fr":
		return 0x0c
	case "de":
		return 0x07
	case "ru":
		return 0x19
	case "es":
		return 0x0a
	case "pt":
		return 0x16
	case "it":
		return 0x10
	default:
		return 0
	}
}
