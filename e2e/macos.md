# macOS 桌面验收

支持范围：macOS 14+、Apple Silicon。使用交互式桌面会话；不要终止已有用户实例。记录系统版本、运行方式、屏幕布局和权限状态。未执行的项目保持待验收。

## 自动入口

```bash
CGO_ENABLED=0 go test ./... -count=1
CGO_ENABLED=0 SOTTO_MACOS_NATIVE=1 go test ./internal/hotkey -v
CGO_ENABLED=0 SOTTO_MACOS_NATIVE=1 go test ./internal/ocr -run '^TestVisionNativeSmoke$' -count=1 -v
CGO_ENABLED=0 SOTTO_MACOS_NATIVE=1 go test ./internal/selection -run '^TestMacIsolatedPasteboardItems$' -count=1 -v
bun run build:mac -skip-dmg -skip-notarize
CGO_ENABLED=0 SOTTO_DESKTOP=1 go test -tags e2e ./e2e -run '^TestMacPanelFocus$' -count=1 -v
```

隔离 pasteboard 测试不触碰 general pasteboard。SCK 探针需要预先授予运行它的进程屏幕录制权限，不自动弹窗请求：

```bash
CGO_ENABLED=0 SOTTO_MACOS_NATIVE=1 go test ./internal/capture -run '^TestMacCaptureNative$' -count=1 -v
```

窗口探针使用上面的 `TestMacPanelFocus` 入口：先编译临时二进制（限时 2 分钟），再直接启动探针（限时 30 秒）。JSON 和退出码表示检查结果，输入完成以真实焦点事件为准；应用内另有 10 秒超时。外层直接持有并回收探针进程，即使原生主线程阻塞也能终止，不经 `go run` 间接启动。`TestMacProbeCancellationReapsProcess` 无需桌面即可验证取消与进程回收。缺少权限、桌面会话和断言失败必须分别记录；不靠跳过失败断言取得通过。

## 手工清单

以下均须实机验收。每项在干净启动和热运行各检查一次，勿将单测结果填作桌面通过。

