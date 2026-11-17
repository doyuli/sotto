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
	tt.SetSize(v.measured.width, v.measured.height)
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
	measured := v.measured
	tt.SetSize(measured.width, measured.height)
	if measured != v.measured || measured.width != 320 || measured.height <= 35 || measured.height >= 150 {
		t.Fatalf("result resized again after expanding from loading: was %+v, now %+v", measured, v.measured)
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
	if v.scroll.MaxY <= 0 || v.scroll.Y != v.scroll.MaxY {
		t.Fatalf("stream did not follow its end: Y=%g MaxY=%g", v.scroll.Y, v.scroll.MaxY)
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

func TestResizingResultKeepsSourceVisibleAndRespectsDragging(t *testing.T) {
	// An anchored status near a screen edge used to grow over its source.
	work := mygo.Rectangle{Width: 1200, Height: 800}
	source := mygo.Rectangle{X: 400, Y: 747, Width: 100, Height: 40}
	loading := place(work, source, size{100, 35}, nil)
	grown := resize(work, source, loading, size{320, 150}, false, nil)
	if overlap(grown, source) != 0 || grown.Y+grown.Height != loading.Y+loading.Height {
		t.Fatalf("growing above the source moved the anchored edge or covered the source: loading=%+v grown=%+v", loading, grown)
	}

	source.Y = 250
	loading = place(work, source, size{100, 35}, nil)
	neighbor := mygo.Rectangle{X: 515, Y: loading.Y, Width: 320, Height: 150}
	grown = resize(work, source, loading, size{320, 150}, false, []mygo.Rectangle{neighbor})
	if overlap(grown, neighbor) != 0 || overlap(grown, source) != 0 {
		t.Fatalf("growing result did not avoid occupied space: %+v", grown)
	}
	dragged := resize(work, source, loading, size{320, 150}, true, []mygo.Rectangle{neighbor})
	if dragged.X != loading.X || dragged.Y != loading.Y {
		t.Fatalf("resizing moved a manually placed result: was %+v, got %+v", loading, dragged)
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
	st.RightClickAt(20, 20)
	if closed != 1 || shortClosed != 1 {
		t.Fatalf("dismissals: long=%d short=%d", closed, shortClosed)
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
