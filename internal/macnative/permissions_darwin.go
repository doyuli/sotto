//go:build darwin

package macnative

import (
	"errors"
	"sync"
	"sync/atomic"

	"github.com/ebitengine/purego/objc"
)

var (
	ErrAccessibility                = errors.New("请在系统设置的隐私与安全性中允许 Sotto 使用辅助功能，然后重试。")
	ErrScreenRecording              = errors.New("请在系统设置的隐私与安全性中允许 Sotto 录制屏幕，然后重试。")
	axTrusted                       func() bool
	axPrompt                        func(objc.ID) bool
	screenAllowed                   func() bool
	screenRequest                   func() bool
	accessibilityAsked, screenAsked atomic.Bool
)

var loadPermissions = sync.OnceValue(func() error {
	if _, err := Framework("AppKit"); err != nil {
		return err
	}
	if err := Bind("ApplicationServices", Symbol{&axTrusted, "AXIsProcessTrusted"}, Symbol{&axPrompt, "AXIsProcessTrustedWithOptions"}); err != nil {
		return err
	}
	return Bind("CoreGraphics", Symbol{&screenAllowed, "CGPreflightScreenCaptureAccess"}, Symbol{&screenRequest, "CGRequestScreenCaptureAccess"})
})

// Ask only after an explicit entry action, outside UI callbacks. Granting
// permission does not replay a selection that may belong to an old app.
func RequireAccessibility(prompt bool) error {
	if err := loadPermissions(); err != nil {
		return err
	}
	if axTrusted() {
		return nil
	}
	if prompt && accessibilityAsked.CompareAndSwap(false, true) {
		Pool(func() {
			opts := Class("NSDictionary").Send(Sel("dictionaryWithObject:forKey:"), Class("NSNumber").Send(Sel("numberWithBool:"), true), String("AXTrustedCheckOptionPrompt"))
			axPrompt(opts)
		})
	}
	return ErrAccessibility
}

func RequireScreenRecording(prompt bool) error {
	if err := loadPermissions(); err != nil {
		return err
	}
	if screenAllowed() {
		return nil
	}
	if prompt && screenAsked.CompareAndSwap(false, true) && screenRequest() {
		return nil
	}
	return ErrScreenRecording
}
