//go:build windows

package ocr

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"image"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// A real pipe and child process exercise recovery without depending on OCR
// language packs or launching the desktop application on CI.
func TestWinRTProtocolWorker(t *testing.T) {
	mode := os.Getenv("SOTTO_TEST_OCR_WORKER")
	if mode == "" {
		return
	}
	if mode == "fail-start" {
		fmt.Println(`{"ready":false,"error":"language pack not available"}`)
		os.Exit(0)
	}
	if mode == "closed-input" {
		_ = os.Stdin.Close()
		fmt.Println(`{"ready":true,"maxDimension":4096}`)
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	fmt.Println(`{"ready":true,"maxDimension":4096}`)
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		fmt.Println(`{"ok":true,"lines":[{"text":"recovered","box":[0,0,1,1]}]}`)
	}
	os.Exit(0)
}

func workerCommand(t *testing.T, mode string) *exec.Cmd {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=^TestWinRTProtocolWorker$")
	cmd.Env = append(os.Environ(), "SOTTO_TEST_OCR_WORKER="+mode)
	return cmd
}

func TestWinRTStartupCanRecoverOnLaterRequest(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	starts := 0
	w := &winrt{command: func() *exec.Cmd {
		starts++
		mode := "fail-start"
		if starts > 4 {
			mode = "ready"
		}
		return workerCommand(t, mode)
	}}
	t.Cleanup(w.killLocked)
	for i := 1; i <= 4; i++ {
		if err := w.ensure(ctx); err == nil || !strings.Contains(err.Error(), "language pack") || starts != i {
			t.Fatalf("startup %d: err=%v starts=%d", i, err, starts)
		}
	}
	if err := w.ensure(ctx); err != nil || starts != 5 {
		t.Fatalf("recovery: err=%v starts=%d", err, starts)
	}
}

func TestWinRTWriteRetryIsBounded(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	starts := 0
	w := &winrt{command: func() *exec.Cmd {
		starts++
		return workerCommand(t, "closed-input")
	}}
	t.Cleanup(w.killLocked)
	_, err := w.ask(ctx, image.NewNRGBA(image.Rect(0, 0, 2, 2)), "en")
	if err == nil || !strings.Contains(err.Error(), "send image") || starts != 2 || w.cmd != nil {
		t.Fatalf("transfer retry budget: err=%v starts=%d worker=%v", err, starts, w.cmd)
	}
}

type brokenWorkerPipe struct{ io.WriteCloser }

func (brokenWorkerPipe) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestWinRTWriteRecoveryAndCancellation(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		t.Run(fmt.Sprint(canceled), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			starts := 0
			w := &winrt{command: func() *exec.Cmd {
				starts++
				return workerCommand(t, "ready")
			}}
			t.Cleanup(w.killLocked)
			if err := w.ensure(ctx); err != nil {
				t.Fatal(err)
			}
			w.stdin = brokenWorkerPipe{w.stdin}
			if canceled {
				cancel()
			}
			lines, err := w.ask(ctx, image.NewNRGBA(image.Rect(0, 0, 2, 2)), "en")
			if canceled {
				if !errors.Is(err, context.Canceled) || starts != 1 {
					t.Fatalf("canceled request restarted: %v starts=%d", err, starts)
				}
			} else if err != nil || starts != 2 || len(lines) != 1 || lines[0].Text != "recovered" {
				t.Fatalf("recovery: lines=%+v err=%v starts=%d", lines, err, starts)
			}
		})
	}
}

func TestWinRTMissingLanguagePackIsNotCached(t *testing.T) {
	// Evaluate the actual worker's Get-Engine function with a pack installer
	// stub: unavailable on the first request, available on the next one.
	start := strings.Index(script, "function Get-Engine(")
	end := strings.Index(script[start:], "\nfunction Send-Line(") + start
	probe := `
$ErrorActionPreference = 'Stop'
$script:engines = @{}
$script:calls = 0
function New-Engine([string]$langTag) {
  $script:calls++
  if ($script:calls -gt 1) { return 'installed' }
  return $null
}
` + script[start:end] + `
if ($null -ne (Get-Engine 'ja')) { throw 'unexpected installed pack' }
if ('installed' -ne (Get-Engine 'ja')) { throw 'missing pack was cached' }
if ('installed' -ne (Get-Engine 'ja')) { throw 'installed pack was lost' }
if ($script:calls -ne 2) { throw 'successful engine was not cached' }
`
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", probe)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("language pack recovery: %v\n%s", err, output)
	}
}
