# Windows Architecture and Platform Integration

Sotto is built as a pure-Go desktop application for Windows and macOS; this document covers Windows (`CGO_ENABLED=0`) targeting 64-bit Windows 10 and Windows 11 (both `amd64` and `arm64`). It avoids cgo by interfacing with the Win32 API, Component Object Model (COM), and native system runtimes via `golang.org/x/sys/windows`, `github.com/go-ole/go-ole`, and `ebitengine/purego`.

This document records the architectural structure, Win32/COM subsystem integrations, memory and clipboard protection disciplines, and runtime models of the Windows platform implementation.

---

## 1. Architectural Overview and Subsystems

```
                      +---------------------------------------+
                      |               Sotto App               |
                      |   (internal/app, internal/ui/bubble)   |
                      +-------------------+-------------------+
                                          |
        +---------------------------------+---------------------------------+
        |                                 |                                 |
        v                                 v                                 v
+-------------------+             +---------------+                 +---------------+
|       MyGo        |             |  Win32 Hotkey |                 | Screen Coords |
| (DirectComposition|             | (internal/    |                 | (internal/    |
|   / Win32 HWND)   |             |   hotkey)     |                 |  screencoords)|
+-------------------+             +-------+-------+                 +-------+-------+
        |                                 |                                 |
        v                                 v                                 v
+-------------------+             RegisterHotKey /                EnumDisplayMonitors /
| Native UI Windows |             Win32 Message Loop              Per-Monitor DPI V2
+-------------------+                     |
        |                                 |
        +---------------------------------+---------------------------------+
        |                                 |                                 |
        v                                 v                                 v
+-------------------+             +-------------------+           +-------------------+
|     Selection     |             |      Capture      |           |        OCR        |
| (internal/        |             | (internal/        |           | (internal/ocr)    |
|   selection)      |             |   capture)        |           +---------+---------+
+---------+---------+             +---------+---------+                     |
          |                                 |                               |
    +-----+-----+                           v                               +-----+-----+
    |           |                     Win32 GDI                             |           |
    v           v                 (BitBlt / DIBSection)                     v           v
   UIA       Guarded                                                     PP-OCRv6     WinRT
 (MTA COM)  (Ctrl+C +                                                   (ONNX 1.31) (PowerShell)
            3 Exclusions)
```

| Subsystem | Windows Technology / API | Underlying Driver | Package Path | Concurrency Model |
|---|---|---|---|---|
| **Window & UI** | Win32 HWND, DirectComposition | MyGo | `internal/ui/` | Main UI thread |
| **DPI & Multi-Monitor** | Per-Monitor DPI V2 (`shcore.dll`) | Win32 / `x/sys` | `internal/screencoords` | Layout snapshot under lock |
| **Global Shortcuts** | `RegisterHotKey`, Win32 Msg Loop | MyGo / Win32 | `internal/hotkey` | Main thread event dispatch |
| **Selection (Primary)** | UI Automation (`IUIAutomation`) | COM MTA / `go-ole` | `internal/selection` | Dedicated MTA worker thread |
| **Selection (Fallback)**| `SendInput` (`Ctrl+C`) + Hooks | Win32 Low-Level Hooks | `internal/selection` | Serial transaction with mutex |
| **Clipboard Safety** | Win32 Clipboard + 3 Exclusion Formats | Win32 API | `internal/selection` | Serialized transaction; sequence checks |
| **Screen Capture** | GDI (`BitBlt`, `CreateDIBSection`) | Win32 GDI | `internal/capture` | Background worker |
| **Offline OCR (Primary)**| PP-OCRv6 Small (ONNX Runtime 1.31.1)| Direct CPU DLLs (`purego`)| `internal/ocr` | Serial admission gate |
| **Offline OCR (Fallback)**| `Windows.Media.Ocr` | PowerShell WinRT Worker | `internal/ocr` | Shared admission worker |
| **Speech Synthesis** | SAPI (`SAPI.SpVoice`) | COM Dispatch (`go-ole`) | `internal/speech` | Locked OS thread |
| **Secure Credentials** | Windows Credential Manager | Managed via `mygo.Secrets` | `internal/keys` | Worker goroutine |

---

## 2. Window Policy, DPI Awareness, and Overlay Lifecycle

### Per-Monitor DPI Awareness V2

