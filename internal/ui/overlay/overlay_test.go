package overlay

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func TestLoadingViewSettlesOnResultAndRespectsReduceMotion(t *testing.T) {
	// A loading animation must stop after completion and with Reduce Motion.
	v := resultView{pending: true, animate: true, limit: size{320, 150}, measure: func() {}}
	tt := ui.NewTester(v.render, 320, 150)
	tt.Frame()
	if !tt.HasText("正在翻译") || v.measured.height != 35 || v.measured.width >= 160 {
		t.Fatalf("loading state is missing or oversized: %+v", v.measured)
	}
	first := bytes.Clone(tt.Image().Pix)
	tt.Advance(170 * time.Millisecond)
	if bytes.Equal(first, tt.Image().Pix) {
		t.Fatal("loading dots did not animate")
	}
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	static := bytes.Clone(tt.Image().Pix)
	tt.Advance(170 * time.Millisecond)
	if !bytes.Equal(static, tt.Image().Pix) {
		t.Fatal("loading dots animated with Reduce Motion")
	}
	tt.SetPreferences(ui.Preferences{})
	v.setText(strings.Repeat("译文已经就绪。", 7), false)
	tt.Frame()
	tt.Frame()
	tt.Advance(resultResizeDuration)
	if v.bounds.Width != v.measured.width || v.bounds.Height != v.measured.height ||
		v.bounds.Height <= 35 || v.bounds.Height >= 150 {
		t.Fatalf("result did not settle at its measured size: bounds=%+v target=%+v", v.bounds, v.measured)
	}
	if tt.HasText("正在翻译") || !tt.HasText(v.text) {
		t.Fatal("translation did not replace the loading state")
	}
	finished := bytes.Clone(tt.Image().Pix)
	tt.Advance(170 * time.Millisecond)
	if !bytes.Equal(finished, tt.Image().Pix) || tt.FramePending() {
		t.Fatal("completed result kept animating")
	}
}

func TestStreamingViewAnimatesHeightInsideFixedWindow(t *testing.T) {
	// A short first chunk used to fill the full-height window.
	v := resultView{pending: true, limit: size{320, 150}, alignBottom: true, measure: func() {}}
	tt := ui.NewTester(v.render, v.limit.width, v.limit.height)
	tt.Frame()

	v.setText("First line.", true)
	tt.Frame()
	tt.Advance(resultResizeDuration)
	if !tt.HasText(v.text) || v.bounds.Height != lineHeight+2*paddingY ||
		tt.Image().RGBAAt(30, 20).A != 0 {
		t.Fatalf("one line should leave the rest of the native window transparent: bounds=%+v", v.bounds)
	}

	v.setText("First line.\nSecond line.\nThird line.", true)
	tt.Frame()
	tt.Frame()
	tt.Advance(resultResizeDuration / 2)
	mid := v.bounds
	if mid.Height <= lineHeight+2*paddingY || mid.Height >= 3*lineHeight+2*paddingY ||
		mid.Y+mid.Height != v.limit.height {
		t.Fatalf("expansion should ease toward the content around a stable bottom edge: %+v", mid)
	}

	v.setText(v.text+"\nFourth line.", true)
	tt.Frame()
	tt.Frame()
	if v.bounds.Height < mid.Height || v.bounds.Height >= 4*lineHeight+2*paddingY {
		t.Fatalf("retargeting should continue from the displayed height: before=%+v now=%+v", mid, v.bounds)
	}
	tt.Advance(resultResizeDuration)
	v.setText(v.text, false)
	tt.Frame()
	tt.Advance(resultResizeDuration)
	if v.bounds.Width >= v.limit.width || v.bounds.Height != 4*lineHeight+2*paddingY || !tt.HasText(v.text) {
		t.Fatalf("completed result did not size to its content: bounds=%+v", v.bounds)
	}
}

