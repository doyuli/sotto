// Package pipeline implements the platform-independent PP-OCRv6 small pipeline.
//
// Recognizer owns its immutable dictionary and configuration. A Runner consumes
// input synchronously and lends output to a synchronous consumer. Consumers must
// finish reading both Data and Shape before returning; only independently owned
// results may escape. Callers serialize access when their Runner is not
// concurrent-safe. No native call is forcibly
// interrupted: cancellation is checked before/after Run and in CPU loops.
//
// All quads use screenshot-local pixels (Bounds().Min is subtracted), ordered
// top-left, top-right, bottom-right, bottom-left. Results own their slices;
// Layout does not mutate them and references their original line indices.
package pipeline