- Sotto initializes with `DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2`.
- `internal/screencoords` maps UIA physical rectangles to MyGo logical units (DIPs). Screenshot selections map overlay-local DIPs to physical snapshot pixels using the same client-area projection as the preview.
- Multi-display setups with mixed scaling (e.g., 100%, 125%, 150%, 200%) and negative coordinate monitors (secondary displays positioned to the left or above the primary monitor) are snapshotted on-demand via `EnumDisplayMonitors` and `GetDpiForMonitor`.

### Native Screenshot Overlays

- Screenshot region selection uses one transparent DirectComposition window (`MyGoSurface`) per display with a usable snapshot.
- Each overlay shows the frozen snapshot captured before the windows opened. OCR receives a crop of that snapshot.
- On submission or cancellation, Sotto clears the preview state, hides each overlay, and schedules window destruction on the UI thread. The next capture creates new windows.
- Visual tokens, veil opacity, and selection geometry follow [DESIGN.md](../DESIGN.md).

### Outside-Click Dismissal

- For unpinned bubbles, Sotto installs a low-level mouse hook (`WH_MOUSE_LL`) via `WatchWindowLeave` in `internal/selection/hook_windows.go` when auto-hide is enabled.
- The hook returns immediately and dispatches an outside-click notification. After checking the current presentation, the manager ends the unpinned session, cancels its work, clears request identities, and hides it. The click continues to the target application.

---

## 3. Selection Text Capture and Clipboard Discipline

Selection capture in `internal/selection` strictly enforces a non-intrusive contract:

### Primary Path: UI Automation (UIA)

- Queries the active element's `TextPattern` interface without touching the system clipboard.
- **Dedicated MTA Thread**: COM calls run on a dedicated Multi-Threaded Apartment (MTA) thread initialized with `CoInitializeEx(0, COINIT_MULTITHREADED)`.
- **Concurrency & Circuit Breaker**:
  - Request timeout: 800 ms (allowing slow accessibility trees like Chromium to warm up).
  - Slot queue timeout: 150 ms (if another COM query is in-flight).
  - Circuit breaker: After 3 consecutive request timeouts, UIA opens a 60-second cooldown breaker. Waiting 150 ms for an occupied slot also skips UIA, but does not increment the timeout count. The actual COM worker holds the slot until it returns, so a permanently blocked provider prevents further UIA calls for that process; guarded clipboard fallback remains available.

### Fallback Path: Guarded Simulated Copy

When UIA returns empty or unsupported, Sotto falls back to simulating `Ctrl+C`:

1. **Safety Checks**: Confirms mouse buttons and keys are released (`waitCopyRelease`) and the target foreground window has not changed.
2. **Low-Level Hook Protection**: Sotto temporarily installs `WH_KEYBOARD_LL` and `WH_MOUSE_LL` hooks. Simulated keystrokes carry a unique `ExtraInfo` marker; the keyboard hook ignores injected events carrying that marker while canceling the copy transaction if genuine user keyboard or mouse input is detected.
3. **Clipboard Restoration with Exclusion Markers**:
   Before sending `Ctrl+C`, Sotto snapshots the current clipboard. When restoring the original content, Sotto writes three Windows clipboard exclusion formats to request exclusion from history, cloud synchronization, and monitor processing:
   - `CanIncludeInClipboardHistory = DWORD(0)`
   - `CanUploadToCloudClipboard = DWORD(0)`
   - `ExcludeClipboardContentFromMonitorProcessing = BYTE(1)`

   These markers apply only to Sotto's writes. Third-party clipboard managers may not honor them, and temporary writes made by the source application in response to `Ctrl+C` are outside Sotto's control. They do not guarantee that content never enters clipboard history.

4. **Format & Budget Limits**:
   - Total snapshot budget: 128 MiB (up to 128 MiB for `CF_DIB` and `CF_DIBV5`; non-DIB formats limited to 32 MiB combined).
   - Palettes (`CF_PALETTE`) are backed up via `LOGPALETTE` (`GetObjectW`, `GetPaletteEntries`) and restored with `CreatePalette`.
   - Synthesizable `CF_BITMAP` is omitted during backup only after DIB data is securely captured.
   - If a full backup is unavailable, Sotto still sends the copy shortcut once. After a sequence change, an ownerless copy can supply stable text while the source remains foreground and input is undisturbed. Automatic restoration requires a matching owner process and sequence. A failed write is logged and the captured selection can still be translated.

