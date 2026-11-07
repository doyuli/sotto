package ocr

import (
	"strings"
	"testing"

	"sotto/internal/ocr/pipeline"
)

func TestSummaryContainsActionableRisksWithoutRecognizedText(t *testing.T) {
	txt := Text{Text: "private screenshot text", Engine: "ppocr-v6-small", Elapsed: "900ms", Attempts: []EngineAttempt{{
		Engine: "ppocr-v6-small", ElapsedUS: 900000,
		Risks:   []string{"discarded-low-score-regions"},
		Timings: EngineTimings{Pipeline: pipeline.Timings{DetectionInferenceUS: 600000}},
	}}}
	got := recognitionSummary(txt, nil)
	for _, want := range []string{"elapsed=900ms", "det=600ms", "risks=discarded-low-score-regions"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %s in %s", want, got)
		}
	}
	for _, unwanted := range []string{txt.Text, "init=", "queue=", "systemRequestUs", "Pipeline:"} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("unwanted %q in %s", unwanted, got)
		}
	}
}
