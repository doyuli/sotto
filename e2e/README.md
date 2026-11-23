# Verification Entry Points

| Layer | Windows | macOS |
| --- | --- | --- |
| Shared logic and native UI Tester | `CGO_ENABLED=0 GOFLAGS=-tags=mygo_nowebview go test ./...` | `CGO_ENABLED=0 go test ./...` |
| Native engines / isolated system objects | Explicitly enable OCR and capture probes | Explicitly enable Vision, isolated pasteboard, and SCK probes |
| Desktop windows, focus, permissions, and multiple displays | [Windows acceptance](windows.md) | [macOS acceptance](macos.md) |

For subsystem architectures and platform integration details, see [Windows Architecture](../docs/windows.md) and [macOS Architecture](../docs/macos.md).

Tester does not create native windows; symbol loading does not prove actual selection capture or screen capture. Cross-compilation is not platform test execution. Default tests do not synthesize input, modify the general pasteboard, or capture the actual screen.

## Minimal Translation Mode

On each desktop platform, verify the following for **简约翻译模式**.

- Switch modes from General settings and with the configured shortcut. The same tray item should show a filled speech bubble in bubble mode and an outlined bubble in minimal mode. On light and dark Windows taskbars, check for a white S in the filled blue bubble and a blue S inside the blue outline. The taskbar should show through around both icons and inside the outlined bubble. On macOS, verify both follow the menu bar tint. Hover to confirm the mode name, then restart in minimal mode and verify the outlined icon appears immediately.
- With the mode off and another app focused, enable it using `⌥F` on macOS or `Ctrl+Alt+F` on Windows. Verify the brief status appears, focus stays in the source app, and the General settings switch shows the mode is on. Restart Sotto while the mode is on and verify it is still enabled. Use the shortcut to turn it off, then record a custom binding in Shortcuts and verify it takes effect.
- Keep two translations open from different selections, including a request that completes after the next selection. Drag and scroll each independently. The first presentation leaves focus in the source app, a right-click closes its own result, and Escape closes the focused result.
- With a slow service, verify the loading dots appear after a short delay and the same window shows the result. Close a loading window and verify no result appears for that request. With Reduce Motion enabled, the loading dots stay still.
- With a streaming service, verify the first translated text replaces the waiting state and more text arrives in the same window. Scroll back while it is generating and verify the reading position is preserved. Close another streaming result and verify its late updates do not reopen it.
- Translate a long paragraph. Verify the 320-DIP width limit and six-line viewport, then scroll to the end without a visible scrollbar. Check the display edge, mixed scaling and, on macOS, a full-screen Space.
- On macOS, switch apps within the same Space and verify results remain, then switch Spaces or put the display to sleep and verify old results have closed and pending translations do not reappear.
- Capture a region previously covered by a translation. Existing results should be absent from the screenshot and restored after submitting or canceling the capture. Move a result or scroll it before starting the capture so its restored position and scroll offset can be checked.
- Verify the selected service follows saved service order. Keep a completed overlay and a streaming overlay visible, then disable the mode. Both should stay in place, the stream should finish in its original overlay, and the next selection should open a full bubble. Repeat using General settings. Toggle while selecting a screenshot region and verify that submitting it still uses the mode in effect when the snip started. Confirm the manual-input shortcut still opens the full panel, and quit from the tray with multiple results visible.

On an interactive Windows desktop, the native first-frame regression test opens a short translation near the bottom of the work area and checks that the card appears with input enabled and without taking focus.

```bash
SOTTO_DESKTOP=1 CGO_ENABLED=0 GOFLAGS=-tags=mygo_nowebview go test ./internal/ui/overlay -run '^TestOverlayAppearsNearWorkAreaBottom$' -v
```

Regular Windows CI runs noninteractive tests, ONNX runtime/model preflight, repeated WinRT worker requests, e2e vet, and DPI initialization checks on x64. It builds both Windows architectures without installers, checks the OCR assets, and scans the executables for the WebView2 loader marker. Regular macOS CI runs noninteractive tests, native image conversion, Vision recognition, and isolated pasteboard tests. It builds and verifies an ad-hoc signed app and DMG and checks that Windows OCR assets are excluded. Full desktop acceptance requires an interactive session, the appropriate permissions, and hardware.

The maintainer previously confirmed Windows and macOS desktop acceptance. See [Windows acceptance](windows.md) and [macOS acceptance](macos.md) for the regression procedures to run after relevant changes.

Environment-variable examples in build and verification commands use POSIX shell syntax. In PowerShell, set `$env:NAME="value"` and run the command separately.
