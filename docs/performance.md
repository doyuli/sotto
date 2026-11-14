# Performance measurements

Use release builds and compare the same workload on the same machine. Go heap
allocation benchmarks do not measure native image storage, GPU surfaces, OCR
arenas, or child processes. Record OS version, CPU/architecture, build commit,
display dimensions/scales, selected services, fixture, and power mode with results.

## Package inventory

```sh
CGO_ENABLED=0 bun run build:win -skip-installer
CGO_ENABLED=0 bun run build:mac -skip-dmg -skip-notarize
python3 -B scripts/performance.py sizes build/windows-amd64 build/windows-arm64 build/darwin-arm64/Sotto.app > sizes.json
```

On Windows use `python` and build/report the Windows directories. CI prints the
complete byte inventory after each platform build. Missing or empty directories
fail instead of reporting a false improvement. Values are logical file sizes,
not filesystem allocation or compressed installer/download size. Report installers
and updater archives separately after release packaging.

Local baseline, 2026-11-14, Go release builds with MyGo 0.8.3 at `3e1929c`:

| Payload (MiB = 1,048,576 bytes) | Windows x64 | Windows ARM64 | macOS ARM64 |
| --- | ---: | ---: | ---: |
| Complete package/app | 65.43 | 62.07 | 12.80 |
| Executable | 17.14 | 15.48 | 12.59 |
| Detection model | 9.47 | 9.47 | — |
| Recognition model | 20.25 | 20.25 | — |
| ONNX Runtime main DLL | 18.40 | 16.70 | — |

Windows model/runtime assets dominate the distribution. The Mac app already omits
all Windows OCR assets. MyGo's production builder already uses `-trimpath -s -w`;
adding strip flags again provides no saving. These reliability changes add a small
amount of executable code; this is a baseline, not a claimed package reduction.

Model quantization or a smaller model requires a separate quality experiment:
compare Chinese/English/Japanese/Latin fixtures, dense UI, low contrast, rotation,
small text, and high DPI; record character error rate, missing lines, latency and
peak memory on both Windows architectures. Reject candidates that reduce language
coverage or regress the accepted fixtures. Validate dictionary compatibility,
license notices and all manifest hashes before replacing assets. Runtime operator
pruning needs equivalent model coverage and both DLL architectures. Neither model
replacement nor on-demand asset download is part of these changes.

## Process counters

```sh
# Supply the actual application PID. Add --pid again for a known OCR worker.
python3 -B scripts/performance.py sample --pid 1234 --duration 90 --interval 1 --label idle-after-ocr > idle.jsonl
```

The sampler only reads explicitly supplied PIDs. It never starts, changes priority,
trims memory, captures screens, or terminates an application. It exits on a failed
read or a reused PID; start a new recording for a restarted process. Python's
standard library is sufficient. The collector itself is a separate process.

- macOS: `proc_pid_rusage(RUSAGE_INFO_V2)` records RSS, physical footprint, CPU time
  and wakeup counters, as defined by the system SDK's `sys/resource.h` and `libproc.h`.
- Windows: [GetProcessMemoryInfo / PROCESS_MEMORY_COUNTERS_EX](https://learn.microsoft.com/en-us/windows/win32/api/psapi/ns-psapi-process_memory_counters_ex)
  records working set and private committed bytes; [GetProcessTimes](https://learn.microsoft.com/en-us/windows/win32/api/processthreadsapi/nf-processthreadsapi-getprocesstimes)
  provides CPU time. Handle count is also recorded. Private bytes are not private
  working set; do not compare them directly to macOS physical footprint.
- `cpu_percent_one_core` uses CPU-time deltas and monotonic elapsed time. One fully
  occupied core is 100%; multiple cores can exceed 100%. The first sample is null.
  Wakeup fields are cumulative; compare deltas. Per-process values stay separate
  because summing RSS/working sets can double-count shared pages.

## Repeatable hardware matrix

Run each sequence at least three times from a fresh app process, with the same
fixtures. Preserve raw JSONL and workload timestamps. Do not force GC or trim the
working set: that obscures normal retention and adds reallocation latency.

| Sequence | Measurements and acceptance |
| --- | --- |
| Cold launch, tray idle 60 s | Startup peak; settled memory, idle CPU and wakeups; no periodic growth |
| Open/close settings and result bubbles 20 times | Peak and retained memory; native windows/handles stop growing |
| Screenshot → cancel 20 times, then idle 60 s | Overlays disappear; 10 s overlay cleanup; no rising retained surfaces |
| OCR same fixture 20 times, then idle 60 s | Peak, latency, output correctness; Windows 30 s model/worker idle release |
| Cancel during OCR, then immediately retry | Cancellation latency, no stale output, next request succeeds; never free a tensor still in use |
| Multi-display with mixed DPI/scales and topology changes | Correct crops, placement and native window cleanup |
| Large clipboard with multiple items/types, cancel, concurrent copy | Responsive input between representations; preserve every type; never overwrite newer content |
| 18,000-rune input and oversized input/output | Bounded work; visible limit error; preserved editable draft and accepted partial result |

Run on Windows x64 and ARM64, and on macOS 14 plus a current macOS release on Apple
Silicon. CI compilation is not hardware acceptance. Full-screen capture,
permissions, focus and outside-click behavior follow the opt-in procedures in
[e2e](../e2e/README.md). The existing maintainer-confirmed desktop acceptance remains
recorded there; this optimization matrix needs fresh measurements for each platform.

## Measured Mac conversion improvement

```sh
CGO_ENABLED=0 SOTTO_MACOS_NATIVE=1 go test ./internal/macnative -run '^$' -bench BenchmarkVisionInputConversion -benchmem -count=3 -benchtime=200ms
```

Apple M2, 1920×1080 deterministic RGBA fixture, three local runs on 2026-11-14:
PNG input preparation took 31.7–33.4 ms and ~927 KB of Go allocations per operation;
the native pixel path took 0.72–0.82 ms and 444–475 B. This benchmark includes the
former Go PNG/NSData preparation or the new CGImage preparation, but excludes
Vision execution, native allocations and application footprint. Native tests also
verify orientation, alpha, subimage stride, owned buffer lifetime, real Vision
recognition, and isolated pasteboard cancellation/version changes.
