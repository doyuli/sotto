//go:build darwin

package speech

import "testing"

func TestMacSpeechQueuedReplacementAndClose(t *testing.T) {
	var queue []func()
	var spoken []string
	released := 0
	e := &macSpeaker{post: func(fn func()) { queue = append(queue, fn) }, play: func(text, lang string) error { spoken = append(spoken, text); return nil }, dispose: func() { released++ }}
	e.speakAsync("old", "en", nil)
	e.speakAsync("new", "en", nil)
	queue[0]()
	queue[1]()
	if len(spoken) != 1 || spoken[0] != "new" {
		t.Fatal(spoken)
	}
	e.speakAsync("late", "en", nil)
	e.close()
	e.close()
	queue[2]()
	queue[3]()
	if len(spoken) != 1 || released != 1 {
		t.Fatalf("spoken=%v released=%d", spoken, released)
	}
}
