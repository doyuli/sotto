package ocr

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestManifestContract(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*manifest)
	}{
		{"schema", func(m *manifest) { m.SchemaVersion++ }},
		{"model order", func(m *manifest) { m.Models[0], m.Models[1] = m.Models[1], m.Models[0] }},
		{"dictionary entries", func(m *manifest) { m.Dictionary.Entries-- }},
		{"dictionary classes", func(m *manifest) { m.Dictionary.Classes-- }},
		{"model classes", func(m *manifest) { m.Models[1].OutputClasses-- }},
		{"model rank", func(m *manifest) { m.Models[0].OutputRank-- }},
		{"tensor name", func(m *manifest) { m.Models[1].InputName = "" }},
		{"duplicate asset", func(m *manifest) { m.Assets[1] = m.Assets[0] }},
		{"asset path", func(m *manifest) { m.Assets[0].File = "../det.onnx" }},
		{"asset hash", func(m *manifest) { m.Assets[0].SHA256 = "bad" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, err := readManifest()
			if err != nil {
				t.Fatal(err)
			}
			tc.change(&m)
			data, err := json.Marshal(m)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := parseManifest(data); err == nil {
				t.Fatal("accepted incompatible manifest")
			}
		})
	}
}

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
