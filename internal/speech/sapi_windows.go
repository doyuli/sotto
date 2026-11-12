//go:build windows

package speech

import (
	"fmt"
	"runtime"

	ole "github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
)

const (
	speakAsync       = 1
	purgeBeforeSpeak = 2
	speakPlainText   = 16
)

type sapi struct {
	voice        *ole.IDispatch
	defaultVoice *ole.VARIANT
}

func runThread(fn func()) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	fn()
}

func newEngine() (engine, error) {
	if err := ole.CoInitializeEx(0, ole.COINIT_MULTITHREADED); err != nil {
		// go-ole reports S_FALSE as an error even though COM initialized.
		if e, ok := err.(*ole.OleError); !ok || e.Code() != 1 {
			return nil, fmt.Errorf("speech: initialize COM: %w", err)
		}
	}
	u, err := oleutil.CreateObject("SAPI.SpVoice")
	if err != nil {
		ole.CoUninitialize()
		return nil, fmt.Errorf("speech: create SAPI voice: %w", err)
	}
	v, err := u.QueryInterface(ole.IID_IDispatch)
	u.Release()
	if err != nil {
		ole.CoUninitialize()
		return nil, fmt.Errorf("speech: query SAPI voice: %w", err)
	}
	def, err := oleutil.GetProperty(v, "Voice")
	if err != nil {
		v.Release()
		ole.CoUninitialize()
		return nil, fmt.Errorf("speech: get default voice: %w", err)
	}
	return &sapi{voice: v, defaultVoice: def}, nil
}

func (s *sapi) speak(text, lang string) error {
	if err := s.call("", speakAsync|purgeBeforeSpeak|speakPlainText); err != nil {
		return err
	}
	token, err := s.selectVoice(lang)
	if err != nil {
		return fmt.Errorf("speech: select voice: %w", err)
	}
	defer token.Clear()
	result, err := oleutil.PutPropertyRef(s.voice, "Voice", token.ToIDispatch())
	if result != nil {
		result.Clear()
	}
	if err != nil {
		return fmt.Errorf("speech: set voice: %w", err)
	}
	return s.call(text, speakAsync|purgeBeforeSpeak|speakPlainText)
}

func (s *sapi) selectVoice(lang string) (*ole.VARIANT, error) {
	primary := primaryLanguage(lang)
	if primary != 0 {
		voices, err := oleutil.CallMethod(s.voice, "GetVoices", "", "")
		if err != nil {
			return nil, err
		}
		defer voices.Clear()
		count, err := oleutil.GetProperty(voices.ToIDispatch(), "Count")
		if err != nil {
			return nil, err
		}
		n := int(count.Val)
		count.Clear()
		attributes := make([]string, n)
		for i := 0; i < n; i++ {
			token, err := oleutil.CallMethod(voices.ToIDispatch(), "Item", i)
			if err != nil {
				return nil, err
			}
			attr, err := oleutil.CallMethod(token.ToIDispatch(), "GetAttribute", "Language")
			if err != nil {
				token.Clear()
				return nil, err
			}
			attributes[i] = attr.ToString()
			attr.Clear()
			token.Clear()
		}
		if index := voiceIndex(lang, attributes); index >= 0 {
			return oleutil.CallMethod(voices.ToIDispatch(), "Item", index)
		}
	}
	// The browser previously fell back to its default voice when no language
	// matched. Reset it explicitly so a preceding request cannot leak its voice.
	s.defaultVoice.ToIDispatch().AddRef()
	v := *s.defaultVoice
	return &v, nil
}

func (s *sapi) call(text string, flags int) error {
	result, err := oleutil.CallMethod(s.voice, "Speak", text, flags)
	if result != nil {
		result.Clear()
	}
	if err != nil {
		return fmt.Errorf("speech: speak: %w", err)
	}
	return nil
}

func (s *sapi) close() {
	_ = s.call("", speakAsync|purgeBeforeSpeak|speakPlainText)
	s.defaultVoice.Clear()
	s.voice.Release()
	ole.CoUninitialize()
}
