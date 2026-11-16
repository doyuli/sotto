<p align="center">
  <img src="resources/icon.png" width="96" height="96" alt="Sotto logo">
</p>

<h1 align="center">Sotto</h1>

<p align="center">
  <strong>A lightweight, unobtrusive desktop translator for Windows and macOS users and developers</strong>
</p>

<p align="center">
  <strong>English</strong> · <a href="README_CN.md">简体中文</a>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/Platform-Windows%2010%2B%20%7C%20macOS%2014%2B-blue" alt="Platform">
  <img src="https://img.shields.io/badge/Arch-x64%20%7C%20arm64-brightgreen" alt="Architecture">
  <img src="https://img.shields.io/badge/Go-1.27.3%2B-00ADD8?logo=go" alt="Go Version">
  <img src="https://img.shields.io/badge/CGO-Disabled-orange" alt="Pure Go">
  <img src="https://img.shields.io/badge/Privacy-Zero%20Telemetry-success" alt="Zero Telemetry">
</p>

<p align="center">
  <img src="docs/banner.png" width="100%" alt="Sotto interface preview">
</p>

---

## Why Sotto?

When reading documentation, researching foreign language resources, or coding, constantly switching between browser tabs or heavy translation apps interrupts your flow. It often leads to clipboard pollution, privacy concerns, and frustration with single-engine inaccuracy.

**Sotto** (meaning *in an undertone* or *whisper*) is designed to be an always-available, silent translation companion right on your desktop. Whether triggered by **non-intrusive text selection**, **offline screen snip OCR**, or a **global shortcut**, Sotto pops up an elegant floating bubble beside your active window, queries multiple translation engines in parallel, and streams results in real time. It hides automatically on blur, keeping you entirely focused on your work.

---

## Features

- **Concurrent Multi-Engine Comparison**: Query multiple translation sources at once with progressive streaming results; fold, copy, or retry cards independently.
- **Non-Intrusive Selection & Clipboard Protection**: Windows reads text via UIA and macOS via Accessibility without stealing focus; simulated copy automatically and fully restores the clipboard with history exclusion markers attached.
- **Offline Screen OCR**: Snip any screen region for instant local recognition and translation; Windows embeds PP-OCR and WinRT OCR, macOS uses native Vision, and images are never uploaded to the cloud.
- **Identifier Naming Styles**: One-click copy for English translations into `camelCase`, `PascalCase`, `snake_case`, `CONSTANT_CASE`, or `kebab-case`.
- **Credential Protection & Zero Telemetry**: Zero tracking, with API keys protected by the system credential manager or keychain.
- **Lightweight & Native**: Pure Go backend (`CGO_ENABLED=0`) with native `mygo/ui` windows; frameless design that respects system themes, hides on blur, or pins in place.

---

## Shortcuts & Operations

Sotto provides three instant entry points with fully customizable global shortcuts:

| Action | Windows Default | macOS Default | Description |
| :--- | :--- | :--- | :--- |
| **Open Translation Panel** | `Ctrl + Alt + A` | `⌥ Option + A` | Opens the typed translation panel; if already open, focuses it and preserves existing text |
| **Screen Snip Translation (OCR)** | `Ctrl + Alt + S` | `⌥ Option + S` | Snips any screen area to extract text locally and query translation engines |
| **Selection Translation** | `Ctrl + Alt + D` | `⌥ Option + D` | Reads highlighted text in the frontmost app and shows a popup bubble beside it |

### In-Bubble Controls
- **Keyboard navigation**: Press `Enter` to translate, `Shift + Enter` for a newline, and `Esc` to dismiss the bubble.
- **Floating controls**: Click the pin icon to keep the bubble open on blur; native text-to-speech pronunciation for source and translated text.
- **Card management**: Fold or expand individual engine cards, and retry any card that encounters an error.

---

## Supported Translation Services

Open **Settings** from the tray menu to toggle services, reorder cards via drag-and-drop, configure API keys, and customize model names:

| Service | Type | Details & Capabilities |
| :--- | :--- | :--- |
| **Bilibili (Index)** | Keyless | Experimental endpoint, enabled by default |
| **Online Dictionary** | Keyless | Phonetics, parts of speech, and English definitions for single words |
| **Google Translate** | Keyless | Disabled by default; toggle on in Settings |
| **DeepL** | API Key | Industry-leading translation quality; supports both DeepL Free and DeepL Pro keys |
| **OpenAI-Compatible** | API Key | Fully custom Base URL and model names (works seamlessly with DeepSeek, Moonshot, Qwen, etc.) |
| **SiliconFlow** | API Key | Aggregated open-source LLMs; defaults to `tencent/Hunyuan-MT-7B` |
| **Zhipu AI** | API Key | GLM family models; defaults to `glm-4.7-flash` |
| **Volcengine (ByteDance)** | API Key | High-performance machine translation by ByteDance |
| **Baidu Translate** | API Key | General text translation API |
| **Tencent Cloud (TMT)** | API Key | Tencent Cloud machine translation service |
| **Youdao** | API Key | NetEase Youdao translation API |
| **Microsoft Azure** | API Key | Azure Cognitive Services Translator |

> [!NOTE]
> Sotto sends text for translation and dictionary lookup to the respective third-party providers. Certain keyless integrations use experimental endpoints. Service availability, free quotas, and pricing follow each provider's latest terms.

---

## Download & Installation

Visit the [GitHub Releases page](https://github.com/doyuli/sotto/releases/latest) to download the package corresponding to your operating system and CPU architecture:

| Operating System | Architecture | Package Format | Notes |
| :--- | :--- | :--- | :--- |
| **Windows 10 / 11** | 64-bit Intel / AMD (x86_64) | `sotto-setup-<version>-amd64.exe` | Standard installer for x64 Windows PCs |
| **Windows 10 / 11** | 64-bit ARM (arm64) | `sotto-setup-<version>-arm64.exe` | For Surface Pro, Qualcomm Snapdragon X devices |
| **macOS 14+** | Apple Silicon (M-series / arm64) | `sotto-<version>-arm64.dmg` | Menu-bar accessory without Dock icon; native Vision OCR |

### macOS First-Launch Guide
1. **Gatekeeper Approval**: The macOS build uses ad-hoc signing. When opening Sotto for the first time after moving it to `Applications`, if prompted by Gatekeeper, navigate to **System Settings → Privacy & Security** and click **"Open Anyway"** ([Apple official guide](https://support.apple.com/en-us/102445)).
2. **Permissions**: Selection translation requires **Accessibility** permission; screen snip translation requires **Screen Recording** permission.

---

## Development & Building

### Prerequisites
- [Go](https://go.dev/) >= 1.27.3
- [Bun](https://bun.sh/) (for the MyGo CLI toolchain)

### Commands

```bash
# 1. Install toolchain dependencies
bun install

# 2. Start desktop development server (with hot reload)
bun run dev

# 3. Run unit tests
CGO_ENABLED=0 go test ./...

# 4. Check native UI usage compliance
bunx mygo vet .

# 5. Build executable for the current architecture
bun run build

# 6. Cross-platform builds
bun run build:win      # Builds both x64 and arm64 Windows installers
bun run build:mac      # Builds the macOS Apple Silicon (arm64) DMG
```

> For automated test suites, platform probes, and desktop acceptance testing, refer to [e2e/README.md](e2e/README.md).

---

## Acknowledgments

- [Easydict](https://github.com/tisfeng/Easydict): For inspiring Sotto's multi-engine comparison design and user experience.
- [Mygo](https://mygo.egoist.dev/): The lightweight, modern Go desktop UI framework powering Sotto.
