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
	Models     []modelSpec `json:"models"`
	Assets     []assetSpec `json:"assets"`
	Dictionary struct {
		File string `json:"file"`
	} `json:"dictionary"`
}

func readManifest() (manifest, error) {
	var m manifest
	if err := json.Unmarshal(modelManifest, &m); err != nil {
		return m, fmt.Errorf("ocr: model manifest: %w", err)
	}
	if len(m.Models) != 2 || len(m.Assets) != 3 {
		return m, fmt.Errorf("ocr: incomplete model manifest")
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
