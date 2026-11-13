//go:build darwin

package capture

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"os"
	"sync"
	"time"

	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo"

	"sotto/internal/macnative"
	"sotto/internal/ocr/pipeline"
)

var (
	imageRetain             func(uintptr) uintptr
	imageRelease            func(uintptr)
	imageWidth, imageHeight func(uintptr) uint64
	imageDestination        func(objc.ID, objc.ID, uint64, uintptr) uintptr
	addImage                func(uintptr, uintptr, uintptr)
	finalizeImage           func(uintptr) bool
)

var loadCapture = sync.OnceValue(func() error {
	if _, err := macnative.Framework("AppKit"); err != nil {
		return err
	}
	if _, err := macnative.Framework("ScreenCaptureKit"); err != nil {
		return err
	}
	if err := macnative.Bind("CoreGraphics", macnative.Symbol{Target: &imageWidth, Name: "CGImageGetWidth"}, macnative.Symbol{Target: &imageHeight, Name: "CGImageGetHeight"}); err != nil {
		return err
	}
	if err := macnative.Bind("CoreFoundation", macnative.Symbol{Target: &imageRetain, Name: "CFRetain"}, macnative.Symbol{Target: &imageRelease, Name: "CFRelease"}); err != nil {
		return err
	}
	return macnative.Bind("ImageIO", macnative.Symbol{Target: &imageDestination, Name: "CGImageDestinationCreateWithData"}, macnative.Symbol{Target: &addImage, Name: "CGImageDestinationAddImage"}, macnative.Symbol{Target: &finalizeImage, Name: "CGImageDestinationFinalize"})
})

var captureAdmission = make(chan struct{}, 1)

func grabDisplayWindow(ctx context.Context, _ uintptr, displayID int64, r Rect) (image.Image, error) {
	if err := loadCapture(); err != nil {
		return nil, err
	}
	if err := macnative.RequireScreenRecording(false); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	type result struct {
		img image.Image
		err error
	}
	done := make(chan result, 1)
	go func() {
		select {
		case captureAdmission <- struct{}{}:
		case <-ctx.Done():
			done <- result{err: ctx.Err()}
			return
		}
		defer func() { <-captureAdmission }()
		// SCK has no cancellation API for a screenshot. This worker owns the
		// gate, blocks and retained objects until native completion, even if
		// the entry's caller has already returned on cancellation.
		img, err := captureDisplay(ctx, displayID, macnative.NewRect(float64(r.X), float64(r.Y), float64(r.W), float64(r.H)))
		done <- result{img, err}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case got := <-done:
		return got.img, got.err
	}
}

