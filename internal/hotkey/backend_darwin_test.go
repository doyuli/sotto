//go:build darwin

package hotkey

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo"
)

const nativeTestShortcut = "Ctrl+Option+Shift+Cmd+F19"

// The helper owns an AppKit main loop on the process's initial thread. Native
// registration is tested in separate processes, not just against our Go map.
func TestMain(m *testing.M) {
	mode := os.Getenv("SOTTO_HOTKEY_TEST_HELPER")
	if mode == "" {
		os.Exit(m.Run())
	}
	mygo.App.SetActivationPolicy(mygo.ActivationPolicyAccessory)
	b := NewBackend()
	mygo.App.OnWindowAllClosed(func() {})
	mygo.App.OnQuit(b.Close)
	mygo.App.WhenReady(func() {
		result := map[string]string{}
		if err := b.Register(nativeTestShortcut, func() {}); err != nil {
			result["error"] = err.Error()
		}
		_ = json.NewEncoder(os.Stdout).Encode(result)
		if mode == "hold" && result["error"] == "" {
			go func() { _, _ = bufio.NewReader(os.Stdin).ReadString('\n'); mygo.App.Quit() }()
		} else {
			mygo.App.Quit()
		}
	})
	if err := mygo.App.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

func TestMacShortcutSymbolsAndKeyCoverage(t *testing.T) {
	if err := loadCarbon(); err != nil {
		t.Fatal(err)
	}
	for _, key := range Keys("darwin") {
		if _, ok := macKeyCodes[key]; !ok {
			t.Errorf("no native key for %s", key)
		}
	}
}

func TestMacExclusiveShortcutRegistration(t *testing.T) {
	if os.Getenv("SOTTO_MACOS_NATIVE") != "1" {
		t.Skip("set SOTTO_MACOS_NATIVE=1 to register temporary system shortcuts")
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	command := func(mode string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, executable)
		cmd.Env = append(os.Environ(), "SOTTO_HOTKEY_TEST_HELPER="+mode)
		cmd.WaitDelay = time.Second
		return cmd
	}
	holder := command("hold")
	stdin, err := holder.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := holder.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := holder.Start(); err != nil {
		t.Fatal(err)
	}
	waited := false
	defer func() {
		_ = stdin.Close()
		if !waited {
			_ = holder.Process.Kill()
			_ = holder.Wait()
		}
	}()
	var held map[string]string
	if err := json.NewDecoder(stdout).Decode(&held); err != nil {
		t.Fatal(err)
	}
	if held["error"] != "" {
		t.Fatalf("test shortcut unavailable: %s", held["error"])
	}
	attempt := func() string {
		output, err := command("try").CombinedOutput()
		if err != nil {
			t.Fatalf("native helper: %v\n%s", err, output)
		}
		var result map[string]string
		if err := json.Unmarshal(output, &result); err != nil {
			t.Fatalf("decode native result: %v\n%s", err, output)
		}
		return result["error"]
	}
	if message := attempt(); !strings.Contains(message, "已被其他应用占用") {
		t.Fatalf("exclusive conflict was not reported: %q", message)
	}
	_, _ = fmt.Fprintln(stdin, "release")
	if err := holder.Wait(); err != nil {
		t.Fatal(err)
	}
	waited = true
	if message := attempt(); message != "" {
		t.Fatalf("released shortcut could not be registered: %s", message)
	}
}
