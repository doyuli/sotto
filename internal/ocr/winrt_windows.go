//go:build windows

package ocr

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	_ "embed"
)

//go:embed winrt.ps1
var script string

const (
	startTimeout = 10 * time.Second
	askTimeout   = 15 * time.Second
	maxRestart   = 3
)

// winrt runs the resident PowerShell process. mu serializes requests —
// one stdin, one stdout — and covers the restart bookkeeping.
type winrt struct {
	mu        sync.Mutex
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	stdout    *bufio.Reader
	langs     []string
	startErr  error
	restarts  int
	startedAt time.Time
}

var engine = &winrt{}

type readyMsg struct {
	Ready     bool     `json:"ready"`
	Languages []string `json:"languages"`
	Error     string   `json:"error"`
}

type askMsg struct {
	OK    bool   `json:"ok"`
	Text  string `json:"text"`
	Lines []Line `json:"lines"`
	Error string `json:"error"`
}

// availableLanguages starts the engine if needed and reports its
// languages. PP-OCR's Chinese model covers Chinese and English; the
// Windows packs are reported only when PP-OCR is unavailable.
func availableLanguages(ctx context.Context) []string {
	if langs := ppocrLanguages(); len(langs) > 0 {
		return langs
	}
	engine.mu.Lock()
	defer engine.mu.Unlock()
	if err := engine.ensure(ctx); err != nil {
		return nil
	}
	return engine.langs
}

// recognize prefers the local PP-OCR pipeline and falls back to the
// Windows recognizer when its models are missing or it saw nothing.
func recognize(ctx context.Context, img image.Image) (Text, error) {
	if t, err := recognizePPOCR(ctx, img); err == nil && strings.TrimSpace(t.Text) != "" {
		return t, nil
	}
	if err := ctx.Err(); err != nil {
		return Text{}, err
	}
	return recognizeWinRT(ctx, img, "")
}

// recognizeWith honors an explicit language tag: PP-OCR's Chinese model
// covers Chinese and English, and anything else belongs to the Windows
// recognizer, which has a pack per language.
func recognizeWith(ctx context.Context, img image.Image, lang string) (Text, error) {
	if lang == "" || strings.HasPrefix(lang, "zh") || strings.HasPrefix(lang, "en") {
		if t, err := recognizePPOCR(ctx, img); err == nil && strings.TrimSpace(t.Text) != "" {
			return t, nil
		}
	}
	if err := ctx.Err(); err != nil {
		return Text{}, err
	}
	return recognizeWinRT(ctx, img, lang)
}

func recognizeWinRT(ctx context.Context, img image.Image, lang string) (Text, error) {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	if err := engine.ensure(ctx); err != nil {
		return Text{}, err
	}
	if img.Bounds().Dx() <= 0 || img.Bounds().Dy() <= 0 {
		return Text{}, fmt.Errorf("ocr: empty image")
	}
	start := time.Now()

	// The recognizer accepts only so large an image; anything bigger is
	// scaled down first. Enlarging is decided after the first pass, from
	// the line heights it measured.
	img = fitDimension(img)
	first, err := engine.ask(ctx, img, lang)
	if err != nil {
		return Text{}, err
	}
	first.Lines = orderLines(first.Lines)

	b := img.Bounds()
	scale := retryScale(first.Lines, b.Dx(), b.Dy(), maxOCRDimension)
	if scale > 1 {
		tw, th := scaledSize(b.Dx(), b.Dy(), scale)
		scaled := resizeBilinear(img, tw, th)
		if second, err := engine.ask(ctx, scaled, lang); err == nil {
			second.Lines = orderLines(second.Lines)
			if countNonSpace(second.Text) > countNonSpace(first.Text) {
				mapLines(second.Lines, scale)
				second.Engine = "winrt"
				second.Elapsed = time.Since(start).Round(time.Millisecond).String()
				return second, nil
			}
		}
	}
	first.Engine = "winrt"
	first.Elapsed = time.Since(start).Round(time.Millisecond).String()
	return first, nil
}

