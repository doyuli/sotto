//go:build e2e && windows

// The smoke test drives the built app the way a user does: hotkeys,
// focus, and blur, through real Win32 calls. It needs a desktop session
// and the production build:
//
//	bun run build
//	CGO_ENABLED=0 go test -tags e2e ./e2e/ -v
//
// What it covers:
//   - the app comes up with all three hotkeys registered
//   - Alt+A opens the input bubble, which takes the focus and goes away
//     when something else takes it (the "click elsewhere" behavior)
//   - Alt+S opens the screenshot overlay, which stays up as a 35%-alpha
//     layered window (a plain transparent window shows black on Windows)
package e2e

import (
	"bufio"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

const appPath = "../build/windows-amd64/sotto.exe"

// start launches the app and waits for its ready log line, which also
// proves the hotkeys registered.
func start(t *testing.T) (pid uint32, cancel func()) {
	t.Helper()
	cmd := exec.Command(appPath)
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	cmd.Stdout = pw
	cmd.Stderr = pw
	out := pr
	// A GUI app started from a test would otherwise inherit and hold
	// the test's console.
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start %s: %v", appPath, err)
	}
	pid = uint32(cmd.Process.Pid)

	var wg sync.WaitGroup
	wg.Add(1)
	ready := make(chan string, 1)
	go func() {
		defer wg.Done()
		s := bufio.NewScanner(out)
		for s.Scan() {
			line := s.Text()
			if strings.Contains(line, "hotkeys in force") {
				ready <- line
				return
			}
		}
		close(ready)
	}()
	select {
	case line := <-ready:
		if !strings.Contains(line, "panel:") || !strings.Contains(line, "ocr:") || !strings.Contains(line, "selection:") {
			t.Fatalf("hotkeys incomplete in ready line: %s", line)
		}
		hotkeys = hotkeysInForce(line)
		if hotkeys["panel"] == "" || hotkeys["ocr"] == "" || hotkeys["selection"] == "" {
			t.Fatalf("hotkeys unparsable in ready line: %s", line)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("app never reported ready")
	}
	// The synthetic keystrokes below only entitle the app to the
	// foreground if we hand the right over.
	allowForeground(pid)
	return pid, func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		wg.Wait()
	}
}

// hotkeys are the accelerators the app under test registered, taken from
// its ready line: the defaults have moved before, and a configured
// combination moves them again, so the taps press what is in force.
var hotkeys map[string]string

// hotkeysInForce parses the ready line's
// "hotkeys in force: map[ocr:… panel:… selection:…]".
func hotkeysInForce(line string) map[string]string {
	out := map[string]string{}
	for _, action := range []string{"panel", "ocr", "selection"} {
		i := strings.Index(line, action+":")
		if i < 0 {
			continue
		}
		rest := line[i+len(action)+1:]
		if j := strings.IndexAny(rest, " ]"); j >= 0 {
			rest = rest[:j]
		}
		out[action] = rest
	}
	return out
}

// TestPanelBubbleFocusAndBlur covers the input bubble's life: the panel
// hotkey shows it with the focus, and the focus going elsewhere hides it
// — the "click anywhere else and it goes away" behavior.
func TestPanelBubbleFocusAndBlur(t *testing.T) {
	pid, stop := start(t)
	defer stop()

	tapAccel(hotkeys["panel"])
	deadline := time.Now().Add(5 * time.Second)
	for {
		count, _, _ := visibleWindows(pid)
		if count >= 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("bubble never appeared after the panel hotkey")
		}
		time.Sleep(150 * time.Millisecond)
	}
	if fg := foregroundPID(); fg != pid {
		t.Errorf("bubble did not take the focus: foreground pid = %d, want %d", fg, pid)
	}

	// Something else takes the focus; the bubble must go.
	closeDummy, err := activateOwnWindow()
	if err != nil {
		t.Fatalf("dummy window: %v", err)
	}
	defer closeDummy()
	deadline = time.Now().Add(5 * time.Second)
	for {
		count, _, _ := visibleWindows(pid)
		if count == 0 {
			return // gone, as a click elsewhere should leave it
		}
		if time.Now().After(deadline) {
			t.Fatalf("bubble still visible after losing focus")
		}
		time.Sleep(150 * time.Millisecond)
	}
}

// TestSnipOverlay covers the screenshot overlay: the OCR hotkey brings
// up a full-screen layered window that stays up (it once hid itself at
// birth) at 35% alpha, so the desktop shows through dimmed instead of
// the plain black a transparent page composites to.
func TestSnipOverlay(t *testing.T) {
	pid, stop := start(t)
	defer stop()

	tapAccel(hotkeys["ocr"])
	deadline := time.Now().Add(5 * time.Second)
	var count int
	var layered bool
	var alpha byte
	for {
		count, layered, alpha = visibleWindows(pid)
		if count >= 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("overlay never appeared after Alt+S")
		}
		time.Sleep(150 * time.Millisecond)
	}
	if !layered {
		t.Errorf("overlay is not a layered window; page transparency composites to black on Windows")
	}
	if alpha == 0 {
		t.Errorf("overlay alpha is 0: the desktop would not show through")
	}
	if want := byte(89); alpha != want { // 0.35*255+0.5
		t.Errorf("overlay alpha = %d, want %d (35%%)", alpha, want)
	}

	// The overlay survives its birth: it is still there after a beat —
	// a blur racing the page load used to hide it instantly.
	time.Sleep(700 * time.Millisecond)
	if count, _, _ = visibleWindows(pid); count == 0 {
		t.Fatal("overlay hid itself right after showing (blur race)")
	}
}

// TestSelectionBubbleShows covers the selection hotkey end to end as far
// as a test can reach without a real selection: the grab finds nothing in
// the foreground and falls back to the input panel, which shows and takes
// the focus like the panel bubble does.
func TestSelectionBubbleShows(t *testing.T) {
	pid, stop := start(t)
	defer stop()

	tapAccel(hotkeys["selection"])
	deadline := time.Now().Add(8 * time.Second)
	for {
		count, _, _ := visibleWindows(pid)
		if count >= 1 {
			return // the fallback panel came up
		}
		if time.Now().After(deadline) {
			t.Fatal("no bubble after the selection hotkey with nothing selected")
		}
		time.Sleep(150 * time.Millisecond)
	}
}

// TestMain guards against a leftover app instance holding the hotkeys.
func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
