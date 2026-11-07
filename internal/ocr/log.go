package ocr

import (
	"fmt"
	"strings"
	"time"
)

// Routine logs omit recognized text and unused fields. Full per-stage data
// remains in Text; failed requests also log all attempts.
func recognitionSummary(t Text, err error) string {
	var out strings.Builder
	fmt.Fprintf(&out, "ocr: engine=%s elapsed=%s retries=%d", t.Engine, t.Elapsed, t.RetryCount)
	if t.Selection != "" {
		fmt.Fprintf(&out, " selection=%s", t.Selection)
	}
	writeTime := func(name string, us int64) {
		if us > 0 {
			fmt.Fprintf(&out, " %s=%s", name, (time.Duration(us) * time.Microsecond).Round(time.Millisecond))
		}
	}
	writeTime("queue", t.QueueWaitUS)
	for _, a := range t.Attempts {
		fmt.Fprintf(&out, " %s={", a.Engine)
		writeTime("total", a.ElapsedUS)
		writeTime("init", a.Timings.InitializationUS)
		if a.Diagnostics.DetectorWidth > 0 {
			fmt.Fprintf(&out, " size=%dx%d", a.Diagnostics.DetectorWidth, a.Diagnostics.DetectorHeight)
		}
		writeTime("pre", a.Timings.Pipeline.DetectionPreprocessUS)
		writeTime("det", a.Timings.Pipeline.DetectionInferenceUS)
		writeTime("rec", a.Timings.Pipeline.RecognitionInferenceUS)
		if len(a.Risks) > 0 {
			fmt.Fprintf(&out, " risks=%s", strings.Join(a.Risks, ","))
		}
		if a.Error != "" {
			fmt.Fprintf(&out, " error=%q", a.Error)
		}
		out.WriteString(" }")
	}
	if err != nil {
		fmt.Fprintf(&out, " error=%q", err)
	}
	return out.String()
}
