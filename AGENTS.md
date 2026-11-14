# Sotto — Agent 工作规则

翻译工具 app：mygo v0.8.3（Go 后端 + 原生截图遮罩 + 原生 `mygo/ui` 气泡与设置），Windows 优先，纯 Go 无 cgo。
产品定位见 `PRODUCT.md`，品牌与视觉规范见 `DESIGN.md`；mygo 的 API 以本地源码为唯一事实来源。

## 开发循环

```bash
export PATH="/c/Program Files/Go/bin:$PATH"   # 仅 Windows Git Bash，Go 不在默认 PATH 时使用
bun run dev            # 开发运行（Go + 原生 UI）
CGO_ENABLED=0 go test ./...
bunx mygo vet .
bun run build          # 生产构建（编译 + 出包）
# Windows：生产包与桌面会话
CGO_ENABLED=0 go test -tags e2e ./e2e/ -v
# macOS：不制作 DMG/公证；桌面探针须交互式会话
bun run build:mac -skip-dmg -skip-notarize
CGO_ENABLED=0 SOTTO_DESKTOP=1 go test -tags e2e ./e2e/ -v
```

## 硬规则

1. **不臆造 mygo API**。以本地源码为准：`go list -m -f '{{.Dir}}' github.com/egoist/mygo`
   （模块缓存 `mygo@v0.8.3`）。窗口/事件/剪贴板/热键的常用签名先读该目录的
   `window.go`、`ipc.go`、`clipboard.go`、`modules.go`，不要凭记忆写。
2. **依赖白名单**：`egoist/mygo`、`ebitengine/purego`（含 `purego/objc`）、`golang.org/x/sys`、`golang.org/x/time`、`go-ole/go-ole`。其余一律标准库；需要新依赖先上报，不要自行添加。macOS 系统能力由本项目直接桥接，不修改 mygo。
3. **CGO_ENABLED=0 恒定**；平台代码放 `*_windows.go` / `*_darwin.go`，未实现的平台保留对应 build tag 的桩。
4. **线程模型**：
   - GlobalShortcut/托盘/菜单回调在**主线程**：只做派发，禁止阻塞；耗时工作 `go` 出去。
   - `Window` 的方法（Show/Hide/SetPosition…）内部自动 marshal，goroutine 可安全调用。
   - `Window.Invalidate()` / `applyState` 用于向原生 `ui` 视图异步发布后台状态；原生视图状态归 UI 线程（或互斥保护后触发 `Invalidate()`），绘制回调不做抓屏或 OCR。
   - UIA/COM 调用只在专用 MTA 线程（见 internal/selection/uia_windows.go）。
   - macOS 仅支持 Apple Silicon（arm64），常驻 Accessory，无 Dock 图标；交互窗口创建时使用 non-activating Panel。翻译结果用 `ShowInactive`，后台回包不激活或聚焦窗口。AppKit 操作回主线程，AX / Vision 在后台，跨异步调用持有的对象与 Block 必须显式管理生命周期。
5. **纯逻辑与原生 UI 的可回归行为必须有单测**（取舍见下文）；selection/capture/ocr 等平台原生行为另需实机验收，单测不能替代桌面验证。
7. **剪贴板纪律**：Windows 模拟复制产生的任何写入必须带三个排除伪格式（见 internal/selection/clipboard_windows.go）；macOS 按 item 保留全部可读取类型，所有权不明时保留恢复备份，不覆盖新内容。禁用剪贴板监听做划词入口。气泡外部点击检测使用 Windows `WH_MOUSE_LL` 或 macOS `NSEvent` monitor，回调必须立即返回。
8. **错误处理**：`fmt.Errorf("...: %w", err)` 包装；服务错误映射为用户可读中文文案（见 internal/translate 的 UserMessage）。
9. 注释对齐现有代码（英文、解释 why）；UI 文案用中文。
10. 遇到 API 行为与文档冲突或预期外的平台异常：记录现象与源码出处，停下上报，不要绕过。
11. **托盘常驻与退出**（mygo 隐蔽坑）：`OnWindowAllClosed` 空监听保活；但退出序列逐个关窗，**任何一个窗口 `OnClose` 里 `PreventDefault` 都会中止整个退出**。本项目在 `OnBeforeQuit` 置 `quitting` 标志，气泡 `OnClose` 见标志即放行——改窗口关闭逻辑时必须保留这条路径。
12. **测试同步维护**：改动功能时，同步更新相关测试，并清理失效用例、过时断言和未使用的测试辅助代码。

