# Windows Desktop Acceptance

Windows manual desktop acceptance has been completed as confirmed by the maintainer. The checklist below remains the procedure for future regression checks. The procedures below are Windows-specific. For the subsystem architecture and Win32/COM platform integration, see [Windows Architecture](../docs/windows.md).

Drive the **built production executable** as a user would: synthesize global hotkeys and inspect windows, focus, and blur. All checks use direct Win32 calls (`x/sys`), without relying on window titles or screenshots.

## Running

Run from the repository root in an interactive Windows desktop session. The smoke harness launches the x64 `build/windows-amd64/Sotto.exe` with the OCR assets beside it.

```bash
bun run build:win:x64 -skip-installer
CGO_ENABLED=0 go test -tags e2e,mygo_nowebview ./e2e/ -v
```

## Coverage

| Test | Verification |
|---|---|
| `TestDPIAwarenessAlreadyInitialized` | The test process uses the same per-monitor DPI V2 mode as the app. Repeated initialization confirms the existing mode rather than treating "already set" as failure. Requires no desktop session and can run separately in CI. |
| `TestPanelAndSelectionShareBubble` | The panel hotkey opens and focuses the input bubble. Three repeated invocations while visible retain the same window and focus. After blur hides it, the selection hotkey with no selected text opens an editable fallback page, reusing the same HWND. |
| `TestSnipOverlay` | The OCR hotkey opens a native screenshot overlay using a transparent DirectComposition window without whole-window fading. Confirms DWM has uncloaked it, `MyGoSurface` exists, and there is no WebView child window; it remains present after 700 ms. Twenty open/cancel cycles alternate Esc and right-click and verify that each round destroys all its HWNDs. Canceling during a drag also destroys each overlay and a new capture can open. |

Hotkey combinations are parsed from the app startup log's "hotkeys in force" line and sent as synthesized input. Tests follow changes to defaults or user bindings rather than hardcoding Alt+A/S/D.

## Known Test Environment Limitations

- At startup, tests confirm per-monitor DPI V2 mode. If it cannot be set and the current mode differs, fail explicitly rather than testing virtualized coordinates under mixed scaling.
- The Windows CI job uses `mygo_nowebview` for ordinary tests, MyGo vet, and OCR preflight on x64. It also vets the `e2e` package with both `e2e` and `mygo_nowebview` and runs only `TestDPIAwarenessAlreadyInitialized` from that package. CI builds both Windows architectures without installers, checks their OCR assets, and checks both executables for the absence of the `CreateCoreWebView2EnvironmentWithOptions` loader marker. Desktop interaction and native ARM64 execution require separate validation.
- Hotkeys are synthesized with `SendInput`. The test process first grants the app foreground permission with `AllowSetForegroundWindow`, modeling the foreground rights of real keyboard hotkeys.
- This smoke test covers screenshot-window opening, cancellation, and reopening after drag cancellation. Native Tester separately covers drag geometry and cancel input. The complete Windows drag-to-capture-to-OCR path is covered by the manual acceptance checklist below. `sotto --ocr <png>` can independently check image recognition.

## Manual Selection and Clipboard Acceptance

Requires a Windows desktop session. Host unit tests and Windows cross-compilation cannot replace native clipboard acceptance.

- Copy an image with Win+Shift+S, WeChat/QQ/Snipaste, or a browser, then double-click words in the VS Code editor and the chat interfaces actually used and trigger selection translation. Compare behavior with plain text, HTML/RTF, copied files, and cut files on the clipboard. Paste the original content after capture and check preservation of images, rich text, file lists, and cut semantics.
- Test ordinary screenshots, transparent PNGs, 4K screenshots, large multi-display screenshots, and palette images separately. Preserve original PNG/other registered formats and both DIB representations. Check dimensions, pixels, transparency, and color; compare raw data for every backed-up format before and after restoration. Confirm requesting CF_BITMAP after restoration still yields a valid bitmap. Preserve CF_PALETTE colors and entry flags as well.
- A successful UIA path bypasses the clipboard entirely. To test image fallback independently, run `sotto.exe --grab clipboard`, switch to the target window, and select text within the 3-second wait. This diagnostic command's stdout JSON includes captured text; added diagnostics go to stderr and contain only format metadata and status. Compare UIA separately with `--grab uia`; persistent slot occupancy must be observed within one long-running process.
- The total snapshot budget is 128 MiB; non-DIB/DIBV5 data remains limited to 32 MiB combined. A 4K DIB+DIBV5 pair can be backed up together. Exceeding either budget, a DIB read failure, an unreadable unknown format, or metafile/owner/display formats must produce an error before simulated copy, leaving old content intact. Conservatively reject a clipboard continuously held open by another process, with logs identifying snapshot/open.
- During selection capture, manually copy new content, switch apps, or interrupt with keys, mouse buttons, or the wheel. New content takes priority; late results do not overwrite the clipboard. Pointer movement alone still allows capture to finish. Recovery failure continues to offer recovery and pauses new simulated copies. An explicit retry restores only a still-matching sequence number; in-memory recovery content does not survive exit.
- Check that Sotto's restoration writes still include all three exclusion formats: CanIncludeInClipboardHistory=DWORD(0), CanUploadToCloudClipboard=DWORD(0), and the presence of ExcludeClipboardContentFromMonitorProcessing. Inspect Windows history and the clipboard managers actually used. Temporary writes made by the target app in response to simulated copy are outside Sotto's control; restoration markers cannot guarantee they never entered history.
- Logs must distinguish UIA `empty`/`miss`, `timeout`, `slot_busy`, `breaker_open`, and late worker returns. A permanently blocked call must not cause additional COM calls. Clipboard failures should include stage, format ID/name, size or budget, and timing, without content. For "无法安全保留" errors, save that run's source → uia → clipboard → end logs, distinguishing the source of the selected text from the source of the original clipboard content.

