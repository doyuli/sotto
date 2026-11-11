//go:build windows

package speech

import (
	"os"
	"testing"
)

// Opt in because minimal Windows CI images may have no speech engine installed.
// Empty utterances exercise COM and voice selection without audible playback.
func TestSAPIIntegration(t *testing.T) {
	if os.Getenv("SOTTO_TEST_SAPI") != "1" {
		t.Skip("set SOTTO_TEST_SAPI=1 to test installed SAPI voices")
	}
	runThread(func() {
		e, err := newEngine()
		if err != nil {
			t.Fatal(err)
		}
		defer e.close()
		for _, lang := range []string{"zh-HANS", "en", "ja", "auto", "latin"} {
			if err := e.speak("", lang); err != nil {
				t.Fatalf("language %s: %v", lang, err)
			}
		}
	})
}
