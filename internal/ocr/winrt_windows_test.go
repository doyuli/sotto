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
		if mode == "no-response" {
			continue
		}
		fmt.Println(`{"ok":true,"lines":[{"text":"recovered","box":[0,0,1,1]}]}`)
	}
	os.Exit(0)
}

func TestWinRTIdleReleaseAndRestart(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	starts := 0
	w := &winrt{command: func() *exec.Cmd { starts++; return workerCommand(t, "ready") }}
	t.Cleanup(w.killLocked)
	f := newIdleFixture()
	f.coordinator.release = func(int) (bool, error) { return w.releaseIdle(), nil }
	for range 20 {
		w.mu.Lock()
		_, err := w.ask(ctx, image.NewNRGBA(image.Rect(0, 0, 2, 2)), "en")
		w.mu.Unlock()
		if err != nil {
			t.Fatal(err)
		}
		cmd := w.cmd
		f.warm(systemOCR)
		// A worker held by another operation must survive TryLock failure.
		w.mu.Lock()
		f.clock.advance(defaultSystemIdleTimeout)
		if w.cmd != cmd || f.clock.active() != 1 {
			t.Fatal("busy worker was killed or lost its retry")
		}
		w.mu.Unlock()
		f.clock.advance(idleRetryDelay)
		if w.cmd != nil || w.stdin != nil || w.stdout != nil || cmd.ProcessState == nil || !cmd.ProcessState.Exited() || w.command == nil || f.clock.active() != 0 {
			t.Fatal("idle release did not reap the worker and preserve its factory")
		}
	}
	if starts != 20 {
		t.Fatalf("restart count %d, want 20", starts)
	}
}

type notifyingWorkerPipe struct {
	io.WriteCloser
	written chan struct{}
}

func (p notifyingWorkerPipe) Write(data []byte) (int, error) {
	n, err := p.WriteCloser.Write(data)
	close(p.written)
	return n, err
}

func TestWinRTRequestCannotBeReapedWhileWaitingForResponse(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	w := &winrt{command: func() *exec.Cmd { return workerCommand(t, "no-response") }}
	t.Cleanup(w.killLocked)
	if err := w.ensure(ctx); err != nil {
		t.Fatal(err)
	}
	cmd := w.cmd
	written := make(chan struct{})
	w.stdin = notifyingWorkerPipe{w.stdin, written}
	f := newIdleFixture()
	f.coordinator.release = func(int) (bool, error) { return w.releaseIdle(), nil }
	f.warm(systemOCR)
	done := make(chan error, 1)
	go func() {
		_, err := recognizeRequest(ctx, image.NewNRGBA(image.Rect(0, 0, 2, 2)), "ko", f.coordinator,
			func(ctx context.Context, img image.Image, lang string, request *idleRequest) (Text, error) {
				request.used[systemOCR] = true
				w.mu.Lock()
				defer w.mu.Unlock()
				_, err := w.ask(ctx, img, lang)
				return Text{}, err
			}, func() engineResources {
				w.mu.Lock()
				defer w.mu.Unlock()
				return engineResources{false, w.cmd != nil}
			})
		done <- err
	}()
	select {
	case <-written:
	case <-ctx.Done():
		t.Fatal("worker never received the request")
	}
	f.clock.advance(defaultSystemIdleTimeout)
	if cmd.ProcessState != nil {
		t.Fatal("in-flight request was killed at the idle deadline")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) || w.cmd != nil || f.clock.active() != 0 || len(f.coordinator.admission) != 0 {
			t.Fatalf("cancellation did not finish the request: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancellation left the worker or request blocked")
	}
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
