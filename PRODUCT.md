# Product

## Platform

web

## Stack

Existing Windows desktop app: pure-Go backend (`CGO_ENABLED=0`), mygo v0.7.4, WebView2, Vite + TypeScript frontend.

## Users

Chinese-first Windows users who translate while working in another app: selected text, a screen region (OCR), or text typed directly into the bubble. They compare several translation services at once and often need code-friendly identifier forms.

## Product Purpose

Sotto puts an always-available translation bubble beside the current task. A hotkey reads the selection or screenshot, the bubble fans the text out to every enabled translation service, and results stream in side by side. Success is a translation the user can read and copy without leaving the app they were in.

## Positioning

A local Windows utility that combines selection capture, OCR, and multi-provider translation in one transient bubble, including copy formats for camelCase, PascalCase, snake_case, CONSTANT_CASE, and kebab-case.

## Operating Context

- Global hotkeys default to `Ctrl+Alt+A` (typed input), `Ctrl+Alt+S` (OCR), and `Ctrl+Alt+D` (selection).
- The bubble opens near the selection or screen region, hides on blur, and can be pinned.
- Settings live in a separate window; API keys live in the OS keyring, never in settings JSON.
- `Enter` translates; `Shift+Enter` inserts a newline.
- Theme follows the Windows light/dark setting.

## Capabilities and Constraints

- Services: Bilibili IndexTranslate, SiliconFlow (Hunyuan-MT-7B), Zhipu (GLM-4.7-Flash), DeepL, any OpenAI-compatible endpoint, Baidu, Volcengine, Tencent Cloud, Youdao, Microsoft Azure, Google (keyless gtx, disabled by default), plus a dictionary card for lone English words. Only IndexTranslate is enabled by default; SiliconFlow and Zhipu each use their own API key.
- Selection via UIA with a clipboard fallback; OCR via PP-OCR with Windows.Media.Ocr fallback.
- Long text is chunked and merged; streamed per-service results arrive progressively.
- English results expose the five naming styles as copy actions.
- Hard technical constraints: Windows first, pure Go, `CGO_ENABLED=0`, mygo-only desktop APIs, whitelisted dependencies.
- The bubble is frameless, fixed-width (480 px), skips the taskbar, and grows to fit its content.

## Brand Commitments

- Name: Sotto. UI copy is Chinese.
- Reusable brand system: a quiet, compact Windows surface with a neutral surface ladder, one blue accent, system sans type, 12–14 px corners, and a single speech-bubble S mark. The full specification lives in `DESIGN.md`.
- No country/flag icons in language selection.
- No third-party translation-platform logos in the bubble.
- No "open in browser" affordance in the bubble.

## Evidence on Hand

- `DESIGN.md` — reusable brand and bubble design tokens.
- Running implementation: `src/components/BubblePage.svelte`, `src/style.css`, `bubbles.go`.

## Product Principles

1. The result is the interface: translation text and copy actions outrank chrome.
2. Comparison is the feature: cards share one grammar so services are scanned, not deciphered.
3. Stay out of the way: open fast, hide on blur, never steal more attention than the task.
4. Local and private by default: keys stay in the OS keyring; no telemetry.
5. Keyboard first: every frequent action is reachable without the mouse.

## Accessibility & Inclusion

- Maintain WCAG AA contrast in both themes, including muted action icons.
- Visible keyboard focus on every control.
- Respect `prefers-reduced-motion`; motion only communicates state.
- Chinese and English text must remain readable at 12–15 px in a compact desktop panel.
