package ocr

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"sotto/internal/ocr/pipeline"
)

//go:embed models.json
var modelManifest []byte

type modelSpec struct {
	File          string `json:"file"`
	InputName     string `json:"inputName"`
	OutputName    string `json:"outputName"`
	OutputRank    int    `json:"outputRank"`
	OutputClasses int64  `json:"outputClasses"`
}
type assetSpec struct {
	File   string `json:"file"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}
type manifest struct {
	SchemaVersion int         `json:"schemaVersion"`
	Models        []modelSpec `json:"models"`
	Assets        []assetSpec `json:"assets"`
	Dictionary    struct {
		File    string `json:"file"`
		Entries int    `json:"entries"`
		Classes int    `json:"classes"`
	} `json:"dictionary"`
}

func readManifest() (manifest, error) {
	return parseManifest(modelManifest)
}

func parseManifest(data []byte) (manifest, error) {
	var m manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return m, fmt.Errorf("ocr: model manifest: %w", err)
	}
	if m.SchemaVersion != 1 || len(m.Models) != 2 || len(m.Assets) != 3 {
		return m, fmt.Errorf("ocr: incomplete model manifest")
	}
	// Session construction relies on this order and the decoder's fixed class
	// contract. Reject incompatible metadata before opening any native session.
	if m.Models[0].File != "det.onnx" || m.Models[0].OutputRank != 4 ||
		m.Models[1].File != "rec.onnx" || m.Models[1].OutputRank != 3 || m.Models[1].OutputClasses != pipeline.Classes ||
		m.Dictionary.File != "ppocr_keys.txt" || m.Dictionary.Entries != pipeline.DictionaryEntries || m.Dictionary.Classes != pipeline.Classes {
		return m, fmt.Errorf("ocr: incompatible model manifest")
	}
	for _, model := range m.Models {
		if model.InputName == "" || model.OutputName == "" {
			return m, fmt.Errorf("ocr: missing model tensor name")
		}
	}
	wanted := map[string]bool{"det.onnx": true, "rec.onnx": true, "ppocr_keys.txt": true}
	for _, asset := range m.Assets {
		hash, err := hex.DecodeString(asset.SHA256)
		if !wanted[asset.File] || asset.Bytes <= 0 || err != nil || len(hash) != sha256.Size {
			return m, fmt.Errorf("ocr: invalid manifest asset: %s", asset.File)
		}
		delete(wanted, asset.File)
	}
	return m, nil
}
func verifyAsset(ctx context.Context, dir string, spec assetSpec) error {
	f, err := os.Open(filepath.Join(dir, spec.File))
	if err != nil {
		return fmt.Errorf("ocr: open %s: %w", spec.File, err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("ocr: stat %s: %w", spec.File, err)
	}
	if !info.Mode().IsRegular() || info.Size() != spec.Bytes {
		return fmt.Errorf("ocr: invalid asset size: %s", spec.File)
	}
	h := sha256.New()
	buf := make([]byte, 64<<10)
	reader := io.LimitReader(f, spec.Bytes+1)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := reader.Read(buf)
		total += int64(n)
		_, _ = h.Write(buf[:n])
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("ocr: read %s: %w", spec.File, err)
		}
	}
	if total != spec.Bytes || hex.EncodeToString(h.Sum(nil)) != spec.SHA256 {
		return fmt.Errorf("ocr: asset checksum mismatch: %s", spec.File)
	}
	return nil
}
