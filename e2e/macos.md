# macOS Desktop Acceptance

Supported scope: macOS 14+, Apple Silicon. Use an interactive desktop session; do not terminate existing user instances. Record the OS version, launch method, display layout, and permission state. Keep unexecuted items marked as pending.

## Automated Entry Points

```bash
CGO_ENABLED=0 go test ./... -count=1
CGO_ENABLED=0 SOTTO_MACOS_NATIVE=1 go test ./internal/hotkey -v
CGO_ENABLED=0 SOTTO_MACOS_NATIVE=1 go test ./internal/ocr -run '^TestVisionNativeSmoke$' -count=1 -v
CGO_ENABLED=0 SOTTO_MACOS_NATIVE=1 go test ./internal/selection -run '^TestMacIsolatedPasteboardItems$' -count=1 -v
bun run build:mac -skip-dmg -skip-notarize
CGO_ENABLED=0 SOTTO_DESKTOP=1 go test -tags e2e ./e2e -run '^TestMacPanelFocus$' -count=1 -v
```

The isolated pasteboard test does not touch the general pasteboard. The SCK probe requires Screen Recording permission to be granted to its process beforehand; it does not automatically request permission through a prompt:

```bash
CGO_ENABLED=0 SOTTO_MACOS_NATIVE=1 go test ./internal/capture -run '^TestMacCaptureNative$' -count=1 -v
```

The window probe uses `TestMacPanelFocus` above: compile a temporary binary first (2-minute limit), then launch the probe directly (30-second limit). JSON and the exit code report check results; input completion is determined by an actual focus event, with a separate 10-second in-app timeout. The outer process directly owns and reaps the probe process, so it can terminate it even if the native main thread blocks; it does not launch indirectly through `go run`. `TestMacProbeCancellationReapsProcess` verifies cancellation and process reaping without a desktop. Record missing permissions, missing desktop sessions, and assertion failures separately; do not obtain a pass by skipping failed assertions.

## Manual Checklist

All items below require acceptance on actual hardware. Check each after a clean start and during warm operation. Do not report unit-test results as desktop passes.

