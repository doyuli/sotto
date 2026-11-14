package translate

import (
	"context"
	"errors"
	"sotto/internal/config"
	"sotto/internal/keys"
	"strings"
	"sync"
	"testing"
)

type unavailableKeys struct{}

func (unavailableKeys) Get(string) string   { return "present" }
func (unavailableKeys) Err(...string) error { return keys.ErrUnavailable }

func TestKeyringFailureDoesNotBlockKeylessTranslation(t *testing.T) {
	// Failed credential reads must settle the affected card without suppressing keyless work.
	cfg := config.Default()
	cfg.Services.Enabled = []string{"openai", "google"}
	all := []Translator{fakeTranslator{name: "openai", res: Result{Service: "openai", Text: "should not run"}}, fakeTranslator{name: "google", res: Result{Service: "google", Text: "translated"}}}
	var mu sync.Mutex
	got := map[string]Result{}
	RunWith(context.Background(), all, cfg, unavailableKeys{}, Request{Text: "hello"}, func(r Result) { mu.Lock(); defer mu.Unlock(); got[r.Service] = r })
	if got["google"].Text != "translated" || got["openai"].Text != "" || !strings.Contains(got["openai"].Err, "凭据") {
		t.Fatal(got)
	}
	if err := Test(context.Background(), "openai", cfg, unavailableKeys{}); !errors.Is(err, keys.ErrUnavailable) {
		t.Fatal(err)
	}
}

func TestDeletingOptionalCredentialKeepsService(t *testing.T) {
	cfg := config.Default()
	cfg.Services.Enabled = []string{"azure", "openai"}
	if got := WithoutCredential(cfg, keys.AzureRegion); len(got) != 2 {
		t.Fatal(got)
	}
}
