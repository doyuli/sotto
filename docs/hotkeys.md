# Hotkey Bindings

`internal/hotkey` centralizes parsing, comparison, and registration of key combinations. Canonical names are `Ctrl / Option / Shift / Cmd` on macOS and `Ctrl / Alt / Shift / Win` on Windows. Modifiers have distinct identities; generic UI modifier bits are converted only at the recording entry point. Names such as Alt/Option and Super/Cmd/Win in existing settings are parsed for the current platform, preserving the same combinations without a version migration. JSON continues to store accelerator strings.

## Recording and Feedback

- Ordinary key-down events and Sotto's own global callbacks enter the same recording flow; translation is not triggered while recording. On macOS, letters and punctuation use MyGo's `PhysicalKey`, while extended function keys and numpad keys retain their `KeyCode` identities.
- Plain Esc cancels; Escape with modifiers can be bound. Arrow keys, punctuation, Tab, Enter, and similar keys can be recorded; missing modifiers or unsupported keys produce an explanation. Recording can continue immediately after a conflict. Unsupported numpad positions (such as macOS numpad Enter) explicitly ask the user to use the main keyboard instead of incorrectly saving the corresponding main-keyboard key.
- "手动设置" uses modifier buttons and a main-key list for combinations intercepted by the system before the recorder receives an event. It shares validation and saving with recording.
- Show "正在保存…" until registration and saving succeed; the old binding remains active. Save tasks run serially, ordinary settings writes use the latest hotkeys, and the final task reads back the actual settings. Closing/reopening the window must not restore old recording state or display late errors from the old window.
- Combinations that cannot be registered at startup remain in the configuration and appear as "未生效" in settings; they are no longer automatically replaced with alternatives. Canceling recording or reopening the window does not hide this status. Setting the same combination again retries registration.
- Translation History defaults to `Option+H` on macOS and `Ctrl+Alt+H` on Windows. Missing or empty values use the platform default. Use **翻译历史快捷键** to change the combination. It shares the other shortcuts' conflict checks and rollback, and the tray/menu-bar command remains available.
- Pressing the global `panel` or `history` shortcut while its transient window is visible closes/dismisses it instead of refocusing or reopening; tray/menu-bar actions and `--panel` still open or focus.

In the History search field, Up and Down move the selected original, Enter copies its first full translation and closes only after a successful clipboard write, and Escape closes without copying. Composition keys remain with the input method.

## Registration Transaction

The commit order in `internal/app/hotkeys.go` is: validate in-app duplicates → register new combinations → save configuration → publish the active mapping → release replaced combinations. Registration or save failures roll back only new registrations; old bindings remain registered. Callbacks dispatch through the current active mapping, so swapping combinations does not require clearing all registrations first.

Windows continues to use MyGo's `RegisterHotKey` wrapper. On macOS, the project bridges Carbon directly through purego, uses `kEventHotKeyExclusive`, and calls `CopySymbolicHotKeys` before registration to check enabled system hotkeys. All Carbon operations run on the main thread; registrations and event handlers are released on exit. No event tap or Input Monitoring permission is added.

MyGo v0.9.2 passes `0` for `HotKeyOptions` in `internal/darwin/hotkey.go`. The local Apple SDK's `CarbonEvents.h` documents that a nonexclusive registration can succeed even when an exclusive owner suppresses event delivery. Sotto checks the enabled system shortcuts and the result of its own exclusive registration.

The system list does not include individual apps' menu shortcuts and cannot cover every third-party interception mechanism. Unexplained missing events must not be labeled as conflicts.

## Verification Boundaries

- Parsing and registration transaction tests cover both platforms' names, existing combination semantics, registration failures, save failures, and combination swaps.
- UI Tester covers recording, global-event routing, retry after errors, manual main-key selection, cancellation/reopening, interleaved asynchronous saves, and startup failure states. Previews include widths of 660/780, light/dark themes, and recording/manual/conflict states.
- `CGO_ENABLED=0 SOTTO_MACOS_NATIVE=1 go test ./internal/hotkey -v` temporarily registers `Ctrl+Option+Shift+Cmd+F19` in two independent AppKit processes to verify cross-process exclusive registration conflicts and re-registration after release. It does not synthesize keyboard input or modify user settings.
- Actual keyboard triggering, input methods/keyboard layouts, system interception, and window focus are covered by the macOS desktop acceptance checklist in `e2e/macos.md`; the maintainer has confirmed completion. Windows cross-compilation is not Windows runtime validation.

## Known Limitation: Third-Party Nonexclusive Registration

macOS does not guarantee reporting third-party nonexclusive hotkey registrations. On 2026-11-14, a probe on macOS 15.7.3 (24G419), Apple Silicon, with `CGO_ENABLED=0` found that Sotto's exclusive registration still succeeded after another process registered the combination nonexclusively through MyGo. The Apple SDK's `CarbonEvents.h` documentation for `HotKeyOptions` states that an exclusive owner may prevent nonexclusive owners from receiving events. This probe checked registration results only, not actual key-event delivery.

As a product tradeoff, this case is not a release blocker and is excluded from regression cases that require successful conflict detection. Keep checks against the public system hotkey list, Sotto's internal duplicate checks, exclusive-registration conflict feedback, and rollback on save failure. Do not promise detection of all third-party registrations or add Input Monitoring or event taps. Related tests verify only these stated guarantees.
