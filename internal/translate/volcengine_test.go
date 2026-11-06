package translate

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"sotto/internal/config"
	"sotto/internal/keys"
)

func TestVolcengineRequiresBothCredentials(t *testing.T) {
	cfg := config.Default()
	cfg.Services.Enabled = append(cfg.Services.Enabled, "volcengine", "baidu")
	creds := fakeKeys{keys.VolcAccessKey: "access"}
	if got := Available(cfg, creds); !reflect.DeepEqual(got, []string{"google"}) {
		t.Errorf("partial credentials: available = %v", got)
	}
	creds[keys.VolcSecretKey] = "secret"
	if got := Available(cfg, creds); !reflect.DeepEqual(got, []string{"google", "volcengine"}) {
		t.Errorf("complete credentials: available = %v", got)
	}
	cfg.Services.Enabled = []string{}
	if got := Available(cfg, creds); !reflect.DeepEqual(got, []string{}) {
		t.Errorf("all disabled: available = %#v", got)
	}
}

func TestVolcengineSign(t *testing.T) {
	payload := []byte(`{"TargetLanguage":"zh","TextList":["Hello world"]}`)
	req := httptest.NewRequest(http.MethodPost, volcEndpoint, nil)
	now := time.Date(2020, 4, 9, 12, 34, 56, 0, time.UTC)
	volcSign(req, payload, "example-access", "example-secret", now)

	const want = "HMAC-SHA256 Credential=example-access/20200409/cn-north-1/translate/request, " +
		"SignedHeaders=content-type;host;x-content-sha256;x-date, " +
		"Signature=6437745e9f7fcbbe39497051a752c9f615027e2f08584c5fde23223624707afc"
	if got := req.Header.Get("Authorization"); got != want {
		t.Errorf("Authorization = %q, want %q", got, want)
	}
}

func TestVolcengineTranslate(t *testing.T) {
	for _, tc := range []struct {
		name, source, request, response string
		status                          int
		err                             error
	}{
		{
			name: "auto", source: "auto", status: http.StatusOK,
			request:  `{"TargetLanguage":"zh","TextList":["Hello world"]}`,
			response: `{"TranslationList":[{"Translation":"你好世界","DetectedSourceLanguage":"en"}]}`,
		},
		{
			name: "explicit source and API error", source: "en", status: http.StatusOK,
			request:  `{"SourceLanguage":"en","TargetLanguage":"zh","TextList":["Hello world"]}`,
			response: `{"ResponseMetadata":{"Error":{"Code":"AccessDenied","CodeN":100013}}}`,
			err:      ErrAccess,
		},
		{
			name: "invalid key", source: "auto", status: http.StatusUnauthorized,
			request:  `{"TargetLanguage":"zh","TextList":["Hello world"]}`,
			response: `{"ResponseMetadata":{"Error":{"Code":"InvalidAccessKey","CodeN":100009}}}`,
			err:      ErrAuth,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil || string(body) != tc.request {
					t.Errorf("body = %s, err = %v", body, err)
				}
				if r.Method != http.MethodPost || r.URL.Query().Get("Action") != "TranslateText" ||
					r.URL.Query().Get("Version") != "2020-06-01" ||
					!strings.HasPrefix(r.Header.Get("Authorization"), "HMAC-SHA256 Credential=access/") {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL)
				}
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.response)
			}))
			defer server.Close()

			v := Volcengine{Client: server.Client(), Endpoint: server.URL + "/?Action=TranslateText&Version=2020-06-01"}
			result, err := v.Translate(context.Background(),
				Request{Text: "Hello world", SourceLang: tc.source, TargetLang: "zh-HANS"},
				fakeKeys{keys.VolcAccessKey: "access", keys.VolcSecretKey: "secret"}, config.Default())
			if !errors.Is(err, tc.err) {
				t.Fatalf("Translate error = %v, want %v", err, tc.err)
			}
			if tc.err == nil && result != (Result{Service: "volcengine", Text: "你好世界", DetectedLang: "en"}) {
				t.Errorf("unexpected result: %+v", result)
			}
		})
	}
}
