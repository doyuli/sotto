# Sotto

Sotto 是一款 Windows 桌面翻译工具：划词、截图或输入文字后，即可在同一个弹窗中对比多个翻译服务的译文，无需切换应用。

**目前仅适配 Windows。** 如果你用的是 macOS，推荐试试 [Easydict](https://github.com/tisfeng/Easydict)，用起来可能更顺手。

## 快捷键

| 功能 | 默认快捷键 |
| --- | --- |
| 输入翻译 | `Ctrl+Alt+A` |
| 截图翻译（OCR） | `Ctrl+Alt+S` |
| 划词翻译 | `Ctrl+Alt+D` |

在托盘设置中可以更改快捷键，启用 DeepL、Google、OpenAI 兼容接口等翻译服务，并为需要凭据的服务配置 API 密钥。英文译文也可以按 `camelCase`、`snake_case` 等命名格式复制。

## 从源码运行

在 Windows 上准备 Go（版本见 [go.mod](go.mod)）、Bun 和 WebView2 Runtime。

```bash
bun install
bun run dev
```

运行 `bun run build` 可将可执行文件构建到 `build/`。

## 致谢

感谢 [Easydict](https://github.com/tisfeng/Easydict) 的作者和贡献者为桌面翻译工具带来的启发。
