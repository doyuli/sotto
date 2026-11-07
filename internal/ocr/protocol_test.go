package ocr

import (
	"bufio"
	"io"
	"strings"
	"testing"
)

func TestWorkerResponseBoundaries(t *testing.T) {
	input := strings.Repeat("x", 4000) + "\n"
	reader := bufio.NewReaderSize(strings.NewReader(input+"next\n"), 32)
	got, err := readProtocolLine(reader)
	if err != nil || got != input {
		t.Fatalf("fragmented response %d %v", len(got), err)
	}
	if got, err := readProtocolLine(reader); err != nil || got != "next\n" {
		t.Fatalf("next response %q %v", got, err)
	}
	if _, err := readProtocolLine(reader); err != io.EOF {
		t.Fatal(err)
	}
	reader = bufio.NewReaderSize(strings.NewReader(strings.Repeat("x", (8<<20)+1)), 1024)
	if _, err := readProtocolLine(reader); err == nil {
		t.Fatal("unbounded worker response")
	}
}