| 前置条件 | 操作 | 预期结果 |
| --- | --- | --- |
| 设置页正在录制快捷键 | 按 Sotto 另一操作的组合，再按一个可用组合；使用方向键、标点和 Tab 组合 | 应用内冲突在当前行显示，不触发翻译；可以继续录制，成功后生效 |
| 某组合被系统或其他应用拦截 | 在“手动设置”选择该组合并保存 | 可检测的占用明确报错；原组合继续可用，不自动换键；不能把未收到按键事件直接判为冲突 |
| 保存过快捷键 | 失焦、取消、关闭/重开设置及重启应用；分别使用中英文输入法 | 录制正确取消；保存的组合保持；启动注册失败显示“未生效”；macOS 配置及日志使用 Option/Cmd |
| 未授予辅助功能/屏幕录制 | 分别触发划词与截图，拒绝后重试，再到系统设置授权 | 说明所需权限与恢复方式；不自动重放旧入口；撤销后再次明确报错 |
| TextEdit/浏览器可访问文本 | 选中文字触发划词；再用不支持 AX 的目标触发复制回退 | 主路径不修改剪贴板；回退只取得当前来源内容；`--grab ax` 与 `--grab clipboard` 可比较两条路径 |
| general pasteboard 中存有可辨认的备份内容 | 分别以文本、富文本、图片、文件 URL、多 item 复制后触发回退，再粘贴检查 | 保留 item 边界和全部可读取表示；未知/不可读取类型保守拒绝；不降级成纯文本恢复 |
| 回退取词正在运行 | 复制新内容、切应用、按键或滚轮打断；触发恢复重试 | 新内容优先；所有权不明不覆盖；保留备份并暂停新回退，明确放弃后才恢复使用 |
| 自动隐藏开启 | 在外部应用触发划词/OCR，不点击气泡；同一 Space 内 ⌘Tab | 结果不抢焦点；切应用结束临时会话，旧流不继续更新；关闭自动隐藏后遵循设置 |
| 输入面板、设置或截图入口 | 明确打开，输入文字、使用快捷键 | 得到键盘输入焦点，Accessory 和 nonactivating 策略保持，无 Dock 图标 |
| 原生下拉菜单打开 | 确认/取消，再点击外部应用；同时检查设置与子窗口 | 内部菜单交互不误关闭，外部交互按自动隐藏处理 |
| 临时气泡有慢请求 | 切 Space、进入全屏应用、睡眠再唤醒，随后立刻开始新入口 | 旧临时工作取消；新入口不被迟到通知关闭；不出现幽灵窗口 |
| 固定气泡有慢请求 | 切 Space 后从菜单栏恢复编号气泡，再关闭 | 隐藏保留工作和内容；恢复不重发翻译；已销毁窗口的旧菜单项不操作新窗口 |
| 固定输入气泡已恢复，另一个划词入口仍在采集 | 取消固定，再按输入面板快捷键 | 重新获取输入入口并撤销旧采集；迟到的划词结果不覆盖输入面板 |
| Retina 与非 Retina 外屏，含左侧/上方负坐标 | 在边缘与菜单栏下框选已知图案，改变屏幕布局再重试 | 遮罩、抓屏范围和气泡锚点一致，截图无本应用着色；显示器变化撤销旧任务 |
| 截图请求已提交 | 快速切入口或取消，多次重复后观察资源 | 旧结果不显示，原生 completion 前保留对象，completion 后释放；不累积 worker/窗口 |
| 钥匙串可用 | 保存/删除密钥，关闭设置再重开，重启应用 | 凭据持久化与设置 JSON 分离；草稿/过期请求不泄漏到新窗口 |
| 系统语音可用 | 连续朗读、替换请求，排队时退出 | 最新请求覆盖旧请求；退出不死锁，关闭后的排队任务不发声 |
| 设置有待保存任务，多种窗口打开 | 托盘退出 | 任务有序排空，所有 OnClose 放行，进程真正结束 |
| 专用签名安装测试环境 | 检查更新、失败重试、下载、安装、重启；关闭自动检查 | 后台只发现版本，安装需点击；关闭自动检查停止后续排程，在途请求由退出取消 |

## 当前验证边界

快捷键检测覆盖 Sotto 内部重复、系统公开列表和独占注册冲突。第三方非独占注册属于已接受的能力限制，不作为交付阻塞或必测项；细节见 [快捷键已知限制](../docs/hotkeys.md#已知限制第三方非独占注册)。不得将通过结果记为完整跨应用冲突检测。

输入焦点探针在开发宿主未收到 `OnFocus` 事件并超时；被动显示、Accessory 和 nonactivating 检查通过。MyGo v0.8.3 的 `internal/darwin/child_window.go:showAboveParent` 在主动 Show 时调用 `makeKeyAndOrderFront:`，`internal/darwin/window.go:IsFocused` 读取 `isKeyWindow`。在继续修改窗口策略前，需要在正常应用与可交互桌面中复现并解释差异。

Vision、隔离 pasteboard 多 item 往返、SCK 小区域抓屏尺寸探针已在开发宿主通过。真实 AX/通用剪贴板恢复、SCK 自身窗口排除、多屏、睡眠和签名安装仍待实机验收。SCK 尺寸探针不证明所有颜色空间、缩放和取消路径正确。未签名/临时签名构建不能作为公证与更新安装证据。

现有隔离 pasteboard 探针仅验证 AppKit 的直接写入/读取，没有经过生产快照/恢复链路。2026-11-14 尝试补充该链路时，测试输出 `NSPasteboard: synchronous promise fulfillment requested from a background thread!`，并发现写入两种表示的 item 返回三种类型；新增探针未通过，试验改动已撤回。源码区别是 `internal/selection/pasteboard_native_darwin_test.go` 在测试 goroutine 内使用 `macnative.Pool`，而生产 `internal/selection/clipboard_darwin.go:pasteboardOnMain` 通过 `mygo.RunOnMain` 调度。按平台异常规则暂停该验证；后续需在正确的主线程运行环境中解释类型变化，覆盖生产快照/恢复、外部写入与取消保护，不能将当前直接往返结果记为这些路径已验证。