// ask sends one image to the resident recognizer and returns what it
// saw. It runs with mu held.
func (w *winrt) ask(ctx context.Context, img image.Image, lang string) (Text, error) {
	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, img); err != nil {
		return Text{}, fmt.Errorf("ocr: %w", err)
	}
	req, err := json.Marshal(map[string]string{
		"png":  base64.StdEncoding.EncodeToString(pngBuf.Bytes()),
		"lang": lang,
	})
	if err != nil {
		return Text{}, err
	}
	if _, err := w.stdin.Write(append(req, '\n')); err != nil {
		// The process died; one restart, then give up.
		if err := w.restart(ctx); err != nil {
			return Text{}, err
		}
		if _, err := w.stdin.Write(append(req, '\n')); err != nil {
			return Text{}, fmt.Errorf("ocr: %w", err)
		}
	}

	line, err := w.readLine(ctx, askTimeout)
	if err != nil {
		// A late answer would still be in the pipe and the next request
		// would read it as its own: tear the process down instead, so the
		// next ensure starts a fresh one.
		w.killLocked()
		return Text{}, fmt.Errorf("ocr: %w", err)
	}
	var res askMsg
	if err := json.Unmarshal([]byte(line), &res); err != nil {
		return Text{}, fmt.Errorf("ocr: bad response: %w", err)
	}
	if !res.OK {
		return Text{}, fmt.Errorf("ocr: %s", res.Error)
	}
	return Text{Text: res.Text, Lines: res.Lines}, nil
}

// ensure starts the process once and waits for its ready line. It runs
// with mu held.
func (w *winrt) ensure(ctx context.Context) error {
	if w.cmd != nil && w.cmd.Process != nil {
		return nil // already up
	}
	if w.restarts >= maxRestart {
		if w.startErr != nil {
			return w.startErr
		}
		return fmt.Errorf("ocr: engine failed to start %d times", w.restarts)
	}
	w.restarts++
	cmd := exec.Command("powershell.exe",
		"-NoProfile", "-NonInteractive", "-STA", "-ExecutionPolicy", "Bypass",
		"-Command", script)
	// A GUI process starting a console one flashes a PowerShell window
	// over everything; HideWindow keeps the engine invisible.
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("ocr: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("ocr: %w", err)
	}
	cmd.Stderr = io.Discard // diagnostics would corrupt the line protocol
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("ocr: %w", err)
	}
	w.cmd = cmd
	w.stdin = stdin
	w.stdout = bufio.NewReaderSize(stdout, 1<<20)
	w.startedAt = time.Now()

	line, err := w.readLine(ctx, startTimeout)
	if err != nil {
		w.killLocked()
		return fmt.Errorf("ocr: engine did not start: %w", err)
	}
	var ready readyMsg
	if err := json.Unmarshal([]byte(line), &ready); err != nil {
		w.killLocked()
		return fmt.Errorf("ocr: bad ready line: %w", err)
	}
	if !ready.Ready {
		w.killLocked()
		w.startErr = fmt.Errorf("ocr: %s", ready.Error)
		if w.restarts >= maxRestart {
			return w.startErr
		}
		return w.ensure(ctx)
	}
	w.langs = ready.Languages
	// A successful start clears the failure budget: the limit is there
	// to stop a crash-restart loop, not to cap the process's lifetime.
	w.restarts = 0
	w.startErr = nil
	return nil
}

// readLine reads one response line, giving up after the timeout. It
// runs with mu held.
func (w *winrt) readLine(ctx context.Context, timeout time.Duration) (string, error) {
	type result struct {
		line string
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		line, err := w.stdout.ReadString('\n')
		ch <- result{line, err}
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case r := <-ch:
		if r.err != nil {
			// The process is gone; the next ensure starts a fresh one.
			w.killLocked()
			return "", fmt.Errorf("engine exited: %w", r.err)
		}
		return strings.TrimRight(r.line, "\r\n"), nil
	case <-timer.C:
		return "", fmt.Errorf("timed out after %s", timeout)
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// restart tears the dead process down so the next ensure starts fresh.
// It runs with mu held.
func (w *winrt) restart(ctx context.Context) error {
	w.killLocked()
	return w.ensure(ctx)
}

func (w *winrt) killLocked() {
	if w.cmd != nil && w.cmd.Process != nil {
		_ = w.cmd.Process.Kill()
		_ = w.cmd.Wait()
	}
	w.cmd = nil
	w.stdin = nil
	w.stdout = nil
}
