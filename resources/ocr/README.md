# PP-OCR resources

The local OCR engine reads these files from the `ocr` directory next to
`sotto.exe` (in a build) or `resources/ocr` (from a checkout).

| File | What it is |
|---|---|
| `det.onnx` | PP-OCRv3 text detector |
| `rec.onnx` | PP-OCRv3 recognizer (Chinese + English) |
| `ppocr_keys.txt` | Recognizer character dictionary |
| `onnxruntime.dll` | ONNX Runtime 1.31.1 runtime |
| `onnxruntime_providers_shared.dll` | ONNX Runtime shared provider library |

## Sources and licences

- `det.onnx` and `rec.onnx` are the `ch_PP-OCRv3_det_infer.onnx` and
  `ch_PP-OCRv3_rec_infer.onnx` models bundled with
  [RapidOCR](https://github.com/RapidAI/RapidOCR) (`rapidocr_onnxruntime`
  on PyPI). They come from PaddleOCR, licensed under the Apache License
  2.0.
- `ppocr_keys.txt` is the recognizer's `character` metadata, exported
  from `rec.onnx` (the PaddleOCR `ppocr_keys_v1.txt` dictionary).
- `onnxruntime.dll` and `onnxruntime_providers_shared.dll` are from
  [ONNX Runtime](https://github.com/microsoft/onnxruntime) 1.31.1,
  licensed under the MIT License.

Keep the models and the runtime version in step: the Go wrapper walks
the ONNX Runtime C API table for API version 31, matching the 1.31
runtime shipped here.
