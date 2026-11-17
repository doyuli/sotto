# Verification Entry Points

| Layer | Windows | macOS |
| --- | --- | --- |
| Shared logic and native UI Tester | `CGO_ENABLED=0 GOFLAGS=-tags=mygo_nowebview go test ./...` | `CGO_ENABLED=0 go test ./...` |
| Native engines / isolated system objects | Explicitly enable OCR and capture probes | Explicitly enable Vision, isolated pasteboard, and SCK probes |
| Desktop windows, focus, permissions, and multiple displays | [Windows acceptance](windows.md) | [macOS acceptance](macos.md) |

For subsystem architectures and platform integration details, see [Windows Architecture](../docs/windows.md) and [macOS Architecture](../docs/macos.md).

Tester does not create native windows; symbol loading does not prove actual selection capture or screen capture. Cross-compilation is not platform test execution. Default tests do not synthesize input, modify the general pasteboard, or capture the actual screen.

Regular Windows CI runs noninteractive tests, ONNX runtime/model preflight, repeated WinRT worker requests, e2e vet, and DPI initialization checks on x64. It builds both Windows architectures without installers, checks the OCR assets, and scans the executables for the WebView2 loader marker. Regular macOS CI runs noninteractive tests, native image conversion, Vision recognition, and isolated pasteboard tests. It builds and verifies an ad-hoc signed app and DMG and checks that Windows OCR assets are excluded. Full desktop acceptance requires an interactive session, the appropriate permissions, and hardware.

The maintainer previously confirmed Windows and macOS desktop acceptance. See [Windows acceptance](windows.md) and [macOS acceptance](macos.md) for the regression procedures to run after relevant changes.

Environment-variable examples in build and verification commands use POSIX shell syntax. In PowerShell, set `$env:NAME="value"` and run the command separately.
