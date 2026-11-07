# Offline OCR

Sotto uses PP-OCRv6 small with ONNX Runtime 1.31.1 for local CPU recognition. Chinese and English use the local models first; other explicit language tags use Windows.Media.Ocr.

## Resources

| File | Purpose |
|---|---|
| `det.onnx` | Text detection |
| `rec.onnx` | Text recognition |
| `ppocr_keys.txt` | Ordered recognition dictionary |
| `LICENSE`, `NOTICE` | Licenses and copyright notices |

[`internal/ocr/models.json`](../../internal/ocr/models.json) records asset sizes, SHA-256 hashes, download URLs and tensor contracts. Update both models and the dictionary together. Tests and the first local initialization verify asset integrity.

Keep the dictionary's original LF bytes, whitespace entries and final newline. `.gitattributes` enforces LF on checkout. To repair an old CRLF checkout, restore the tracked dictionary and rebuild:

```powershell
git checkout-index --force -- resources/ocr/ppocr_keys.txt
bun run build:win:x64
```

Runtime DLLs live in `resources/windows-amd64/ocr` and `resources/windows-arm64/ocr`. Builds combine the shared assets and architecture-specific DLLs into `ocr/` beside `Sotto.exe`. `SOTTO_OCR_DIR` overrides that directory and must contain the complete model set and matching DLLs.

## Recognition behavior

- Sessions initialize on first use and are reused serially, with two inference threads per session by default.
- Detection targets a short side of 448, capped at 1 Mi pixels and a 4,096-pixel side. Recognition crops use the original screenshot.
- Empty local results or text-confidence risks can trigger one system OCR fallback. Discarded low-score detection regions, low-confidence whitespace and geometry-only risks do not trigger it. Replacement requires coverage of local text regions and preservation of trusted text.
- Failed initialization is retried on a later request. A broken system-worker pipe gets one transfer retry. Cancellation prevents stale results from being published but cannot interrupt native inference already running.

Source images are limited to 16 Mi pixels and a 16,384-pixel side; detection to 1,000 candidates; recognition inputs to 2,048 pixels wide. Exceeded budgets return an error or use system fallback. Complex tables, vertical/inverted text and exact code indentation are not guaranteed.

## Diagnostics

During development, capture the same region several times in the app and inspect the terminal's `ocr:` summary: total time, selected engine, detection size, main stage durations, risk reasons and fallback costs. Failed requests additionally log full attempt details. These logs exclude recognized text and image data. Full diagnostics remain available in the recognition result; numeric fields ending in `Us` are microseconds.

The first successful local call includes model verification and session initialization. Later calls reuse the sessions. Detection/recognition inference timings include the native bridge and output copy. Total OCR time includes queue wait, but excludes screenshot capture and translation. Both engine attempts remain visible when fallback supplies the final result.

For comparisons, set environment variables before launching a fresh app process:

| `SOTTO_OCR_PROFILE` | Short-side target | Long-side cap | Pixel budget |
|---|---:|---:|---:|
| `balanced` (default) | 448 | 4,096 | 1 Mi |
| `legacy` | 736 | 4,096 | 4 Mi |
| `bounded` (experimental) | 64 | 960 | 1 Mi |

`SOTTO_OCR_DETECTOR_THREADS` overrides detector threads (1–16, default 2); recognition stays at 2. Configuration is cached after successful initialization. Smaller detection inputs and additional threads require accuracy and performance checks on the target device.

For a same-image 2/4-thread comparison on Windows, set `SOTTO_OCR_BENCH_IMAGE` to an absolute PNG path and run `go test ./internal/ocr -run '^$' -bench '^BenchmarkDetectorThreads$' -benchtime=3x`. The benchmark warms both configurations, checks their recognized text matches, and reports full pipeline and detector timings. CPU usage and thermal impact require observation on the target device.

## Build and verification

```powershell
$env:CGO_ENABLED = "0"
go test ./internal/ocr/...
bun run build:win
```

On Windows, verify packaged runtime loading and model output shapes:

```powershell
$env:SOTTO_OCR_PREFLIGHT = "1"
go test ./internal/ocr -run '^TestV6SmallRuntimePreflight$' -count=1 -v
Remove-Item Env:SOTTO_OCR_PREFLIGHT
```

The preflight does not measure recognition accuracy. Check real screenshots on x64 and ARM64: small text, light/dark backgrounds, mixed Chinese/English, code and numbers. Also check repeated captures, cancellation, missing language packs and corrupt resources.