func TestResultResizeRespectsReduceMotion(t *testing.T) {
	// The system preference also applies to the panel's size transition.
	v := resultView{pending: true, limit: size{320, 150}, measure: func() {}}
	tt := ui.NewTester(v.render, 320, 150)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	v.setText("First line.\nSecond line.", true)
	tt.Frame()
	tt.Frame()
	if v.bounds.Height != 2*lineHeight+2*paddingY || tt.FramePending() {
		t.Fatalf("reduced motion should settle without animation: bounds=%+v", v.bounds)
	}
}

func TestStreamingViewFollowsNewLinesUntilReaderScrollsBack(t *testing.T) {
	// Appending text must not pull the reader away from an earlier paragraph.
	v := resultView{pending: true, limit: size{320, 150}, measure: func() {}}
	tt := ui.NewTester(v.render, 320, 150)
	tt.Frame()
	v.setText("开头已经译好。", true)
	tt.Frame()
	if tt.HasText("正在翻译") || !tt.HasText(v.text) || v.measured.width != 320 {
		t.Fatal("first chunk did not replace loading at the streaming width")
	}

	long := strings.Repeat("另一个已经译好的句子。\n", 12)
	v.setText(v.text+"\n"+long+"接下来的内容。", true)
	tt.Frame()
	tt.Frame()
	tt.Advance(resultResizeDuration)
	if v.scroll.MaxY <= 0 || v.scroll.Y != v.scroll.MaxY {
		t.Fatalf("stream did not follow its end: Y=%g MaxY=%g", v.scroll.Y, v.scroll.MaxY)
	}
	// Scrolling over the card's padding used to stop following even though
	// the scroll container never received the event.
	tt.Wheel(30, paddingY/2, 0, -65)
	v.setText(v.text+"\n继续跟随。", true)
	tt.Frame()
	if v.scroll.Y != v.scroll.MaxY {
		t.Fatalf("wheel on padding stopped following: Y=%g MaxY=%g", v.scroll.Y, v.scroll.MaxY)
	}
	// While wheel motion has only been scheduled, the reported offset is
	// still at the end. A chunk in that frame used to swallow the notch.
	tt.Wheel(30, 30, 0, -65)
	v.setText(v.text+"\n还有一句。", true)
	tt.Frame()
	tt.Advance(300 * time.Millisecond)
	offset := v.scroll.Y
	if offset >= v.scroll.MaxY {
		t.Fatal("could not scroll back while translating")
	}
	v.setText(v.text+"\n"+long+"最后一个句子。", true)
	tt.Frame()
	if v.scroll.Y != offset || v.measured.height != 150 {
		t.Fatalf("stream moved the reader: offset=%g want=%g, height=%d", v.scroll.Y, offset, v.measured.height)
	}
	tt.Wheel(30, 30, 0, v.scroll.MaxY)
	tt.Advance(300 * time.Millisecond)
	v.setText(v.text+"\n回到底部后继续跟随。", true)
	tt.Frame()
	if v.scroll.Y != v.scroll.MaxY {
		t.Fatalf("stream did not resume following: Y=%g MaxY=%g", v.scroll.Y, v.scroll.MaxY)
	}
	tt.Scroll(30, 30, 0, -65)
	offset = v.scroll.Y
	v.setText(v.text, false)
	tt.Frame()
	if v.scroll.Y != offset {
		t.Fatalf("completion moved the reader: offset=%g want=%g", v.scroll.Y, offset)
	}
}

func TestResultMeasuresBeforePainting(t *testing.T) {
	// The window can become visible before its second frame reaches the screen.
	v := resultView{text: "短译文", limit: size{320, 150}, measuring: true, measure: func() {}}
	tt := ui.NewTester(v.render, v.limit.width, v.limit.height)
	tt.Frame()
	if v.measured.height != lineHeight+2*paddingY {
		t.Fatalf("transparent frame did not measure the translation: %+v", v.measured)
	}
	if tt.Image().RGBAAt(20, 20).A != 0 {
		t.Fatal("measurement frame painted a card at the provisional position")
	}
	v.measuring = false
	v.alignBottom = true
	tt.Frame()
	if tt.Image().RGBAAt(20, 20).A != 0 || tt.Image().RGBAAt(20, 130).A == 0 {
		t.Fatal("card did not paint at its final position")
	}
}