| Preconditions | Action | Expected Result |
| --- | --- | --- |
| Settings is recording a hotkey | Press another Sotto action's combination, then an available one; try arrow-key, punctuation, and Tab combinations | In-app conflicts appear on the current row without triggering translation; recording can continue, and a successful combination takes effect |
| A combination is intercepted by the system or another app | Choose it in "手动设置" and save | Detectable conflicts produce an explicit error; the old binding remains usable without automatic substitution; missing key events alone must not be classified as a conflict |
| A hotkey has been saved | Blur, cancel, close/reopen settings, and restart the app; use both Chinese and English input methods | Recording cancels correctly; saved combinations persist; startup registration failures show "未生效"; macOS configuration and logs use Option/Cmd |
| Accessibility/Screen Recording permission is missing | Trigger selection and screenshots separately, retry after denial, then grant access in System Settings | Explain required permissions and recovery; do not automatically replay old entries; revoking permission produces an explicit error again |
| Accessible text in TextEdit/a browser | Select text and trigger translation; then trigger copy fallback in a target without AX support | The primary path does not modify the clipboard; fallback reads only the current source; compare paths with `--grab ax` and `--grab clipboard` |
| Recognizable backup content on the general pasteboard | Copy text, rich text, images, file URLs, and multiple items separately; trigger fallback, then paste to inspect | Preserve item boundaries and all readable representations; conservatively reject unknown/unreadable types; do not downgrade recovery to plain text |
| Fallback selection capture is running | Interrupt by copying new content, switching apps, pressing keys, or scrolling; trigger recovery retry | New content takes priority; uncertain ownership prevents overwriting; retain the backup and pause new fallback attempts until explicitly discarded |
| Auto-hide is enabled | Trigger selection/OCR in an external app without clicking the bubble; use ⌘Tab within the same Space | Results do not steal focus; switching apps ends transient sessions and old streams stop updating; disabling auto-hide follows the setting |
| Input panel, settings, or screenshot entry | Explicitly open it, type text, and use shortcuts | Keyboard focus is acquired while Accessory and nonactivating policies remain intact, with no Dock icon |
| A native dropdown menu is open | Confirm/cancel, then click an external app; also check settings and child windows | Internal menu interaction does not dismiss unexpectedly; external interaction follows auto-hide |
| A transient bubble has a slow request | Switch Spaces, enter a full-screen app, sleep/wake, then immediately start a new entry | Old transient work is canceled; late notifications do not close the new entry; no ghost windows appear |
| A pinned bubble has a slow request | Switch Spaces, restore the numbered bubble from the menu bar, then close it | Hiding preserves work and content; restoration does not resend translation; stale menu items for destroyed windows do not affect new windows |
| A pinned input bubble has been restored while another selection entry is still capturing | Unpin, then press the input-panel hotkey | Reacquire the input entry and revoke old capture; late selection results do not overwrite the input panel |
| Retina and non-Retina external displays, including negative coordinates to the left/above | Select known patterns near edges and below the menu bar; change the display layout and retry | Overlay, capture area, and bubble anchor agree; screenshots contain no app tint; display changes revoke old tasks |
| A screenshot request has been submitted | Quickly switch entries or cancel; repeat and inspect resources | Old results do not appear; retain objects until native completion, then release them; workers/windows do not accumulate |
| Keychain is available | Save/delete keys, close/reopen settings, and restart the app | Credential persistence remains separate from settings JSON; drafts/stale requests do not leak into the new window |
| System speech is available | Speak repeatedly, replace requests, and quit while work is queued | The latest request supersedes older requests; quitting does not deadlock; queued work does not speak after closure |
| Settings saves are pending and several windows are open | Quit from the tray/menu bar | Tasks drain in order, all OnClose handlers allow closure, and the process actually exits |
| Dedicated signed-installation test environment | Check updates, retry failures, download, install, and restart; disable automatic checks | Background work only discovers versions; installation requires a click; disabling automatic checks stops future scheduling, and quitting cancels in-flight requests |

## Current Verification Boundaries

Hotkey detection covers Sotto's internal duplicates, the public system list, and exclusive registration conflicts. Third-party nonexclusive registrations are an accepted capability limitation, not a release blocker or mandatory test item; see [known hotkey limitation](../docs/hotkeys.md#known-limitation-third-party-nonexclusive-registration). Do not report a pass as complete cross-app conflict detection.

The input-focus probe timed out on the development host without receiving `OnFocus`; passive presentation, Accessory, and nonactivating checks passed. In MyGo v0.8.3, `internal/darwin/child_window.go:showAboveParent` calls `makeKeyAndOrderFront:` for active Show, and `internal/darwin/window.go:IsFocused` reads `isKeyWindow`. Reproduce and explain the discrepancy in a normal app and interactive desktop before further changes to window policy.

Vision, isolated multi-item pasteboard round trips, and the SCK small-region capture-size probe passed on the development host. Real AX/general-pasteboard recovery, SCK self-window exclusion, multiple displays, sleep, and signed installation still require hardware acceptance. The SCK size probe does not prove all color-space, scaling, or cancellation paths. Unsigned/ad-hoc-signed builds are not evidence of notarization or update installation.

The existing isolated pasteboard probe verifies only direct AppKit writes/reads, without exercising the production snapshot/recovery path. An attempt to add that path on 2026-11-14 produced `NSPasteboard: synchronous promise fulfillment requested from a background thread!` and found that an item written with two representations returned three types. The added probe failed, and the experimental changes were reverted. The source-level difference is that `internal/selection/pasteboard_native_darwin_test.go` uses `macnative.Pool` in a test goroutine, while production `internal/selection/clipboard_darwin.go:pasteboardOnMain` dispatches through `mygo.RunOnMain`. This validation is paused under the platform-anomaly rule. Follow-up must explain the type change in a proper main-thread runtime and cover production snapshot/recovery, external writes, and cancellation protection. The current direct round-trip result must not be reported as validation of those paths.
