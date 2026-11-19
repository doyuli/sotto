// Package usermessage converts errors to short text for the desktop UI.
package usermessage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/url"
	"os"
	"regexp"
	"strings"
	"unicode"
)

type explanation string

func (e explanation) Error() string { return string(e) }

// New marks a message that has already been checked for display.
func New(message string) error { return explanation(message) }

// Explain attaches a known user-facing message while preserving the diagnostic cause.
func Explain(err error, message string) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w: %w", New(message), err)
}

// Format adds the operation to an error message when a safe detail is available.
func Format(operation string, err error) string {
	detail := Text(err)
	if detail == "" || detail == operation {
		return operation
	}
	return operation + "：" + detail
}

func Text(err error) string {
	var explained explanation
	var networkErr net.Error
	var urlErr *url.Error
	var syntaxErr *json.SyntaxError
	var typeErr *json.UnmarshalTypeError
	switch {
	case err == nil:
		return ""
	case errors.As(err, &explained):
		return string(explained)
	case errors.Is(err, context.Canceled):
		return "操作已取消"
	case errors.Is(err, context.DeadlineExceeded) || errors.As(err, &networkErr) && networkErr.Timeout():
		return "连接超时，请稍后重试"
	case errors.As(err, &urlErr):
		return "网络连接失败，请检查网络后重试"
	case errors.Is(err, fs.ErrPermission):
		return "权限不足，请检查权限后重试"
	case diskFull(err):
		return "磁盘空间不足，请清理空间后重试"
	case errors.Is(err, fs.ErrNotExist):
		return "所需文件或目录不存在，请检查后重试"
	case errors.As(err, &syntaxErr), errors.As(err, &typeErr):
		return "数据格式异常，请稍后重试"
	case errors.Is(err, io.ErrUnexpectedEOF), errors.Is(err, io.EOF):
		return "数据不完整，请稍后重试"
	}
	// Path and link errors carry names in addition to the useful system cause.
	var pathErr *fs.PathError
	var linkErr *os.LinkError
	switch {
	case errors.As(err, &pathErr):
		return Text(pathErr.Err)
	case errors.As(err, &linkErr):
		return Text(linkErr.Err)
	}
	return summarize(err.Error())
}

// Keep only the prose before URLs, paths, structured payloads or credential
// fields. Cutting the suffix also covers quoted values and paths with spaces.
var diagnosticSuffix = regexp.MustCompile(`(?i)[<{\x5b]` +
	`|[a-z][a-z0-9+.-]*://|\b(?:mailto|data):|\b[\w.+-]+@[\w.-]+\.[a-z]{2,}` +
	`|(?:^|[\s:：="'\x60（(])(?:[a-z]:[/\\]|\\\\|(?:~|\.{1,2})?/)` +
	`|\b(?:authorization|bearer|password|passphrase|credentials?|client[ _-]secret|secret(?:[ _-]?key)?|(?:api|access|private)[ _-]?key|(?:access[ _-]?)?token|cookie)\b` +
	`|\b(?:sk-|gh[pousr]_|github_pat_)[\w-]+`)

func summarize(raw string) string {
	line := strings.TrimSpace(raw)
	if i := strings.IndexAny(line, "\r\n\v\f\u0085\u2028\u2029"); i >= 0 {
		line = line[:i]
	}
	line = strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return ' '
		}
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, line)
	line = strings.TrimSpace(strings.ReplaceAll(strings.TrimPrefix(line, "mygo:"), "mygo: ", ""))
	if match := diagnosticSuffix.FindStringIndex(line); match != nil {
		line = line[:match[0]]
	}
	line = strings.Trim(strings.Join(strings.Fields(line), " "), " :：,，;；=-\"'（(")
	const maxRunes = 120
	count := 0
	for i := range line {
		if count == maxRunes {
			return strings.TrimSpace(line[:i]) + "…"
		}
		count++
	}
	return line
}
