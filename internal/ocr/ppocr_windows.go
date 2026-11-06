//go:build windows

package ocr

import (
	"context"
	"fmt"
	"image"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// PP-OCR: the detector finds text boxes, the recognizer reads each box.
// Both run on the ONNX Runtime loaded by onnx_windows.go, with the
// standard PaddleOCR/RapidOCR pre- and post-processing: DB detection
// with a connected-component box decode, then CTC decoding against the
// recognizer's character dictionary.

const (
	ppAnchorThreshold = 0.30 // detector probability threshold
	ppBoxThreshold    = 0.50 // mean probability a box must reach
	ppUnclipRatio     = 1.6
	ppMinDetSide      = 64
	ppMaxDetSide      = 4000
	// Cap on detector input pixels: the float tensor is 3 channels × 4
	// bytes per pixel, so 6 MP keeps it around 72 MB even for a 4K shot.
	ppMaxDetPixels = 6_000_000
	ppRecHeight    = 48
	ppRecMaxWidth  = 3200
	ppTextScore    = 0.50
)

type ppOCR struct {
	once sync.Once
	det  *ortSession
	rec  *ortSession
	keys []string
	err  error
}

var ppEngine ppOCR

// recognizePPOCR runs the local PP-OCR pipeline; the caller falls back
// to the Windows recognizer when it is unavailable or returns nothing.
func recognizePPOCR(ctx context.Context, img image.Image) (Text, error) {
	ppEngine.once.Do(func() { ppEngine.err = ppEngine.init() })
	if ppEngine.err != nil {
		return Text{}, ppEngine.err
	}
	b := img.Bounds()
	if b.Dx() <= 0 || b.Dy() <= 0 {
		return Text{}, fmt.Errorf("ocr: empty image")
	}
	start := time.Now()

	boxes, err := ppEngine.detect(ctx, img)
	if err != nil {
		return Text{}, err
	}
	lines := make([]Line, 0, len(boxes))
	for _, box := range boxes {
		if err := ctx.Err(); err != nil {
			return Text{}, err
		}
		text, score := ppEngine.recognizeBox(img, box)
		if strings.TrimSpace(text) == "" || score < ppTextScore {
			continue
		}
		lines = append(lines, Line{Text: text, Box: box})
	}
	lines = orderLines(lines)
	parts := make([]string, len(lines))
	for i, l := range lines {
		parts[i] = l.Text
	}
	return Text{
		Text:    strings.Join(parts, "\n"),
		Lines:   lines,
		Engine:  "ppocr",
		Elapsed: time.Since(start).Round(time.Millisecond).String(),
	}, nil
}

// ppocrLanguages reports the languages the local PP-OCR model covers, or
// nil when its model files are missing.
func ppocrLanguages() []string {
	ppEngine.once.Do(func() { ppEngine.err = ppEngine.init() })
	if ppEngine.err != nil {
		return nil
	}
	return []string{"zh-Hans", "en"}
}

func (p *ppOCR) init() error {
	dir, _ := ocrResourcePaths()
	keys, err := loadPPOCRKeys(filepath.Join(dir, "ppocr_keys.txt"))
	if err != nil {
		return err
	}
	det, err := newORTSession(filepath.Join(dir, "det.onnx"), "x", "sigmoid_0.tmp_0")
	if err != nil {
		return err
	}
	rec, err := newORTSession(filepath.Join(dir, "rec.onnx"), "x", "softmax_5.tmp_0")
	if err != nil {
		return err
	}
	p.det, p.rec, p.keys = det, rec, keys
	return nil
}

func loadPPOCRKeys(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("ocr: read dictionary: %w", err)
	}
	text := strings.TrimRight(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	keys := strings.Split(text, "\n")
	if len(keys) == 0 {
		return nil, fmt.Errorf("ocr: empty dictionary")
	}
	// The recognizer exposes len(keys)+2 classes: blank at 0, the
	// dictionary at 1..n, and a trailing space.
	if keys[len(keys)-1] != " " {
		keys = append(keys, " ")
	}
	return keys, nil
}

// detect prepares the detector input, runs it and decodes boxes back in
// source-image coordinates.
func (p *ppOCR) detect(ctx context.Context, img image.Image) ([][4]int, error) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	ratio := 1.0
	if min(w, h) < ppMinDetSide {
		ratio = float64(ppMinDetSide) / float64(min(w, h))
	}
	sw := int(math.Round(float64(w) * ratio))
	sh := int(math.Round(float64(h) * ratio))
	if max(sw, sh) > ppMaxDetSide {
		ratio = float64(ppMaxDetSide) / float64(max(sw, sh))
		sw = int(math.Round(float64(sw) * ratio))
		sh = int(math.Round(float64(sh) * ratio))
	}
	inputW := roundToMultipleOf32(sw)
	inputH := roundToMultipleOf32(sh)
	// A 4K capture would otherwise need a ~100 MB float tensor.
	if inputW*inputH > ppMaxDetPixels {
		r := math.Sqrt(float64(ppMaxDetPixels) / float64(inputW*inputH))
		inputW = roundToMultipleOf32(int(float64(inputW) * r))
		inputH = roundToMultipleOf32(int(float64(inputH) * r))
	}

	cw, ch := inputW, inputH
	chSize := cw * ch
	data := make([]float32, 3*chSize)
	src := newPixelSource(img)
	// RGB, ImageNet mean/std, nearest-neighbour sampling. PP-OCR's
	// preprocessing expects RGB.
	for y := 0; y < ch; y++ {
		sy := min(h-1, int((float64(y)+0.5)*float64(h)/float64(ch)))
		for x := 0; x < cw; x++ {
			sx := min(w-1, int((float64(x)+0.5)*float64(w)/float64(cw)))
			r, g, bl := src.at(sx, sy)
			i := y*cw + x
			data[i] = float32((float64(r)/255 - 0.485) / 0.229)
			data[chSize+i] = float32((float64(g)/255 - 0.456) / 0.224)
			data[2*chSize+i] = float32((float64(bl)/255 - 0.406) / 0.225)
		}
	}
	out, dims, err := p.det.run(data, []int64{1, 3, int64(ch), int64(cw)})
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(dims) < 3 || dims[len(dims)-2] <= 0 || dims[len(dims)-1] <= 0 {
		return nil, fmt.Errorf("ocr: unexpected detector output shape %v", dims)
	}
	mapH := int(dims[len(dims)-2])
	mapW := int(dims[len(dims)-1])
	if len(out) < mapH*mapW {
		return nil, fmt.Errorf("ocr: short detector output")
	}
	probs := out[:mapH*mapW]
	// Some exports emit logits rather than sigmoid probabilities.
	for i, v := range probs {
		if v < 0 || v > 1 {
			probs[i] = float32(1 / (1 + math.Exp(-float64(v))))
		}
	}

	visited := make([]bool, len(probs))
	queue := make([]int32, 0, 1024)
	boxes := make([][4]int, 0, 16)
	scaleX := float64(w) / float64(cw)
	scaleY := float64(h) / float64(ch)
	for y := 0; y < mapH; y++ {
		for x := 0; x < mapW; x++ {
			start := y*mapW + x
			if visited[start] || probs[start] < ppAnchorThreshold {
				continue
			}
			queue = queue[:0]
			queue = append(queue, int32(start))
			visited[start] = true
			minX, maxX, minY, maxY := x, x, y, y
			area := 0
			score := float32(0)
			for head := 0; head < len(queue); head++ {
				cur := int(queue[head])
				cx, cy := cur%mapW, cur/mapW
				if cx < minX {
					minX = cx
				}
				if cx > maxX {
					maxX = cx
				}
				if cy < minY {
					minY = cy
				}
				if cy > maxY {
					maxY = cy
				}
				area++
				score += probs[cur]
				for dy := -1; dy <= 1; dy++ {
					for dx := -1; dx <= 1; dx++ {
						if dx == 0 && dy == 0 {
							continue
						}
						nx, ny := cx+dx, cy+dy
						if nx < 0 || nx >= mapW || ny < 0 || ny >= mapH {
							continue
						}
						n := ny*mapW + nx
						if !visited[n] && probs[n] >= ppAnchorThreshold {
							visited[n] = true
							queue = append(queue, int32(n))
						}
					}
				}
			}
			bw, bh := maxX-minX+1, maxY-minY+1
			if bw < 3 || bh < 3 || score/float32(area) < ppBoxThreshold {
				continue
			}
			dist := float64(bw*bh) * ppUnclipRatio / (2 * float64(bw+bh))
			x1 := clampFloat((float64(minX)-dist)*float64(cw)/float64(mapW)*scaleX, 0, float64(w))
			y1 := clampFloat((float64(minY)-dist)*float64(ch)/float64(mapH)*scaleY, 0, float64(h))
			x2 := clampFloat((float64(maxX)+1+dist)*float64(cw)/float64(mapW)*scaleX, 0, float64(w))
			y2 := clampFloat((float64(maxY)+1+dist)*float64(ch)/float64(mapH)*scaleY, 0, float64(h))
			if x2-x1 < 2 || y2-y1 < 2 {
				continue
			}
			boxes = append(boxes, [4]int{
				int(math.Floor(x1)), int(math.Floor(y1)),
				int(math.Ceil(x2 - x1)), int(math.Ceil(y2 - y1)),
			})
		}
	}
	return boxes, nil
}

