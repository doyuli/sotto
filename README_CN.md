<p align="center">
  <img src="resources/icon.png" width="96" height="96" alt="Sotto logo">
</p>

<h1 align="center">Sotto</h1>

<p align="center">
  <strong>专为 Windows 与 macOS 用户及开发者打造的轻量、无感、高效桌面翻译工具</strong>
</p>

<p align="center">
  <a href="README.md">English</a> · <strong>简体中文</strong>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/Platform-Windows%2010%2B%20%7C%20macOS%2014%2B-blue" alt="Platform">
  <img src="https://img.shields.io/badge/Arch-x64%20%7C%20arm64-brightgreen" alt="Architecture">
  <img src="https://img.shields.io/badge/Go-1.27.3%2B-00ADD8?logo=go" alt="Go Version">
  <img src="https://img.shields.io/badge/CGO-Disabled-orange" alt="Pure Go">
  <img src="https://img.shields.io/badge/Privacy-Zero%20Telemetry-success" alt="Zero Telemetry">
</p>

---

## 为什么选择 Sotto？

在查阅多语言文档、浏览外文资料或编写代码时，频繁切换翻译网页或庞大的翻译软件不仅打断思路，还伴随着剪贴板污染、隐私泄漏和单一引擎翻译不准确的困扰。

**Sotto**（意为轻声、私语）致力于成为桌面端始终就绪、不惹人注目的翻译助手。通过**无感划词**、**离线截图 OCR** 或**快捷键输入**，Sotto 会在当前工作窗口旁弹出轻量气泡，同时并发请求多个翻译源并流式呈现，译完即用，失焦即收，让你始终专注于当前任务。

---

## 核心特性

- **双模式按需切换**：可在「设置」中开启「简约翻译模式」，划词与截图仅悬浮纯译文，输入框快捷键仍随时呼出完整气泡。
- **本地翻译历史**：支持按原文或译文检索历史记录，一键复制结果或命名格式，并支持换源重译。
- **多引擎流式对比**：一次触发，多个翻译源并发查询，流式呈现，支持独立折叠、复制与重试。
- **划词翻译**：无需切换应用，尽量保留原有剪贴板内容。需要复制取词时，下次粘贴可能得到选中的文字。
- **离线截图 OCR**：框选屏幕即刻本地识别并翻译，Windows 内置 PP-OCR 与 WinRT OCR，macOS 原生 Vision 识别，图片无需上传网络。
- **变量命名转换**：一键将英文释义转换为 `camelCase`、`PascalCase`、`snake_case`、`CONSTANT_CASE`、`kebab-case` 代码命名格式。
- **凭据保护与零遥测**：零埋点，API 密钥由系统凭据管理器或钥匙串保护。
- **轻量原生体验**：纯 Go 后端（`CGO_ENABLED=0`）搭配原生窗口；无边框设计跟随系统深浅色主题，失焦自动收起，亦可钉住常驻。

> **全能气泡 vs 沉浸简约**：Sotto 提供两种互补的呈现方式，兼顾多引擎深度对照与无打扰沉浸阅读。

<p align="center">
  <img src="docs/modes-preview.png" width="100%" alt="Sotto 气泡模式与简约翻译模式对比预览">
</p>

---

## 快捷键与操作

可在「设置 → 快捷键」自定义以下全局快捷键。

| 功能 | Windows 默认 | macOS 默认 | 说明 |
| :--- | :--- | :--- | :--- |
| **打开翻译面板** | `Ctrl + Alt + A` | `⌥ Option + A` | 呼出输入翻译面板；已打开时再次按下关闭 |
| **屏幕截图翻译 (OCR)** | `Ctrl + Alt + S` | `⌥ Option + S` | 框选屏幕任意区域，完全本地识别文字并即时发起翻译 |
| **选中文本翻译 (划词)** | `Ctrl + Alt + D` | `⌥ Option + D` | 自动读取当前前台应用中选中的文本并在光标旁弹出气泡 |
| **切换简约翻译模式** | `Ctrl + Alt + F` | `⌥ Option + F` | 切换后续划词和截图的显示模式。已有译文留在原位，进行中的翻译按原模式完成。 |
| **打开翻译历史** | `Ctrl + Alt + H` | `⌥ Option + H` | 打开或再次按下关闭「翻译历史」，也可从托盘或菜单栏进入 |

### 交互与快捷控制
- **气泡模式**：输入框内按 `Enter` 立即翻译，`Shift + Enter` 换行，`Esc` 关闭；点击图钉图标可固定窗口防止失焦收起；支持原文与译文本地语音朗读，单张卡片可独立折叠或重试。
- **简约翻译模式**：划词或截图后在目标区域旁浮现纯译文浮窗，多个结果可独立并存；按住任意位置即可拖拽移动，长文本支持内部平滑滚动；按 `Esc` 或鼠标右键关闭，输入面板快捷键仍可随时呼出完整气泡。
- **翻译历史**：支持搜索原文与译文，用 `↑` / `↓` 切换条目，按 `Enter` 复制首条译文并关闭，按 `Esc` 或点击窗外关闭；支持复制原文、译文与命名格式、语音朗读及换源重译，保留期限可在「设置」中配置。

