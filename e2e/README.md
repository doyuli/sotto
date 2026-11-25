# Verification Entry Points

| Layer | Windows | macOS |
| --- | --- | --- |
| Shared logic and native UI Tester | `CGO_ENABLED=0 GOFLAGS=-tags=mygo_nowebview go test ./...` | `CGO_ENABLED=0 go test ./...` |
| Native engines / isolated system objects | Explicitly enable OCR and capture probes | Explicitly enable Vision, isolated pasteboard, and SCK probes |
| Desktop windows, focus, permissions, and multiple displays | [Windows acceptance](windows.md) | [macOS acceptance](macos.md) |

For subsystem architectures and platform integration details, see [Windows Architecture](../docs/windows.md) and [macOS Architecture](../docs/macos.md).

Tester does not create native windows; symbol loading does not prove actual selection capture or screen capture. Cross-compilation is not platform test execution. Default tests do not synthesize input, modify the general pasteboard, or capture the actual screen.

## Translation History

Run the SQLite acceptance cases and the native UI Tester on the host. The tests use temporary history databases and a fake clipboard.

```bash
CGO_ENABLED=0 go test ./internal/history ./internal/ui/history ./internal/ui/settings ./internal/app -run '^TestHistory' -count=5
```

Set `GOFLAGS=-tags=mygo_nowebview` on Windows. On macOS, use the SQLite library from a production bundle to verify its actual load and database operations separately from window behavior.

```bash
CGO_ENABLED=0 bun run build:mac -skip-dmg -skip-notarize
MYGO_NATIVE_SQLITECORE="$PWD/build/darwin-arm64/Sotto.app/Contents/Resources/libSqlitecore.dylib" \
  CGO_ENABLED=0 go test ./internal/history -count=1 -v
```

On Windows, verify the architecture-specific package includes `Sqlitecore.dll` beside `Sotto.exe`. With that package on an interactive Windows desktop, check history creation and readback after restart.

- In an external editor, translate at least two entries. Open **翻译历史** from the tray/menu bar and a global shortcut; press the shortcut again while open to confirm it closes and returns focus. Verify the panel shortcut likewise closes a visible transient input panel (without closing pinned, selection, or OCR bubbles).
- Type a search quickly and press Enter before the filter settles: it must not copy a result from the previous query. Search for a phrase found only in a later provider's translation, clear search via its `×` button, use Up/Down to select an original, and press Enter. Verify history closes, focus returns to the editor, and pasting yields the first stored provider's complete translation (including newlines).
- Look up a word for which **有道查词与翻译** returns both pronunciations, and verify both appear and copy from history. Play source, translation, and dictionary headword audio via the speaker buttons, and confirm switching or dismissing mid-speech behaves cleanly.
- Open history across multiple displays (including displays left/above primary and mixed scales) to confirm placement and saved-bounds preservation. Check the borderless search bar at the top: drag blank space around search to move, resize from edges, and close via the `×` button without changing the clipboard.
- Verify outside click and app-switching dismiss history without copying, while clicking search, **换源翻译**, or the clear-history confirmation keeps it open. With a Chinese IME, confirm Up/Down/Enter during composition do not copy or close, while Escape after composition closes without copying. Double-click an original, copy the original (`复制原文`), copy per-provider results, copy English naming chips (`aB`/`AB`/`a_b`/`A_B`/`a-b`), and collapse/expand result titles; confirm the window stays open and shows toast/checkmark feedback.
- Verify **换源翻译** lists enabled untranslated services in order, omits **词典** for non-English/multi-word entries (and hides the menu when only ineligible **词典** remains), and hides once all eligible services have results. Verify **重译** updates only that service, disabled services remain copyable, switching entries keeps retry feedback scoped, offline retry stays layout-stable without button/icon flicker, and closing mid-request still saves when recording is on.
- Dismiss a normal input bubble immediately after a completed translation appears. Reopen history and verify the completed result is saved; closing before completion must not record partial text.
- Restart the packaged app to verify persistence, provider order, pagination counts, turning off recording without losing existing rows, retention-shortening confirmation, single/full deletion, and quitting from tray/menu bar while open. On macOS, confirm no Dock icon or Space jump; on Windows, confirm foreground focus returns to the editor.

## Minimal Translation Mode

On each desktop platform, verify the following for **简约翻译模式**.

- Switch modes from the tray menu, General settings, and the configured shortcut. Reopen the menu to verify its checkmark after each switch. The same tray item should show a filled speech bubble in bubble mode and an outlined bubble in minimal mode. On light and dark Windows taskbars, check for a white S in the filled blue bubble and a blue S on a translucent white fill in the outlined bubble. Both icons should be transparent outside the bubble. On macOS, verify both follow the menu bar tint. Hover to confirm the mode name, then restart in minimal mode and verify the outlined icon appears immediately.
- With the mode off and another app focused, enable it using `⌥F` on macOS or `Ctrl+Alt+F` on Windows. Verify focus stays in the source app and the General settings switch shows the mode is on. Restart Sotto while the mode is on and verify it is still enabled. Use the shortcut to turn it off. Check that switching in either direction by shortcut or tray menu shows no status popup. Record a custom binding in Shortcuts and verify it takes effect.
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
