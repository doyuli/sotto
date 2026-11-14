# Product

## Platform

Windows desktop; experimental macOS 14+ support on Apple silicon (arm64) only.

## Stack

Existing Windows desktop app: pure-Go backend (`CGO_ENABLED=0`), mygo v0.8.3 native screenshot selection, and native `mygo/ui` bubbles and settings.

The macOS port retains MyGo v0.8.3 and bridges public system APIs through
`purego` / `purego/objc`: Accessibility selection with a guarded pasteboard
fallback, ScreenCaptureKit capture, Vision OCR and system speech. It remains
Accessory for its entire lifetime, with no Dock icon. Nonactivating panels
appear in the current full-screen Space; only explicit input, settings and
screenshot actions take keyboard focus. Background results never activate the
app.

## Platform capabilities

| Capability | Windows | macOS (experimental) |
| --- | --- | --- |
| Selection | UIA, guarded simulated copy | Accessibility, guarded simulated copy |
| Clipboard | Format snapshots and exclusion markers | All readable representations per item; uncertain ownership retains a recovery backup |
| Capture / OCR | GDI; PP-OCR with WinRT fallback | ScreenCaptureKit; Vision |
| Speech | SAPI | AVFoundation |
| Credentials | Credential Manager | Keychain |
| Default shortcuts | Ctrl+Alt+A/S/D | Option+A/S/D |
| Updates | Update window | Menu bar discovery and explicit installation |
| Permissions | System access policies | Accessibility and screen recording |

Explicitly saved shortcuts remain unchanged, including earlier Alt+A/S/D combinations. Missing values use platform defaults; defaults are not inferred from saved values for migration.

Passive Mac results do not request focus. Explicit input/settings/screenshot actions request keyboard focus without activating the app. Application switches honor auto-hide; workspace changes end transient work and hide fixed bubbles while preserving their independent jobs for explicit restoration. Native focus and multi-display behavior require the desktop acceptance checks in `e2e/macos.md`.

## Users

Chinese-first Windows users who translate while working in another app: selected text, a screen region (OCR), or text typed directly into the bubble. They compare several translation services at once and often need code-friendly identifier forms.

## Product Purpose

Sotto puts an always-available translation bubble beside the current task. A hotkey reads the selection or screenshot, the bubble fans the text out to every enabled translation service, and results stream in side by side. Success is a translation the user can read and copy without leaving the app they were in.

## Positioning

A local Windows utility that combines selection capture, OCR, and multi-provider translation in one transient bubble, including copy formats for camelCase, PascalCase, snake_case, CONSTANT_CASE, and kebab-case.

## Operating Context

- Global hotkeys default to `Ctrl+Alt+A` (typed input), `Ctrl+Alt+S` (OCR), and `Ctrl+Alt+D` (selection) on Windows; Mac uses `⌥A`, `⌥S`, and `⌥D` respectively.
- The bubble opens near the selection or screen region, hides on blur, and can be pinned.
- Settings live in a separate window; API keys live in the OS keyring, never in settings JSON.
- `Enter` translates; `Shift+Enter` inserts a newline.
- Theme follows the operating system light/dark setting.

## Capabilities and Constraints

- Services: Bilibili IndexTranslate, SiliconFlow, Zhipu, DeepL, any OpenAI-compatible endpoint, Baidu, Volcengine, Tencent Cloud, Youdao, Microsoft Azure, Google (keyless gtx), plus a dictionary card for lone English words. Only IndexTranslate is enabled by default.
- OpenAI, SiliconFlow, and Zhipu support custom models with independent API keys. Defaults: `gpt-4o-mini`, `tencent/Hunyuan-MT-7B`, and `glm-4.7-flash`, respectively. Blank models restore defaults; pricing follows the provider. Connection tests require successfully saved settings.
- On Windows, selection via UIA with a clipboard fallback; OCR via PP-OCR, using Windows.Media.Ocr when the local engine fails or returns empty text. Confidence risks remain diagnostic. Explicit languages outside the local model's scope go directly to Windows OCR.
- Screenshot selection uses a native overlay per display over the live desktop. A light translucent veil indicates selection mode; dragging adds a selection border, a light translucent blue fill and a DIP size badge. Unused overlays are reused; an overlay that has begun a selection is replaced before its next appearance to avoid flashing the previous selection. Dragging stays on one display; Esc, right-click or lost pointer capture cancels. On Windows, the overlay hides before GDI captures its local DIP rectangle; selecting a region does not freeze the desktop or preload OCR.
- PP-OCR releases its sessions after 5 minutes idle. Windows OCR stops its worker after 30 seconds idle. Both reload on demand. The shared ONNX runtime stays loaded, and OCR and fallback share one admission gate.
- Long text is chunked and merged; streamed per-service results arrive progressively.
- English results expose the five naming styles as copy actions.
- Hard technical constraints: Windows first, pure Go, `CGO_ENABLED=0`, pinned mygo desktop/UI APIs and a project-owned purego macOS bridge, whitelisted dependencies.
- The bubble is frameless, fixed-width (480 px), skips the taskbar, and grows to fit its content.

## Brand Commitments

- Name: Sotto. UI copy is Chinese.
- Reusable brand system: a quiet, compact Windows surface with a neutral surface ladder, one blue accent, system sans type, 12–14 px corners, and a single speech-bubble S mark. The full specification lives in `DESIGN.md`.
- No country/flag icons in language selection.
- No third-party translation-platform logos in the bubble.
- No "open in browser" affordance in the bubble.

## Evidence on Hand

- `DESIGN.md` — reusable brand and bubble design tokens.
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
