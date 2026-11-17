# Product

## Platform

Windows 10/11 (amd64 and arm64) and macOS 14+ on Apple silicon (arm64). Both platforms are supported release targets.

## Stack

Both desktop apps use a pure-Go backend (`CGO_ENABLED=0`) and MyGo for native screenshot selection, translation bubbles, and settings. The Windows updater uses `mygo/ui`, and the Windows release build scripts exclude WebView2.

On macOS, `purego` and `purego/objc` bridge Accessibility selection with a guarded pasteboard
fallback, ScreenCaptureKit capture, Vision OCR, and system speech. Sotto runs
as an accessory app without a Dock icon. Nonactivating panels
appear in the current full-screen Space; only explicit input, settings and
screenshot actions take keyboard focus. Background results never activate the
app.

## Platform capabilities

| Capability | Windows | macOS |
| --- | --- | --- |
| Selection | UIA, guarded simulated copy | Accessibility, guarded simulated copy |
| Clipboard | Format snapshots and exclusion markers | All readable representations per item; uncertain ownership retains a recovery backup |
| Capture / OCR | GDI; PP-OCR with WinRT fallback | ScreenCaptureKit; Vision |
| Speech | SAPI | AVFoundation |
| Credentials | Credential Manager | Keychain |
| Default shortcuts | Ctrl+Alt+A/S/D | Option+A/S/D |
| Updates | Native update window | Menu bar discovery and explicit installation |
| Permissions | System access policies | Accessibility and screen recording |

Explicitly saved shortcuts remain unchanged, including earlier Alt+A/S/D combinations. Missing values use platform defaults; defaults are not inferred from saved values for migration.

Passive Mac results do not request focus. Explicit input/settings/screenshot actions request keyboard focus without activating the app. Application switches honor auto-hide; workspace changes end transient work and hide fixed bubbles while preserving their independent jobs for explicit restoration. Native focus and multi-display regression checks are documented in `e2e/macos.md`.

## Users

Chinese-first Windows and macOS users who translate while working in another app: selected text, a screen region (OCR), or text typed directly into the bubble. They compare several translation services at once and often need code-friendly identifier forms.

## Product Purpose

Sotto puts an always-available translation bubble beside the current task. A hotkey reads the selection or screenshot, the bubble fans the text out to every enabled translation service, and results stream in side by side. Success is a translation the user can read and copy without leaving the app they were in.

## Positioning

A local Windows and macOS utility that combines selection capture, OCR, and multi-provider translation in one transient bubble, including copy formats for camelCase, PascalCase, snake_case, CONSTANT_CASE, and kebab-case.

## Operating Context

- Global hotkeys default to `Ctrl+Alt+A` (typed input), `Ctrl+Alt+S` (OCR), and `Ctrl+Alt+D` (selection) on Windows; Mac uses `⌥A`, `⌥S`, and `⌥D` respectively.
- The bubble opens near the selection or screen region, hides on blur, and can be pinned.
- Settings live in a separate window; API keys live in the OS keyring, never in settings JSON.
- `Enter` translates; `Shift+Enter` inserts a newline.
- Theme follows the operating system light/dark setting.

## Capabilities and Constraints

- Services: Bilibili IndexTranslate, SiliconFlow, Zhipu, DeepL, any OpenAI-compatible endpoint, Baidu, Volcengine, Tencent Cloud, Youdao, Microsoft Azure, Google (keyless gtx), plus a dictionary card for lone English words. Only IndexTranslate is enabled by default.
- OpenAI, SiliconFlow, and Zhipu support custom models with independent API keys. Defaults: `gpt-4o-mini`, `tencent/Hunyuan-MT-7B`, and `glm-4.7-flash`, respectively. Blank models restore defaults; pricing follows the provider. Connection tests require successfully saved settings.
- Selection translation: Windows reads text via UIA with guarded clipboard fallback; macOS uses Accessibility with guarded pasteboard fallback. Both paths snapshot readable clipboard content before simulated copy and retain a recovery backup on restoration failure; ownership/version guards prioritize newer user copies.
- Offline OCR: Windows uses local PP-OCR with WinRT fallback; macOS uses Apple Vision. Windows releases PP-OCR sessions and stops its system-OCR worker on their idle deadlines (see `resources/windows/ocr/README.md`); macOS creates and releases Vision request/handler objects per recognition call, with no Sotto-managed idle model session.
- Screen snip: Sotto takes display snapshots before opening native selection overlays. OCR uses the selected pixels from the same snapshot shown in the preview. Interaction and visual tokens follow `DESIGN.md`.
- Window presentation: The translation bubble is frameless, fixed-width, skips the taskbar, and sizes dynamically to content within display bounds. Full lifecycle and state contracts follow `docs/translation-state.md`.
- Long text is chunked and merged; streamed per-service results arrive progressively.
- English results expose the five naming styles as copy actions.
- Hard technical constraints: Windows and macOS, pure Go, `CGO_ENABLED=0`, pinned MyGo desktop/UI APIs and a project-owned purego macOS bridge, whitelisted dependencies.

## Brand Commitments

- Name: Sotto. UI copy is Chinese.
- Reusable brand system: a quiet, compact Windows surface with a neutral surface ladder, one blue accent, system sans type, 12–14 px corners, and a single speech-bubble S mark. The full specification lives in `DESIGN.md`.
- No country/flag icons in language selection.
- No third-party translation-platform logos in the bubble.
- No "open in browser" affordance in the bubble.

## Evidence on Hand

- `DESIGN.md` — reusable brand and bubble design tokens.
- `docs/translation-state.md` — window state, concurrency, and presentation lifecycle.
- `docs/hotkeys.md` — key combination parsing, registration transactions, and limitations.
- `docs/windows.md` — Windows Win32/COM architecture, UIA, clipboard exclusions, and GDI capture.
- `docs/macos.md` — macOS purego architecture, framework bridges, and window policies.
- `resources/windows/ocr/README.md` — Windows PP-OCR runtime assets, routing, and idle budgets.
- Running implementation: `internal/ui/bubble/view.go`, `internal/ui/settings/view.go`, `internal/ui/bubble/manager.go`.

## Product Principles

1. The result is the interface: translation text and copy actions outrank chrome.
2. Comparison is the feature: cards share one grammar so services are scanned, not deciphered.
3. Stay out of the way: open fast, hide on blur, never steal more attention than the task.
4. Local and private by default: keys stay in the OS keyring; no telemetry.
5. Keyboard first: translation and settings controls support keyboard access; service ordering uses pointer drag.

## Accessibility & Inclusion

- Maintain WCAG AA contrast in both themes, including muted action icons.
- Visible keyboard focus on every control.
- Respect the system’s reduced-motion preference; motion only communicates state.
- Chinese and English text must remain readable at 12–15 px in a compact desktop panel.
