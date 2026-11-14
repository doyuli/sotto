//go:build darwin

package selection

import (
	"bytes"
	"github.com/ebitengine/purego/objc"
	"os"
	"sotto/internal/macnative"
	"testing"
)

func TestMacIsolatedPasteboardItems(t *testing.T) {
	if os.Getenv("SOTTO_MACOS_NATIVE") != "1" {
		t.Skip("set SOTTO_MACOS_NATIVE=1 for isolated pasteboard round trip")
	}
	if _, err := macnative.Framework("AppKit"); err != nil {
		t.Fatal(err)
	}
	macnative.Pool(func() {
		board := macnative.Class("NSPasteboard").Send(macnative.Sel("pasteboardWithUniqueName"))
		defer board.Send(macnative.Sel("releaseGlobally"))
		objects := macnative.Array()
		expected := [][]byte{[]byte("selected text"), []byte{0, 1, 255}}
		for _, payload := range expected {
			item := macnative.Class("NSPasteboardItem").Send(macnative.Sel("alloc")).Send(macnative.Sel("init"))
			item.Send(macnative.Sel("autorelease"))
			if !objc.Send[bool](item, macnative.Sel("setData:forType:"), macnative.Data(payload), macnative.String("app.sotto.test")) {
				t.Fatal("prepare item")
			}
			objects.Send(macnative.Sel("addObject:"), item)
		}
		board.Send(macnative.Sel("clearContents"))
		if !objc.Send[bool](board, macnative.Sel("writeObjects:"), objects) {
			t.Fatal("write items")
		}
		items := macnative.Items(board.Send(macnative.Sel("pasteboardItems")))
		if len(items) != len(expected) {
			t.Fatal("lost item boundary")
		}
		for i, item := range items {
			got, err := macnative.Bytes(item.Send(macnative.Sel("dataForType:"), macnative.String("app.sotto.test")), 1024)
			if err != nil || !bytes.Equal(got, expected[i]) {
				t.Fatalf("item %d: %v", i, err)
			}
		}
	})
}
