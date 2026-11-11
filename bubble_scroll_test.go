package main

import (
	"fmt"
	"image"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// Short lines leave the scrollbar gutter clear of text in either theme.
func newBubbleScrollTester(t *testing.T, dark bool) (*bubbleView, *ui.Tester) {
	t.Helper()
	cfg := testBubbleConfig()
	v := newBubbleView(1, cfg, bubbleViewActions{})
	v.applyState(BubbleState{
		ID: 1, Generation: 1, SessionID: "scroll-test", ShowID: 1, LayoutID: 1,
		ReqID: "scroll-request", Trigger: "selection",
		Text: strings.Repeat("source line\n", 40), Settings: cfg,
	}, false)
	v.applyResult(TranslationUpdateMsg{ReqID: "scroll-request", Service: "google", Text: strings.Repeat("translated line\n", 80)})
	tt := ui.NewTester(v.render, 480, 360)
	tt.SetDark(dark)
	// TextArea initially places its caret at the end. Start interaction tests
	// at Home and let that independent reveal's indicator expire.
	r := bubbleScrollInputRect(t, tt)
	tt.ClickAt(r.X+20, r.Y+15)
	tt.Key(ui.Ctrl, ui.KeyHome)
	tt.Advance(bubbleScrollbarDelay + bubbleScrollbarFade)
	if v.scroll.MaxY <= 0 || v.composerScroll.MaxY <= 0 {
		t.Fatalf("fixture must overflow both regions: body=%+v input=%+v", v.scroll, v.composerScroll)
	}
	return v, tt
}

func bubbleScrollInputRect(t *testing.T, tt *ui.Tester) ui.Rect {
	t.Helper()
	r, ok := tt.Find("\u8981\u7ffb\u8bd1\u7684\u539f\u6587")
	if !ok || r.W <= 0 || r.H <= 0 {
		t.Fatal("composer bounds not found")
	}
	return r
}

func bubbleScrollBodyRect() ui.Rect { return ui.Rect{X: 0, Y: 30, W: 480, H: 330} }

// Check the entire clear gutter rather than a thumb's font-dependent position.
// No golden image or text rasterization is involved.
func assertBubbleScrollInk(t *testing.T, tt *ui.Tester, r ui.Rect, background ui.Color, visible bool) {
	t.Helper()
	im := tt.Image()
	strip := image.Rect(int(r.X+r.W)-7, int(r.Y)+5, int(r.X+r.W)-3, int(r.Y+r.H)-5).Intersect(im.Bounds())
	ink := 0
	for y := strip.Min.Y; y < strip.Max.Y; y++ {
		for x := strip.Min.X; x < strip.Max.X; x++ {
			p := im.RGBAAt(x, y)
			if p.R != background.R || p.G != background.G || p.B != background.B {
				ink++
			}
		}
	}
	if (ink > 0) != visible {
		t.Errorf("scrollbar gutter %v: ink=%d, want visible=%v", strip, ink, visible)
	}
}

func TestBubbleScrollbarFadeClock(t *testing.T) {
	for _, base := range []*ui.Theme{ui.LightTheme(), ui.DarkTheme()} {
		t.Run(fmt.Sprintf("dark=%t", base.Dark), func(t *testing.T) {
			var indicator bubbleScrollbar
			var scroll ui.ScrollState
			frames := 0
			tt := ui.NewTester(func(c *ui.Context) {
				frames++
				indicator.themed(c, &scroll, base)
			}, 30, 30)
			check := func(opacity float32) {
				t.Helper()
				if got, want := indicator.theme.Scrollbar, base.Scrollbar.Alpha(opacity); got != want {
					t.Errorf("thumb color=%+v, want %+v", got, want)
				}
				if base.Scrollbar.A == 0 || indicator.theme.ScrollbarWidth != base.ScrollbarWidth {
					t.Fatal("indicator changed the base theme or scrollbar width")
				}
			}
			check(0)
			scroll.Y = 12
			tt.Frame()
			check(1)
			tt.Advance(bubbleScrollbarDelay)
			check(1)
			tt.Advance(4 * bubbleScrollbarFrame)
			check(1 - float32(4*bubbleScrollbarFrame)/float32(bubbleScrollbarFade))
			// A stationary rebuild must not renew the activity deadline.
			tt.Frame()
			check(1 - float32(4*bubbleScrollbarFrame)/float32(bubbleScrollbarFade))
			scroll.Y = 24
			tt.Frame()
			check(1)
			tt.Advance(bubbleScrollbarDelay + bubbleScrollbarFade)
			check(0)
			idleFrames := frames
			tt.Advance(time.Second)
			if frames != idleFrames {
				t.Fatal("completed fade kept requesting frames")
			}
			if scroll.Y != 24 {
				t.Fatal("fade changed the scroll offset")
			}
			scroll.X = 5
			tt.Frame()
			check(1)
			indicator.reset(&scroll)
			tt.Frame()
			if indicator.theme.Scrollbar.A != 0 {
				t.Fatal("reset retained visible ink")
			}
			tt.Advance(bubbleScrollbarDelay + bubbleScrollbarFade)
			check(0)
		})
	}
}

func TestBubbleScrollInitialLongComposerNoHoverInk(t *testing.T) {
	cfg := testBubbleConfig()
	v := newBubbleView(1, cfg, bubbleViewActions{})
	v.applyState(BubbleState{
		ID: 1, Generation: 1, SessionID: "initial-scroll", ShowID: 1, LayoutID: 1,
		Trigger: "selection", Text: strings.Repeat("source line\n", 40), Settings: cfg,
	}, false)
	tt := ui.NewTester(v.render, 480, 360)
	r := bubbleScrollInputRect(t, tt)
	t.Logf("initial input offset=%g observed=%g alpha=%d pending=%t", v.composerScroll.Y, v.inputScrollbar.y, v.inputScrollbar.theme.Scrollbar.A, tt.FramePending())
	assertBubbleScrollInk(t, tt, r, lightBubblePalette.composer, false)
	before := v.composerScroll.Y
	tt.Move(r.X+r.W-5, r.Y+20)
	if v.composerScroll.Y != before {
		t.Fatal("hover changed initial scroll offset")
	}
	assertBubbleScrollInk(t, tt, r, lightBubblePalette.composer, false)
}

func TestBubbleScrollVisibilityAndIndependentTimers(t *testing.T) {
	for _, dark := range []bool{false, true} {
		t.Run(fmt.Sprintf("dark=%t", dark), func(t *testing.T) {
			v, tt := newBubbleScrollTester(t, dark)
			p := lightBubblePalette
			if dark {
				p = darkBubblePalette
			}
			check := func(body, input bool) {
				t.Helper()
				assertBubbleScrollInk(t, tt, bubbleScrollBodyRect(), p.panel, body)
				assertBubbleScrollInk(t, tt, bubbleScrollInputRect(t, tt), p.composer, input)
			}
			check(false, false)
			tt.Move(475, 100)
			check(false, false)
			r := bubbleScrollInputRect(t, tt)
			tt.Move(r.X+r.W-5, r.Y+20)
			tt.Advance(bubbleScrollbarDelay + bubbleScrollbarFade)
			check(false, false)

			tt.Scroll(470, 250, 0, 12)
			if v.scroll.Y != 12 || v.composerScroll.Y != 0 {
				t.Fatalf("body scroll changed wrong offsets: body=%g input=%g", v.scroll.Y, v.composerScroll.Y)
			}
			check(true, false)
			tt.Advance(400 * time.Millisecond)
			r = bubbleScrollInputRect(t, tt)
			tt.Scroll(r.X+r.W/2, r.Y+r.H/2, 0, 24)
			if v.scroll.Y != 12 || v.composerScroll.Y != 24 {
				t.Fatalf("input scroll changed wrong offsets: body=%g input=%g", v.scroll.Y, v.composerScroll.Y)
			}
			check(true, true)
			tt.Advance(499 * time.Millisecond)
			check(true, true)
			tt.Advance(time.Millisecond)
			check(true, true) // 900 ms starts the fade rather than hiding abruptly.
			tt.Advance(bubbleScrollbarFade / 2)
			check(true, true)
			// The last scheduled refresh may precede this clock instant by one
			// frame; verify partial ink without forcing an extra build.
			if got := v.bodyScrollbar.theme.Scrollbar.A; got == 0 || got >= p.theme.Scrollbar.A {
				t.Errorf("body did not partially fade: alpha=%d", got)
			}
			if got := v.inputScrollbar.theme.Scrollbar; got != p.theme.Scrollbar {
				t.Errorf("body fade changed input color: %+v", got)
			}
			tt.Advance(bubbleScrollbarFade / 2)
			check(false, true)
			tt.Advance(250 * time.Millisecond)
			check(false, true) // The input's own idle delay has just expired.
			tt.Advance(bubbleScrollbarFade)
			check(false, false)
			if v.scroll.Y != 12 || v.composerScroll.Y != 24 {
				t.Fatal("hiding indicators changed scroll offsets")
			}
		})
	}
}

func TestBubbleScrollWheelPreservesOffsets(t *testing.T) {
	for _, input := range []bool{false, true} {
		t.Run(fmt.Sprintf("input=%t", input), func(t *testing.T) {
			v, tt := newBubbleScrollTester(t, false)
			scroll, indicator, bg, r := &v.scroll, &v.bodyScrollbar, lightBubblePalette.panel, bubbleScrollBodyRect()
			x, y := float32(470), float32(250)
			if input {
				scroll, indicator, bg = &v.composerScroll, &v.inputScrollbar, lightBubblePalette.composer
				r = bubbleScrollInputRect(t, tt)
				x, y = r.X+r.W/2, r.Y+r.H/2
			}
			tt.Wheel(x, y, 0, 96)
			tt.Advance(400 * time.Millisecond)
			before := scroll.Y
			if before <= 0 {
				t.Fatal("wheel did not scroll")
			}
			assertBubbleScrollInk(t, tt, r, bg, true)
			if indicator.theme.Scrollbar.A == 0 {
				t.Error("wheel offset was not observed")
			}
			tt.Advance(bubbleScrollbarDelay + bubbleScrollbarFade)
			assertBubbleScrollInk(t, tt, r, bg, false)
			if scroll.Y != before {
				t.Fatalf("idle offset=%g, want %g", scroll.Y, before)
			}
			tt.Wheel(x, y, 0, -48)
			tt.Advance(400 * time.Millisecond)
			if scroll.Y >= before {
				t.Fatalf("reverse wheel offset=%g, was %g", scroll.Y, before)
			}
			assertBubbleScrollInk(t, tt, r, bg, true)
		})
	}
}

func TestBubbleScrollKeyboardRevealWithoutExtraFrame(t *testing.T) {
	v, tt := newBubbleScrollTester(t, false)
	r := bubbleScrollInputRect(t, tt)
	tt.ClickAt(r.X+20, r.Y+15)
	tt.Key(ui.Ctrl, ui.KeyHome)
	tt.Advance(bubbleScrollbarDelay + bubbleScrollbarFade)
	assertBubbleScrollInk(t, tt, r, lightBubblePalette.composer, false)
	tt.Key(ui.Ctrl, ui.KeyEnd)
	if v.composerScroll.Y <= 0 {
		t.Fatal("Ctrl+End did not reveal the last line")
	}
	// Do not call Frame: caret reveal changes the offset during layout, after
	// themed ran. A forced build would conceal a missing follow-up frame.
	if v.inputScrollbar.y != v.composerScroll.Y || v.inputScrollbar.theme.Scrollbar.A == 0 {
		t.Errorf("post-layout offset not observed: indicator=%g actual=%g alpha=%d pending=%t", v.inputScrollbar.y, v.composerScroll.Y, v.inputScrollbar.theme.Scrollbar.A, tt.FramePending())
	}
	assertBubbleScrollInk(t, tt, r, lightBubblePalette.composer, true)
	before := v.composerScroll.Y
	tt.Advance(899 * time.Millisecond)
	assertBubbleScrollInk(t, tt, r, lightBubblePalette.composer, true)
	tt.Advance(time.Millisecond)
	assertBubbleScrollInk(t, tt, r, lightBubblePalette.composer, true)
	tt.Advance(bubbleScrollbarFade)
	assertBubbleScrollInk(t, tt, r, lightBubblePalette.composer, false)
	if v.composerScroll.Y != before {
		t.Fatal("hiding changed keyboard scroll offset")
	}
	tt.Key(ui.Ctrl, ui.KeyHome)
	if v.composerScroll.Y != 0 {
		t.Fatalf("Ctrl+Home offset=%g, want 0", v.composerScroll.Y)
	}
}

func TestBubbleScrollDragSurvivesStationaryHold(t *testing.T) {
	for _, input := range []bool{false, true} {
		t.Run(fmt.Sprintf("input=%t", input), func(t *testing.T) {
			v, tt := newBubbleScrollTester(t, false)
			r, scroll, bg := bubbleScrollBodyRect(), &v.scroll, lightBubblePalette.panel
			x, y := float32(470), float32(250)
			if input {
				r, scroll, bg = bubbleScrollInputRect(t, tt), &v.composerScroll, lightBubblePalette.composer
				x, y = r.X+r.W/2, r.Y+r.H/2
			}
			tt.Scroll(x, y, 0, 24)
			bar := ui.Scrollbar{Box: r, View: r.H, Content: r.H + scroll.MaxY, Offset: scroll.Y}
			start, length := bar.Thumb()
			x, y = r.X+r.W-5, start+length/2
			tt.Press(x, y)
			tt.Move(x, y+8)
			before := scroll.Y
			if before <= 24 {
				t.Fatal("fixture did not start a thumb drag")
			}
			tt.Advance(2 * time.Second)
			// Ink may disappear while the thumb is held still; capture must
			// survive so the next movement continues the same drag.
			assertBubbleScrollInk(t, tt, r, bg, false)
			if scroll.Y != before {
				t.Fatal("stationary hold changed offset")
			}
			tt.Move(x, y+16)
			if scroll.Y <= before {
				t.Fatalf("drag lost capture after pause: before=%g after=%g", before, scroll.Y)
			}
			assertBubbleScrollInk(t, tt, r, bg, true)
			tt.Release(x, y+16)
			before = scroll.Y
			tt.Advance(bubbleScrollbarDelay + bubbleScrollbarFade)
			assertBubbleScrollInk(t, tt, r, bg, false)
			if scroll.Y != before {
				t.Fatal("release/hide changed drag offset")
			}
		})
	}
}
