# 快捷键绑定

`internal/hotkey` 统一解析、比较与注册组合键。macOS 的标准名称是 `Ctrl / Option / Shift / Cmd`，Windows 是 `Ctrl / Alt / Shift / Win`；修饰键有独立标识，UI 的通用 modifier bits 只在录制入口转换。现有配置中的 Alt/Option、Super/Cmd/Win 等名称按当前平台解析，保留相同组合，不引入版本迁移。JSON 仍保存 accelerator 字符串。

## 录制与反馈

- 普通 key-down 和 Sotto 自己的全局回调进入同一录制流程；正在录制时不触发翻译。macOS 字母和标点使用 MyGo 的 `PhysicalKey`，扩展功能键和小键盘保留 `KeyCode` 身份。
- 普通 Esc 取消；带修饰键的 Escape 可以绑定。方向键、标点、Tab、Enter 等可以录制；缺少修饰键或不支持的按键会显示原因。冲突后可直接继续录制。不支持的小键盘位置（例如 macOS 小键盘 Enter）明确提示改用主键盘，不会误存为主键盘的对应键。
- “手动设置”通过修饰键按钮和主键列表设置组合，用于系统已经拦截按键、录制器收不到事件的情况。它与录制共用验证和保存流程。
- 注册及保存成功前显示“正在保存…”，旧绑定继续工作。保存任务串行执行，普通设置写入使用最新快捷键，最后一个任务回读实际设置。关闭/重开窗口不会恢复旧录制状态或显示旧窗口的迟到错误。
- 启动时无法注册的组合保留在配置中，并在设置页显示“未生效”；不再自动替换成候选键位。取消录制或重开窗口不会隐藏这个状态，可通过再次设置同一组合重试。

## 注册事务

`internal/app/hotkeys.go` 的提交顺序为：验证应用内重复 → 注册新增组合 → 保存配置 → 发布生效映射 → 释放被替换的组合。注册或保存失败只撤销新增注册，旧绑定不释放。回调按当前生效映射派发，因此组合交换不需要先清空全部注册。

Windows 继续使用 MyGo 的 `RegisterHotKey` 封装。macOS 在项目内通过 purego 直接桥接 Carbon，使用 `kEventHotKeyExclusive`，并在注册前调用 `CopySymbolicHotKeys` 检查启用的系统快捷键。所有 Carbon 操作回主线程，退出时释放注册和事件处理器；不添加事件 tap 或输入监控权限。

依据：本机 Apple SDK 的 `CarbonEvents.h` 中 `HotKeyOptions`、`RegisterEventHotKey`、`CopySymbolicHotKeys` 定义。MyGo v0.8.3 的 `internal/darwin/hotkey.go` 使用非独占选项 0；Apple 明确说明这种注册可能成功但被独占持有者抑制。因此 Sotto 不用 MyGo 的非独占返回值推断无冲突，也不修改 MyGo 源码。

系统列表不包含各应用的菜单快捷键，无法保证覆盖所有第三方拦截机制。未知的收不到事件不能直接标记为冲突。

## 验证边界

- 解析和注册事务测试覆盖两平台名称、旧组合语义、注册失败、保存失败及组合交换。
- UI Tester 覆盖录制、全局事件分流、错误后重试、手动选择主键、取消/重开、异步保存交错与启动失败状态；预览包含 660/780 宽度、深浅色和录制/手动/冲突状态。
- `CGO_ENABLED=0 SOTTO_MACOS_NATIVE=1 go test ./internal/hotkey -v` 使用两个独立 AppKit 进程临时注册 `Ctrl+Option+Shift+Cmd+F19`，验证跨进程独占注册冲突及释放后重新注册。测试不合成键盘输入，不修改用户配置。
- 真实键盘触发、输入法/键盘布局、系统拦截和窗口焦点仍需按 `e2e/macos.md` 实机验收。Windows 交叉编译不代表 Windows 实机验证。

## 已知限制：第三方非独占注册

macOS 不保证报告第三方的非独占快捷键占用。2026-11-14 在 macOS 15.7.3（24G419）、Apple Silicon、`CGO_ENABLED=0` 下实测：另一进程通过 MyGo 非独占注册后，Sotto 的独占注册仍返回成功。Apple SDK `CarbonEvents.h` 的 `HotKeyOptions` 说明独占持有者可能阻止非独占持有者接收事件；本次探针只验证注册结果，没有验证实际按键分发。

按产品取舍，该情况不作为交付阻塞，也不纳入要求检测成功的回归用例。保留系统公开快捷键检查、Sotto 内部重复检查、独占注册冲突反馈和保存失败回滚；不承诺检测所有第三方占用，不额外引入输入监控或事件 tap。相关测试仅验证上述承诺范围。
