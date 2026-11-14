package keys

import (
	"errors"
	"github.com/egoist/mygo"
	"testing"
)

func TestSnapshotDistinguishesMissingAndUnreadable(t *testing.T) {
	// A locked keyring used to silently look like a deleted credential.
	failure := errors.New("locked")
	calls := 0
	read := func(name string) ([]byte, error) {
		calls++
		if name == OpenAIKey {
			return nil, failure
		}
		return nil, mygo.ErrSecretNotFound
	}
	got := snapshot([]string{OpenAIKey, DeeplKey}, read)
	if calls != 2 || !errors.Is(got.Err(OpenAIKey), ErrUnavailable) || !errors.Is(got.Err(OpenAIKey), failure) || got.Err(DeeplKey) != nil {
		t.Fatalf("calls=%d errors=%v", calls, got.Err())
	}
	recovered := snapshot([]string{OpenAIKey}, func(string) ([]byte, error) { return []byte("restored"), nil })
	if recovered.Get(OpenAIKey) != "restored" || recovered.Err() != nil {
		t.Fatal("read failure did not recover")
	}
}

func TestKeylessSnapshotDoesNotReadSystemCredentials(t *testing.T) {
	got := snapshot(ForServices("google", "index-translate"), func(string) ([]byte, error) { t.Fatal("read unrelated credentials"); return nil, nil })
	if got.Err() != nil {
		t.Fatal(got.Err())
	}
}
