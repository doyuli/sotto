package ocr

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"log"
	"math"
	"reflect"
	"strings"
	"testing"

	"sotto/internal/ocr/pipeline"
)

func TestAttemptDiagnosticsSurviveFallbackAndFailure(t *testing.T) {
	for _, localFails := range []bool{false, true} {
		for _, systemFails := range []bool{false, true} {
			t.Run(fmt.Sprintf("local=%t/system=%t", localFails, systemFails), func(t *testing.T) {
				failure := errors.New("inference failed")
				local := func(context.Context, image.Image, string) (Text, error) {
					text := Text{Engine: "ppocr-v6-small", Diagnostics: pipeline.Diagnostics{DetectorWidth: 2144, DetectorHeight: 448}, timings: EngineTimings{InitializationUS: 100, Pipeline: pipeline.Timings{DetectionInferenceUS: 500}}}
					if localFails {
						return text, failure
					}
					return text, nil // Empty local result must still retain costs.
				}
				system := func(context.Context, image.Image, string) (Text, error) {
					text := Text{Engine: "winrt", timings: EngineTimings{SystemRequestUS: 200}}
					if systemFails {
						return text, failure
					}
					text.Lines = []pipeline.Line{testLine("recovered", 0, 0, 100, 24, -1)}
					return text, nil
				}
				got, err := recognizeEngines(context.Background(), nil, "", local, system)
				if systemFails != (err != nil) || len(got.Attempts) != 2 {
					t.Fatalf("%+v %v", got, err)
				}
				a, b := got.Attempts[0], got.Attempts[1]
				if a.Engine != "ppocr-v6-small" || a.Timings.InitializationUS != 100 || a.Timings.Pipeline.DetectionInferenceUS != 500 || a.Diagnostics.DetectorWidth != 2144 || b.Engine != "winrt" || b.Timings.SystemRequestUS != 200 {
					t.Fatalf("lost attempt costs: %+v", got.Attempts)
				}
				if (a.Error != "") != localFails || (b.Error != "") != systemFails {
					t.Fatalf("lost errors: %+v", got.Attempts)
				}
				if systemFails && (got.Text != "" || len(got.Lines) != 0) {
					t.Fatal("error published partial text")
				}
			})
		}
	}
}

func TestLocalFailureIsLoggedBeforeFallback(t *testing.T) {
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(previous) })
	failure := errors.New("invalid asset size: ppocr_keys.txt")
	local := func(context.Context, image.Image, string) (Text, error) { return Text{}, failure }
	system := func(context.Context, image.Image, string) (Text, error) {
		if !strings.Contains(output.String(), failure.Error()) {
			t.Fatal("local failure was not logged before fallback")
		}
		return Text{Engine: "winrt", Lines: []pipeline.Line{testLine("system text", 0, 0, 100, 24, -1)}}, nil
	}
	got, err := recognizeEngines(context.Background(), nil, "", local, system)
	if err != nil || got.Selection != "local-unavailable" || got.RetryCount != 1 {
		t.Fatalf("result %+v error %v", got, err)
	}
}

func TestLowConfidenceWhitespaceDoesNotScheduleFallback(t *testing.T) {
	for _, token := range []string{" ", "\t", "\u3000", "x"} {
		t.Run(fmt.Sprintf("%q", token), func(t *testing.T) {
			line := testLine("Hello Sotto", 0, 0, 200, 24, .95)
			line.Tokens = []pipeline.Token{{Text: "Hello", Score: .99}, {Text: token, Score: .54}, {Text: "Sotto", Score: .99}}
			local := func(context.Context, image.Image, string) (Text, error) {
				return Text{Engine: "local", Lines: []pipeline.Line{line}}, nil
			}
			calls := 0
			system := func(context.Context, image.Image, string) (Text, error) {
				calls++
				return Text{}, nil
			}
			got, err := recognizeEngines(context.Background(), nil, "", local, system)
			wantCalls := 0
			if token == "x" {
				wantCalls = 1
			}
			if err != nil || calls != wantCalls || got.RetryCount != wantCalls || got.Engine != "local" {
				t.Fatalf("result %+v error %v calls=%d want=%d", got, err, calls, wantCalls)
			}
			if wantCalls == 1 && !reflect.DeepEqual(got.Risks[0].Tokens, []int{1}) {
				t.Fatalf("lost original token index: %+v", got.Risks)
			}
		})
	}
}

