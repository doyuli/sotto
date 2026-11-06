package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/egoist/mygo"

	"sotto/internal/capture"
	"sotto/internal/ocr"
)

// contextWithTimeout keeps the call sites short.
func contextWithTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}

// snipManager owns the full-virtual-screen overlay the user drags the
// OCR rectangle on. One window, hidden and reused; a fresh query reloads
// its page through a changing hash, so the drag state resets.
type snipManager struct {
	mu   sync.Mutex
	win  *mygo.Window
	once sync.Once
}

// Show brings the overlay up, on top of everything.
func (s *snipManager) Show() {
	s.once.Do(func() { s.create() })
	s.mu.Lock()
	win := s.win
	s.mu.Unlock()
	if win == nil {
		log.Printf("snip: no window")
		return
	}
	// A changed hash remounts the page, resetting the drag state.
	win.Page().LoadURL(fmt.Sprintf("/#/snip?t=%d", time.Now().UnixNano()))
	win.Show()
}

// Hide takes the overlay down without processing anything.
func (s *snipManager) Hide() {
	s.mu.Lock()
	win := s.win
	s.mu.Unlock()
	if win != nil {
		win.Hide()
	}
}

// origin returns the overlay window's DIP position, the virtual
// screen's top-left corner, which the page's client coordinates are
// relative to.
func (s *snipManager) origin() (x, y int, ok bool) {
	s.mu.Lock()
	win := s.win
	s.mu.Unlock()
	if win == nil {
		return 0, 0, false
	}
	x, y = win.Position()
	return x, y, true
}

func (s *snipManager) create() {
	// One window over the whole virtual screen, in DIPs: the union of
	// every display's bounds, which can start negative.
	left, top, right, bottom := 0, 0, 0, 0
	first := true
	for _, d := range mygo.Screen.Displays() {
		if first {
			left, top, right, bottom = d.Bounds.X, d.Bounds.Y, d.Bounds.X+d.Bounds.Width, d.Bounds.Y+d.Bounds.Height
			first = false
			continue
		}
		left = min(left, d.Bounds.X)
		top = min(top, d.Bounds.Y)
		right = max(right, d.Bounds.X+d.Bounds.Width)
		bottom = max(bottom, d.Bounds.Y+d.Bounds.Height)
	}
	if first {
		log.Printf("snip: no displays")
		return
	}
	// The veil is the window's own opacity, not page transparency: on
	// Windows a plain Transparent window composites through a GDI
	// redirection bitmap without alpha, which shows black where the
	// page is transparent. A layered window (Opacity) dims the desktop
	// for real, and the page only draws the selection box and hints.
	w := mygo.NewWindow(mygo.WindowOptions{
		Title:  "Sotto 截图",
		URL:    "/#/snip",
		X:      left,
		Y:      top,
		Width:  right - left,
		Height: bottom - top,
		// Created hidden: a window visible while its page is still
		// loading loses the focus it grabbed, and the blur handler
		// would hide it at birth, before anyone saw it.
		Hidden:          true,
		Frameless:       true,
		Panel:           true,
		SkipTaskbar:     true,
		AlwaysOnTop:     true,
		DisableResize:   true,
		Opacity:         0.35,
		BackgroundColor: "#10141c",
		Page:            mygo.PageOptions{DevTools: mygo.DevToolsAuto},
	})
	s.mu.Lock()
	s.win = w
	s.mu.Unlock()
	// Going elsewhere cancels the shot, as Snipaste's does — but not
	// within a moment of showing: a synthetic or locked-out foreground
	// attempt loses the focus instantly, which would kill the shot at
	// birth. Real Alt+Tab happens later and still cancels.
	shownAt := time.Time{}
	w.OnBlur(func() {
		if time.Since(shownAt) > 500*time.Millisecond {
			w.Hide()
		}
	})
	w.OnShow(func() { shownAt = time.Now() })
}

// snipRect is what the page reports: the dragged rectangle in client
// coordinates, DIPs from the overlay's top-left corner.
type snipRect struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

// snipDone closes the shot: hide the overlay, capture the rectangle,
// recognize it, and hand the text to a bubble that starts translating.
// The heavy work runs on a goroutine — this comes in from the page.
func (a *application) snipDone(rect *snipRect) {
	a.snip.Hide()
	if rect == nil || rect.W < 8 || rect.H < 8 {
		return // click without a drag, or Esc: nothing to do
	}
	ox, oy, ok := a.snip.origin()
	if !ok {
		return
	}
	dip := capture.Rect{X: ox + rect.X, Y: oy + rect.Y, W: rect.W, H: rect.H}
	go func() {
		// Hide is synchronous, but the compositor may still have the veil
		// on screen for a frame or two; capturing it would dim the shot
		// and cost the recognizer contrast it needs.
		time.Sleep(120 * time.Millisecond)
		start := time.Now()
		img, err := capture.Grab(dip)
		if err != nil {
			a.notify("Sotto", "截屏失败："+err.Error())
			return
		}
		ctx, cancel := contextWithTimeout(30 * time.Second)
		defer cancel()
		text, err := ocr.Recognize(ctx, img)
		if err != nil {
			a.notify("Sotto", "OCR 失败："+err.Error())
			return
		}
		if text.Text == "" {
			a.notify("Sotto", "未识别到文字：框选范围再大一点，或包含完整的一两行")
			return
		}
		log.Printf("ocr: %d chars in %s (%s)", len(text.Text), text.Elapsed, time.Since(start).Round(time.Millisecond))
		// The bubble opens just below the shot, without stealing the
		// focus from whatever app the user was reading.
		anchor := &Anchor{X: dip.X, Y: dip.Y + dip.H}
		shown := a.bubbles.ShowAt(a.settings(), "ocr", text.Text, anchor)
		a.runTranslation(shown.Win, RunReq{
			ReqID:  shown.ReqID,
			Source: "ocr",
			Text:   text.Text,
		})
	}()
}
