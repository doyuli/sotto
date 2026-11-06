package translate

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"sotto/internal/config"
	"sotto/internal/keys"
)

// TestTC3Authorization pins the signer to Tencent Cloud's documented
// signature-v3 example (cvm DescribeInstances, 2019-02-25). The doc
// masks the example SecretKey, so the key-derivation chain is checked
// against the doc's signing-key hex: HMAC over the documented string to
// sign with the documented key reproduces the documented signature.
func TestTC3Authorization(t *testing.T) {
	// The example's exact request payload, spaces included.
	payload := `{"Limit": 1, "Filters": [{"Values": ["\u672a\u547d\u540d"], "Name": "instance-name"}]}`
	if got := sha256hex([]byte(payload)); got != "35e9c5b0e3ae67532d3c9f17ead6c90222632e5b1ff7f6e89887f1398934f064" {
		t.Fatalf("hashed payload = %s", got)
	}
	canonical := tc3CanonicalRequest("cvm.tencentcloudapi.com", "DescribeInstances", []byte(payload))
	if got := sha256hex([]byte(canonical)); got != "7019a55be8395899b900fb5564e4200d984910f34794a27cb3fb7d10ff6a1e84" {
		t.Errorf("hashed canonical request = %s", got)
	}
	sts := tc3StringToSign("1551113065", "2019-02-25/cvm/tc3_request",
		"7019a55be8395899b900fb5564e4200d984910f34794a27cb3fb7d10ff6a1e84")
	wantSTS := "TC3-HMAC-SHA256\n1551113065\n2019-02-25/cvm/tc3_request\n" +
		"7019a55be8395899b900fb5564e4200d984910f34794a27cb3fb7d10ff6a1e84"
	if sts != wantSTS {
		t.Errorf("string to sign = %q, want %q", sts, wantSTS)
	}
	kSigning, err := hex.DecodeString("b596b923aad85185e2d1f6659d2a062e0a86731226e021e61bfe06f7ed05f5af")
	if err != nil {
		t.Fatal(err)
	}
	if got := hex.EncodeToString(hmacSHA256(kSigning, []byte(sts))); got != "10b1a37a7301a02ca19a647ad722d5e43b4b3cff309d421d85b46093f6ab6c4f" {
		t.Errorf("signature = %s, want the documented one", got)
	}
	// The header assembles as the doc specifies.
	now := time.Unix(1551113065, 0)
	auth := tc3Authorization("AKIDz8krbsJ5yKBZQpn74WFkmLPx5EXAMPLE", "k", "cvm",
		"cvm.tencentcloudapi.com", "DescribeInstances", []byte(payload), now)
	wantPrefix := "TC3-HMAC-SHA256 " +
		"Credential=AKIDz8krbsJ5yKBZQpn74WFkmLPx5EXAMPLE/2019-02-25/cvm/tc3_request" +
		", SignedHeaders=content-type;host;x-tc-action, Signature="
	if !strings.HasPrefix(auth, wantPrefix) {
		t.Errorf("authorization = %s, want prefix %s", auth, wantPrefix)
	}
}

func TestTencentTranslate(t *testing.T) {
	now := time.Unix(1700000000, 0)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-TC-Action"); got != "TextTranslate" {
			t.Errorf("action = %q", got)
		}
		if got := r.Header.Get("X-TC-Version"); got != tencentVersion {
			t.Errorf("version = %q", got)
		}
		if got := r.Header.Get("X-TC-Timestamp"); got != "1700000000" {
			t.Errorf("timestamp = %q", got)
		}
		// The signature must be the one this very request produces.
		body, _ := io.ReadAll(r.Body)
		want := tc3Authorization("id-1", "secret-1", tencentService,
			r.Host, "TextTranslate", body, now)
		if got := r.Header.Get("Authorization"); got != want {
			t.Errorf("authorization = %q, want %q", got, want)
		}
		var in struct {
			Source string `json:"Source"`
			Target string `json:"Target"`
			Text   string `json:"Text"`
		}
		if err := json.Unmarshal(body, &in); err != nil {
			t.Errorf("bad payload: %v", err)
		}
		if in.Source != "auto" || in.Target != "zh" || in.Text != "hello" {
			t.Errorf("payload = %+v", in)
		}
		fmt.Fprint(w, `{"Response":{"Translation":"你好","Source":"en","Target":"zh","RequestId":"r"}}`)
	}))
	t.Cleanup(srv.Close)
	tn := Tencent{Client: srv.Client(), Endpoint: srv.URL, Now: func() time.Time { return now }}
	res, err := tn.Translate(context.Background(), Request{Text: "hello", TargetLang: "zh-HANS"},
		fakeKeys{keys.TencentSecretID: "id-1", keys.TencentSecretKey: "secret-1"}, config.Settings{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "你好" || res.Service != "tencent" || res.DetectedLang != "en" {
		t.Errorf("res = %+v", res)
	}
}

func TestTencentErrors(t *testing.T) {
	tests := []struct {
		code    string
		wantErr error
	}{
		{"AuthFailure.SignatureFailure", ErrAuth},
		{"UnauthorizedOperation", ErrAuth},
	}
	for _, tt := range tests {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(w, `{"Response":{"Error":{"Code":%q,"Message":"x"}}}`, tt.code)
		}))
		tn := Tencent{Client: srv.Client(), Endpoint: srv.URL}
		_, err := tn.Translate(context.Background(), Request{Text: "x", TargetLang: "en"},
			fakeKeys{keys.TencentSecretID: "i", keys.TencentSecretKey: "s"}, config.Settings{})
		if !errors.Is(err, tt.wantErr) {
			t.Errorf("code %s: err = %v, want %v", tt.code, err, tt.wantErr)
		}
		srv.Close()
	}
}

// TestTencentHostIsSigned checks the host extraction the signature
// covers: the endpoint's authority, port included.
func TestTencentHostIsSigned(t *testing.T) {
	if got := tencentHost("https://tmt.tencentcloudapi.com"); got != "tmt.tencentcloudapi.com" {
		t.Errorf("host = %q", got)
	}
	if got := tencentHost("http://127.0.0.1:12345"); got != "127.0.0.1:12345" {
		t.Errorf("host = %q", got)
	}
}
