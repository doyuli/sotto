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
//     Escape/right-click cancel, unused windows are reused, and a dragged
//     surface is replaced before it can show a previous selection
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

// TestPanelBubbleFocusAndBlur covers the input bubble's life: the panel
// hotkey shows it with the focus, and the focus going elsewhere hides it
// — the "click anywhere else and it goes away" behavior.
func TestPanelBubbleFocusAndBlur(t *testing.T) {
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

	// Something else takes the focus; the bubble must go.
	closeDummy, err := activateOwnWindow()
	if err != nil {
		t.Fatalf("dummy window: %v", err)
	}
	defer closeDummy()
	deadline = time.Now().Add(5 * time.Second)
	for {
		count := len(visibleHandles(pid))
		if count == 0 {
			return // gone, as a click elsewhere should leave it
		}
		if time.Now().After(deadline) {
			t.Fatalf("bubble still visible after losing focus")
		}
		time.Sleep(150 * time.Millisecond)
	}
}

// TestSnipOverlay verifies the transparent composition window, native content,
// keyboard/mouse cancellation and clean surface reuse over the live desktop.
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
	waitHidden := func() {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for len(visibleHandles(pid)) != 0 {
			if time.Now().After(deadline) {
				t.Fatal("overlay did not cancel")
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	first := open()
	slices.Sort(first)
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
	for cycle := range 20 {
		if cycle != 0 {
			handles := open()
			slices.Sort(handles)
			if !slices.Equal(first, handles) {
				t.Fatal("opening/canceling screenshots did not reuse the per-display windows")
			}
		}
		if cycle%2 == 0 {
			tapAccel("Escape")
		} else {
			rightClick()
		}
		waitHidden()
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
	waitHidden()
	release()
	after := open()
	if len(before) != len(after) || slices.Contains(after, active) {
		t.Fatal("a surface with a previous selection was shown again")
	}
	for _, hwnd := range before {
		if hwnd != active && !slices.Contains(after, hwnd) {
			t.Fatal("an unused display's overlay was unnecessarily replaced")
		}
	}
	tapAccel("Escape")
	waitHidden()
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
		count := len(visibleHandles(pid))
		if count >= 1 {
			return // the fallback panel came up
		}
		if time.Now().After(deadline) {
			t.Fatal("no bubble after the selection hotkey with nothing selected")
		}
		time.Sleep(150 * time.Millisecond)
	}
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