## Manual Native Screenshot Acceptance and Measurement

Pure logic and `ui.NewTester` cover selection gestures, cancellation, stale tasks, and cropping the same pixels displayed in the preview. Tester cannot replace actual Windows composition and multi-display validation.

- On single and multiple displays, including negative coordinates and mixed 100% / 125% / 150% / 200% scaling, verify the frozen preview covers each display. The selection border and DIP size label must agree with the final physical crop. Select cross-display content in separate operations.
- Open a native dropdown or Explorer context menu, then press the screenshot shortcut. Check that the first preview still shows the menu and that OCR receives its text. Try a Chromium menu as well.
- Submit or cancel with Esc/right-click, change the desktop, then reopen capture. The first frame shows the new desktop with a light veil and no previous selection or size label. Repeat with the pointer on an auto-hidden taskbar and with visible taskbars on multiple screens.
- While dragging a selection, press the screenshot shortcut again. The replacement preview and crop contain no trace of the first overlay. Press Esc before a slow snapshot opens and confirm no overlay appears later.
- Repeatedly trigger screenshots, invoke input/selection during capture, switch apps, lose pointer capture, and use Alt+F4. End the current unpinned bubble session; cancellation must not submit OCR or create stale ghost windows. Cross-display focus changes and startup must not cause unintended hiding.
- Disconnect a display or change resolution/layout/scaling during selection. Pending snapshots and active selections are canceled. Reopening uses the current layout.
- After selection, the crop contains no selection tint, border, or size label. Real Chinese/English recognition, system fallback, and OCR's 5-minute idle release continue working.
- Check border and size-label readability on light/dark desktops. Quitting from the tray must actually close all native windows.

## Manual Bubble Edge and Long-Text Acceptance

Requires a Windows desktop session.

Windows tests in `internal/selection` use hidden windows and `NotifyWinEvent` to cover HWND ownership, foreground-notification filtering, foreground notifications without bubble blur, and shared-hook removal. Native menu activation, dismissal, and cross-app switching are covered by the manual checks below.

- Trigger selection and OCR at all four screen corners and above the taskbar. Keep margins; expand left when space to the right is insufficient and upward when space below is insufficient.
- Stream a result in a bubble that opened above its anchor. After the first text appears, check that new lines keep the top edge and the first line in place. Scroll to later text when the bubble reaches the work-area margin. Collapse and expand a card while a second service adds text on the same line, and check that the height transition still runs.
- With auto-hide enabled, change source and target languages using both mouse and keyboard. Opening native dropdowns, confirming an option, or canceling with Esc keeps the bubble visible; confirmation retranslates in the selected languages. Also check dropdowns extending beyond bubble bounds, initial display without focus, pinned bubbles, and disabled auto-hide. Clicking another app or using Alt+Tab, both while a dropdown is open and after confirmation, must still follow auto-hide.
- Open the input bubble on a secondary display and edit a draft; blur to hide, disconnect that display, then press the input hotkey. Clear the input and old translations, and place the bubble on a connected display. Disconnect a display with a visible bubble, change the layout, or reduce resolution; position and maximum height update together. Late results after the current session ends must not update the window or return it to old coordinates.
- Use long source text and multiple long translations. Source text has a height cap; translations expand fully without extra scrollbars inside cards or on the page itself. Content below the header scrolls together; the logo and close/pin buttons remain fixed. The scrollbar starts below the header; wheel input over the header must not scroll content.
- Hover alone does not show scrollbars. Wheel, trackpad, and keyboard scrolling show them; they fade after roughly 900 ms of inactivity. Text wrapping and cards remain stable as scrollbars appear/disappear.
- Pin and drag to a shorter display, continue translation, then unpin. Preserve the dragged position, keep new content within the work area, and retain draggable scrollbars.
- Check 100%, 125%, 150%, and 200% scaling in both themes. Also verify a blank new session from the input hotkey, hiding on blur, and tray quit.

## Manual Streaming Translation Acceptance

Requires a Windows desktop session. Local HTTP simulations and native UI tests cover protocol and state but cannot replace hardware acceptance.

