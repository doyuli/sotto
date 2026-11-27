<p align="center">
  <img src="resources/icon.png" width="88" height="88" alt="Sotto logo">
</p>

<h1 align="center">Sotto</h1>

<p align="center">
  <strong>Select, snip, compare side by side — a lightweight native translator for Windows &amp; macOS</strong>
</p>

<p align="center">
  <strong>English</strong> · <a href="README_CN.md">简体中文</a>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/Platform-Windows%2010%2B%20%7C%20macOS%2014%2B-blue" alt="Platform">
  <img src="https://img.shields.io/badge/Pure%20Go-No%20Electron%20%2F%20WebView-00ADD8" alt="Pure Go Native">
  <img src="https://img.shields.io/badge/Privacy-Zero%20Telemetry-success" alt="Zero Telemetry">
  <img src="https://img.shields.io/badge/License-GPL--3.0-lightgrey" alt="License">
</p>

<p align="center">
  Non-intrusive Selection · Offline Screen OCR · Streaming Multi-Engine Comparison · Minimal Overlays · Identifier Naming Styles
</p>

<p align="center">
  <img src="docs/modes-preview.png" width="100%" alt="Sotto Bubble Mode and Minimal Translation Mode preview">
</p>

---

## Features

### 1. Dual Modes: Deep Comparison or Quiet Reading
- **Bubble Mode**: Query multiple translation engines concurrently with live streaming results; fold or retry individual cards, pin the window in place, and play native system TTS.
- **Minimal Translation Mode**: Display selection and snip results as compact, text-only floating overlays right beside your target region. Multiple results coexist independently with smooth scrolling and drag-to-reposition, keeping reading and side-by-side work distraction-free—while the input shortcut still summons the full bubble anytime.

### 2. Crafted for Code & Daily Reading
- **Identifier Naming Styles**: Copy English translations with one click into `camelCase`, `PascalCase`, `snake_case`, `CONSTANT_CASE`, or `kebab-case`.
- **Smart Text Preprocessing**: Optionally auto-split `camelCase`/`snake_case` identifiers, strip code comments and Markdown syntax, or flatten line breaks from PDFs and docs before translating.
- **Searchable Local History**: Search saved originals and translations instantly, navigate with keyboard shortcuts to copy on `Enter`, play TTS, or re-translate with other engines.

### 3. Native, Lightweight & Private by Default
- **Pure-Go Native UI without WebView**: Built with `CGO_ENABLED=0` (excludes WebView2 on Windows; runs as a menu-bar accessory with no Dock icon on macOS). Non-activating result overlays stay out of your focus and dismiss automatically on blur.
- **Clipboard-Friendly Selection**: Reads selected text via OS accessibility APIs (Windows UIA / macOS AX) first, preserving your existing clipboard whenever possible.
- **Offline Screen OCR & Dictionary**: Windows embeds PP-OCR (with WinRT fallback) and macOS uses native Apple Vision—screenshots never leave your machine. A bundled offline dictionary gives instant phonetics and Chinese definitions for single English words.
- **Zero Telemetry & OS Keyring**: Zero analytics or tracking; API keys live securely in Windows Credential Manager or macOS Keychain.

---

## Translation Engines

Toggle engines, drag to reorder cards, or configure custom LLM endpoints right inside **Settings**:

| Category | Supported Services | Details |
| :--- | :--- | :--- |
| **Keyless · Ready Out of the Box** | **Offline Dictionary**, Youdao Dictionary & Translation, Bilibili (Index), Google Translate, **Magpie** | Offline dictionary works without network; Magpie auto-discovers local gateway models and routes |
| **AI LLMs** | **OpenAI-Compatible**, SiliconFlow, Zhipu AI | Streaming responses with custom Base URLs and models (DeepSeek, Qwen, GLM, etc.) |
| **Machine Translation APIs** | **DeepL** (Free / Pro), Volcengine (ByteDance), Baidu Translate, Tencent Cloud (TMT), Youdao, Microsoft Azure | Bring your own API keys for fast, reliable side-by-side comparison |

