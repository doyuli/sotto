package pipeline

import (
	"context"
	"math"
	"os"
	"reflect"
	"strings"
	"testing"
)

func testDictionary() []byte {
	return []byte("A\n中\n" + strings.Repeat("字\n", DictionaryEntries-2))
}
func ctcTensor(classes int, ids []int, scores []float32) Tensor {
	t := Tensor{make([]float32, classes*len(ids)), []int64{1, int64(len(ids)), int64(classes)}}
	for step, id := range ids {
		t.Data[step*classes+id] = scores[step]
	}
	return t
}

func TestDictionaryContract(t *testing.T) {
	labels, err := ParseDictionary(testDictionary())
	if err != nil {
		t.Fatal(err)
	}
	if len(labels) != Classes || labels[0] != "" || labels[1] != "A" || labels[2] != "中" || labels[Classes-1] != " " {
		t.Fatal("dictionary shifted")
	}
	data := []byte(strings.Replace(string(testDictionary()), "A\n", " \r\n", 1))
	labels, err = ParseDictionary(data)
	if err != nil || labels[1] != " " {
		t.Fatalf("whitespace label trimmed: %q %v", labels, err)
	}
	for _, bad := range [][]byte{nil, []byte("a\nb\n"), append([]byte{0xff}, testDictionary()...), []byte(strings.Replace(string(testDictionary()), "A\n", "\n", 1))} {
		if _, err := ParseDictionary(bad); err == nil {
			t.Fatal("invalid dictionary accepted")
		}
	}
}

func TestPackagedDictionaryContract(t *testing.T) {
	data, err := os.ReadFile("../../../resources/ocr/ppocr_keys.txt")
	if err != nil {
		t.Fatal(err)
	}
	labels, err := ParseDictionary(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(labels) != 18710 {
		t.Fatal(len(labels))
	}
	if strings.Join(labels[1:len(labels)-1], "\n")+"\n" != string(data) {
		t.Fatal("dictionary was reordered or normalized")
	}
}

func TestCTCBlankDuplicatesUnicodeAndBatch(t *testing.T) {
	labels := []string{"", "A", "中", " "}
	input := ctcTensor(4, []int{1, 1, 0, 1, 2, 3, 3, 0}, []float32{.9, .2, 1, .7, .8, .6, .1, 1})
	got, err := decode(context.Background(), input, labels, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].text != "AA中 " || math.Abs(got[0].score-.75) > 1e-6 {
		t.Fatalf("decode %+v", got)
	}
	var steps []int
	for _, token := range got[0].tokens {
		steps = append(steps, token.Step)
	}
	if !reflect.DeepEqual(steps, []int{0, 3, 4, 5}) {
		t.Fatal(steps)
	}
	input.Data = append(input.Data, make([]float32, len(input.Data))...)
	input.Shape[0] = 2
	got, err = decode(context.Background(), input, labels, 1024)
	if err != nil || len(got) != 2 || got[1].text != "" || got[1].score != 0 {
		t.Fatalf("batch %+v %v", got, err)
	}
	input.Shape[2] = 5
	if _, err := decode(context.Background(), input, labels, 1024); err == nil {
		t.Fatal("bad shape accepted")
	}
	input = ctcTensor(4, []int{0}, []float32{1})
	if _, err := decode(context.Background(), input, labels[:3], 1024); err == nil {
		t.Fatal("class mismatch accepted")
	}
}
