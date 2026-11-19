package usermessage

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"strings"
	"syscall"
	"testing"
)

func TestTextUsesKnownMessagesAndRedactsUnknownDetails(t *testing.T) {
	// UI errors can wrap operating-system diagnostics or contain untrusted text.
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"permission", &fs.PathError{Op: "open", Path: "/Users/private/settings.json", Err: fs.ErrPermission}, "权限不足，请检查权限后重试"},
		{"disk full", fmt.Errorf("save settings: %w", syscall.ENOSPC), "磁盘空间不足，请清理空间后重试"},
		{"timeout", &url.Error{Op: "Get", URL: "https://example.invalid/?token=fixture", Err: context.DeadlineExceeded}, "连接超时，请稍后重试"},
		{"network failure", &url.Error{Op: "Post", URL: "https://example.invalid/", Err: errors.New("connection refused")}, "网络连接失败，请检查网络后重试"},
		{"unknown file cause", &fs.PathError{Op: "open", Path: "/Users/private/settings.json", Err: errors.New("unexpected lock")}, "unexpected lock"},
		{"unknown framework error", errors.New("mygo: unexpected lock\tC:\\Users\\Private Folder\\settings.json\nfollow-up trace"), "unexpected lock"},
		{"path after Chinese colon", errors.New("保存失败：/Users/example/settings.json"), "保存失败"},
		{"unknown URL", errors.New("endpoint refused https://example.invalid/?token=fixture"), "endpoint refused"},
		{"unknown credential", errors.New("unexpected reply: Authorization: Bearer fixture"), "unexpected reply"},
		{"client secret field", errors.New(`request rejected client_secret="fixture with spaces"`), "request rejected"},
		{"untrusted HTML", errors.New("<html>arbitrary response data</html>"), ""},
		{"first line", errors.New("unknown \u202Efailure\u2028debug trace"), "unknown failure"},
		{"length", errors.New(strings.Repeat("错", 121)), strings.Repeat("错", 120) + "…"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Text(tc.err); got != tc.want {
				t.Fatalf("message = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestExplainRetainsOriginalCause(t *testing.T) {
	cause := errors.New("mygo: could not open the browser")
	err := fmt.Errorf("open console: %w", Explain(cause, "请检查默认浏览器设置后重试"))
	if !errors.Is(err, cause) || Format("打开控制台失败", err) != "打开控制台失败：请检查默认浏览器设置后重试" {
		t.Fatalf("cause or user message lost: %v", err)
	}
}
