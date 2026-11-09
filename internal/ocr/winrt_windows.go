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
)

// winrt runs the resident PowerShell process. mu serializes requests —
// one stdin, one stdout — and covers process replacement.
type winrt struct {
	mu           sync.Mutex
	cmd          *exec.Cmd
	stdin        io.WriteCloser
	stdout       *bufio.Reader
	maxDimension int
	command      func() *exec.Cmd // Optional worker factory for protocol tests.
}

var engine = &winrt{}

type readyMsg struct {
	Ready        bool   `json:"ready"`
	Error        string `json:"error"`
	MaxDimension int    `json:"maxDimension"`
}

type askMsg struct {
	OK    bool         `json:"ok"`
	Lines []systemLine `json:"lines"`
	Error string       `json:"error"`
}

func recognizeWith(ctx context.Context, img image.Image, lang string, request *idleRequest) (Text, error) {
	local := func(ctx context.Context, img image.Image, lang string) (Text, error) {
		return recognizePPOCR(ctx, img, lang, request)
	}
	system := func(ctx context.Context, img image.Image, lang string) (Text, error) {
		return recognizeWinRT(ctx, img, lang, request)
	}
	return recognizeEngines(ctx, img, strings.TrimSpace(lang), local, system)
}

func recognizeWinRT(ctx context.Context, img image.Image, lang string, request *idleRequest) (Text, error) {
	t := Text{Engine: "winrt"}
	engine.mu.Lock()
	defer engine.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Text{}, err
	}
	request.used[systemOCR] = true
	start := time.Now()
	starting := engine.cmd == nil
	err := engine.ensure(ctx)
	if starting {
		t.timings.InitializationUS = time.Since(start).Microseconds()
	}
	if err != nil {
		return t, err
	}
	original := img.Bounds()
	start = time.Now()
	img, err = fitSystemImage(ctx, img, engine.maxDimension)
	t.timings.SystemPreprocessUS = time.Since(start).Microseconds()
	if err != nil {
		return t, err
	}
	start = time.Now()
	lines, err := engine.ask(ctx, img, lang)
	t.timings.SystemRequestUS = time.Since(start).Microseconds()
	if err != nil {
		return t, err
	}
	mapped, err := systemLines(lines, original, img.Bounds())
	if err != nil {
		return t, err
	}
	t.Lines = mapped
	return t, nil
}

// ask sends one image to the resident recognizer and returns what it
// saw. It runs with mu held.
func (w *winrt) ask(ctx context.Context, img image.Image, lang string) ([]systemLine, error) {
	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, img); err != nil {
		return nil, fmt.Errorf("ocr: %w", err)
	}
	req, err := json.Marshal(map[string]string{
		"png":  base64.StdEncoding.EncodeToString(pngBuf.Bytes()),
		"lang": lang,
	})
	if err != nil {
		return nil, err
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := w.ensure(ctx); err != nil {
			return nil, err
		}
		if err := w.writeRequest(ctx, append(req, '\n')); err != nil {
			if attempt == 0 && ctx.Err() == nil {
				// writeRequest discarded the broken worker. Retry the transfer
				// once; never replay a request after receiving a response.
				continue
			}
			return nil, fmt.Errorf("ocr: send image: %w", err)
		}
		break
	}

	line, err := w.readLine(ctx, askTimeout)
	if err != nil {
		// A late answer would still be in the pipe and the next request
		// would read it as its own: tear the process down instead, so the
		// next ensure starts a fresh one.
		w.killLocked()
		return nil, fmt.Errorf("ocr: %w", err)
	}
	var res askMsg
	if err := json.Unmarshal([]byte(line), &res); err != nil {
		return nil, fmt.Errorf("ocr: bad response: %w", err)
	}
	if !res.OK {
		return nil, fmt.Errorf("ocr: %s", res.Error)
	}
	return res.Lines, nil
}

// ensure starts the process once and waits for its ready line. It runs
// with mu held.
func (w *winrt) ensure(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if w.cmd != nil && w.cmd.Process != nil {
		return nil // already up
	}
	// One startup attempt per ensure, with no permanent failure cache. A later
	// request can recover after the environment or language packs are repaired.
	cmd := exec.Command("powershell.exe",
		"-NoProfile", "-NonInteractive", "-STA", "-ExecutionPolicy", "Bypass",
		"-Command", script)
	// A GUI process starting a console one flashes a PowerShell window
	// over everything; HideWindow keeps the engine invisible.
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if w.command != nil {
		cmd = w.command()
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("ocr: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return fmt.Errorf("ocr: %w", err)
	}
	cmd.Stderr = io.Discard // diagnostics would corrupt the line protocol
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return fmt.Errorf("ocr: %w", err)
	}
	w.cmd = cmd
	w.stdin = stdin
	w.stdout = bufio.NewReaderSize(stdout, 1<<20)

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
		return fmt.Errorf("ocr: %s", ready.Error)
	}
	w.maxDimension = min(maxOCRDimension, ready.MaxDimension)
	if w.maxDimension < 32 {
		w.killLocked()
		return fmt.Errorf("ocr: invalid system image limit")
	}
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
	stdout := w.stdout
	go func() {
		line, err := readProtocolLine(stdout)
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

// A blocked pipe write must not hold the admission gate after cancellation.
func (w *winrt) writeRequest(ctx context.Context, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	stdin := w.stdin
	done := make(chan error, 1)
	go func() { _, err := stdin.Write(data); done <- err }()
	timer := time.NewTimer(askTimeout)
	defer timer.Stop()
	select {
	case err := <-done:
		if err != nil {
			w.killLocked()
		}
		return err
	case <-ctx.Done():
		w.killLocked()
		return ctx.Err()
	case <-timer.C:
		w.killLocked()
		return fmt.Errorf("image transfer timed out")
	}
}

func (w *winrt) killLocked() {
	if w.stdin != nil {
		_ = w.stdin.Close()
	}
	if w.cmd != nil && w.cmd.Process != nil {
		_ = w.cmd.Process.Kill()
		_ = w.cmd.Wait()
	}
	w.cmd = nil
	w.stdin = nil
	w.stdout = nil
	w.maxDimension = 0
}
