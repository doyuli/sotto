package translate

import (
	"context"
	"errors"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"sotto/internal/config"
)

func TestOversizedInputNeverStartsTranslation(t *testing.T) {
	// Per-chunk limits used to allow an arbitrarily large multi-service job.
	cfg := config.Default()
	cfg.Services.Enabled = []string{"one", "two"}
	one, two := &recordingTranslator{name: "one"}, &recordingTranslator{name: "two"}
	got := 0
	RunWith(context.Background(), []Translator{one, two}, cfg, nil, Request{Text: strings.Repeat("字", MaxInputRunes+1)}, func(r Result) {
		got++
		if r.Streaming || r.Text != "" || r.Err != UserMessage(ErrInputLimit) {
			t.Error(r)
		}
	})
	if got != 2 || len(one.texts)+len(two.texts) != 0 {
		t.Fatal("oversized job started requests")
	}
	if err := ValidateInput(strings.Repeat("😀", MaxInputRunes)); err != nil {
		t.Fatal("budget counts UTF-8 bytes instead of characters")
	}
}

type limitStream struct{ calls int }

func (*limitStream) Name() string                             { return "limit" }
func (*limitStream) Available(KeyStore, config.Settings) bool { return true }
func (*limitStream) Translate(context.Context, Request, KeyStore, config.Settings) (Result, error) {
	panic("stream expected")
}
func (s *limitStream) Stream(ctx context.Context, _ Request, _ KeyStore, _ config.Settings, emit func(Result)) (Result, error) {
	s.calls++
	if s.calls == 1 {
		return Result{Text: strings.Repeat("a", maxResultBytes-5)}, nil
	}
	emit(Result{Text: "ok"})
	emit(Result{Text: "oversized"})
	if ctx.Err() == nil {
		return Result{}, errors.New("over-budget stream was not canceled")
	}
	return Result{}, ctx.Err()
}

func TestCumulativeResultLimitCancelsAndKeepsSafeText(t *testing.T) {
	// Each chunk can fit the provider limit while their combined output does not.
	engine := &limitStream{}
	cfg := config.Default()
	cfg.Services.Enabled = []string{engine.Name()}
	var res Result
	RunWith(context.Background(), []Translator{engine}, cfg, nil, Request{Text: strings.Repeat("x", maxChunkRunes*2+1)}, func(r Result) {
		if len(r.Text) > maxResultBytes {
			t.Error("published over-budget output")
		}
		res = r
	})
	if res.Err != UserMessage(ErrResultLimit) || res.Streaming || engine.calls != 2 || !strings.HasSuffix(res.Text, "\nok") || len(res.Text) > maxResultBytes {
		t.Fatalf("calls=%d bytes=%d err=%s", engine.calls, len(res.Text), res.Err)
	}
}

func TestProgressCoalescesPrefixesAndStops(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var got []string
		progress, stop := streamProgressWithPrefix(context.Background(), func(r Result) { got = append(got, r.Text) })
		progress("first\n", Result{Text: "a"})
		progress("first\n", Result{Text: "ab"})
		progress("first\nsecond\n", Result{Text: "c"})
		time.Sleep(40 * time.Millisecond)
		synctest.Wait()
		if len(got) != 2 || got[1] != "first\nsecond\nc" {
			t.Fatal(got)
		}
		progress("obsolete\n", Result{Text: "d"})
		stop()
		time.Sleep(time.Second)
		synctest.Wait()
		if len(got) != 2 {
			t.Fatal("partial emitted after terminal result")
		}
	})
}

func BenchmarkSplitLongText(b *testing.B) {
	text := strings.Repeat("A paragraph of documentation.\n", 500)
	b.ReportAllocs()
	for b.Loop() {
		parts := splitLongText(text, maxChunkRunes)
		if len(parts) == 0 {
			b.Fatal("empty")
		}
	}
}
