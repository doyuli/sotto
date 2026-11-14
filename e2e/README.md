# Verification Entry Points

| Layer | Windows | macOS |
| --- | --- | --- |
| Shared logic and native UI Tester | `CGO_ENABLED=0 go test ./...` | Same command |
| Native engines / isolated system objects | Explicitly enable OCR and capture probes | Explicitly enable Vision, isolated pasteboard, and SCK probes |
| Desktop windows, focus, permissions, and multiple displays | [Windows acceptance](windows.md) | [macOS acceptance](macos.md) |

For subsystem architectures and platform integration details, see [Windows Architecture](../docs/windows.md) and [macOS Architecture](../docs/macos.md).

Tester does not create native windows; symbol loading does not prove actual selection capture or screen capture. Cross-compilation is not platform test execution. Default tests do not synthesize input, modify the general pasteboard, or capture the actual screen.

Regular Windows CI runs noninteractive tests, OCR preflight, e2e vet, and DPI initialization checks. Regular macOS CI runs noninteractive tests and the Vision smoke test. Full desktop acceptance requires an interactive session, the appropriate permissions, and hardware.

Windows and macOS desktop acceptance have been completed as confirmed by the maintainer; see [Windows acceptance](windows.md) and [macOS acceptance](macos.md) for the covered scenarios and regression procedures.

Environment-variable examples in build and verification commands use POSIX shell syntax. In PowerShell, set `$env:NAME="value"` and run the command separately.
