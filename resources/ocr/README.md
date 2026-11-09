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

[`internal/ocr/models.json`](../../internal/ocr/models.json) records asset sizes, SHA-256 hashes, download URLs and tensor contracts. Update both models and the dictionary together. Tests and every local session initialization verify asset integrity.

Keep the dictionary's original LF bytes, whitespace entries and final newline. `.gitattributes` enforces LF on checkout. To repair an old CRLF checkout, restore the tracked dictionary and rebuild:

```powershell
git checkout-index --force -- resources/ocr/ppocr_keys.txt
bun run build:win:x64
```

Runtime DLLs live in `resources/windows-amd64/ocr` and `resources/windows-arm64/ocr`. Builds combine the shared assets and architecture-specific DLLs into `ocr/` beside `Sotto.exe`. `SOTTO_OCR_DIR` overrides that directory and must contain the complete model set and matching DLLs.

## Recognition behavior

- The screenshot UI supplies no language hint. It tries PPOCR first without detecting the screenshot's script in advance. Unsupported text may be misrecognized with high confidence, so system fallback is not guaranteed to recover it.
- Sessions initialize on first use and are reused serially until idle release. Detection defaults to `min(4, runtime.NumCPU())` inference threads; recognition uses 2.
- Detection targets a short side of 448, capped at 1 Mi pixels and a 4,096-pixel side. Recognition crops use the original screenshot.
- Empty local results or text-confidence risks can trigger one system OCR fallback. Discarded low-score detection regions, low-confidence whitespace and geometry-only risks do not trigger it. Replacement requires coverage of local text regions and preservation of trusted text.
- Failed initialization is retried on a later request. A broken system-worker pipe gets one transfer retry. Cancellation prevents stale results from being published but cannot interrupt native inference already running.

Source images are limited to 16 Mi pixels and a 16,384-pixel side; detection to 1,000 candidates; recognition inputs to 2,048 pixels wide. Exceeded budgets return an error or use system fallback. Complex tables, vertical/inverted text and exact code indentation are not guaranteed.

## Idle resource lifetime

Each engine has its own 5-minute idle deadline, measured from the actual completion of its most recent request. Using system OCR does not renew idle local sessions, or vice versa. Local inference and system fallback share one admission gate for the full request. Queued requests, active inference and cancellation while a native call is still running prevent idle release; cancellation alone does not mean inference has returned. Once admission is returned, already-expired resources are scheduled immediately. Admission/worker contention uses a one-second retry deadline that requests using the other engine do not postpone.

When idle, Sotto closes both local model sessions, drops the recognizer and dictionary references, and terminates and waits for an idle PowerShell worker. The next request initializes the resources it needs again; failed initialization is not cached. ONNX Runtime's successful DLL, environment and shared memory-info initialization remain loaded for the process lifetime. Idle cleanup does not unload the runtime, release the screenshot/bubble WebViews, force a GC or call `FreeOSMemory`. It targets retained OCR resources, not peak inference memory or a guaranteed return to the pre-OCR baseline.

`SOTTO_OCR_IDLE` is a diagnostic duration override, read once at process startup, for example `5s` or `1m30s`. Only positive durations are accepted; invalid values produce one diagnostic and use 5 minutes. There is no UI setting or zero-duration keep-alive mode. Use the default 5-minute duration for at least one acceptance run. Routine idle release adds no log message; an `init` stage on the next OCR summary indicates reconstruction.

## Diagnostics

During development, capture the same region several times in the app and inspect the terminal's `ocr:` summary: total time, selected engine, detection size, main stage durations, risk reasons and fallback costs. Failed requests additionally log full attempt details. These logs exclude recognized text and image data. Full diagnostics remain available in the recognition result; numeric fields ending in `Us` are microseconds.

The first successful local call, and the first call after idle release, include model verification and session initialization. Calls while warm reuse the sessions. Detection/recognition inference timings include the native bridge and output copy. Total OCR time includes queue wait, but excludes screenshot capture and translation. Both engine attempts remain visible when fallback supplies the final result.

For comparisons, set environment variables before launching a fresh app process:

| `SOTTO_OCR_PROFILE` | Short-side target | Long-side cap | Pixel budget |
|---|---:|---:|---:|
| `balanced` (default) | 448 | 4,096 | 1 Mi |
| `legacy` | 736 | 4,096 | 4 Mi |
| `bounded` (experimental) | 64 | 960 | 1 Mi |

`SOTTO_OCR_DETECTOR_THREADS` overrides detector threads (1–16). The default uses up to 4 threads, capped by the logical CPU count; explicit overrides are not capped. Recognition stays at 2. Configuration is cached with the local recognizer and read again when sessions rebuild. Smaller detection inputs and additional threads require accuracy and performance checks on the target device.

### Windows memory and latency comparisons

Keep a build of the previous commit and fixed, non-private screenshot samples. Compare builds on the same Windows host, CPU, architecture and environment. Record the main process and its PowerShell worker separately; include PID, working set, Private Bytes, Go heap, thread/handle counts and actual sample times. Do not interpret working-set trimming alone as reclaimed private allocations.

For each build, record the unused baseline, initialized resources, repeated complete OCR calls, and the first complete OCR after idle release. Compare cold, warm and rebuilt requests using at least 5 observations per group, reporting median and maximum total, init, detection, recognition, fallback and queue-wait times. The added cost is rebuilt **complete OCR minus warm complete OCR**, not session construction alone. Reconstruction begins after screenshot selection finishes, so the selection gesture does not hide this delay.

Use a temporary diagnostic probe around resource release under admission to timestamp actual cleanup completion and measure session-close and worker-termination/Wait durations. Also control a request arriving during cleanup and record its queue wait and total time. Sample natural memory immediately, 30 seconds and 120 seconds after cleanup completes without another OCR request; record sampling offsets. Keep any explicit GC or `FreeOSMemory` experiment in separate runs. No such operations are part of the normal release path.

Repeat at least 20 release/rebuild cycles and check for growing private memory, worker count, handles or threads. Verify a new worker can recognize after release, use every 2–3 minutes keeps sessions warm, canceled native inference is never closed midway, and real Chinese/English/long screenshots and language-pack fallback retain their results. Hosted CI validates lifecycle and native session reconstruction but does not replace this desktop memory, accuracy and latency acceptance.

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

The preflight also closes and rebuilds the session pair 20 times while checking that the shared runtime is retained. The default tests use fake runners/clocks and helper child processes; native model loading and inference remain opt-in. The preflight does not measure recognition accuracy or end-to-end reconstruction latency. Check real screenshots on x64 and ARM64: small text, light/dark backgrounds, mixed Chinese/English, code and numbers. Also check repeated captures, cancellation, idle release/rebuild, missing language packs and corrupt resources.
