//go:build darwin && e2e

package e2e

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"syscall"
	"testing"
	"time"
)

func TestMacPanelFocus(t *testing.T) {
	if os.Getenv("SOTTO_DESKTOP") != "1" {
		t.Skip("set SOTTO_DESKTOP=1 in an interactive desktop session")
	}
	executable := filepath.Join(t.TempDir(), "sotto-probe")
	buildCtx, stopBuild := context.WithTimeout(context.Background(), 2*time.Minute)
	defer stopBuild()
	build := exec.CommandContext(buildCtx, "go", "build", "-tags", "e2e", "-o", executable, ".")
	build.Dir = ".."
	build.WaitDelay = time.Second
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build native desktop probe: %v\n%s", err, output)
	}
	stopBuild()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := macProbeCommand(ctx, executable, "--native-smoke")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("native desktop probe: %v\n%s", err, output)
	}
	t.Log(string(output))
}

func macProbeCommand(ctx context.Context, executable string, args ...string) *exec.Cmd {
	// Own the actual probe process so cancellation kills and reaps it even
	// when its native main thread cannot service the internal timeout.
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.WaitDelay = time.Second
	return cmd
}

func TestMacProbeCancellationReapsProcess(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := macProbeCommand(ctx, executable, "-test.run=^TestMacProbeProcessHelper$", "--", "sotto-probe-child")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	// Always reap the owned child, including when the readiness check fails.
	waited := false
	t.Cleanup(func() {
		cancel()
		if !waited {
			_ = cmd.Wait()
		}
	})
	ready := make(chan error, 1)
	go func() {
		line, err := bufio.NewReader(stdout).ReadString('\n')
		if err == nil && line != "ready\n" {
			err = errors.New("unexpected child output")
		}
		ready <- err
	}()
	select {
	case err := <-ready:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("probe child did not become ready")
	}
	cancel()
	err = cmd.Wait()
	waited = true
	if err == nil || cmd.ProcessState == nil || cmd.ProcessState.Success() {
		t.Fatalf("canceled probe was not reaped: state=%v err=%v", cmd.ProcessState, err)
	}
	if err := cmd.Process.Signal(syscall.Signal(0)); !errors.Is(err, os.ErrProcessDone) {
		t.Fatalf("probe process still usable after cancellation: %v", err)
	}
}

func TestMacProbeProcessHelper(t *testing.T) {
	if !slices.Contains(os.Args, "sotto-probe-child") {
		t.Skip("subprocess helper")
	}
	// Stay blocked without relying on a sleep or a running AppKit loop.
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM)
	defer signal.Stop(signals)
	if _, err := io.WriteString(os.Stdout, "ready\n"); err != nil {
		t.Fatal(err)
	}
	<-signals
}
