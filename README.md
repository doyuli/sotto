# Sotto

Sotto 是一款 Windows 桌面翻译工具。选中文字、框选屏幕区域，或直接输入文本，即可在当前应用旁边查看译文。

## 功能

- 在同一个弹窗里**对比多个译文**，支持 Google、DeepL 和 OpenAI 兼容接口等翻译服务。
- **固定翻译弹窗**，在其他应用里继续查阅译文。
- **按代码命名格式复制英文译文**，支持 `camelCase`、`PascalCase`、`snake_case`、`CONSTANT_CASE` 和 `kebab-case`。

## 快捷键

| 功能 | 默认快捷键 |
| --- | --- |
| 输入翻译 | `Ctrl+Alt+A` |
| 截图翻译（OCR） | `Ctrl+Alt+S` |
| 划词翻译 | `Ctrl+Alt+D` |

从系统托盘打开「设置」，可以修改快捷键、选择翻译服务并配置 API 密钥。

## 从源码运行

在 Windows 上准备 Go（版本见 [go.mod](go.mod)）、Bun 和 WebView2 Runtime。

```bash
bun install
bun run dev
```

运行 `bun run build` 可将可执行文件构建到 `build/`。

## 致谢

感谢 [Easydict](https://github.com/tisfeng/Easydict) 的作者和贡献者为桌面翻译工具带来的启发。