---

## 支持的翻译服务

可在系统托盘进入「设置」页面，自由勾选启用引擎、拖拽调整卡片展示顺序，或配置各服务 API 密钥与自定义模型：

| 引擎 / 服务 | 类型 | 说明与特性 |
| :--- | :--- | :--- |
| **Bilibili (Index)** | 免密钥 | 实验性接口，默认开启 |
| **离线词典** | 免密钥 | 查询单个英文词的音标和中文释义 |
| **Google 翻译** | 免密钥 | 默认关闭，可在设置中启用 |
| **有道查词与翻译** | 免密钥 | 网页翻译，可在完整气泡中查询单个英文词的中文释义 |
| **DeepL** | API 密钥 | 业界领先的翻译质量，支持 DeepL Free 与 Pro Key |
| **OpenAI 兼容接口** | API 密钥 | 支持自定义 Base URL 与模型名（轻松接入 DeepSeek、Moonshot、通义千问等） |
| **硅基流动 (SiliconFlow)** | API 密钥 | 聚合多款优秀开源大模型，默认搭载 `tencent/Hunyuan-MT-7B` |
| **智谱 AI (Zhipu)** | API 密钥 | 支持智谱开放平台模型，默认搭载 `glm-4.7-flash` |
| **火山引擎 (ByteDance)** | API 密钥 | 字节跳动机器翻译大模型与高效文本翻译 |
| **百度翻译** | API 密钥 | 百度翻译开放平台通用文本翻译接口 |
| **腾讯云 (TMT)** | API 密钥 | 腾讯机器翻译服务 |
| **网易有道** | API 密钥 | 有道智云专业翻译接口 |
| **微软 Azure** | API 密钥 | Azure 认知服务文本翻译 |

> [!NOTE]
> 翻译文本会发送给选中的第三方服务，词典查询在本地完成。部分免密钥接口属于实验性网络端点。服务可用性、免费额度和计费以各服务商的最新政策为准。

---

## 下载与安装

请前往 [GitHub Releases 页面](https://github.com/doyuli/sotto/releases/latest) 下载对应系统与硬件架构的安装包：

| 操作系统 | 适用硬件与架构 | 安装包文件 | 说明 |
| :--- | :--- | :--- | :--- |
| **Windows 10 / 11** | 64 位 Intel / AMD (x86_64) | `sotto-setup-<version>-amd64.exe` | 适用于普通 Windows PC |
| **Windows 10 / 11** | 64 位 ARM (arm64) | `sotto-setup-<version>-arm64.exe` | 适用于 Surface Pro、高通 Snapdragon X 等设备 |
| **macOS 14+** | Apple Silicon (M 系列芯片 / arm64) | `sotto-<version>-arm64.dmg` | 常驻菜单栏无 Dock 图标，原生 Vision 识别 |

### macOS 首次使用提示
1. **安全性放行**：目前 macOS 软件包采用 ad-hoc 签名。将应用拖入 `Applications` 首次打开时，若系统拦截提示，请在 **系统设置 → 隐私与安全性** 中点击 **“仍要打开”**（详情参考 [Apple 官方支持说明](https://support.apple.com/zh-cn/102445)）。
2. **系统权限**：划词翻译需授予 **辅助功能 (Accessibility)** 权限，截图识别需授予 **屏幕录制 (Screen Recording)** 权限。

---

## 本地开发与构建

### 环境准备
- [Go](https://go.dev/) >= 1.27.3
- [Bun](https://bun.sh/)（用于运行 MyGo CLI 工具链）

### 开发命令

```bash
# 1. 安装项目工具链依赖
bun install

# 2. 启动原生桌面开发环境（支持热重载）
bun run dev

# 3. 运行项目单元测试
CGO_ENABLED=0 go test ./...

# 4. 检查 mygo/ui 原生组件用法合规性
bunx mygo vet .

# 5. 构建受支持平台的发布产物
bun run build:win      # 构建 Windows x64 和 arm64 的可执行文件、OCR 资源及 NSIS 安装包
bun run build:mac      # 构建 Apple 芯片版 .app 和 DMG（在 macOS 上运行）
```

Windows 打包脚本通过 `mygo_nowebview` 排除 WebView2。只构建一种架构时，运行 `bun run build:win:x64` 或 `bun run build:win:arm64`。加上 `-skip-installer` 可只生成可执行文件和运行所需的资源。在 macOS 上可用 `bun run build:mac -skip-dmg -skip-notarize` 构建并签署用于本地验证的 `.app`。

> 自动化测试与系统平台端到端验收指南请参阅 [e2e/README.md](e2e/README.md)。

---

## 致谢

- [Easydict](https://github.com/tisfeng/Easydict)：为 Sotto 的设计交互与多服务对比体验提供了灵感与范式。
- [Mygo](https://mygo.egoist.dev/)：提供了轻量优雅的 Go 桌面 UI 应用框架支持。
- [LINUX DO](https://linux.do/)：社区为 Sotto 提供了支持。

---

## 开源许可

GPL-3.0-only。详见 [LICENSE](LICENSE)。