func TestStreamChoosesRoomToGrowAwayFromSource(t *testing.T) {
	// One line fitted below the source but grew upward over it at the screen edge.
	for _, source := range []mygo.Rectangle{
		{X: 400, Y: 700, Width: 200, Height: 30},
		{X: 1000, Y: 80, Width: 100, Height: 620},
	} {
		work := mygo.Rectangle{Width: 1200, Height: 800}
		limit := limits(work)
		panel := initialPanel(work, source, size{100, 35}, limit, nil)
		frame, inset, right, bottom := reservePanel(work, panel, limit,
			panel.X+panel.Width <= source.X, panel.Y+panel.Height <= source.Y)
		p := Result{view: resultView{
			text: "第一行。", pending: true, limit: limit, inset: inset,
			alignRight: right, alignBottom: bottom, measure: func() {},
		}}
		tt := ui.NewTester(p.view.render, frame.Width, frame.Height)
		tt.Frame()
		if got := p.visibleBounds(frame); overlap(got, source) != 0 {
			t.Fatalf("initial panel covers source: panel=%+v source=%+v", got, source)
		}
		p.view.setText(strings.Repeat("继续显示译文。\n", 8), true)
		tt.Frame()
		tt.Frame()
		tt.Advance(resultResizeDuration)
		if got := p.visibleBounds(frame); overlap(got, source) != 0 {
			t.Fatalf("stream grew over source: panel=%+v source=%+v", got, source)
		}
	}
}

func TestResultViewScrollsAndClosesIndependently(t *testing.T) {
	// Scrolling and closing one result must leave other results alone.
	initial := mygo.Point{X: 40, Y: 50}
	cursor := mygo.Point{X: 50, Y: 60}
	focused, closed := 0, 0
	long := resultView{
		text:  strings.TrimSpace(strings.Repeat("这是一行已经翻译的文字。\n", 12)),
		limit: size{320, 150}, measure: func() {},
		cursor: func() mygo.Point { return cursor }, position: func() mygo.Point { return initial },
		move: func(p mygo.Point) { initial = p }, focus: func() { focused++ }, dismiss: func() { closed++ },
	}
	shortClosed := 0
	short := resultView{text: "先从最小的改动看起。", limit: long.limit,
		measure: func() {}, dismiss: func() { shortClosed++ }}
	tt := ui.NewTester(long.render, 320, 150)
	st := ui.NewTester(short.render, 320, 150)
	tt.Frame()
	st.Frame()
	tt.Frame()
	tt.Advance(resultResizeDuration)
	if long.measured.height != 150 || short.measured.height != 35 || short.measured.width >= 320 {
		t.Fatalf("unexpected measured sizes: long=%+v short=%+v", long.measured, short.measured)
	}
	tt.Scroll(50, 40, 0, 100)
	if long.scroll.Y <= 0 || short.scroll.Y != 0 {
		t.Fatalf("scroll positions: long=%v short=%v", long.scroll.Y, short.scroll.Y)
	}
	tt.Press(50, 60)
	cursor.X += 20
	tt.Move(70, 60)
	tt.Release(70, 60)
	if focused != 1 || initial.X != 60 {
		t.Fatalf("left drag: focus=%d position=%+v", focused, initial)
	}
	tt.Key(0, ui.KeyEscape)
	st.RightClickAt(20, 110)
	if shortClosed != 0 {
		t.Fatal("the space outside the visible panel received a click")
	}
	st.RightClickAt(20, 20)
	if closed != 1 || shortClosed != 1 {
		t.Fatalf("dismissals: long=%d short=%d", closed, shortClosed)
	}
}

