package ocr

import (
	"reflect"
	"testing"
)

func TestVisionLanguageMapping(t *testing.T) {
	supported := []string{"en-US", "zh-Hans", "zh-Hant", "ja-JP"}
	for _, tc := range []struct {
		hint string
		want []string
	}{{"zh-HANS", []string{"zh-Hans"}}, {"zh-TW", []string{"zh-Hant"}}, {"ja", []string{"ja-JP"}}, {"en-GB", []string{"en-US"}}, {"", []string{"zh-Hans", "zh-Hant", "en-US"}}} {
		got, err := visionLanguages(tc.hint, supported)
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%s: %v %v", tc.hint, got, err)
		}
	}
	if _, err := visionLanguages("ko", supported); err == nil {
		t.Fatal("accepted unsupported language")
	}
}

func TestVisionCoordinatesFlipIntoLayoutSpace(t *testing.T) {
	q := visionQuad(.1, .7, .4, .2, 1000, 500)
	if q[0].X != 100 || q[1].X != 500 || q[0].Y < 49.999 || q[0].Y > 50.001 || q[2].Y < 149.999 || q[2].Y > 150.001 {
		t.Fatalf("quad=%+v", q)
	}
}
