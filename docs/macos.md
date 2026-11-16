# macOS Architecture and Platform Bridge

Sotto's macOS implementation is a pure-Go desktop integration (`CGO_ENABLED=0`) targeting macOS 14+ on Apple Silicon (`arm64`). Rather than relying on cgo or modifying the underlying MyGo runtime, Sotto bridges Apple system frameworks directly using `ebitengine/purego` and `purego/objc`.

This document records the architectural structure, subsystem bridges, threading and memory contracts, and platform constraints of the macOS port.

---

## 1. Architectural Overview and System Frameworks

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
|     MyGo v0.8.3   |             |   Carbon Hotkey |                 |   MacNative     |
|   (AppKit Windows)|             | (internal/hotkey)               | (internal/     |
+-------------------+             +-------+-------+                 |   macnative)  |
        |                                 |                         +-------+-------+
        |                                 v                                 |
        |                      Carbon Event Manager                         |
        |                      (kEventHotKeyExclusive)                      |
        |                                                                   v
        +----------------------------------------------------+   AppKit / NSWorkspace / NSEvent
                                                             |   CoreGraphics / Permissions
                                                             v
        +----------------------------------------------------+------------------------------+
        |                                                    |                              |
        v                                                    v                              v
+-------------------+                              +-------------------+          +-------------------+
|     Selection     |                              |      Capture      |          |        OCR        |
| (internal/        |                              | (internal/        |          | (internal/ocr)    |
|   selection)      |                              |   capture)        |          +---------+---------+
+---------+---------+                              +---------+---------+                    |
          |                                                  |                              v
          +-------------------+                              v                        Apple Vision
          |                   |                       ScreenCaptureKit             (VNRecognizeText)
          v                   v                      (SCContentFilter)
     Accessibility      NSPasteboard
     (AXUIElement)     (Guarded Copy)
