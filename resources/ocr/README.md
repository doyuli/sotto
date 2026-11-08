# Offline OCR

Sotto uses PaddleOCR's PP-OCRv6 small models, executed locally on CPU by ONNX Runtime 1.31.1. The [PaddleOCR model documentation](https://github.com/PaddlePaddle/PaddleOCR/blob/main/docs/version3.x/algorithm/PP-OCRv6/PP-OCRv6.en.md) defines their 50-language scope: Simplified/Traditional Chinese, English, Japanese, and 46 Latin-script languages. This does not cover all languages.

Sotto implements language routing within that model scope. Requests without a language hint and explicit supported languages try PPOCR first. Unsupported explicit languages (such as Korean, Russian, Arabic, and Thai) go directly to Windows.Media.Ocr and require an installed OCR language pack. The requested language tag is retained when system fallback is needed.

Non-Latin script variants of the Latin languages are excluded, including regional defaults such as `az-IR`, `az-RU`, `uz-AF`, `uz-CN`, and `ku-IQ` from [Unicode CLDR](https://github.com/unicode-org/cldr-json/blob/main/cldr-json/cldr-core/supplemental/likelySubtags.json). Explicit scripts take precedence: `az-Latn-IR` can use PPOCR. Serbian requires `sr-Latn`. The recognition dictionary and model output classes are fixed together; adding dictionary entries alone cannot add language support.

Tags with an extlang subtag (such as `ms-zsm-Arab-MY`) go directly to Windows OCR: Sotto does not infer the model's support for a more specific language from the primary language tag. Numeric regions such as `es-419` continue to use the normal language routing.

## Resources

The ONNX models and matching dictionary are distributed by [RapidOCR v3.10.0](https://github.com/RapidAI/RapidOCR/blob/v3.10.0/python/rapidocr/default_models.yaml). RapidOCR is the resource source, not the inference engine or Sotto's routing implementation.

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

- The screenshot UI supplies no language hint. It tries PPOCR first without detecting the screenshot's script in advance. Unsupported text may be misrecognized with high confidence, so system fallback is not guaranteed to recover it.
- Sessions initialize on first use and are reused serially. Detection defaults to `min(4, runtime.NumCPU())` inference threads; recognition uses 2.
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

`SOTTO_OCR_DETECTOR_THREADS` overrides detector threads (1–16). The default uses up to 4 threads, capped by the logical CPU count; explicit overrides are not capped. Recognition stays at 2. Configuration is cached after successful initialization. Smaller detection inputs and additional threads require accuracy and performance checks on the target device.

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
