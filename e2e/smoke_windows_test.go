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
//   - the panel hotkey opens the input bubble, which takes focus and goes away
//     when something else takes it (the "click elsewhere" behavior)
//   - the OCR hotkey opens translucent native screenshot overlays;
//     Escape/right-click cancel and destroy the windows, including after a drag
package e2e

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"
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

// TestPanelAndSelectionShareBubble covers focus and blur, then checks that a
// different entry brings back the very same window.
func TestPanelAndSelectionShareBubble(t *testing.T) {
	pid, stop := start(t)
	defer stop()

	tapAccel(hotkeys["panel"])
	deadline := time.Now().Add(5 * time.Second)
	for {
		count := len(visibleHandles(pid))
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

	// Reopening a visible input panel keeps its native window and focus. Draft
	// and in-flight request preservation are covered by the native UI/state tests.
	handles := visibleHandles(pid)
	for range 3 {
		tapAccel(hotkeys["panel"])
		time.Sleep(100 * time.Millisecond)
		if current := visibleHandles(pid); !slices.Equal(current, handles) {
			t.Fatalf("repeated panel hotkey changed visible windows: got %v, want %v", current, handles)
		}
		if fg := foregroundPID(); fg != pid {
			t.Fatalf("repeated panel hotkey lost focus: foreground pid = %d", fg)
		}
	}

	// Something else takes the focus; the bubble must go.
	closeDummy, err := activateOwnWindow()
	if err != nil {
		t.Fatalf("dummy window: %v", err)
	}
	defer closeDummy()
	deadline = time.Now().Add(5 * time.Second)
	for len(visibleHandles(pid)) != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("bubble still visible after losing focus")
		}
		time.Sleep(150 * time.Millisecond)
	}

	// The dummy window has no selection. The selection hotkey still opens
	// the editable fallback, which must use the hidden panel's HWND.
	allowForeground(pid)
	tapAccel(hotkeys["selection"])
	deadline = time.Now().Add(8 * time.Second)
	for {
		if current := visibleHandles(pid); len(current) != 0 {
			if !slices.Equal(current, handles) {
				t.Fatalf("selection did not reuse the input bubble: got %v, want %v", current, handles)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no bubble after the selection hotkey with nothing selected")
		}
		time.Sleep(150 * time.Millisecond)
	}
}

// TestSnipOverlay verifies the transparent composition window, native content,
// keyboard/mouse cancellation and window cleanup over the live desktop.
func TestSnipOverlay(t *testing.T) {
	pid, stop := start(t)
	defer stop()
	open := func() []uintptr {
		t.Helper()
		start := time.Now()
		tapAccel(hotkeys["ocr"])
		for time.Since(start) < 5*time.Second {
			if handles, ready := nativeSnipReady(pid); ready {
				t.Logf("native overlays ready in %s", time.Since(start).Round(time.Millisecond))
				return handles
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatal("native screenshot overlay never presented after the OCR hotkey")
		return nil
	}
	waitClosed := func(handles []uintptr) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for slices.ContainsFunc(handles, func(hwnd uintptr) bool {
			alive, _, _ := procIsWindow.Call(hwnd)
			return alive != 0
		}) {
			if time.Now().After(deadline) {
				t.Fatal("canceled overlays did not release their windows")
			}
			time.Sleep(50 * time.Millisecond)
		}
		if len(visibleHandles(pid)) != 0 {
			t.Fatal("canceling a screenshot left a visible window")
		}
	}
	first := open()
	for _, hwnd := range first {
		ex, _, _ := procGetWindowLongW.Call(hwnd, gwlExStyle)
		if ex&wsExNoRedirectionBitmap == 0 {
			t.Fatal("transparent native overlay did not use a composition surface")
		}
		if ex&wsExLayered != 0 {
			var key uint32
			var alpha byte
			var flags uint32
			if ok, _, _ := procGetLayeredWindowAttributes.Call(hwnd, uintptr(unsafe.Pointer(&key)), uintptr(unsafe.Pointer(&alpha)), uintptr(unsafe.Pointer(&flags))); ok != 0 && flags&lwaAlpha != 0 && alpha != 255 {
				t.Fatalf("overlay applied whole-window dimming: alpha=%d", alpha)
			}
		}
	}
	time.Sleep(700 * time.Millisecond)
	if _, ready := nativeSnipReady(pid); !ready {
		t.Fatal("native overlay hid itself after presentation")
	}
	handles := first
	for cycle := range 20 {
		if cycle != 0 {
			handles = open()
			if len(handles) != len(first) {
				t.Fatalf("reopen showed %d overlays, want %d", len(handles), len(first))
			}
		}
		if cycle%2 == 0 {
			tapAccel("Escape")
		} else {
			rightClick()
		}
		waitClosed(handles)
	}
	before := open()
	active, _, _ := procGetForegroundWindow.Call()
	if !slices.Contains(before, active) {
		t.Fatal("screenshot did not focus one of its overlays")
	}
	release, err := beginSnipDrag(active)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	time.Sleep(150 * time.Millisecond)
	tapAccel("Escape")
	waitClosed(before)
	release()
	after := open()
	if len(before) != len(after) {
		t.Fatalf("reopen after canceled drag showed %d overlays, want %d", len(after), len(before))
	}
	tapAccel("Escape")
	waitClosed(after)
}

// Use the app's DPI context so window rectangles and synthesized drags share
// physical screen coordinates, including on a differently scaled side monitor.
func TestMain(m *testing.M) {
	if err := setTestDPIAwareness(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}