- Enable Bilibili and a configured OpenAI-compatible service together and translate long text. Skeletons disappear when the first text arrives; the title shows "生成中" and the body grows progressively. New text fades in over roughly 150 ms without repeatedly flashing old text. With system reduced motion enabled, new text appears immediately.
- Copy and speech buttons are disabled during generation, and English translations do not request naming formats. Successful completion enables actions and shows naming formats for the complete translation.
- Disconnect the network after receiving partial text. Preserve text, show an incomplete notice, and keep copy disabled. Restore the network and retry one card; other cards and reading position remain intact.
- Use a slow model or controlled mock endpoint. Continuous heartbeats/reasoning/translation beyond 30 seconds must not hit an overall time limit. After text arrives, 30 consecutive seconds without data preserves text and reports generation timeout. A successful OpenAI connection test shows "连接成功".
- Return model-unavailable, insufficient-balance, rate-limit, and excessive-input errors from a mock endpoint. Connection tests and normal translation give consistent messages; streaming errors preserve existing text.
- Submit new source text, change languages, or close/reopen during generation. Old streams must not overwrite new content; canceled cards must not remain stuck on "生成中".
- Scroll, select text, and collapse cards during generation. Text selection remains stable and focus does not jump. Check both themes and short work areas.

## Manual Model Configuration Acceptance

- Change OpenAI, SiliconFlow, and Zhipu models separately. Settings persist across restart and remain independent; leaving a model blank restores its default.
- Changing a model clears the previous test result. Model editing is disabled during testing, and the OpenAI address is locked as well.
- Simulate a settings-save failure. The UI rolls back to the last successfully saved configuration; the connection test aborts and asks the user to save again. After another edit and successful save, testing uses the new configuration.

## Manual Bubble New-Session and Initial-Size Acceptance

Requires a Windows desktop session.

- Complete a long translation that expands the bubble beyond 400 DIPs, then blur and trigger screenshot translation. Blur ends the old unpinned session. The new bubble's first visible frame uses the short recognition-state size without flashing old translations or height; it grows normally after recognition.
- Take screenshots in rapid succession, close during recognition, and take another screenshot during recognition. Old OCR and layout acknowledgments must not redisplay the bubble or contaminate a new session. First capture after a cold start must also display normally.
- With the input panel visible, type an unsubmitted draft, manually choose languages, start translation, and press the input hotkey again. The window does not hide or flash; draft, languages, translations, and in-flight requests remain, and input receives focus. Repeatedly invoke before initial layout completes; only the latest valid session appears.
- After entering an unsubmitted draft or completing translation, blur or close the input panel, then press its hotkey. The new panel is empty with no old source text or cards, and accepts input and translation normally.
- Trigger slow selection capture while input is visible, then press the input hotkey before capture returns. Start a blank input session and revoke old capture; late results must not take over the window. Switching from screenshot selection to input also cancels overlays. Pinned windows always remain independent.
- With a controlled slow service, repeatedly close, change sessions, and quit. Late native height measurements and state updates must not redisplay old windows or expose old content. Initial presentation and sizing also have coverage in `internal/ui/bubble/ui_test.go`, `internal/ui/bubble/layout_test.go`, and regression tests.
- Pin an old bubble, then open new input, selection, or OCR. Pinned content and work remain; the new bubble is independent. Unpin while the new bubble is visible and while it is hidden: confirm the new bubble's HWND is destroyed, the unpinned window retains its content, and the next invocation starts a new session in it. Repeat with auto-hide disabled.
- Check initial size, anchors, and display boundaries at 100% / 125% / 150% / 200% scaling and on a negative-coordinate secondary display. Confirm tray quit still closes all windows.
- With a fast translation service, repeatedly transition from a short loading state to a long translation. Late short layouts must not shrink or persistently clip long content. Streaming growth and card collapse must update window size accordingly.
- Blur to dismiss a bubble during streaming. The current session is canceled; later size updates must not redisplay it. Pin another bubble still translating, then trigger a new entry; the old pinned bubble must continue growing with its translation.

## Manual Native Settings Acceptance

`internal/ui/settings/view_test.go` covers settings saves, credential operations, model configuration, ordering, and keyboard interaction. Use the following Windows desktop checks for regression acceptance:

- Open settings from the tray and bubble; confirm singleton reuse, correct reopening after closure, and process exit through tray quit while settings is open.
- Check all four tabs, long service lists, and minimum window size in both themes at 100% / 125% / 150% / 200% scaling.
- Operate controls with Tab, Shift+Tab, Enter, Space, and Esc. Check focus and credential drafts after drag reordering and service-group changes.
- Actually save and clear keys, then restart to check Windows Credential Manager persistence. Model and address fields remain locked during connection tests; results after window closure must not affect the new window.

## Manual Native Updater Acceptance

- In a Windows build with updates configured, open “检查更新…” from the tray and verify that the native updater displays status and release notes. Use a test update to check download progress, installation, and the restart action.
