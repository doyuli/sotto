# Sotto — Agent 工作规则

翻译工具 app：mygo v0.7.4（Go 后端 + WebView2 web 前端），Windows 优先，纯 Go 无 cgo。
产品定位见 `PRODUCT.md`，品牌与视觉规范见 `DESIGN.md`；mygo 的 API 以本地源码为唯一事实来源。

## 开发循环

```bash
export PATH="/c/Program Files/Go/bin:$PATH"   # go 不在 Git Bash 默认 PATH
bun run dev            # 开发运行（vite:5173 + Go + WebView2，热重载）
bunx mygo generate     # 改了 Go 绑定服务/事件后必须重新生成 src/mygo.ts
bun run typecheck      # svelte-check（Svelte + TypeScript）
CGO_ENABLED=0 go test ./...
bunx mygo vet .
bun run build          # 生产构建（类型检查 + Vite + 嵌入 + 出包）
CGO_ENABLED=0 go test -tags e2e ./e2e/ -v   # e2e 冒烟（需先 build，需桌面会话，见 e2e/README.md）
```

## 硬规则

1. **不臆造 mygo API**。以本地源码为准：`go list -m -f '{{.Dir}}' github.com/egoist/mygo`
   （模块缓存 `mygo@v0.7.4`）。窗口/事件/剪贴板/热键的常用签名先读该目录的
   `window.go`、`ipc.go`、`clipboard.go`、`modules.go`，不要凭记忆写。
2. **依赖白名单**：`egoist/mygo`、`golang.org/x/sys`、`golang.org/x/time`、`go-ole/go-ole`。其余一律标准库；需要新依赖先上报，不要自行添加。
3. **CGO_ENABLED=0 恒定**；平台代码放 `*_windows.go`（`//go:build windows`）配 `!windows` 桩。
4. **线程模型**：
   - GlobalShortcut/托盘/菜单回调在**主线程**：只做派发，禁止阻塞；耗时工作 `go` 出去。
   - `Window` 的方法（Show/Hide/SetPosition…）内部自动 marshal，goroutine 可安全调用。
   - `Event.Emit` 任意 goroutine 可调；但 DOM 未就绪只暂存有限条——初始数据用"页面 mount 主动拉"（`Ready`），事件只做增量。
   - `Window.Update` 是 native UI 专用，本项目禁用。
   - UIA/COM 调用只在专用 MTA 线程（见 internal/selection/uia_windows.go）。
5. **改 Go 绑定/事件后必须** `bunx mygo generate` → `bun run typecheck`，否则前端调不到新方法。
6. **纯逻辑包必须有单测**（config/langdetect/naming/translate）；平台包（selection/capture/ocr）用手工清单验收。
7. **剪贴板纪律**：模拟复制产生的任何写入必须带三个排除伪格式（见 internal/selection/clipboard_windows.go）；禁用剪贴板监听做划词入口。气泡外部点击检测走 `WH_MOUSE_LL` 鼠标钩子（internal/selection/hook_windows.go），回调必须立即返回。
8. **错误处理**：`fmt.Errorf("...: %w", err)` 包装；服务错误映射为用户可读中文文案（见 internal/translate 的 UserMessage）。
9. 注释对齐现有代码（英文、解释 why）；UI 文案用中文。
10. 遇到 API 行为与文档冲突或预期外的平台异常：记录现象与源码出处，停下上报，不要绕过。
11. **托盘常驻与退出**（mygo 隐蔽坑）：`OnWindowAllClosed` 空监听保活；但退出序列逐个关窗，**任何一个窗口 `OnClose` 里 `PreventDefault` 都会中止整个退出**。本项目在 `OnBeforeQuit` 置 `quitting` 标志，气泡 `OnClose` 见标志即放行——改窗口关闭逻辑时必须保留这条路径。

## Git 约定

- 提交信息遵循 Conventional Commits 规范（`<type>[optional scope]: <description>`），保持项目一致性。
- 不提交生成物以外的杂物；`node_modules/ dist/ build/` 已忽略。
- 提交前跑验证命令（见上）。

## 项目结构速览

```
main.go / app.go / bubbles.go / services.go / events.go   # 应用组装与绑定面
internal/config    # Settings + 原子读写（key 不进 JSON，走 mygo.Secrets）
internal/keys      # Secrets 封装
internal/langdetect# Unicode 脚本粗判（纯函数）
internal/naming    # 标识符拆词 + 5 种命名风格（纯函数）
internal/translate # Translator 接口 + fan-out + deepl/openai/baidu/tencent/youdao/azure/google/dictionary
internal/selection # 划词：UIA 主路径 + Ctrl+C 兜底；气泡外部点击检测
internal/capture   # GDI 截屏
internal/ocr       # PP-OCR（ONNX Runtime 直调）+ Windows.Media.Ocr 兜底
src/               # 前端（Svelte 5 多入口）：bubble.html / settings.html / snip.html
```
