package ocr

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestPackagedModels(t *testing.T) {
	m, err := readManifest()
	if err != nil {
		t.Fatal(err)
	}
	for _, asset := range m.Assets {
		if err := verifyAsset(context.Background(), "../../resources/ocr", asset); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAssetCorruptionAndCancellation(t *testing.T) {
	dir := t.TempDir()
	data := []byte("model bytes")
	sum := sha256.Sum256(data)
	spec := assetSpec{File: "model.onnx", Bytes: int64(len(data)), SHA256: hex.EncodeToString(sum[:])}
	path := filepath.Join(dir, spec.File)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := verifyAsset(context.Background(), dir, spec); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := verifyAsset(ctx, dir, spec); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	data[0] = 'x'
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := verifyAsset(context.Background(), dir, spec); err == nil {
		t.Fatal("same-size corruption accepted")
	}
	if err := os.WriteFile(path, data[:2], 0600); err != nil {
		t.Fatal(err)
	}
	if err := verifyAsset(context.Background(), dir, spec); err == nil {
		t.Fatal("truncated model accepted")
	}
}
