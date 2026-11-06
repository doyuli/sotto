<p align="center">
  <img src="resources/icon.png" width="96" height="96" alt="Sotto logo">
</p>

<h1 align="center">Sotto</h1>

<p align="center">
  <strong>English</strong> · <a href="README_CN.md">简体中文</a>
</p>

Sotto is a desktop translator for Windows. Select text, capture part of your screen, or type into the translation panel to see results beside the app you’re using.

[Download for Windows](https://github.com/doyuli/sotto/releases/latest). Choose the `amd64` installer for x64 PCs or `arm64` for Windows on ARM. The app interface is in Simplified Chinese.

## Features

- **Compare translations** from services such as Google Translate, DeepL, and OpenAI-compatible endpoints in one popup.
- **Extract text from screenshots** with on-device OCR, then translate it with your selected services.
- **Pin the popup** to keep translations visible while you work in other apps.
- **Copy English translations as code identifiers** in `camelCase`, `PascalCase`, `snake_case`, `CONSTANT_CASE`, or `kebab-case`.

## Shortcuts

| Action | Default shortcut |
| --- | --- |
| Open the translation panel | `Ctrl+Alt+A` |
| Translate a screen region (OCR) | `Ctrl+Alt+S` |
| Translate selected text | `Ctrl+Alt+D` |

In the panel, press `Enter` to translate, `Shift+Enter` to insert a line break, and `Esc` to close it.

Open **Settings** (设置) from the system tray to change shortcuts and select translation services. For services that require an API key, Sotto stores the key in Windows Credential Manager.

## Development

On Windows, install Go, Bun, and the WebView2 Runtime. See [go.mod](go.mod) for the required Go version.

```bash
bun install
bun run dev
```

Run `bun run build` for your current Windows architecture, or `bun run build:win` for both x64 and ARM64. Build output goes to `build/`.

## Acknowledgments

Thanks to the authors and contributors of [Easydict](https://github.com/tisfeng/Easydict) for inspiring Sotto.