// recognizeBox crops a box, feeds the recognizer and decodes its CTC
// output.
func (p *ppOCR) recognizeBox(img image.Image, box [4]int) (string, float64) {
	crop := cropRGB(img, box)
	if crop.w <= 0 || crop.h <= 0 {
		return "", 0
	}
	// Vertical text: rotate a tall crop onto its side (a 90° turn, the
	// usual PP-OCR preprocessing).
	if crop.h > crop.w*3/2 {
		crop = rotate90(crop)
	}
	targetW := int(math.Ceil(float64(ppRecHeight) * float64(crop.w) / float64(crop.h)))
	if targetW < 1 {
		targetW = 1
	}
	if targetW > ppRecMaxWidth {
		targetW = ppRecMaxWidth
	}
	chSize := ppRecHeight * targetW
	data := make([]float32, 3*chSize)
	for y := 0; y < ppRecHeight; y++ {
		sy := min(crop.h-1, int((float64(y)+0.5)*float64(crop.h)/float64(ppRecHeight)))
		for x := 0; x < targetW; x++ {
			sx := min(crop.w-1, int((float64(x)+0.5)*float64(crop.w)/float64(targetW)))
			si := (sy*crop.w + sx) * 3
			i := y*targetW + x
			data[i] = float32(crop.pix[si+2])/127.5 - 1
			data[chSize+i] = float32(crop.pix[si+1])/127.5 - 1
			data[2*chSize+i] = float32(crop.pix[si])/127.5 - 1
		}
	}
	out, dims, err := p.rec.run(data, []int64{1, 3, ppRecHeight, int64(targetW)})
	if err != nil || len(dims) != 3 || dims[2] <= 1 {
		return "", 0
	}
	timeSteps, classes := int(dims[1]), int(dims[2])
	if classes != len(p.keys)+1 {
		return "", 0
	}
	var sb strings.Builder
	var confSum float64
	var confN int
	prev := -1
	for t := 0; t < timeSteps; t++ {
		off := t * classes
		best, bestV := 0, out[off]
		for c := 1; c < classes; c++ {
			if out[off+c] > bestV {
				best, bestV = c, out[off+c]
			}
		}
		if best != 0 && best != prev {
			idx := best - 1
			if idx >= 0 && idx < len(p.keys) {
				sb.WriteString(p.keys[idx])
				confSum += float64(bestV)
				confN++
			}
		}
		prev = best
	}
	conf := 0.0
	if confN > 0 {
		conf = confSum / float64(confN)
	}
	return strings.TrimSpace(sb.String()), conf
}

