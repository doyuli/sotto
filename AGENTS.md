# Sotto — Agent Working Rules

Translation utility built with mygo v0.8.3 (Go backend, native screenshot overlays, and native `mygo/ui` bubbles and settings). Windows and macOS, pure Go, no cgo.
See `PRODUCT.md` for product positioning and `DESIGN.md` for brand and visual guidelines. Local mygo source code is the sole source of truth for its APIs.

## Development Loop

```bash
export PATH="/c/Program Files/Go/bin:$PATH"   # Windows Git Bash only, if Go is not already on PATH
bun run dev            # Development run (Go + native UI)
CGO_ENABLED=0 go test ./...
bunx mygo vet .
bun run build          # Production build (compile + package)
# Windows: production package and desktop session
CGO_ENABLED=0 go test -tags e2e ./e2e/ -v
# macOS: skip DMG/notarization; desktop probes require an interactive session
bun run build:mac -skip-dmg -skip-notarize
CGO_ENABLED=0 SOTTO_DESKTOP=1 go test -tags e2e ./e2e/ -v
```

## Hard Rules

1. **Do not invent mygo APIs.** Consult the local source: `go list -m -f '{{.Dir}}' github.com/egoist/mygo`
   (module cache: `mygo@v0.8.3`). Before using common window/event/clipboard/hotkey signatures, read
   `window.go`, `ipc.go`, `clipboard.go`, and `modules.go` in that directory; do not write them from memory.
2. **Dependency allowlist**: `egoist/mygo`, `ebitengine/purego` (including `purego/objc`), `golang.org/x/sys`, `golang.org/x/time`, and `go-ole/go-ole`. Use the standard library for everything else. Report the need for a new dependency before adding one. Bridge macOS system capabilities directly in this project; do not modify mygo.
3. **Always use CGO_ENABLED=0.** Put platform code in `*_windows.go` / `*_darwin.go`; keep stubs with the appropriate build tags for unimplemented platforms.
4. **Threading model**:
   - GlobalShortcut/tray/menu callbacks run on the **main thread**: dispatch only, never block; move expensive work into a goroutine.
   - `Window` methods (Show/Hide/SetPosition, etc.) marshal internally and are safe to call from goroutines.
   - Use `Window.Invalidate()` / `applyState` to publish background state asynchronously to native `ui` views. Native view state belongs to the UI thread, or must be protected by a mutex before triggering `Invalidate()`. Do not capture screens or run OCR in paint callbacks.
   - UIA/COM calls run only on the dedicated MTA thread (see internal/selection/uia_windows.go).
   - macOS supports Apple Silicon (arm64) only. Keep the app in Accessory mode with no Dock icon; create interactive windows as nonactivating panels. Use `ShowInactive` for translation results; background results must not activate or focus windows. Run AppKit operations on the main thread, and AX / Vision in the background. Explicitly manage the lifetimes of objects and Blocks retained across asynchronous calls.
5. **Pure logic and native UI behavior that can be regression-tested require unit tests** (see the tradeoffs below). Platform-native behavior such as selection/capture/ocr also requires acceptance testing on actual hardware; unit tests cannot replace desktop validation.
7. **Clipboard discipline**: Every Windows write produced by simulated copy must include the three exclusion pseudo-formats (see internal/selection/clipboard_windows.go). On macOS, preserve all readable types per item; retain the recovery backup when ownership is uncertain, and never overwrite new content. Do not use clipboard monitoring to trigger selection translation. Detect clicks outside bubbles with Windows `WH_MOUSE_LL` or a macOS `NSEvent` monitor; callbacks must return immediately.
8. **Error handling**: Wrap errors with `fmt.Errorf("...: %w", err)`. Map service errors to user-readable Chinese messages (see UserMessage in internal/translate).
9. Match existing comments: English, explaining why. UI copy is Chinese.
10. If API behavior conflicts with documentation or an unexpected platform anomaly occurs, record the behavior and source-code references, stop, and report it. Do not work around it.
11. **Tray residency and quitting** (a subtle mygo pitfall): An empty `OnWindowAllClosed` listener keeps the app running, but quitting closes windows one by one, and **calling `PreventDefault` in any window's `OnClose` aborts the entire quit sequence**. This project sets `quitting` in `OnBeforeQuit`; bubble `OnClose` handlers allow closure when that flag is set. Preserve this path whenever changing window-close logic.
12. **Maintain tests alongside changes**: Update relevant tests when changing functionality, and remove invalid cases, outdated assertions, and unused test helpers.

## Testing Tradeoffs and Methods

