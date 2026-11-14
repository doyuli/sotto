# 验证入口

| 层级 | Windows | macOS |
| --- | --- | --- |
| 共享逻辑与原生 UI Tester | `CGO_ENABLED=0 go test ./...` | 同左 |
| 原生引擎/隔离系统对象 | 显式启用 OCR、抓屏探针 | 显式启用 Vision、隔离 pasteboard、SCK 探针 |
| 桌面窗口、焦点、权限与多屏 | [Windows 验收](windows.md) | [macOS 验收](macos.md) |

Tester 不创建原生窗口；符号加载不等同于真实取词/抓屏。交叉编译不等同于平台测试执行。默认测试不合成输入、不修改 general pasteboard，也不进行真实屏幕捕获。

Windows 普通 CI 运行无交互测试、OCR preflight、e2e vet 和 DPI 初始化检查；macOS 普通 CI 运行无交互测试和 Vision smoke。完整桌面验收需要交互式会话、对应权限和硬件。

构建与验证命令中的环境变量示例使用 POSIX shell；PowerShell 使用 `$env:NAME="value"` 后单独运行命令。