type rgbCrop struct {
	pix  []byte // BGR
	w, h int
}

type pixelSource struct {
	img   image.Image
	nrgba *image.NRGBA
}

func newPixelSource(img image.Image) pixelSource {
	s := pixelSource{img: img}
	if n, ok := img.(*image.NRGBA); ok {
		s.nrgba = n
	}
	return s
}

// at reads one pixel, taking the direct path for the NRGBA captures the
// screen grabber produces and falling back to image.Image's interface for
// anything else.
func (s pixelSource) at(x, y int) (r, g, b uint8) {
	if s.nrgba != nil {
		i := s.nrgba.PixOffset(x, y)
		return s.nrgba.Pix[i], s.nrgba.Pix[i+1], s.nrgba.Pix[i+2]
	}
	return atRGB(s.img, x, y)
}

func cropRGB(img image.Image, box [4]int) rgbCrop {
	b := img.Bounds()
	x1 := clampInt(box[0], b.Min.X, b.Max.X-1)
	y1 := clampInt(box[1], b.Min.Y, b.Max.Y-1)
	x2 := clampInt(box[0]+box[2], x1+1, b.Max.X)
	y2 := clampInt(box[1]+box[3], y1+1, b.Max.Y)
	w, h := x2-x1, y2-y1
	pix := make([]byte, w*h*3)
	src := newPixelSource(img)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r, g, bl := src.at(x1+x, y1+y)
			i := (y*w + x) * 3
			pix[i], pix[i+1], pix[i+2] = bl, g, r
		}
	}
	return rgbCrop{pix: pix, w: w, h: h}
}

func rotate90(c rgbCrop) rgbCrop {
	out := rgbCrop{pix: make([]byte, len(c.pix)), w: c.h, h: c.w}
	for y := 0; y < c.h; y++ {
		for x := 0; x < c.w; x++ {
			// (x,y) -> (y, w-1-x): the crop turns onto its side, matching
			// the PP-OCR worker's rotation.
			nx, ny := y, c.w-1-x
			si := (y*c.w + x) * 3
			di := (ny*out.w + nx) * 3
			out.pix[di], out.pix[di+1], out.pix[di+2] = c.pix[si], c.pix[si+1], c.pix[si+2]
		}
	}
	return out
}

func atRGB(img image.Image, x, y int) (r, g, b uint8) {
	c := img.At(x, y)
	rr, gg, bb, _ := c.RGBA()
	return uint8(rr >> 8), uint8(gg >> 8), uint8(bb >> 8)
}

func roundToMultipleOf32(v int) int {
	if v%32 == 0 {
		return max(32, v)
	}
	return max(32, (v/32+1)*32)
}

func clampFloat(v, lo, hi float64) float64 {
	return math.Min(math.Max(v, lo), hi)
}

func clampInt(v, lo, hi int) int {
	return min(max(v, lo), hi)
}
