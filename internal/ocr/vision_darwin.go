//go:build darwin

package ocr

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"sync"
	"time"

	"github.com/ebitengine/purego/objc"

	"sotto/internal/macnative"
	"sotto/internal/ocr/pipeline"
)

var loadVision = sync.OnceValue(func() error {
	if _, err := macnative.Framework("Foundation"); err != nil {
		return err
	}
	_, err := macnative.Framework("Vision")
	return err
})

func recognizeWith(ctx context.Context, img image.Image, lang string, _ *idleRequest) (Text, error) {
	start := time.Now()
	t, err := runEngine(ctx, img, lang, recognizeVision)
	t.Attempts = []EngineAttempt{{Engine: "vision", ElapsedUS: time.Since(start).Microseconds(), Timings: t.timings, Diagnostics: t.Diagnostics}}
	if err != nil {
		t.Attempts[0].Error = err.Error()
	}
	return t, err
}

func recognizeVision(ctx context.Context, img image.Image, lang string) (result Text, err error) {
	if err = loadVision(); err != nil {
		return result, err
	}
	start := time.Now()
	var encoded bytes.Buffer
	encoder := png.Encoder{CompressionLevel: png.BestSpeed}
	if err = encoder.Encode(&encoded, img); err != nil {
		return result, fmt.Errorf("encode Vision input: %w", err)
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	result.Engine = "vision"
	result.timings.SystemPreprocessUS = time.Since(start).Microseconds()
	macnative.Pool(func() {
		request := macnative.Class("VNRecognizeTextRequest").Send(macnative.Sel("alloc")).Send(macnative.Sel("init"))
		if request == 0 {
			err = fmt.Errorf("create Vision request")
			return
		}
		defer request.Send(macnative.Sel("release"))
		request.Send(macnative.Sel("setRecognitionLevel:"), int64(0))
		request.Send(macnative.Sel("setUsesLanguageCorrection:"), true)
		request.Send(macnative.Sel("setAutomaticallyDetectsLanguage:"), lang == "")
		var nativeErr objc.ID
		supported := request.Send(macnative.Sel("supportedRecognitionLanguagesAndReturnError:"), &nativeErr)
		if supported == 0 {
			err = macnative.NativeError("query Vision languages", nativeErr)
			return
		}
		var names []string
		for _, name := range macnative.Items(supported) {
			names = append(names, macnative.GoString(name))
		}
		languages, langErr := visionLanguages(lang, names)
		if langErr != nil {
			err = langErr
			return
		}
		list := macnative.Array()
		for _, name := range languages {
			list.Send(macnative.Sel("addObject:"), macnative.String(name))
		}
		request.Send(macnative.Sel("setRecognitionLanguages:"), list)
		handler := macnative.Class("VNImageRequestHandler").Send(macnative.Sel("alloc")).Send(macnative.Sel("initWithData:options:"), macnative.Data(encoded.Bytes()), macnative.Class("NSDictionary").Send(macnative.Sel("dictionary")))
		if handler == 0 {
			err = fmt.Errorf("create Vision image handler")
			return
		}
		defer handler.Send(macnative.Sel("release"))
		canceled := make(chan struct{})
		stop := context.AfterFunc(ctx, func() { macnative.Pool(func() { request.Send(macnative.Sel("cancel")) }); close(canceled) })
		defer func() {
			if !stop() {
				<-canceled
			}
		}()
		started := time.Now()
		if !objc.Send[bool](handler, macnative.Sel("performRequests:error:"), macnative.Array(request), &nativeErr) {
			err = macnative.NativeError("recognize image", nativeErr)
			return
		}
		result.timings.SystemRequestUS = time.Since(started).Microseconds()
		if err = ctx.Err(); err != nil {
			return
		}
		observations := request.Send(macnative.Sel("results"))
		if objc.Send[uint64](observations, macnative.Sel("count")) > uint64(pipeline.DefaultConfig().MaxCandidates) {
			err = fmt.Errorf("%w: too many Vision observations", pipeline.ErrLimit)
			return
		}
		for _, observation := range macnative.Items(observations) {
			candidates := macnative.Items(observation.Send(macnative.Sel("topCandidates:"), uint64(1)))
			if len(candidates) == 0 {
				continue
			}
			candidate := candidates[0]
			text := macnative.GoString(candidate.Send(macnative.Sel("string")))
			if text == "" {
				continue
			}
			r := objc.Send[macnative.Rect](observation, macnative.Sel("boundingBox"))
			result.Lines = append(result.Lines, pipeline.Line{Text: text, Score: float64(objc.Send[float32](candidate, macnative.Sel("confidence"))), HasScore: true,
				Quad: visionQuad(r.Origin.X, r.Origin.Y, r.Size.Width, r.Size.Height, img.Bounds().Dx(), img.Bounds().Dy())})
		}
	})
	return result, err
}