> [!NOTE]
> Dictionary lookups and screen OCR run entirely locally. Online translation text is sent only to the providers you explicitly enable.

---

## Shortcuts & Usage

All global shortcuts can be customized in **Settings → Shortcuts**:

| Action | Windows Default | macOS Default | Description |
| :--- | :--- | :--- | :--- |
| **Open Translation Panel** | `Ctrl + Alt + A` | `⌥ Option + A` | Toggle the input bubble (`Enter` to translate, `Shift + Enter` for newline) |
| **Screen Snip Translation** | `Ctrl + Alt + S` | `⌥ Option + S` | Capture any screen region for local OCR and instant translation |
| **Selection Translation** | `Ctrl + Alt + D` | `⌥ Option + D` | Read highlighted text in the active app and pop up results beside it |
| **Toggle Minimal Mode** | `Ctrl + Alt + F` | `⌥ Option + F` | Switch future selection/snip results between full Bubble and text-only Overlay |
| **Open Translation History** | `Ctrl + Alt + H` | `⌥ Option + H` | Toggle **翻译历史** to search, copy, or re-translate past entries |

- **Bubble Window**: Automatically hides on blur, or click the pin icon to keep it visible; press `Esc` to close.
- **Minimal Overlay**: Drag anywhere to move, scroll smoothly through long results, and press `Esc` or right-click to dismiss.

---

## Download & Installation

Download the package for your platform from [GitHub Releases](https://github.com/doyuli/sotto/releases/latest):

| OS | Architecture | Package |
| :--- | :--- | :--- |
| **Windows 10 / 11** | x64 (`x86_64`) | `sotto-setup-<version>-amd64.exe` |
| **Windows 10 / 11** | ARM64 (`arm64`) | `sotto-setup-<version>-arm64.exe` |
| **macOS 14+** | Apple Silicon (`arm64`) | `sotto-<version>-arm64.dmg` |

<details>
<summary><strong>macOS First-Launch Guide (Gatekeeper & Permissions)</strong></summary>

1. **Gatekeeper Approval**: The macOS build uses ad-hoc signing. After dragging Sotto to `Applications`, if Gatekeeper blocks the first launch, open **System Settings → Privacy & Security** and click **"Open Anyway"** ([Apple Support](https://support.apple.com/en-us/102445)).
2. **Permissions**: Selection translation requires **Accessibility** permission; screen snip OCR requires **Screen Recording** permission.

</details>

---

## Development & Building

Requires [Go](https://go.dev/) >= 1.27.3 and [Bun](https://bun.sh/):

```bash
bun install                 # Install MyGo CLI toolchain
bun run dev                 # Run desktop app with hot reload
CGO_ENABLED=0 go test ./... # Run unit tests and UI regression cases
bunx mygo vet .             # Check MyGo UI usage and lifetimes
```

<details>
<summary><strong>Release Builds & E2E Testing</strong></summary>

```bash
bun run build:win   # Build Windows x64/arm64 executables, OCR assets, and NSIS installers
bun run build:mac   # Build Apple Silicon .app bundle and DMG (run on macOS)
```

- For single-architecture Windows builds, use `bun run build:win:x64` or `bun run build:win:arm64` (add `-skip-installer` to skip NSIS packaging). For local macOS `.app` verification, run `bun run build:mac -skip-dmg -skip-notarize`.
- See [e2e/README.md](e2e/README.md) for platform probes and desktop acceptance tests.

</details>

---

## Acknowledgments

- [Easydict](https://github.com/tisfeng/Easydict): For inspiring Sotto's multi-engine comparison design and user experience.
- [Mygo](https://mygo.egoist.dev/): The lightweight, modern Go desktop UI framework powering Sotto.
- [LINUX DO](https://linux.do/): For the community's support of Sotto.

---

## License

GPL-3.0-only. See [LICENSE](LICENSE).
