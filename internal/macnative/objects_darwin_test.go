//go:build darwin

package macnative

import (
	"bytes"
	"testing"

	"github.com/ebitengine/purego/objc"
)

func TestNativeObjectOwnershipAndRectABI(t *testing.T) {
	if _, err := Framework("Foundation"); err != nil {
		t.Fatal(err)
	}
	var text string
	var data []byte
	var rect Rect
	Pool(func() {
		text = GoString(String("Sotto 中文 🫧\x00tail"))
		var err error
		data, err = Bytes(Data([]byte{0, 1, 2, 255}), 4)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Bytes(Data([]byte{1, 2}), 1); err == nil {
			t.Fatal("accepted data beyond budget")
		}
		value := Class("NSValue").Send(Sel("valueWithRect:"), NewRect(-100, 230, 17.5, 48.25))
		rect = objc.Send[Rect](value, Sel("rectValue"))
	})
	if text != "Sotto 中文 🫧\x00tail" || !bytes.Equal(data, []byte{0, 1, 2, 255}) || rect != NewRect(-100, 230, 17.5, 48.25) {
		t.Fatalf("native copies/ABI: %q %v %+v", text, data, rect)
	}
}

func TestPermissionChecksDoNotPrompt(t *testing.T) {
	for _, err := range []error{RequireAccessibility(false), RequireScreenRecording(false)} {
		if err != nil && err != ErrAccessibility && err != ErrScreenRecording {
			t.Fatal(err)
		}
	}
}
