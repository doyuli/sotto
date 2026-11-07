package pipeline

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

type LayoutOptions struct {
	PreserveLines bool
}

type Block struct {
	Kind string  `json:"kind"` // paragraph, heading, list, or lines
	Rows [][]int `json:"rows"` // Each row references original line indices, left to right.
	Text string  `json:"text"`
}

type Document struct {
	Blocks []Block `json:"blocks"`
	Text   string  `json:"text"`
}

type layoutRow struct {
	indices []int
	box     bounds
	text    string
}

// Layout separates strong whitespace gutters before joining fragments. It
// changes only inter-line separators; characters within a line are preserved.
func Layout(lines []Line, options LayoutOptions) (Document, error) {
	var doc Document
	if len(lines) > 2000 {
		return doc, fmt.Errorf("%w: layout lines", ErrLimit)
	}
	indices := make([]int, 0, len(lines))
	boxes := make([]bounds, len(lines))
	textBytes := 0
	for i, line := range lines {
		textBytes += len(line.Text)
		if textBytes > 1<<20 {
			return doc, fmt.Errorf("%w: layout text", ErrLimit)
		}
		if !utf8.ValidString(line.Text) || strings.ContainsAny(line.Text, "\r\n") {
			return doc, fmt.Errorf("layout line %d: expected one UTF-8 line", i)
		}
		if !validQuad(line.Quad) {
			return doc, fmt.Errorf("layout line %d: invalid quad", i)
		}
		b := line.Quad.bounds()
		if max(math.Abs(b.x0), math.Abs(b.x1), math.Abs(b.y0), math.Abs(b.y1)) > 1e7 {
			return doc, fmt.Errorf("layout line %d: coordinate out of range", i)
		}
		boxes[i] = b
		if strings.TrimSpace(line.Text) != "" {
			indices = append(indices, i)
		}
	}
	groups := partition(indices, boxes, 0)
	for _, group := range groups {
		rows := makeRows(group, lines, boxes, options.PreserveLines)
		if len(rows) == 0 {
			continue
		}
		heights := make([]float64, len(rows))
		region := rows[0].box
		for i, r := range rows {
			heights[i] = r.box.height()
			region = region.union(r.box)
		}
		height := median(heights)
		var block *Block
		for i, row := range rows {
			kind := "paragraph"
			if options.PreserveLines || literal(row.text) || math.Abs(lines[row.indices[0]].Quad[1].Y-lines[row.indices[0]].Quad[0].Y) > .3*row.box.height() {
				kind = "lines"
			} else if listItem(row.text) {
				kind = "list"
			} else if row.box.height() > height*1.3 {
				kind = "heading"
			}
			join := false
			if i > 0 && block != nil && kind == "paragraph" && block.Kind == "paragraph" {
				prev := rows[i-1]
				gap := row.box.y0 - prev.box.y1
				indent := math.Abs(row.box.x0 - prev.box.x0)
				// A short previous row, changed indentation, large gap, or
				// multiple boxes on one baseline makes joining ambiguous.
				join = gap >= 0 && gap <= .8*min(row.box.height(), prev.box.height()) &&
					indent <= .6*height && prev.box.width() >= .72*region.width() &&
					max(row.box.height(), prev.box.height()) <= 1.3*min(row.box.height(), prev.box.height()) &&
					proseLike(prev.text)
			}
			if !join {
				doc.Blocks = append(doc.Blocks, Block{Kind: kind, Text: row.text, Rows: [][]int{row.indices}})
				block = &doc.Blocks[len(doc.Blocks)-1]
			} else {
				block.Text = joinText(block.Text, row.text, false)
				block.Rows = append(block.Rows, row.indices)
			}
		}
	}
	var text strings.Builder
	for i, b := range doc.Blocks {
		if i > 0 {
			prev := doc.Blocks[i-1]
			if (b.Kind == "lines" && prev.Kind == "lines") || (b.Kind == "list" && prev.Kind == "list") {
				text.WriteByte('\n')
			} else {
				text.WriteString("\n\n")
			}
		}
		text.WriteString(b.Text)
	}
	doc.Text = text.String()
	return doc, nil
}

func median(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	copy := append([]float64(nil), values...)
	sort.Float64s(copy)
	return copy[len(copy)/2]
}

func columnCut(indices []int, boxes []bounds) ([]int, []int) {
	if len(indices) < 2 {
		return nil, nil
	}
	ordered := append([]int(nil), indices...)
	sort.SliceStable(ordered, func(i, j int) bool { return boxes[ordered[i]].x0 < boxes[ordered[j]].x0 })
	heights := make([]float64, len(indices))
	for i, index := range indices {
		heights[i] = boxes[index].height()
	}
	minGap := max(12, median(heights)*2)
	end := boxes[ordered[0]].x1
	best, cut := minGap, -1
	for i := 1; i < len(ordered); i++ {
		b := boxes[ordered[i]]
		if b.x0-end > best {
			best, cut = b.x0-end, i
		}
		end = max(end, b.x1)
	}
	if cut < 0 {
		return nil, nil
	}
	return ordered[:cut], ordered[cut:]
}