func testLine(text string, x, y, w, h, score float64) pipeline.Line {
	return pipeline.Line{Text: text, Quad: pipeline.Quad{{X: x, Y: y}, {X: x + w, Y: y}, {X: x + w, Y: y + h}, {X: x, Y: y + h}}, Score: score, HasScore: score >= 0}
}

func TestEngineRoutingAndBudget(t *testing.T) {
	good := Text{Engine: "local", Lines: []pipeline.Line{testLine("Hello Sotto", 0, 0, 200, 24, .95)}}
	weak := Text{Engine: "local", Lines: []pipeline.Line{testLine("Hello Sotto", 0, 0, 200, 24, .5)}}
	system := Text{Engine: "winrt", Lines: []pipeline.Line{testLine("Hello Sotto", 0, 0, 200, 24, -1)}}
	failure := errors.New("language pack not available")
	tests := []struct {
		name, lang              string
		first                   Text
		localErr, systemErr     error
		wantEngine              string
		localCalls, systemCalls int
		wantErr                 error
	}{
		{"good local", "", good, nil, nil, "local", 1, 0, nil},
		{"explicit english", "en-US", good, nil, nil, "local", 1, 0, nil},
		{"explicit chinese", "ZH-hans", good, nil, nil, "local", 1, 0, nil},
		{"explicit japanese", "ja", good, nil, nil, "local", 1, 0, nil},
		{"explicit french", "fr-FR", good, nil, nil, "local", 1, 0, nil},
		{"explicit korean", "ko", good, nil, nil, "local", 1, 0, nil},
		{"japanese low confidence", "ja", weak, nil, nil, "winrt", 1, 1, nil},
		{"japanese missing models", "ja", Text{}, failure, nil, "winrt", 1, 1, nil},
		{"japanese empty local", "ja", Text{}, nil, nil, "winrt", 1, 1, nil},
		{"japanese missing system", "ja", weak, nil, failure, "local", 1, 1, nil},
		{"japanese local canceled", "ja", Text{}, context.Canceled, nil, "", 1, 0, context.Canceled},
		{"low confidence", "", weak, nil, nil, "winrt", 1, 1, nil},
		{"missing models", "", Text{}, failure, nil, "winrt", 1, 1, nil},
		{"empty local", "", Text{}, nil, nil, "winrt", 1, 1, nil},
		{"empty local missing system", "", Text{}, nil, failure, "", 1, 1, failure},
		{"cleaned empty local missing system", "", Text{Lines: []pipeline.Line{testLine("\ufffd", 0, 0, 100, 20, .95)}}, nil, failure, "", 1, 1, failure},
		{"missing system", "", weak, nil, failure, "local", 1, 1, nil},
		{"both failed", "", Text{}, failure, failure, "", 1, 1, failure},
		{"local canceled", "", Text{}, context.Canceled, nil, "", 1, 0, context.Canceled},
		{"system canceled", "", weak, nil, context.Canceled, "", 1, 1, context.Canceled},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lc, sc := 0, 0
			local := func(_ context.Context, _ image.Image, lang string) (Text, error) {
				if lang != tt.lang || sc != 0 {
					t.Fatalf("local must run first with language %q, got %q after %d system calls", tt.lang, lang, sc)
				}
				lc++
				return tt.first, tt.localErr
			}
			sys := func(_ context.Context, _ image.Image, lang string) (Text, error) {
				if lang != tt.lang || lc != 1 {
					t.Fatalf("fallback must retain language %q after local attempt, got %q after %d local calls", tt.lang, lang, lc)
				}
				sc++
				return system, tt.systemErr
			}
			got, err := recognizeEngines(context.Background(), nil, tt.lang, local, sys)
			if !errors.Is(err, tt.wantErr) || got.Engine != tt.wantEngine || lc != tt.localCalls || sc != tt.systemCalls {
				t.Fatalf("result %+v error %v calls %d/%d", got, err, lc, sc)
			}
			if got.RetryCount > 1 {
				t.Fatal("retry budget exceeded")
			}
			if errors.Is(err, failure) && !strings.Contains(UserMessage(err), "语言包") {
				t.Fatalf("lost language-pack message: %s", UserMessage(err))
			}
		})
	}
}

