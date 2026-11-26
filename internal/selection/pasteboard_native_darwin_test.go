//go:build darwin

package selection

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"

	"github.com/ebitengine/purego/objc"

	"sotto/internal/macnative"
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
		// Snapshotting used to monopolize one AppKit turn and ignore cancellation.
		turns := 0
		dispatch := func(fn func()) { turns++; macnative.Pool(fn) }
		saved, err := snapshotMacPasteboard(context.Background(), func() objc.ID { return board }, dispatch)
		if err != nil || saved == nil || len(saved) != len(expected) {
			t.Fatalf("snapshot: %v, %v", saved, err)
		}
		for i, payload := range expected {
			if len(saved[i]) != 1 || !bytes.Equal(saved[i][0].data, payload) {
				t.Fatalf("snapshot item %d lost data", i)
			}
		}
		if turns < len(expected)+2 {
			t.Fatal("did not yield between representations")
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		turns = 0
		_, err = snapshotMacPasteboard(ctx, func() objc.ID { return board }, func(fn func()) {
			turns++
			if turns == 2 {
				cancel()
			}
			macnative.Pool(fn)
		})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation: %v", err)
		}

		// Replace the original items before verifying the production write-back.
		board.Send(macnative.Sel("clearContents"))
		if !objc.Send[bool](board, macnative.Sel("setString:forType:"), macnative.String("temporary selection"), macnative.String("public.utf8-plain-text")) {
			t.Fatal("write temporary selection")
		}
		seq := objc.Send[int64](board, macnative.Sel("changeCount"))
		if err := restorePasteboard(board, saved, seq, func() bool { return true }); err != nil {
			t.Fatalf("restore: %v", err)
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
		turns = 0
		_, err = snapshotMacPasteboard(context.Background(), func() objc.ID { return board }, func(fn func()) {
			turns++
			if turns == 2 {
				board.Send(macnative.Sel("clearContents"))
			}
			macnative.Pool(fn)
		})
		if !errors.Is(err, ErrSourceChanged) {
			t.Fatalf("changed clipboard: %v", err)
		}
		_, err = snapshotMacPasteboard(context.Background(), func() objc.ID { t.Fatal("dispatch dropped"); return 0 }, func(func()) {})
		if !errors.Is(err, ErrSourceChanged) {
			t.Fatalf("shutdown: %v", err)
		}
	})
}
