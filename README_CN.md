<p align="center">
  <img src="resources/icon.png" width="96" height="96" alt="Sotto 标志">
</p>

<h1 align="center">Sotto</h1>

<p align="center">
  <strong>专为 Windows 用户与开发者打造的轻量桌面翻译工具</strong>
</p>

<p align="center">
  <a href="README.md">English</a> · <strong>简体中文</strong>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/Platform-Windows%2010%20%7C%2011-blue?logo=windows" alt="Platform">
  <img src="https://img.shields.io/badge/Arch-x64%20%7C%20arm64-brightgreen" alt="Architecture">
  <img src="https://img.shields.io/badge/Go-1.27.3%2B-00ADD8?logo=go" alt="Go">
  <img src="https://img.shields.io/badge/CGO-Disabled-orange" alt="Pure Go">
  <img src="https://img.shields.io/badge/Privacy-Zero%20Telemetry-success" alt="Zero Telemetry">
</p>

<p align="center">
  <img src="docs/banner.png" width="100%" alt="Sotto">
</p>

Sotto 是一款专注于流畅与克制的 Windows 桌面翻译工具。无论是在日常查阅文档、编写代码还是处理多语言内容时，只需选中文字、框选屏幕或按下快捷键呼出面板，多个翻译引擎的译文便会在当前应用旁以独立气泡并排呈现。

集多源流式对比、离线截图 OCR、无感划词保护剪贴板、代码变量命名转换于一体，无需频繁切换应用，助你始终专注当下工作。

[下载 Windows 最新版](https://github.com/doyuli/sotto/releases/latest) · 传统 PC（Intel / AMD）请下载 `amd64` 安装包，Surface Pro 等 ARM 设备请下载 `arm64` 安装包。

macOS 14+ 实验性支持可从源码构建，仅支持 Apple Silicon（arm64）。Mac 版常驻菜单栏、无 Dock 图标，通过辅助功能取词，使用 ScreenCaptureKit + Vision 本地截图识别。签名发布需配置并启用可选的 Mac 发布任务。

---

## 核心特性

- ⚡ **多引擎并发对比**：一次查询，多个翻译源同屏流式返回，先到先看；支持独立折叠与重试。
- 🖱️ **无感划词，不破剪贴板**：优先通过 UIA 读取选中文本；回退模拟复制时自动还原剪贴板，不污染剪贴板历史。
- 📷 **离线双引擎截图 OCR**：框选屏幕即刻本地识别翻译，内置 PP-OCR 与原生 WinRT OCR，不上传图片更私密。
- 💻 **变量命名一键转换**：一键将英文译文转换为 `camelCase`、`PascalCase`、`snake_case`、`CONSTANT_CASE`、`kebab-case` 命名格式。
- 🔒 **凭据安全与零遥测**：纯本地运行、零埋点；API 密钥存入 Windows 凭据管理器加密保护，不存明文配置。
- 🎨 **轻量克制，融入系统**：纯 Go 后端 + 原生 `mygo/ui` 窗口；无边框设计，跟随深浅主题，失焦收起或一键钉住常驻。

---

## 支持的翻译服务

| 服务 | 类型 | 说明 |
| --- | --- | --- |
| **Google 翻译** | 免密钥 | 可选的免密钥接口，默认关闭 |
| **Bilibili (Index)** | 免密钥 | 实验性接口，开箱即用 |
| **内置词典** | 免密钥 | 英文单词自动展示音标与释义 |
| **DeepL** | API Key | 优质翻译，支持 Free 与 Pro Key |
| **OpenAI 兼容** | API Key | 支持自定义 Base URL 与模型（DeepSeek、Moonshot、通义等） |
| **硅基流动** | API Key | 模型可自定义，默认 `tencent/Hunyuan-MT-7B`；需完成实名认证 |
| **智谱** | API Key | 模型可自定义，默认 `glm-4.7-flash` |
| **火山引擎** | API Key | 字节跳动机器翻译服务 |
| **百度翻译** | API Key | 通用文本翻译 |
| **腾讯云 (TMT)** | API Key | 机器翻译开放接口 |
| **网易有道** | API Key | 有道智云翻译接口 |
| **微软 Azure** | API Key | Cognitive Services 文本翻译 |

> 翻译与查词由第三方服务提供，相关文本会发送至对应服务商。部分免密钥接口为非官方或实验性接口；服务可用性、免费额度及价格以服务商最新规定为准。译文仅供参考。

> 在系统托盘菜单中打开「设置」，可启用/禁用服务、调整卡片排序、管理密钥与配置模型。模型留空恢复默认值。

---

## 快捷键与操作

| 功能 | Windows 默认快捷键 | 说明 |
| --- | --- | --- |
| **打开翻译面板** | `Ctrl+Alt+A` | 呼出翻译面板，已打开时聚焦并保留内容 |
| **截图翻译 (OCR)** | `Ctrl+Alt+S` | 框选屏幕任意区域，本地提取文字并翻译 |
| **选中文本翻译 (划词)** | `Ctrl+Alt+D` | 读取当前焦点程序中选中的文字并就近弹出气泡 |

- **Mac 默认快捷键**：`⌥A` 打开面板，`⌥S` 截图翻译，`⌥D` 划词翻译；可在设置中自定义，也支持 Command 组合键。
- **面板内操作**：`Enter` 翻译，`Shift+Enter` 换行，`Esc` 关闭。
- **更多交互**：支持点击 📌 钉住常驻、🔊 朗读原文/译文、单卡片折叠与出错重试。

---

## 系统要求与下载

- **操作系统**：Windows 10 / Windows 11
- **硬件架构**：
  - 64 位 x86 电脑（Intel / AMD）：下载 `sotto-setup-<version>-amd64.exe`
  - 64 位 ARM 电脑（如 Surface Pro / Snapdragon X）：下载 `sotto-setup-<version>-arm64.exe`

👉 [前往 GitHub Releases 页面下载最新版本](https://github.com/doyuli/sotto/releases/latest)

---

## 开发与构建

需准备 Go（>= 1.27.3）和 Bun。MyGo 模块与 `mygo-cli` 均固定为 0.8.3。

根目录 `main.go` 嵌入应用图标并调用 `internal/app.Run`。生命周期、快捷键、服务组装和入口任务编排统一放在 `internal/app`；原生 UI 组件和平台实现继续由现有包负责。

```bash
bun install                  # 安装依赖
bun run dev                  # 开发运行（热重载）
CGO_ENABLED=0 go test ./...   # 单元测试
bunx mygo vet .              # 检查原生 UI 用法
bun run build                # 构建当前架构安装包
bun run build:win            # 同时构建 x64 与 arm64 安装包
bun run build:mac            # 在 Mac 上构建 Apple Silicon（arm64）应用
```

> 原生 UI 测试位于 `internal/ui/{bubble,settings,snip}`，使用 `ui.NewTester`。Windows 集成测试与桌面手工验收见 [e2e/README.md](e2e/README.md)。默认单测不会运行需显式启用的 OCR 运行时探针和桌面 e2e。

---

## 致谢

感谢 [Easydict](https://github.com/tisfeng/Easydict) 为 Sotto 带来的灵感与启发。

感谢 [Mygo](https://mygo.egoist.dev/) 为 Sotto 提供应用开发框架。
