<p align="center">
  <img src="resources/icon.png" width="96" height="96" alt="Sotto 标志">
</p>

<h1 align="center">Sotto</h1>

<p align="center">
  <a href="README.md">English</a> · <strong>简体中文</strong>
</p>

Sotto 是一款 Windows 桌面翻译工具。选中文字、框选屏幕区域，或在翻译面板输入文本，即可在当前应用旁边查看译文。

[下载 Windows 版](https://github.com/doyuli/sotto/releases/latest)。x64 电脑请选择 `amd64` 安装器，Windows on ARM 电脑请选择 `arm64` 安装器。

## 功能

- 在同一个弹窗里**对比多个译文**，支持 Google 翻译、DeepL 和 OpenAI 兼容接口等翻译服务。
- **在本机识别截图中的文字**，再交给你选择的翻译服务。
- **固定翻译弹窗**，在其他应用里继续查阅译文。
- **按代码命名格式复制英文译文**，支持 `camelCase`、`PascalCase`、`snake_case`、`CONSTANT_CASE` 和 `kebab-case`。

## 快捷键

| 功能 | 默认快捷键 |
| --- | --- |
| 打开翻译面板 | `Ctrl+Alt+A` |
| 截图翻译（OCR） | `Ctrl+Alt+S` |
| 划词翻译 | `Ctrl+Alt+D` |

在面板里按 `Enter` 翻译，按 `Shift+Enter` 换行，按 `Esc` 关闭。

从系统托盘打开「设置」，可以修改快捷键和选择翻译服务。对于需要 API 密钥的服务，Sotto 会将密钥保存在 Windows 凭据管理器中。

## 从源码运行

在 Windows 上准备 Go、Bun 和 WebView2 Runtime。所需的 Go 版本见 [go.mod](go.mod)。

```bash
bun install
bun run dev
```

运行 `bun run build` 可构建当前 Windows 架构的版本，运行 `bun run build:win` 则同时构建 x64 和 ARM64。产物位于 `build/`。

## 致谢

感谢 [Easydict](https://github.com/tisfeng/Easydict) 的作者和贡献者为 Sotto 带来的启发。
