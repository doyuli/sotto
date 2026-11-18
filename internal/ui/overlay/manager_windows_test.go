package overlay

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/egoist/mygo"

	"sotto/internal/platform"
)

const overlayDesktopHelper = "SOTTO_OVERLAY_DESKTOP_HELPER"

func TestMain(m *testing.M) {
	if os.Getenv("SOTTO_DESKTOP") != "1" || os.Getenv(overlayDesktopHelper) != "1" {
		os.Exit(m.Run())
	}
	// MyGo must own the process's initial thread to draw native windows.
	var manager Manager
	resultErr := errors.New("the placed translation never drew a frame")
	mygo.App.WhenReady(func() {
		work := mygo.Screen.PrimaryDisplay().WorkArea
		source := mygo.Rectangle{X: work.X + work.Width/3, Y: work.Y + work.Height - 100, Width: 200, Height: 30}
		p := manager.show("短译文", source, platform.WorkspaceGeneration(), nil)
		onBounds := p.view.boundsChanged
		p.view.boundsChanged = func() {
			onBounds()
			if !p.view.measuring {
				p.window.Update(func() {
					bounds := p.view.bounds
					switch {
					case !p.window.IsVisible() || p.window.IsFocused():
						resultErr = errors.New("expected a visible, unfocused translation")
					case !p.view.alignBottom || bounds.Width <= 0 || bounds.Y <= 0 || p.input != bounds:
						resultErr = fmt.Errorf("expected bottom-aligned input at the drawn card: bounds=%+v input=%+v", bounds, p.input)
					default:
						resultErr = nil
					}
					mygo.App.Quit()
				})
			}
		}
	})
	if err := mygo.App.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if resultErr != nil {
		fmt.Fprintln(os.Stderr, resultErr)
		os.Exit(1)
	}
	os.Exit(0)
}

func TestOverlayAppearsNearWorkAreaBottom(t *testing.T) {
	// Repositioning a short card used to wait for a second frame while hidden.
	if os.Getenv("SOTTO_DESKTOP") != "1" {
		t.Skip("set SOTTO_DESKTOP=1 in an interactive Windows desktop session")
	}
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path)
	cmd.Env = append(cmd.Environ(), overlayDesktopHelper+"=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("native translation frame did not present: %v (deadline: %v)\n%s", err, ctx.Err(), out)
	}
}