func TestDraggedPanelReachesScreenEdgeWithRoomToGrow(t *testing.T) {
	// The larger native frame should not keep a short visible panel away from the edge.
	work := mygo.Rectangle{X: -900, Y: -100, Width: 900, Height: 700}
	visible := mygo.Rectangle{X: -112, Y: 553, Width: 100, Height: 35}
	frame, inset, right, bottom := reservePanel(work, visible, limits(work), false, false)
	if !right || !bottom || overlap(work, frame) != frame.Width*frame.Height {
		t.Fatalf("the native frame should reserve space above and to the left: %+v", frame)
	}
	if frame.X+frame.Width-inset.X != visible.X+visible.Width ||
		frame.Y+frame.Height-inset.Y != visible.Y+visible.Height {
		t.Fatalf("reserving space moved the panel away from the drop point: frame=%+v inset=%+v", frame, inset)
	}
}

func TestCompactPanelStaysBelowSourceWhenBackingDoesNotFit(t *testing.T) {
	// Placing the full transparent frame first used to put the visible line over the source.
	work := mygo.Rectangle{Width: 1200, Height: 800}
	source := mygo.Rectangle{X: 135, Y: 30, Width: 1000, Height: 700}
	limit := limits(work)
	panel := initialPanel(work, source, size{320, 35}, limit, nil)
	frame, inset, right, bottom := reservePanel(work, panel, limit, false, false)
	p := Result{view: resultView{
		text: "译文。", pending: true, limit: limit, inset: inset,
		alignRight: right, alignBottom: bottom, measure: func() {},
	}}
	tt := ui.NewTester(p.view.render, frame.Width, frame.Height)
	tt.Frame()
	if got := p.visibleBounds(frame); got != panel || overlap(got, source) != 0 {
		t.Fatalf("reserved frame moved the visible panel over the source: got=%+v want=%+v", got, panel)
	}
}

func TestPanelKeepsPositionAfterWorkAreaChanges(t *testing.T) {
	// Rebuilding the native frame must use the visible panel's position and anchor.
	work := mygo.Rectangle{X: -900, Y: -100, Width: 900, Height: 700}
	panel := mygo.Rectangle{X: -150, Y: 530, Width: 100, Height: 35}
	frame, inset, right, bottom := reservePanel(work, panel, limits(work), false, false)
	p := Result{view: resultView{
		text: "短译文", pending: true, limit: limits(work), inset: inset,
		alignRight: right, alignBottom: bottom, measure: func() {},
	}}
	tt := ui.NewTester(p.view.render, frame.Width, frame.Height)
	tt.Frame()
	before := p.visibleBounds(frame)
	work.Height -= 100
	frame, inset, right, bottom = reservePanel(work, before, limits(work), right, bottom)
	p.view.inset, p.view.alignRight, p.view.alignBottom = inset, right, bottom
	tt.Frame()
	if got := p.visibleBounds(frame); got != fit(work, before) {
		t.Fatalf("work-area change moved the panel beyond clamping: got=%+v want=%+v", got, fit(work, before))
	}
}

func TestPlacementAvoidsSourceAndOtherResults(t *testing.T) {
	work := mygo.Rectangle{X: -900, Y: -100, Width: 900, Height: 700}
	source := mygo.Rectangle{X: -350, Y: 150, Width: 160, Height: 30}
	s := size{320, 150}
	first := place(work, source, s, nil)
	second := place(work, source, s, []mygo.Rectangle{first})
	if overlap(first, source) != 0 || overlap(second, source) != 0 || overlap(first, second) != 0 {
		t.Fatalf("overlapping placements: source=%+v first=%+v second=%+v", source, first, second)
	}
	for _, r := range []mygo.Rectangle{first, second} {
		if overlap(work, r) != r.Width*r.Height {
			t.Fatalf("placement %+v outside %+v", r, work)
		}
	}
}
