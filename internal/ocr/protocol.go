package ocr

import (
	"bufio"
	"fmt"
)

func readProtocolLine(reader *bufio.Reader) (string, error) {
	const limit = 8 << 20
	var line []byte
	for {
		part, err := reader.ReadSlice('\n')
		if len(part) > limit-len(line) {
			return "", fmt.Errorf("ocr: oversized worker response")
		}
		line = append(line, part...)
		if err == bufio.ErrBufferFull {
			continue
		}
		return string(line), err
	}
}