```

| Subsystem | macOS Framework | Purego Bridge | Package Path |
|---|---|---|---|
| **App Lifecycle & Events** | AppKit, NSWorkspace | Objective-C runtime & blocks | `internal/macnative` |
| **Global Shortcuts** | Carbon Events | C symbols (`RegisterEventHotKey`) | `internal/hotkey` |
| **Selection Capture** | ApplicationServices (AX) | C symbols (`AXUIElementCopyAttributeValue`) | `internal/selection` |
| **Guarded Copy Fallback** | AppKit, CoreGraphics, Carbon | Objective-C (`NSPasteboard`) & CGEvent | `internal/selection` |
| **Screen Region Capture** | ScreenCaptureKit, CoreGraphics | Objective-C (`SCScreenshotManager`) | `internal/capture` |
| **Offline Screen OCR** | Vision, CoreGraphics | Objective-C (`VNRecognizeTextRequest`) | `internal/ocr` |
| **Speech Synthesis** | AVFoundation | Objective-C (`AVSpeechSynthesizer`) | `internal/speech` |
| **Secure Credentials** | Security (Keychain) | Managed via `mygo.Secrets` | `internal/keys` |

---

## 2. Window Policy and Workspace Lifecycle

### Accessory Application Policy

- Sotto runs strictly with `NSApplicationActivationPolicyAccessory`. It does not show an icon in the macOS Dock or the Cmd+Tab application switcher; it resides solely in the system menu bar (status item).
- Passive presentation: Translation bubbles triggered by selection or OCR appear via inactive window presentation (`ShowInactive`). They never steal keyboard focus or activate the application, ensuring the user's active editor or browser remains undisturbed.
- Active presentation: Explicit user-initiated actions (clicking the bubble, pressing the typed input hotkey `⌥A`, opening Settings, or entering screenshot selection) explicitly request keyboard focus.

### Multi-Space and Outside-Click Dismissal

- **Transient vs Pinned Bubbles**: With auto-hide enabled, blur or an outside click ends the unpinned presentation: cancel translation, retries, and recognition, clear request identities, and hide the window. Disabling auto-hide preserves the presentation on blur/outside clicks. Pinned bubbles keep independent sessions; workspace and sleep transitions hide them as described below.
- **Outside-Click Detection**: `WatchWindowLeave` in `internal/selection/selection_darwin.go` combines local/global `NSEvent` monitors from `internal/macnative/events_darwin.go` with application-activation notifications. Callbacks dispatch dismissal work and return immediately; local callbacks return the original event. Auto-hide and current presentation identity are checked before dismissal.
- **Workspace and Sleep Transitions**: `internal/macnative/workspace_darwin.go` registers for:
  - `NSWorkspaceActiveSpaceDidChangeNotification`
  - `NSWorkspaceWillSleepNotification`
  - `NSWorkspaceScreensDidSleepNotification`
  Each transition increments an atomic `workspaceGeneration`. In-flight transient requests are invalidated and dismissed so that stale translation bubbles do not appear in a newly entered Space or upon wake. Fixed/pinned bubbles hide their native surface while their background worker jobs remain intact for explicit menu-bar restoration.

---

## 3. Selection Text Capture

Selection translation follows a two-tier strategy in `internal/selection`:

1. **Primary Path (Accessibility / AXUIElement)**:
   - Queries the frontmost application via `NSWorkspace.frontmostApplication`.
   - Obtains the focused UI element using `AXUIElementCreateApplication` and `kAXFocusedUIElementAttribute`.
   - Reads `kAXSelectedTextAttribute`. If text is present, it returns directly without touching the system clipboard.
2. **Fallback Path (Guarded Simulated Copy)**:
   - When AX yields no selected text (`ErrNoSelection` or an empty successful result), Sotto may simulate `⌘C` via `CGEventPost`; other AX errors are returned. `IsSecureEventInputEnabled` blocks simulated copy in secure input contexts, returning `ErrClipboardUnsafe` before injection.
   - **Pasteboard Preservation**: Before simulating copy, Sotto snapshots the current `NSPasteboard` content across all items, preserving all readable representations (plain text, RTF, images, file URLs) rather than downgrading to plain text.
   - **Guards**: Pasteboard `changeCount` checks detect version changes; they do not identify the writer. The transaction also checks the source application, workspace generation, held controls, secure input, and local/global input events. If ownership becomes uncertain, retain the recovery backup, refuse to overwrite newer content, and pause further simulated copies until recovery is resolved or discarded. macOS provides no pasteboard lock or compare-and-swap operation, so these checks cannot make version validation and writing atomic.

---

## 4. Screen Capture and Offline OCR

Unlike the Windows build, which bundles ONNX Runtime DLLs and PP-OCRv6 neural network models, the macOS port requires **zero bundled OCR models or external binaries**:

### ScreenCaptureKit Capture (`internal/capture/capture_darwin.go`)

- Lists shareable content once per shortcut and takes available display snapshots before showing the selection overlays using `SCScreenshotManager.captureImageWithFilter:configuration:completionHandler:`. A failed display leaves the other snapshots usable.
- **Self-Window Exclusion**: The capture filter (`SCContentFilter`) explicitly excludes Sotto's own transparent selection overlay window and active translation bubbles so they never appear in the captured image.
- The preview and the OCR crop share the same display snapshot. Selection geometry accounts for the actual overlay client origin and the snapshot's physical pixel dimensions.

### Apple Vision OCR (`internal/ocr/vision_darwin.go`)

- Text recognition runs entirely on-device through Apple's `Vision.framework` using `VNRecognizeTextRequest` configured with `VNRequestTextRecognitionLevelAccurate`.
- Sotto delegates compute-device selection to Vision and makes no guarantee about CPU, GPU, or Neural Engine usage. OCR makes no network requests and does not upload captured images; recognized text is passed to the configured translation services.
- Without an explicit language hint, `visionLanguages` selects the available Simplified Chinese (`zh-Hans`), Traditional Chinese (`zh-Hant`), and English (`en-US`) entries and enables automatic language detection. Explicit hints are normalized and matched against the system-supported languages; an unavailable language returns an error.
- Vision is the only macOS OCR engine; there is no secondary-engine fallback. Recognition results are normalized into Sotto's shared `ocr.Text` pipeline structures.
- Capture converts CGImage directly into owned sRGB RGBA pixels. Vision receives a native CGImage snapshot of those pixels, avoiding PNG encoding/decoding in both bridges. Native pixel storage is scoped to the synchronous consumer; subimages and premultiplied alpha are supported.
- Each call creates and releases its own `VNRecognizeTextRequest` and `VNImageRequestHandler`. Sotto does not retain model sessions with a Windows-style idle deadline; Vision manages its internal resources.

---

## 5. Global Hotkeys and Carbon Bridge

Managed in `internal/hotkey/backend_darwin.go`:
- Global hotkeys use Apple's Carbon Event Manager (`RegisterEventHotKey`).
- Hotkeys are registered with `kEventHotKeyExclusive` so that Sotto receives priority over nonexclusive handlers.
- Before committing registration, Sotto queries `CopySymbolicHotKeys` to detect collisions with enabled macOS system shortcuts (e.g., Spotlight, Mission Control).
- All Carbon registration and event handler unregistration calls are strictly serialized on the main thread (`mygo.RunOnMain`).
- Standard platform accelerator conventions: `Option` (`⌥`), `Command` (`⌘`), `Control` (`⌃`), `Shift` (`⇧`).

---

## 6. Permissions and Recovery

Two macOS system permissions are required:

| Permission | Purpose | System Check API | User Prompt API |
|---|---|---|---|
| **Accessibility** | Reading selected text via AXUIElement | `AXIsProcessTrusted()` | `AXIsProcessTrustedWithOptions` (with `kAXTrustedCheckOptionPrompt`) |
| **Screen Recording** | Region snip via ScreenCaptureKit | `CGPreflightScreenCaptureAccess()` | `CGRequestScreenCaptureAccess()` |

- Permission checks are performed lazily only upon an explicit user action (never during app startup or background loops).
- If permission is denied, Sotto displays an actionable error in the bubble guiding the user to macOS System Settings (`Privacy & Security`), without replaying stale selections. The menu bar provides links to the relevant permission settings.

---

## 7. Threading, Memory, and Lifetimes

1. **Main Thread Confinement**:
   - AppKit window mutations, menu updates, Carbon registration, and `NSEvent` monitors must execute on the OS main thread via `mygo.RunOnMain` / `macnative.PostMain`.
2. **Background Processing**:
   - Vision OCR recognition, ScreenCaptureKit pixel conversion, AX element polling, and HTTP translation streaming execute in background goroutines.
3. **Autorelease Pool Discipline**:
   - Background operations and bridge callbacks wrap Objective-C calls in `macnative.Pool(func())` to drain autoreleased objects on the thread that owns the pool. Objects created with `alloc`/`init` or explicitly retained still require matching releases; a pool does not release those ownership claims.
4. **Block Lifetimes**:
   - `objc.Block` values used for notification observers and event monitors remain owned until unregistration, then are released with `block.Release()`. ScreenCaptureKit completion blocks and retained native objects remain alive until native completion, even when the calling entry has already been canceled; cancellation does not release them early.

---

## 8. Updates, Signing, and Release Packaging

`internal/platform/updates_darwin.go` presents update status in the menu bar instead of opening the stock updater window, preserving the Accessory application's focus policy.

- **Discovery and installation**: Automatic checks, when enabled, begin after 10 seconds and repeat every 24 hours. Background checks only discover versions. The user clicks to download/install a discovered update and then clicks "重启以完成更新" to relaunch. Disabling automatic checks stops future scheduling; quitting cancels in-flight work.
- **Update authentication**: `mygo.config.ts` enables the GitHub updater when `MYGO_UPDATER_PUBLIC_KEY` is provided. MyGo's download/install engine verifies update archive signatures using the embedded public key. This archive signature is separate from Apple code signing and notarization. Builds without updater configuration show "当前构建未启用更新".
- **Apple distribution**: The mandatory macOS job in `.github/workflows/release.yml` runs for every release. It explicitly uses ad-hoc signing (`-sign -`) and skips notarization, so it needs no Apple Developer Program account or Developer ID certificate. It still requires the updater signing keys and uploads the arm64 DMG, signed update archive, and update manifest to the draft release. `.github/workflows/publish-release.yml` continues to require these macOS assets and both Windows architectures before publishing.
- **First launch**: Ad-hoc signing does not establish an Apple-verified developer identity or notarization. After copying the app to Applications and attempting to open it, users who trust the repository's download may need to select **System Settings → Privacy & Security → Open Anyway**, following [Apple's instructions](https://support.apple.com/en-us/102445). Managed Macs may restrict this exception. Do not disable Gatekeeper globally. Accessibility and screen recording permissions are still requested independently. Fresh-download Gatekeeper behavior and permission persistence across ad-hoc updates require desktop acceptance; local build verification alone does not cover them.
- **Local builds**: `bun run build:mac -skip-dmg -skip-notarize` builds an app for local validation. It does not exercise DMG creation or notarization. To validate the no-account DMG path locally, use `bun run build:mac -sign - -skip-notarize` (without `-upload`).

### Pasteboard snapshot scheduling

All NSPasteboard and NSPasteboardItem access stays on the main thread. Snapshotting
returns to the event loop between representations, retains an immutable NSData
copy, and copies its bytes into Go storage on the selection goroutine. Each turn
checks cancellation and the original change count, including a final check before
accepting the snapshot. A synchronous promised-data provider can still block its
individual `dataForType:` call; this is not a hard timeout. Restoration and recovery
ownership guards remain unchanged.
