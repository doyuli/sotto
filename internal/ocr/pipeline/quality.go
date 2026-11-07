package pipeline

import (
	"math"
	"strings"
	"unicode"
)

type Risk struct {
	Reason string `json:"reason"`
	Line   int    `json:"line"` // -1 refers to a detector-wide observation.
	Tokens []int  `json:"tokens"`
}

type Assessment struct {
	Risks        []Risk `json:"risks"`
	SuggestRetry bool   `json:"suggestRetry"`
}

// Assess reports observable risks, not accuracy or cross-engine scores. It
// cannot discover missing regions that produced no boxes. The caller controls
// whether one extra inference is affordable, and which engine to use.
func Assess(result Result) Assessment {
	a := Assessment{}
	add := func(reason string, line int, tokens []int) { a.Risks = append(a.Risks, Risk{reason, line, tokens}) }
	if len(result.Lines) == 0 {
		add("no-text", -1, nil)
	}
	if result.Diagnostics.LowScoreRegions > 0 {
		add("discarded-low-score-regions", -1, nil)
	}
	if result.Diagnostics.DegenerateBoxes > 0 {
		add("degenerate-regions", -1, nil)
	}
	for i, line := range result.Lines {
		if strings.TrimSpace(line.Text) == "" {
			add("empty-line", i, nil)
		}
		if !validQuad(line.Quad) {
			add("invalid-geometry", i, nil)
		} else {
			h := min(distance(line.Quad[0], line.Quad[3]), distance(line.Quad[1], line.Quad[2]))
			if h < 14 {
				add("small-text", i, nil)
			}
			if math.Abs(line.Quad[1].Y-line.Quad[0].Y) > h*.5 {
				add("steep-line", i, nil)
			}
		}
		if line.HasScore && (!probability(line.Score) || line.Score < .8) {
			add("low-line-confidence", i, nil)
		}
		var low []int
		for j, token := range line.Tokens {
			if !probability(token.Score) || token.Score < .6 {
				low = append(low, j)
			}
		}
		if len(low) > 0 {
			add("low-token-confidence", i, low)
		}
		for _, r := range line.Text {
			if r == '\ufffd' || (unicode.IsControl(r) && r != '\t') {
				add("suspicious-character", i, nil)
				break
			}
		}
	}
	a.SuggestRetry = len(a.Risks) > 0
	return a
}