func partition(indices []int, boxes []bounds, depth int) [][]int {
	if len(indices) == 0 {
		return nil
	}
	if len(indices) < 2 || depth >= 32 {
		return [][]int{indices}
	}
	if left, right := columnCut(indices, boxes); len(left) > 0 {
		return append(partition(left, boxes, depth+1), partition(right, boxes, depth+1)...)
	}
	ordered := append([]int(nil), indices...)
	sort.SliceStable(ordered, func(i, j int) bool { return boxes[ordered[i]].y0 < boxes[ordered[j]].y0 })
	heights := make([]float64, len(indices))
	for i, index := range indices {
		heights[i] = boxes[index].height()
	}
	height := median(heights)
	end := boxes[ordered[0]].y1
	best, cut := 0.0, -1
	for i := 1; i < len(ordered); i++ {
		gap := boxes[ordered[i]].y0 - end
		if gap > best {
			best, cut = gap, i
		}
		end = max(end, boxes[ordered[i]].y1)
	}
	if cut > 0 {
		// A spanning heading can hide a column gutter. Split at its blank
		// band only when this exposes columns or a clear paragraph gap.
		left, _ := columnCut(ordered[cut:], boxes)
		right, _ := columnCut(ordered[:cut], boxes)
		if best > height || len(left) > 0 || len(right) > 0 {
			return append(partition(ordered[:cut], boxes, depth+1), partition(ordered[cut:], boxes, depth+1)...)
		}
	}
	return [][]int{ordered}
}

func makeRows(indices []int, lines []Line, boxes []bounds, preserve bool) []layoutRow {
	ordered := append([]int(nil), indices...)
	sort.SliceStable(ordered, func(i, j int) bool {
		a, b := boxes[ordered[i]], boxes[ordered[j]]
		if a.y0 != b.y0 {
			return a.y0 < b.y0
		}
		return a.x0 < b.x0
	})
	var rows []layoutRow
	for _, index := range ordered {
		b := boxes[index]
		match := -1
		if !preserve {
			for j := len(rows) - 1; j >= 0; j-- {
				r := rows[j].box
				if b.y0-r.y1 > max(b.height(), r.height()) {
					break
				}
				if math.Abs(b.y1-r.y1) <= .35*min(b.height(), r.height()) &&
					max(b.height(), r.height()) <= 1.5*min(b.height(), r.height()) {
					match = j
					break
				}
			}
		}
		if match < 0 {
			rows = append(rows, layoutRow{[]int{index}, b, ""})
		} else {
			rows[match].indices = append(rows[match].indices, index)
			rows[match].box = rows[match].box.union(b)
		}
	}
	var merged []layoutRow
	for i := range rows {
		r := &rows[i]
		sort.SliceStable(r.indices, func(i, j int) bool { return boxes[r.indices[i]].x0 < boxes[r.indices[j]].x0 })
		start := len(merged)
		for _, index := range r.indices {
			b := boxes[index]
			join := false
			if len(merged) > start {
				prev := &merged[len(merged)-1]
				gap := b.x0 - prev.box.x1
				join = gap >= -.1*min(b.height(), prev.box.height()) && gap <= 1.5*min(b.height(), prev.box.height())
				if join {
					prev.indices = append(prev.indices, index)
					prev.box = prev.box.union(b)
					prev.text = joinText(prev.text, lines[index].Text, true)
				}
			}
			if !join {
				merged = append(merged, layoutRow{[]int{index}, b, lines[index].Text})
			}
		}
	}
	return merged
}

func proseLike(text string) bool {
	// Short UI labels on adjacent rows should not become a sentence.
	cjkCount := 0
	for _, r := range text {
		if cjk(r) {
			cjkCount++
		}
	}
	return cjkCount >= 6 || utf8.RuneCountInString(text) >= 20 || len(strings.Fields(text)) >= 3
}

func cjk(r rune) bool {
	return unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana)
}
func joinText(a, b string, fragment bool) string {
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	last, _ := utf8.DecodeLastRuneInString(a)
	first, _ := utf8.DecodeRuneInString(b)
	if unicode.IsSpace(last) || unicode.IsSpace(first) {
		return a + b
	}
	if strings.ContainsRune("，。！？、：；）】》」』,.!?;:)]}", first) || strings.ContainsRune("（【《「『([{", last) {
		return a + b
	}
	if cjk(last) || cjk(first) {
		return a + b
	}
	// Preserve a visible hyphen; never remove it as a guessed soft hyphen.
	if !fragment && (last == '-' || last == '‐') {
		return a + "\n" + b
	}
	return a + " " + b
}

func listItem(text string) bool {
	s := strings.TrimSpace(text)
	for _, prefix := range []string{"- ", "* ", "+ ", "•", "●", "▪", "☐", "☑"} {
		if strings.HasPrefix(s, prefix) {
			return true
		}
	}
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	return i > 0 && i < len(s) && (s[i] == '.' || s[i] == ')' || strings.HasPrefix(s[i:], "、"))
}

func literal(text string) bool {
	s := strings.TrimSpace(text)
	if strings.Contains(text, "\t") || strings.Contains(text, "  ") || strings.Contains(text, "|") {
		return true
	}
	if strings.ContainsAny(s, "{}") || strings.Contains(s, ":=") || strings.HasSuffix(s, ";") {
		return true
	}
	for _, prefix := range []string{"//", "#", "func ", "def ", "class ", "const ", "let ", "var ", "import ", "return ", "http://", "https://"} {
		if strings.HasPrefix(s, prefix) {
			return true
		}
	}
	return false
}
