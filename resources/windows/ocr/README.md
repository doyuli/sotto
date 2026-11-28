# Windows offline OCR

On Windows, Sotto uses PaddleOCR's PP-OCRv6 small models, executed locally on CPU by ONNX Runtime 1.31.1 (for overall Windows platform architecture, see [Windows Architecture](../../../docs/windows.md)). The [PaddleOCR model documentation](https://github.com/PaddlePaddle/PaddleOCR/blob/main/docs/version3.x/algorithm/PP-OCRv6/PP-OCRv6.en.md) defines their 50-language scope: Simplified/Traditional Chinese, English, Japanese, and 46 Latin-script languages. This does not cover all languages.

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

[`internal/ocr/models.json`](../../../internal/ocr/models.json) records asset sizes, SHA-256 hashes, download URLs and tensor contracts. Update both models and the dictionary together. Tests and every local session initialization verify asset integrity.

Keep the dictionary's original LF bytes, whitespace entries and final newline. `.gitattributes` enforces LF on checkout. To repair an old CRLF checkout, restore the tracked dictionary and rebuild:

```powershell
git checkout-index --force -- resources/windows/ocr/ppocr_keys.txt
bun run build:win:x64
```

The models and dictionary live in `resources/windows/ocr`, shared by both Windows architectures. macOS uses the system Vision framework and ships neither these assets nor ONNX Runtime. Runtime DLLs live in `resources/windows-amd64/ocr` and `resources/windows-arm64/ocr`. MyGo builds combine the Windows assets and architecture-specific DLLs into `ocr/` beside `Sotto.exe`. `SOTTO_OCR_DIR` overrides that directory and must contain the complete model set and matching DLLs.

## Recognition behavior

- The screenshot UI supplies no language hint. It tries PPOCR first without detecting the screenshot's script in advance. Nonempty misrecognitions of unsupported scripts are returned as local results.
- Sessions initialize on first use and are reused serially until idle release. Detection defaults to `min(4, runtime.NumCPU())` inference threads; recognition uses 2.
- Detection targets a short side of 448, capped at 1 Mi pixels and a 4,096-pixel side. Recognition crops use the original screenshot.
- Successful local results with nonempty text after cleanup and layout are returned immediately. Confidence, character, empty-line and geometry risks remain in diagnostics. Local errors or an empty final text trigger one system OCR fallback. Cancellation and request deadlines terminate the request. Explicit language hints outside the local model's scope go directly to system OCR.
- Failed initialization is retried on a later request. A broken system-worker pipe gets one transfer retry. Cancellation sets the current ONNX Runtime run-options termination flag from a background callback. The request waits for native inference and that callback to return before releasing tensors or run options; each new run receives fresh options. This is cooperative native cancellation, not a hard deadline for every kernel.

Source images are limited to 16 Mi pixels and a 16,384-pixel side; detection to 1,000 candidates; recognition inputs to 2,048 pixels wide. Exceeded budgets return an error or use system fallback. Complex tables, vertical/inverted text and exact code indentation are not guaranteed.

Screenshot capture applies the same source-image budget after DPI conversion and client-bound clipping, before creating a GDI DIB or Go image. Win32 coordinate ranges and pixel-storage arithmetic are checked before narrowing or allocation. Oversized selections ask the user to select a smaller region.

Native model outputs are borrowed by synchronous pipeline consumers: full probability validation, detection postprocessing and CTC decoding finish before `OrtValue` is released. The bridge no longer copies complete output tensors into Go slices. Both tensor data and shape are valid only during the consumer callback; published lines and tokens own their results. Cancellation never releases a native output during consumption, and consumer errors or panics unwind native cleanup and the session lock. This removes output-copy allocations, not model outputs, ORT arenas or a guaranteed amount of working set.

## Idle resource lifetime

PP-OCR has a 5-minute idle deadline, and Windows OCR has a 30-second idle deadline. Each is measured from the actual completion of the most recent request that used that engine. Using system OCR does not renew idle local sessions, or vice versa. Local inference and system fallback share one admission gate for the full request. Queued requests, active inference and cancellation while a native call is still running prevent idle release; cancellation alone does not mean inference has returned. Once admission is returned, already-expired resources are scheduled immediately. Admission/worker contention uses a one-second retry deadline that requests using the other engine do not postpone.

When idle, Sotto closes both local model sessions, drops the recognizer and dictionary references, and terminates and waits for an idle PowerShell worker. The next request initializes the resources it needs again; failed initialization is not cached. Local idle release also frees the shared memory-info object and environment, then unloads ONNX Runtime. A dismissed, unpinned translation bubble is destroyed after five minutes hidden. Used screenshot overlays are destroyed after their capture leases return, and unused hidden overlays expire after ten seconds. After translation or capture activity, fifteen minutes with no visible windows or active jobs triggers one background `debug.FreeOSMemory()` call. The amount returned depends on the idle Go heap.