## 测试取舍与方法

- 优先覆盖输入及其结果：用 `ui.NewTester` 驱动点击、按键、拖拽、焦点和滚动，断言状态变化或外部动作。例如快捷键录制与保存、截图取消、旧任务回包被拒绝；回归测试用英文短注释解释原先的错误。
- 不为文案清单、表单行序或实现细节写机械断言，也不测试 MyGo 自身的控件或动画实现。默认值只有属于明确契约时才断言，例如配置迁移、兼容性和平台快捷键；这些保护不能因简化测试而删除。
- 异步结果用 channel、fake runner/clock 或 `testing/synctest` 控制，不靠固定的真实 `time.Sleep` 猜测完成时机。沿用现有 `Actions` 注入和测试辅助代码；替换全局钩子后用 `t.Cleanup` 恢复。
- 测试动画或延迟刷新时用 Tester 的 `Advance` 推进时间，不依赖真实时钟的相位。检查空闲刷新预算时注意：`FramePending()` 不包含尚未到期的 `After` / `Painter.After`，不能单凭它断言没有后续刷新。
- 视觉效果以渲染图或原生窗口检查为主；设置面板可用现有 `SOTTO_SETTINGS_PREVIEW` 导出预览。像素断言只保护遮罩覆盖、透明度等明确行为，避免整图逐像素绑定布局和抗锯齿细节。
- 新增或修改 UI 回归用例后，用 `CGO_ENABLED=0 go test <包路径> -run '^Test用例名$' -count=5` 检查稳定性。发现偶发失败要修正同步或断言，不通过自动重试掩盖；提交前仍执行上面的完整验证。

## Git 约定

- 提交信息遵循 Conventional Commits 规范（`<type>[optional scope]: <description>`），保持项目一致性。
- PR 描述遵循 `.github/pull_request_template.md`，按 `Summary`、`Verification`、`Scope` 组织，说明最终改动、实际验证结果和改动范围，并注明待验证项。
- 不提交生成物以外的杂物；`node_modules/ dist/ build/` 已忽略。
- 提交前跑验证命令（见上）。

## 项目结构速览

```
main.go             # 嵌入应用图标并调用 app.Run
internal/app        # 生命周期、控制器、快捷键、服务组装、入口任务与截图编排
internal/ui/bubble   # 气泡窗口池、原生视图、布局和翻译会话
internal/ui/settings # 原生设置视图与异步保存状态
internal/ui/snip     # 框选遮罩、屏幕生命周期和截图窗口租约
internal/ui/shared   # 气泡和设置共用的展示名称与图标
internal/platform    # URL、更新、CLI 诊断与平台窗口策略
internal/config    # Settings + 原子读写（key 不进 JSON，走 mygo.Secrets）
internal/keys      # Secrets 封装
internal/langdetect# Unicode 脚本粗判（纯函数）
internal/naming    # 标识符拆词 + 5 种命名风格（纯函数）
internal/translate # Translator 接口 + fan-out + deepl/openai/baidu/tencent/youdao/azure/google/dictionary
internal/selection # Windows UIA / macOS AX 主路径 + 模拟复制兜底；气泡外部点击检测
internal/capture   # Windows GDI / macOS ScreenCaptureKit 截屏
internal/ocr       # Windows PP-OCR + Windows.Media.Ocr 兜底；macOS Vision
```

## 平台验证与坐标契约

- 共享逻辑与 Tester 用例放普通 `_test.go`；平台 API 放 `_windows_test.go` / `_darwin_test.go`。接收平台参数的纯函数在所有宿主覆盖两平台，不复制测试。
- 真实抓屏、权限和桌面交互必须显式启用。Windows 抓屏使用 `SOTTO_CAPTURE_NATIVE=1`；Mac 原生引擎/隔离 pasteboard 使用 `SOTTO_MACOS_NATIVE=1`，桌面测试另需 `SOTTO_DESKTOP=1`。入口及验证边界见 `e2e/README.md`。
- 符号加载、编译、Tester、真实系统调用和桌面验收分别记录；交叉编译不得写为平台测试已通过。
- UI/global screen 坐标采用 MyGo 逻辑空间；snip 手势是 overlay-local。`capture.GrabDisplayWindow` 在 Windows 接收 overlay-local DIPs，在 Mac 接收 display-local points 与稳定 display ID；输出图像及 OCR 四边形使用 physical pixels。