func TestGeometryRisksDoNotScheduleUnusableFallback(t *testing.T) {
	small := testLine("Hello Sotto", 0, 0, 200, 12, .99)
	steep := testLine("Hello Sotto", 0, 0, 200, 24, .99)
	steep.Quad[1].Y += 20
	steep.Quad[2].Y += 20
	for _, tc := range []struct {
		name  string
		line  pipeline.Line
		stats pipeline.Diagnostics
	}{
		{"small-text", small, pipeline.Diagnostics{}},
		{"steep-line", steep, pipeline.Diagnostics{}},
		{"degenerate-regions", testLine("Hello Sotto", 0, 0, 200, 24, .99), pipeline.Diagnostics{DegenerateBoxes: 1}},
		{"discarded-low-score-regions", testLine("Hello Sotto", 0, 0, 200, 24, .99), pipeline.Diagnostics{LowScoreRegions: 2}},
	} {
		for _, weak := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/weak=%t", tc.name, weak), func(t *testing.T) {
				line := tc.line
				if weak {
					line.Score = .5
				}
				local := func(context.Context, image.Image, string) (Text, error) {
					return Text{Engine: "local", Lines: []pipeline.Line{line}, Diagnostics: tc.stats}, nil
				}
				calls := 0
				system := func(context.Context, image.Image, string) (Text, error) {
					calls++
					return Text{}, errors.New("system unavailable")
				}
				got, err := recognizeEngines(context.Background(), nil, "", local, system)
				wantCalls := 0
				if weak {
					wantCalls = 1
				}
				if err != nil || calls != wantCalls || got.RetryCount != wantCalls || got.Engine != "local" || got.Text != line.Text {
					t.Fatalf("result %+v err=%v system calls=%d, want %d", got, err, calls, wantCalls)
				}
				found := false
				for _, risk := range got.Risks {
					found = found || risk.Reason == tc.name
				}
				if !found {
					t.Fatalf("diagnostic risk %s lost: %+v", tc.name, got.Risks)
				}
			})
		}
	}
}

func TestSelectionPreservesTrustedTextAndCoverage(t *testing.T) {
	first := Text{Lines: []pipeline.Line{testLine("Order ID 12345", 0, 0, 180, 20, .96), testLine("uncertain text", 0, 30, 180, 20, .4)}}
	first, _ = finish(first)
	tests := []struct {
		name  string
		lines []pipeline.Line
		want  bool
	}{
		{"aligned correction", []pipeline.Line{testLine("Order ID 12345", 0, 0, 180, 20, -1), testLine("corrected text", 0, 30, 180, 20, -1)}, true},
		{"more text wrong digit", []pipeline.Line{testLine("Order ID 12346 with more characters", 0, 0, 180, 20, -1), testLine("corrected text", 0, 30, 180, 20, -1)}, false},
		{"missing region", []pipeline.Line{testLine("Order ID 12345", 0, 0, 180, 20, -1)}, false},
		{"different position", []pipeline.Line{testLine("Order ID 12345 corrected text", 300, 300, 200, 50, -1)}, false},
		{"suspicious characters", []pipeline.Line{testLine("Order ID 12345", 0, 0, 180, 20, -1), testLine("bad\ufffd text", 0, 30, 180, 20, -1)}, false},
		{"empty", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			second, err := finish(Text{Lines: tt.lines})
			if err != nil {
				t.Fatal(err)
			}
			if got := preferSystem(first, second); got != tt.want {
				t.Fatalf("preferSystem=%v want %v", got, tt.want)
			}
		})
	}
	small, _ := finish(Text{Lines: []pipeline.Line{testLine("small trusted text", 0, 0, 100, 10, .99)}})
	other, _ := finish(Text{Lines: []pipeline.Line{testLine("small trusted text", 0, 0, 100, 10, -1)}})
	if preferSystem(small, other) {
		t.Fatal("unknown confidence is not evidence of improvement")
	}
}

