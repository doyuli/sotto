package translate

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"sotto/internal/config"
	"sotto/internal/keys"

	"golang.org/x/time/rate"
)

func TestBaiduSharesBudgetAcrossRuns(t *testing.T) {
	// Isolate the process-wide budget from other tests without sleeping.
	previous := baiduLimiter
	baiduLimiter = rate.NewLimiter(1, 1)
	t.Cleanup(func() { baiduLimiter = previous })
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		fmt.Fprint(w, `{"from":"en","to":"zh","trans_result":[{"dst":"你好"}]}`)
	}))
	defer server.Close()
	provider := func() Baidu {
		for _, translator := range DefaultTranslators() {
			if b, ok := translator.(Baidu); ok {
				b.Client, b.Endpoint = server.Client(), server.URL
				return b
			}
		}
		t.Fatal("default translators are missing Baidu")
		return Baidu{}
	}
	first, second := provider(), provider()
	credentials := fakeKeys{keys.BaiduAppID: "app", keys.BaiduKey: "secret"}
	req := Request{Text: "hello", SourceLang: "en", TargetLang: "zh-HANS"}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := first.Translate(ctx, req, credentials, config.Default()); err != nil {
		t.Fatal(err)
	}
	// No fresh burst is available to a second bubble or connection test.
	// Wait must reject a deadline shorter than the next available token.
	ctx, cancelSecond := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancelSecond()
	if _, err := second.Translate(ctx, req, credentials, config.Default()); err == nil {
		t.Fatal("second run bypassed the shared rate limit")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("HTTP calls = %d, want 1", got)
	}
}