- Prioritize inputs and outcomes: use `ui.NewTester` to drive clicks, keys, dragging, focus, and scrolling, then assert state changes or external actions. Examples include recording and saving hotkeys, canceling screenshots, and rejecting stale results. Add a brief English comment explaining the original bug in regression tests.
- Do not write mechanical assertions for copy inventories, form-row order, or implementation details, or test MyGo's own widgets and animation internals. Assert defaults only when they are explicit contracts, such as configuration migration, compatibility, and platform hotkeys. Do not remove these protections to simplify tests.
- Control asynchronous results with channels, fake runners/clocks, or `testing/synctest`; do not guess completion times with fixed real `time.Sleep` calls. Reuse existing `Actions` injection and test helpers. Restore replaced global hooks with `t.Cleanup`.
- For animations or delayed refreshes, advance time with the Tester's `Advance`; do not depend on the real clock's phase. When checking idle refresh budgets, remember that `FramePending()` excludes `After` / `Painter.After` callbacks that are not yet due. It alone cannot prove there will be no later refresh.
- Verify visuals primarily with rendered images or native windows. Use the existing `SOTTO_SETTINGS_PREVIEW` to export settings previews. Pixel assertions should protect explicit behavior such as overlay coverage and opacity; avoid tying entire images to layout and antialiasing details.
- After adding or changing UI regression cases, check stability with `CGO_ENABLED=0 go test <package-path> -run '^TestCaseName$' -count=5`. Fix synchronization or assertions when failures are intermittent; do not mask them with automatic retries. Still run the full verification above before committing.

## Git Conventions

- Use Conventional Commits (`<type>[optional scope]: <description>`) to stay consistent with the project.
- Follow `.github/pull_request_template.md`, organized into `Summary`, `Verification`, and `Scope`. Describe the final change, actual verification results, and scope, and identify pending validation.
- Do not commit unrelated clutter; `node_modules/ dist/ build/` are already ignored.
- Run the verification commands above before committing.

## Project Structure at a Glance

```
main.go               # Embeds the app icon and calls app.Run
internal/app          # Lifecycle, controllers, hotkeys, service assembly, entry tasks, screenshot orchestration
internal/ui/bubble    # Bubble window pool, native views, layout, and translation sessions
internal/ui/settings  # Native settings view and asynchronous save state
internal/ui/snip      # Selection overlays, display snapshots, and screenshot sessions
internal/ui/shared    # Display names and icons shared by bubbles and settings
internal/platform     # URLs, updates, CLI diagnostics, and platform window policies
internal/config       # Settings + atomic reads/writes (keys use mygo.Secrets, never JSON)
internal/keys         # Secrets wrapper
internal/hotkey       # Hotkey parsing, normalization, registration transaction; macOS Carbon bridge
internal/langdetect   # Coarse Unicode-script detection (pure functions)
internal/languages    # Supported language definitions and tags
internal/naming       # Identifier splitting + five naming styles (pure functions)
internal/translate    # Translator interface + fan-out + deepl/openai/baidu/tencent/youdao/azure/google/dictionary
internal/selection    # Windows UIA / macOS AX primary path + simulated-copy fallback; outside-click detection
internal/capture      # Windows GDI / macOS ScreenCaptureKit capture
internal/ocr          # Windows PP-OCR + Windows.Media.Ocr fallback; macOS Vision
internal/speech       # Text-to-speech synthesis (Windows SAPI / macOS AVFoundation)
internal/screencoords # Screen coordinate conversions between DIPs and physical pixels
internal/macnative    # Purego macOS system bridge (workspace, geometry, permissions)
```

## Platform Verification and Coordinate Contracts

- Put shared logic and Tester cases in ordinary `_test.go` files, and platform API tests in `_windows_test.go` / `_darwin_test.go`. Pure functions that accept a platform parameter must cover both platforms on every host; do not duplicate tests.
- Real capture, permissions, and desktop interaction require explicit opt-in. Use `SOTTO_CAPTURE_NATIVE=1` for Windows capture, `SOTTO_MACOS_NATIVE=1` for Mac native engines/isolated pasteboards, and additionally `SOTTO_DESKTOP=1` for desktop tests. See `e2e/README.md` for entry points and verification boundaries.
- Record symbol loading, compilation, Tester coverage, real system calls, and desktop acceptance separately. Do not report cross-compilation as a platform test pass.
- UI/global-screen coordinates use MyGo logical space; snip gestures are overlay-local. `capture.SnapshotDisplays` takes MyGo display IDs and returns physical pixels for the displays it captured. The framework displays and crops the snapshot using the same projection from display bounds into the overlay content area. OCR quadrilaterals use physical pixels.