func TestFinishRebuildsTextWithoutDestroyingRawLines(t *testing.T) {
	lines := []pipeline.Line{testLine("right column", 300, 0, 150, 20, -1), testLine("左 栏 。", 0, 0, 100, 20, -1)}
	before := append([]pipeline.Line(nil), lines...)
	got, err := finish(Text{Text: "stale engine string", Lines: lines})
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != "左栏。\n\nright column" || len(got.Blocks) != 2 {
		t.Fatalf("layout %+v", got)
	}
	if !reflect.DeepEqual(lines, before) || got.Lines[1].Text != "左 栏 。" {
		t.Fatal("raw OCR text changed")
	}
}

func TestSystemCoordinatesAndNoInventedConfidence(t *testing.T) {
	lines, err := systemLines([]systemLine{{Text: "test", Box: [4]float64{10, 20, 100, 40}}}, image.Rect(50, 80, 1050, 581), image.Rect(0, 0, 500, 250))
	if err != nil {
		t.Fatal(err)
	}
	want := pipeline.Quad{{X: 20, Y: 40.08}, {X: 220, Y: 40.08}, {X: 220, Y: 120.24}, {X: 20, Y: 120.24}}
	for i, p := range lines[0].Quad {
		if math.Abs(p.X-want[i].X) > 1e-8 || math.Abs(p.Y-want[i].Y) > 1e-8 {
			t.Fatalf("mapping %v", lines[0].Quad)
		}
	}
	if lines[0].HasScore {
		t.Fatal("system confidence invented")
	}
	for _, box := range [][4]float64{{-1, 0, 2, 2}, {0, 0, 0, 2}, {math.NaN(), 0, 1, 1}, {501, 0, 1, 1}} {
		if _, err := systemLines([]systemLine{{Box: box}}, image.Rect(0, 0, 500, 250), image.Rect(0, 0, 500, 250)); err == nil {
			t.Fatalf("accepted box %v", box)
		}
	}
}

func TestCancellationAfterInferenceDropsResult(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	local := func(context.Context, image.Image, string) (Text, error) {
		cancel()
		return Text{Lines: []pipeline.Line{testLine("stale", 0, 0, 100, 20, .99)}}, nil
	}
	sys := func(context.Context, image.Image, string) (Text, error) {
		t.Fatal("fallback after cancel")
		return Text{}, nil
	}
	got, err := recognizeEngines(ctx, nil, "", local, sys)
	if !errors.Is(err, context.Canceled) || got.Text != "" {
		t.Fatalf("late result %+v %v", got, err)
	}
}

func TestTensorBoundsAndUserMessage(t *testing.T) {
	for _, shape := range [][]int64{nil, {0, 1}, {-1, 2}, {1, 1, 1, 1, 1}, {math.MaxInt64, 2}, {1, 1, 1, 32<<20 + 1}} {
		if _, err := tensorElements(shape); err == nil {
			t.Fatalf("accepted shape %v", shape)
		}
	}
	if n, err := tensorElements([]int64{1, 3, 48, 320}); err != nil || n != 46080 {
		t.Fatalf("elements %d %v", n, err)
	}
	if UserMessage(pipeline.ErrLimit) == UserMessage(errors.New("generic")) {
		t.Fatal("limit message missing")
	}
}
