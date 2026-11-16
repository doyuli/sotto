//go:build darwin

package capture

import (
	"context"
	"fmt"
	"image"
	"os"
	"sync"

	"github.com/ebitengine/purego/objc"

	"sotto/internal/macnative"
	"sotto/internal/ocr/pipeline"
)

var (
	imageRetain             func(uintptr) uintptr
	imageRelease            func(uintptr)
	imageWidth, imageHeight func(uintptr) uint64
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
	return nil
})

func captureDisplays(ctx context.Context, ids []int64, emit func(snapshotResult)) (err error) {
	if err := loadCapture(); err != nil {
		return err
	}
	if err := macnative.RequireScreenRecording(false); err != nil {
		return err
	}
	macnative.Pool(func() {
		if err = ctx.Err(); err != nil {
			return
		}
		content, listErr := shareableContent()
		if listErr != nil {
			err = listErr
			return
		}
		defer content.Send(macnative.Sel("release"))
		displays := make(map[int64]objc.ID)
		for _, display := range macnative.Items(content.Send(macnative.Sel("displays"))) {
			id := int64(objc.Send[uint32](display, macnative.Sel("displayID")))
			displays[id] = display
		}
		excluded := macnative.Array()
		for _, window := range macnative.Items(content.Send(macnative.Sel("windows"))) {
			owner := window.Send(macnative.Sel("owningApplication"))
			if objc.Send[int32](owner, macnative.Sel("processID")) == int32(os.Getpid()) {
				excluded.Send(macnative.Sel("addObject:"), window)
			}
		}
		captureEachDisplay(ctx, ids, func(id int64, maxPixels int) (image.Image, error) {
			display := displays[id]
			if display == 0 {
				return nil, fmt.Errorf("capture display no longer available")
			}
			return captureDisplay(ctx, display, excluded, maxPixels)
		}, emit)
	})
	return err
}

// The capture worker owns the block until ScreenCaptureKit completes, even if
// the UI's deadline has expired. The returned content is retained for the batch.
func shareableContent() (objc.ID, error) {
	type result struct {
		content objc.ID
		err     error
	}
	results := make(chan result, 1)
	block := objc.NewBlock(func(_ objc.Block, content, nativeErr objc.ID) {
		macnative.Pool(func() {
			if nativeErr != 0 || content == 0 {
				results <- result{err: macnative.NativeError("list shareable content", nativeErr)}
				return
			}
			content.Send(macnative.Sel("retain"))
			results <- result{content: content}
		})
	})
	defer block.Release()
	macnative.Class("SCShareableContent").Send(macnative.Sel("getShareableContentExcludingDesktopWindows:onScreenWindowsOnly:completionHandler:"), false, true, block)
	got := <-results
	return got.content, got.err
}

func captureDisplay(ctx context.Context, display, excluded objc.ID, maxPixels int) (img image.Image, err error) {
	macnative.Pool(func() {
		dw, dh := objc.Send[int64](display, macnative.Sel("width")), objc.Send[int64](display, macnative.Sel("height"))
		r := macnative.NewRect(0, 0, float64(dw), float64(dh))
		filter := macnative.Class("SCContentFilter").Send(macnative.Sel("alloc")).Send(macnative.Sel("initWithDisplay:excludingWindows:"), display, excluded)
		defer filter.Send(macnative.Sel("release"))
		info := macnative.Class("SCShareableContent").Send(macnative.Sel("infoForFilter:"), filter)
		scale := float64(objc.Send[float32](info, macnative.Sel("pointPixelScale")))
		limits := pipeline.DefaultConfig()
		width, height, dimErr := macnative.CaptureDimensions(r, scale, limits.MaxImageSide, maxPixels)
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
		// Convert while the retained CGImage is alive; only owned Go pixels
		// escape into OCR. CoreGraphics performs the source-to-sRGB conversion.
		img, err = macnative.CopyNativeImage(shot.image, maxPixels)
	})
	return img, err
}
