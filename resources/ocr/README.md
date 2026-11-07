# Offline OCR

Sotto bundles PP-OCRv6 small detection and recognition models with ONNX Runtime 1.31.1 for local CPU inference. Chinese and English use the local models first; other explicit language tags use Windows.Media.Ocr.

## Resources and builds

| File | Purpose |
|---|---|
| `det.onnx` | Text detector, 9,929,594 bytes |
| `rec.onnx` | Text recognizer, 21,234,383 bytes |
| `ppocr_keys.txt` | 18,708 dictionary entries, 74,947 bytes |
| `LICENSE`, `NOTICE` | Model and algorithm licensing and copyright notices |

`internal/ocr/models.json` is the embedded model manifest. It records download URLs, SHA-256 hashes, tensor names and class counts. Update the detector, recognizer and dictionary together. The three assets total 31,238,924 bytes and are included in the repository.

Runtime DLLs are stored in `resources/windows-amd64/ocr` and `resources/windows-arm64/ocr`. Packaging merges the shared models and the target architecture's DLLs into `ocr/` beside `Sotto.exe`. `SOTTO_OCR_DIR` can override this location with a directory containing the complete model set and matching DLLs.

```bash
CGO_ENABLED=0 go test ./internal/ocr/...  # Includes model integrity checks
bun run build:win                       # Builds x64 and ARM64; NSIS creates installers when available
```

Go tests and the first local OCR initialization verify model integrity. Installer sizes depend on the executable and compression settings.

## Results and scheduling

- Sessions load on first use and are reused serially, with two inference threads per session.
- Raw lines retain text, quadrilaterals, recognition scores, token scores and detection diagnostics. Coordinates start at the screenshot's top-left pixel, independent of `image.Bounds().Min`.
- Both engines share layout reconstruction for fragments, simple columns, paragraphs, headings and lists. Final `Text` is generated from cleaned layout blocks while raw lines remain available for inspection.
- An empty local result or a replaceable text-confidence risk can trigger one system OCR call. Small text, steep lines and degenerate regions alone remain diagnostics and do not trigger a call. Replacement requires coverage of existing text regions, preservation of trusted text and no obvious anomalies. Text length alone does not determine the result.
- Each request allows one local recognition stage and at most one system OCR call. The system worker does not perform additional enlargement retries or language-pack inference sweeps.
- Cancellation stops queued work, checks between stages and prevents stale results from being published. It cannot interrupt a native Run already in progress. Failed initialization releases unfinished sessions and runtime resources; only successful initialization is cached, so repaired files can be retried on the next request.

`internal/ocr/pipeline` uses only the Go standard library and receives synchronous inference through `Runner.Run(ctx, Tensor)`. Output tensors must own their data. The native bridge checks float32 type, rank, dimension products and element counts before copying outputs.

## Limits

Default limits are 16 Mi pixels and a 16,384-pixel side for the source image; 4 Mi pixels and a 4,096-pixel side for detection; 1,000 candidates; a 2,048-pixel recognition input width; and 32 Mi float32 elements per tensor. The detector limits enlargement or downsizes the full screenshot to fit its side, pixel and input-tensor budgets, keeping 32-pixel alignment. Other exceeded budgets return an error or use the single system fallback. Text is never silently truncated. System OCR boxes are mapped back using the actual horizontal and vertical resize ratios.

Image processing uses BGR channels, 0.5 normalization, bilinear resizing, minimum rotated rectangles, continuous geometric expansion and cubic perspective sampling. Holes do not produce separate text boxes. Rasterization, rounding and threshold-sensitive detections still require validation on real screenshots.

Complex tables, arbitrary columns, vertical or inverted text and exact code indentation are outside the current guarantees. Confidence and risk signals cannot detect every missing region.

## Verification

Automated tests cover model integrity, processing, layout, result selection, cancellation and resource limits. On Windows, the opt-in native probe first attempts a missing runtime path, then recovers with the current architecture's packaged DLLs. It loads both models and runs zero tensors to check output shapes, class counts and probabilities. Run from the project directory in PowerShell:

```powershell
$env:CGO_ENABLED = "0"
$env:SOTTO_OCR_PREFLIGHT = "1"
go test ./internal/ocr -run '^TestV6SmallRuntimePreflight$' -count=1 -v
Remove-Item Env:SOTTO_OCR_PREFLIGHT
```

Zero-tensor checks do not measure recognition accuracy. Windows x64 and ARM64 still need validation for:

- `Sotto.exe --ocr <png> [lang]` output, including raw lines, final text, selected engine, retry count and errors.
- Mixed Chinese and English, small text, light and dark backgrounds, UI screenshots, PDFs, columns, lists, numbers and paths, checked against manually verified text.
- Different DPI settings, rapid reselection, cancellation, missing language packs, missing or corrupt models, repeated recognition and shutdown.
- Cold and warm latency, p95, peak memory, memory trends over 50 consecutive requests and installer size.

Windows native inference, desktop interaction and real screenshot accuracy have not yet been verified in the current macOS development environment. Cross-platform tests and cross-compilation do not replace these checks.
