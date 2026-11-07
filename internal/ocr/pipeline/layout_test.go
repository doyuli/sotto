package pipeline

import (
	"reflect"
	"testing"
)

func line(text string, x, y, w, h float64) Line {
	return Line{Text: text, Quad: Quad{{x, y}, {x + w, y}, {x + w, y + h}, {x, y + h}}}
}

func TestLayoutParagraphsAndBoundaries(t *testing.T) {
	cases := []struct {
		name  string
		lines []Line
		want  string
	}{
		{"korean", []Line{line("안녕하세요 새로운 문장을", 0, 0, 200, 16), line("계속 작성합니다.", 0, 20, 150, 16)}, "안녕하세요 새로운 문장을 계속 작성합니다."},
		{"english", []Line{line("This is the first part of", 0, 0, 200, 16), line("an English paragraph.", 0, 20, 180, 16)}, "This is the first part of an English paragraph."},
		{"cjk", []Line{line("这是连续中文段落的", 0, 0, 200, 16), line("第二行内容。", 0, 20, 100, 16)}, "这是连续中文段落的第二行内容。"},
		{"mixed", []Line{line("我们可以在这里使用 Windows", 0, 0, 200, 16), line("进行截图翻译。", 0, 20, 150, 16)}, "我们可以在这里使用 Windows进行截图翻译。"},
		{"ui-labels", []Line{line("Save", 0, 0, 40, 16), line("Cancel", 0, 20, 40, 16)}, "Save\n\nCancel"},
		{"blank", []Line{line("First paragraph.", 0, 0, 200, 16), line("Another paragraph.", 0, 52, 200, 16)}, "First paragraph.\n\nAnother paragraph."},
		{"list", []Line{line("- First item", 0, 0, 100, 16), line("2. Second item", 0, 20, 100, 16)}, "- First item\n2. Second item"},
		{"code", []Line{line("func main() {", 0, 0, 200, 16), line("  return value", 0, 20, 190, 16), line("}", 0, 40, 10, 16)}, "func main() {\n  return value\n}"},
		{"table", []Line{line("Name | Value", 0, 0, 200, 16), line("Count | 100", 0, 20, 200, 16)}, "Name | Value\nCount | 100"},
		{"hyphen", []Line{line("A long English hyphen-", 0, 0, 200, 16), line("ated word stays intact", 0, 20, 190, 16)}, "A long English hyphen-\nated word stays intact"},
		{"identifiers", []Line{line("https://a.test/path-1", 0, 0, 200, 16), line("C:\\Dir\\file_v2.txt", 0, 20, 200, 16)}, "https://a.test/path-1\n\nC:\\Dir\\file_v2.txt"},
		{"heading", []Line{line("Title", 0, 0, 210, 32), line("This is a paragraph with", 0, 40, 200, 16), line("a second line of text.", 0, 60, 190, 16)}, "Title\n\nThis is a paragraph with a second line of text."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := Layout(tc.lines, LayoutOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if doc.Text != tc.want {
				t.Fatalf("got %q want %q", doc.Text, tc.want)
			}
		})
	}
}

func TestLayoutColumnsHeadingAndFragments(t *testing.T) {
	lines := []Line{line("right column starts here", 320, 40, 200, 16), line("left column continues", 0, 60, 180, 16), line("left column starts here", 0, 40, 200, 16), line("right column continues", 320, 60, 180, 16), line("A heading across both columns", 0, 0, 520, 24)}
	copy := append([]Line(nil), lines...)
	doc, err := Layout(lines, LayoutOptions{})
	if err != nil {
		t.Fatal(err)
	}
	want := "A heading across both columns\n\nleft column starts here left column continues\n\nright column starts here right column continues"
	if doc.Text != want {
		t.Fatalf("column order %q", doc.Text)
	}
	if !reflect.DeepEqual(lines, copy) {
		t.Fatal("mutated original lines")
	}
	var order []int
	for _, block := range doc.Blocks {
		for _, row := range block.Rows {
			order = append(order, row...)
		}
	}
	if !reflect.DeepEqual(order, []int{4, 2, 1, 0, 3}) {
		t.Fatal(order)
	}
	fragments := []Line{line("world", 55, 1, 40, 16), line("Hello", 0, 0, 48, 16), line("。", 96, 0, 8, 16)}
	doc, err = Layout(fragments, LayoutOptions{})
	if err != nil || doc.Text != "Hello world。" {
		t.Fatalf("fragments %q %v", doc.Text, err)
	}
	if !reflect.DeepEqual(doc.Blocks[0].Rows[0], []int{1, 0, 2}) {
		t.Fatal(doc.Blocks)
	}
}

func TestLayoutPreserveAndInvalid(t *testing.T) {
	lines := []Line{line("First long line of text", 0, 0, 200, 16), line("Second long line of text", 0, 20, 200, 16)}
	doc, err := Layout(lines, LayoutOptions{PreserveLines: true})
	if err != nil || doc.Text != "First long line of text\nSecond long line of text" {
		t.Fatalf("preserve %q %v", doc.Text, err)
	}
	if _, err := Layout([]Line{{Text: "bad", Quad: Quad{}}}, LayoutOptions{}); err == nil {
		t.Fatal("invalid quad")
	}
	lines[0].Text = "embedded\nline"
	if _, err := Layout(lines, LayoutOptions{}); err == nil {
		t.Fatal("embedded newline accepted")
	}
}

func TestAssessKnownAndUnknownScores(t *testing.T) {
	unknown := line("System OCR text", 0, 0, 200, 20)
	if got := Assess(Result{Lines: []Line{unknown}}); len(got.Risks) != 0 {
		t.Fatalf("invented WinRT score: %+v", got)
	}
	small := line("A\ufffd", 0, 0, 100, 10)
	small.HasScore = true
	small.Score = .4
	small.Tokens = []Token{{Text: "A", Score: .3, Step: 0}}
	a := Assess(Result{Lines: []Line{small}, Diagnostics: Diagnostics{LowScoreRegions: 2}})
	reasons := map[string]bool{}
	for _, risk := range a.Risks {
		reasons[risk.Reason] = true
	}
	for _, want := range []string{"small-text", "low-line-confidence", "low-token-confidence", "suspicious-character", "discarded-low-score-regions"} {
		if !reasons[want] {
			t.Fatalf("missing %s in %+v", want, a)
		}
	}
}