---

## 4. Screen Region Capture and Geometry

Implemented in `internal/capture/capture_windows.go`:
- Uses Win32 GDI: captures the virtual screen DC using `CreateCompatibleDC`, `CreateDIBSection`, and `BitBlt`.
- `capture.SnapshotDisplays` resolves each MyGo display ID as an `HMONITOR` and reads its physical bounds before capture. Available snapshots are ready before showing selection overlays. `TestBitBltSmoke` exercises this path on the primary display.
- The selection overlay projects its display snapshot into client coordinates. The OCR crop uses the inverse projection, including mixed DPI and negative screen origins.
- Snapshots have a combined 64 Mi pixel budget. Selections remain limited to 16 Mi pixels or 16,384 pixels per side before crop allocation.

---

## 5. Offline OCR Engine and Runtime Integration

Dual-engine architecture for Windows offline recognition:

### Primary: PP-OCRv6 Small via ONNX Runtime 1.31.1

- **CPU Inference**: Directly loads CPU-optimized ONNX Runtime DLLs (`onnxruntime.dll` and `onnxruntime_providers_shared.dll`) dynamically via `purego`.
- **Models**: Uses RapidOCR v3.10.0 distributed PP-OCRv6 small models (`det.onnx`, `rec.onnx`, and `ppocr_keys.txt`), covering 50 languages.
- **Packaging**: MyGo builds place the models and architecture-specific DLLs into `ocr/` beside `Sotto.exe` (`resources/windows-amd64/ocr` or `resources/windows-arm64/ocr`).
- **Zero-Copy Tensor Borrowing**: Native model output tensors are consumed directly during callback execution before releasing `OrtValue`, avoiding full tensor copy overhead.

### Fallback: Windows.Media.Ocr (WinRT)

- Native Windows OCR runs via an out-of-process PowerShell worker script (`winrt.ps1`).
- Used when PP-OCR fails, returns empty text, or when the requested language is outside PP-OCR's Latin/CJK scope (e.g., Korean, Arabic, Thai, Russian) with installed Windows OCR language packs.

### Idle Resource Lifecycle

- **PP-OCR**: Model sessions are closed and recognizer/dictionary references are dropped after **5 minutes** idle, measured from actual request completion. The shared ONNX Runtime stays loaded; this does not guarantee a particular working-set reduction.
- **Windows OCR**: PowerShell worker process terminates after **30 seconds** of idle time.
- Both engines share a unified admission gate and reload transparently on the next request.
- Detailed memory benchmarking protocols and model hash verification are documented in [resources/windows/ocr/README.md](../resources/windows/ocr/README.md).

---

## 6. Speech Synthesis and Credentials

- **Speech Synthesis (`internal/speech/sapi_windows.go`)**:
  - Uses Windows SAPI via `SAPI.SpVoice` through COM dispatch (`github.com/go-ole/go-ole`).
  - Executed on a thread locked with `runtime.LockOSThread()`.
  - Supports asynchronous speech with `speakAsync` and purges previous speech queues on new requests.
- **Credential Storage (`internal/keys`)**:
  - API keys are encrypted and stored in the **Windows Credential Manager** via `mygo.Secrets`. No plain-text secrets are stored in settings JSON.

---

## 7. Updates and Packaging

- `internal/platform/updates_other.go` registers MyGo's native updater with `plugins/updater/native.New`. The tray menu opens the updater and settings control automatic checks.
- The `build:win`, `build:win:x64`, and `build:win:arm64` scripts set `mygo_nowebview` for the packaged app. The generic `bun run build` and `bun run dev` scripts use the caller's build flags.
- The two-architecture build emits `build/windows-amd64/Sotto.exe` and `build/windows-arm64/Sotto.exe` with their respective OCR assets. MyGo also produces NSIS installers when NSIS is available and `-skip-installer` is absent.

## 8. Desktop Verification and Acceptance

- Automated smoke tests are in `e2e/smoke_windows_test.go`, with DPI mode checks in `e2e/dpi_windows_test.go`; execution instructions are in [e2e/windows.md](../e2e/windows.md).
- Real hardware acceptance tests covering multi-DPI layouts, UIA/clipboard preservation across apps, and PP-OCR memory baselines follow [e2e/windows.md](../e2e/windows.md).