Set `SOTTO_OCR_IDLE` for PP-OCR or `SOTTO_OCR_WINRT_IDLE` for Windows OCR. Sotto reads both variables at startup. Each accepts positive durations such as `5s` and `1m30s`. An empty value uses that engine's default. An invalid value logs a diagnostic and uses the same default. Use both default deadlines for at least one acceptance run. An `init` stage on the next OCR summary indicates reconstruction after idle release.

## Diagnostics

During development, capture the same region several times in the app and inspect the terminal's `ocr:` summary: total time, selected engine, detection size, main stage durations, risk reasons and fallback costs. Failed requests additionally log full attempt details. These logs exclude recognized text and image data. Full diagnostics remain available in the recognition result; numeric fields ending in `Us` are microseconds.

The first successful local call, and the first call after idle release, include model verification and session initialization. Calls while warm reuse the sessions. Detection/recognition inference timings include the native bridge and cleanup, excluding synchronous output consumers. Detection postprocessing and CTC decoding retain their separate timing fields. Total OCR time includes queue wait, but excludes screenshot capture and translation. Both engine attempts remain visible when fallback supplies the final result.

For comparisons, set environment variables before launching a fresh app process:

| `SOTTO_OCR_PROFILE` | Short-side target | Long-side cap | Pixel budget |
|---|---:|---:|---:|
| `balanced` (default) | 448 | 4,096 | 1 Mi |
| `legacy` | 736 | 4,096 | 4 Mi |
| `bounded` (experimental) | 64 | 960 | 1 Mi |

`SOTTO_OCR_DETECTOR_THREADS` overrides detector threads (1–16). The default uses up to 4 threads, capped by the logical CPU count; explicit overrides are not capped. Recognition stays at 2. Configuration is cached with the local recognizer and read again when sessions rebuild. Smaller detection inputs and additional threads require accuracy and performance checks on the target device.

### Windows memory and latency comparisons

Keep a build of the previous commit and fixed, non-private screenshot samples. Compare builds on the same Windows host, CPU, architecture and environment. Use Task Manager to observe CPU, working set, commit size, thread and handle counts for the main process and its PowerShell worker separately. Record the build and scenario alongside each observation; a smaller working set alone does not establish that all allocations were released.

For each build, compare a fresh idle app, the first OCR, repeated warm OCR, the idle state after resources are released, and the first OCR after rebuilding resources. Observe end-to-end responsiveness after completing the screenshot selection, including requests arriving during cleanup. Note visible pauses and whether memory settles after idle; custom heap probes and precise sampling schedules are not required for acceptance.

Repeat at least 20 release/rebuild cycles and check for growing private memory, worker count, handles or threads. With the defaults, verify that using PP-OCR every 2–3 minutes keeps sessions warm and that two Windows OCR calls within 30 seconds reuse the worker. Check that a new worker can recognize after release, canceled native inference is never closed midway, and real Chinese/English/long screenshots and language-pack fallback retain their results. Hosted CI validates lifecycle and native session reconstruction but does not replace this desktop memory, accuracy and latency acceptance.

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

The preflight also exercises consumer errors, cancellation and panic recovery before reusing each native session, then closes and rebuilds the session pair 20 times while checking that the shared runtime is released and rebuilt. The default tests use fake runners/clocks and helper child processes; native model loading and inference remain opt-in. The preflight does not measure recognition accuracy or end-to-end reconstruction latency. Check real screenshots on x64 and ARM64: small text, light/dark backgrounds, mixed Chinese/English, code and numbers. Confirm nonempty local results with confidence risks or partially empty lines use one local attempt and retain their diagnostics. Empty final text and corrupt local resources should attempt system OCR once. Also check both engines finding no text, missing language packs, repeated captures, cancellation and idle release/rebuild.

To compare Go allocation traffic with and without the former recognition-output copy:

```powershell
$env:SOTTO_OCR_PREFLIGHT = "1"
go test ./internal/ocr -run '^$' -bench '^BenchmarkV6SmallRecognitionOutput$' -benchmem -count=3
Remove-Item Env:SOTTO_OCR_PREFLIGHT
```

Both benchmark cases run the same warm native recognition model and checksum consumer on a zero input. `copied` adds a full output copy; `borrowed` consumes native storage directly. The comparison reports Go allocation traffic and bridge/inference time, excluding image preprocessing and CTC decoding. Native allocations and complete-OCR peak/idle process memory require desktop sampling.

Capture acceptance additionally covers 1080p/4K selections, mixed DPI and negative monitor origins, and rejecting an over-budget physical selection before GDI allocation. A valid clipped selection must still work even when its unclipped dimensions exceed the budget. Check the smaller-selection error text in the screenshot bubble.


The Windows OCR worker reads UTF-8 requests through a buffered StreamReader,
clears per-request managed payload references after disposing native images, and
still expires after 30 seconds. Run `SOTTO_OCR_PREFLIGHT=1 go test ./internal/ocr
-run '^TestWinRTNativeRepeatedRequests$' -count=1 -v` on Windows to exercise two
requests against the real worker and installed language packs. Default Windows
tests parse the embedded PowerShell script and exercise pipe recovery separately.