func captureDisplay(ctx context.Context, displayID int64, r macnative.Rect) (img image.Image, err error) {
	macnative.Pool(func() {
		type contentResult struct {
			content objc.ID
			err     error
		}
		contents := make(chan contentResult, 1)
		contentBlock := objc.NewBlock(func(_ objc.Block, content, nativeErr objc.ID) {
			macnative.Pool(func() {
				if nativeErr != 0 || content == 0 {
					contents <- contentResult{err: macnative.NativeError("list shareable content", nativeErr)}
					return
				}
				content.Send(macnative.Sel("retain"))
				contents <- contentResult{content: content}
			})
		})
		macnative.Class("SCShareableContent").Send(macnative.Sel("getShareableContentExcludingDesktopWindows:onScreenWindowsOnly:completionHandler:"), false, true, contentBlock)
		got := <-contents
		contentBlock.Release()
		if got.err != nil {
			err = got.err
			return
		}
		defer got.content.Send(macnative.Sel("release"))
		if err = ctx.Err(); err != nil {
			return
		}
		var display objc.ID
		for _, d := range macnative.Items(got.content.Send(macnative.Sel("displays"))) {
			if int64(objc.Send[uint32](d, macnative.Sel("displayID"))) == displayID {
				display = d
				break
			}
		}
		if display == 0 {
			err = fmt.Errorf("capture display no longer available")
			return
		}
		dw, dh := objc.Send[int64](display, macnative.Sel("width")), objc.Send[int64](display, macnative.Sel("height"))
		if r.Origin.X < 0 || r.Origin.Y < 0 || r.Size.Width <= 0 || r.Size.Height <= 0 || r.Origin.X+r.Size.Width > float64(dw) || r.Origin.Y+r.Size.Height > float64(dh) {
			err = fmt.Errorf("capture outside display")
			return
		}
		excluded := macnative.Array()
		for _, w := range macnative.Items(got.content.Send(macnative.Sel("windows"))) {
			owner := w.Send(macnative.Sel("owningApplication"))
			if objc.Send[int32](owner, macnative.Sel("processID")) == int32(os.Getpid()) {
				excluded.Send(macnative.Sel("addObject:"), w)
			}
		}
		filter := macnative.Class("SCContentFilter").Send(macnative.Sel("alloc")).Send(macnative.Sel("initWithDisplay:excludingWindows:"), display, excluded)
		defer filter.Send(macnative.Sel("release"))
		info := macnative.Class("SCShareableContent").Send(macnative.Sel("infoForFilter:"), filter)
		scale := float64(objc.Send[float32](info, macnative.Sel("pointPixelScale")))
		limits := pipeline.DefaultConfig()
		width, height, dimErr := macnative.CaptureDimensions(r, scale, limits.MaxImageSide, limits.MaxImagePixels)
		if dimErr != nil {
			err = fmt.Errorf("%w: %w", ErrLimit, dimErr)
			return
		}
		config := macnative.Class("SCStreamConfiguration").Send(macnative.Sel("alloc")).Send(macnative.Sel("init"))
		defer config.Send(macnative.Sel("release"))
		config.Send(macnative.Sel("setSourceRect:"), r)
		config.Send(macnative.Sel("setWidth:"), uint64(width))
		config.Send(macnative.Sel("setHeight:"), uint64(height))
		config.Send(macnative.Sel("setShowsCursor:"), false)
		type imageResult struct {
			image uintptr
			err   error
		}
		images := make(chan imageResult, 1)
		block := objc.NewBlock(func(_ objc.Block, image uintptr, nativeErr objc.ID) {
			macnative.Pool(func() {
				if nativeErr != 0 || image == 0 {
					images <- imageResult{err: macnative.NativeError("capture screenshot", nativeErr)}
					return
				}
				images <- imageResult{image: imageRetain(image)}
			})
		})
		macnative.Class("SCScreenshotManager").Send(macnative.Sel("captureImageWithFilter:configuration:completionHandler:"), filter, config, block)
		shot := <-images
		block.Release()
		if shot.err != nil {
			err = shot.err
			return
		}
		defer imageRelease(shot.image)
		actualW, actualH := imageWidth(shot.image), imageHeight(shot.image)
		if actualW != uint64(width) || actualH != uint64(height) {
			err = fmt.Errorf("unexpected screenshot dimensions: %dx%d, expected %dx%d", actualW, actualH, width, height)
			return
		}
		if err = ctx.Err(); err != nil {
			return
		}
		// ImageIO handles the native color space, alpha and row orientation.
		// Copy out before releasing the CGImage; no borrowed native pixels
		// escape into the asynchronous OCR or UI pipeline.
		data := macnative.Class("NSMutableData").Send(macnative.Sel("data"))
		dest := imageDestination(data, macnative.String("public.png"), 1, 0)
		if dest == 0 {
			err = fmt.Errorf("create screenshot encoder")
			return
		}
		defer imageRelease(dest)
		addImage(dest, shot.image, 0)
		if !finalizeImage(dest) {
			err = fmt.Errorf("encode screenshot")
			return
		}
		payload, dataErr := macnative.Bytes(data, limits.MaxImagePixels*5)
		if dataErr != nil {
			err = dataErr
			return
		}
		img, err = png.Decode(bytes.NewReader(payload))
	})
	return img, err
}

func grabWindow(handle uintptr, r Rect) (image.Image, error) {
	var displayID int64
	mygo.RunOnMain(func() {
		// This compatibility entry is only called with a live overlay handle.
		frame := objc.Send[macnative.Rect](objc.ID(handle), macnative.Sel("frame"))
		bounds := macnative.CocoaRect(frame, float64(mygo.Screen.PrimaryDisplay().Bounds.Height))
		displayID = mygo.Screen.DisplayMatching(mygo.Rectangle{X: int(bounds.Origin.X), Y: int(bounds.Origin.Y), Width: int(bounds.Size.Width), Height: int(bounds.Size.Height)}).ID
	})
	return grabDisplayWindow(context.Background(), handle, displayID, r)
}

func grab(r Rect) (image.Image, error) {
	d := mygo.Screen.DisplayMatching(mygo.Rectangle{X: r.X, Y: r.Y, Width: r.W, Height: r.H})
	return grabDisplayWindow(context.Background(), 0, d.ID, Rect{r.X - d.Bounds.X, r.Y - d.Bounds.Y, r.W, r.H})
}
