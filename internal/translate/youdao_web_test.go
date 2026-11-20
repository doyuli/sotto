package translate

import (
	"crypto/md5"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"sotto/internal/config"
)

func TestYoudaoWebWordUsesDictionaryEndpoint(t *testing.T) {
	// A dictionary lookup must never send the word through the web translator.
	for _, tc := range []struct {
		name string
		body string
		want Result
	}{
		{
			name: "entry with both accents",
			body: `{"ec":{"word":{"usphone":"ˈklæsɪkl","ukphone":"ˈklæsɪkəl","trs":[{"pos":"adj.","tran":"古典的"},{"tran":"经典的"}]}}}`,
			want: Result{Service: "youdaoweb", Text: "/ˈklæsɪkl/\nadj. 古典的\n经典的", Dictionary: &DictionaryEntry{
				Word: "classical", Meanings: "adj. 古典的\n经典的",
				UKPhonetic: "ˈklæsɪkəl", USPhonetic: "ˈklæsɪkl",
			}},
		},
		{
			name: "entry with only UK phonetic",
			body: `{"ec":{"word":{"ukphone":" ˈklæsɪkəl ","trs":[{"tran":"古典的"}]}}}`,
			want: Result{Service: "youdaoweb", Text: "/ˈklæsɪkəl/\n古典的", Dictionary: &DictionaryEntry{
				Word: "classical", Meanings: "古典的", UKPhonetic: "ˈklæsɪkəl",
			}},
		},
		{
			name: "entry without phonetic",
			body: `{"ec":{"word":{"trs":[{"tran":"古典的"}]}}}`,
			want: Result{Service: "youdaoweb", Text: "古典的", Dictionary: &DictionaryEntry{Word: "classical", Meanings: "古典的"}},
		},
		{
			name: "entry missing",
			body: `{"meta":{"isHasSimpleDict":"0"}}`,
			want: Result{Service: "youdaoweb", Err: "有道词典未找到该词条"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/jsonapi_s" ||
					r.URL.Query().Get("doctype") != "json" || r.URL.Query().Get("jsonversion") != "4" {
					t.Errorf("unexpected dictionary request: %s %s", r.Method, r.URL.Path)
					http.NotFound(w, r)
					return
				}
				if err := r.ParseForm(); err != nil {
					t.Error(err)
				}
				want := url.Values{"q": {"classical"}, "le": {"en"}, "client": {"web"}, "t": {"6"},
					"keyfrom": {"webdict"}, "sign": {"5537e0e0a47bb19d364384fdfb543a92"}}
				if !reflect.DeepEqual(r.PostForm, want) {
					t.Errorf("dictionary form = %v, want %v", r.PostForm, want)
				}
				fmt.Fprint(w, tc.body)
			}))
			defer srv.Close()
			y := YoudaoWeb{client: srv.Client(), endpoint: srv.URL, homepage: srv.URL}
			cfg := config.Default()
			cfg.Services.Enabled = []string{"youdaoweb"}
			var results []Result
			RunWith(t.Context(), []Translator{y}, cfg, nil, Request{
				Text: "classical", SourceLang: "auto", TargetLang: "zh-HANS",
			}, func(result Result) {
				results = append(results, result)
			})
			if len(results) != 1 || !reflect.DeepEqual(results[0], tc.want) {
				t.Fatalf("results = %+v, want %+v", results, tc.want)
			}
		})
	}
}

func TestYoudaoWebUsesTranslationSession(t *testing.T) {
	// The web translator needs a session cookie even when translating a single word.
	const sentencePayload = "7_oMt60L0Y8PJjRgmb-maw4Ov3_wYA1iW3FmYQXbCbbhWSdj1joHVLj-FV-jvXzCG7TV7OkHdf5il4qxVkw7xPNBHDHDD1oFdd5QSx0nj_K8H7IKzNhpI-p2Y5lLCNFDFNEjoNPkjN4mXEMEYKTzkMBOIExugOIt9G55IOjNLZs="
	const wordPayload = "7_oMt60L0Y8PJjRgmb-maw4Ov3_wYA1iW3FmYQXbCbYQIuNShpD6Kvxq_Hb_GJStKH7VMRwpZvEaxmE-Xp7M-48jQscaIFQAWLoAq3RrUqg="
	const sentenceTranslation = "你好，世界。\n这是一次测试。"
	const wordTranslation = "古典的"
	t.Run("sentence", func(t *testing.T) {
		req := Request{Text: "Hello world.\nThis is a test.", SourceLang: "auto", TargetLang: "zh-HANS"}
		testYoudaoWebTranslationSession(t, req, map[string]string{req.Text: sentencePayload}, sentenceTranslation)
	})
	t.Run("word with translation only", func(t *testing.T) {
		req := Request{Text: "classical", SourceLang: "auto", TargetLang: "zh-HANS", TranslationOnly: true}
		testYoudaoWebTranslationSession(t, req, map[string]string{req.Text: wordPayload}, wordTranslation)
	})
	t.Run("mixed scripts", func(t *testing.T) {
		// A Latin letter alongside a letter from an unrecognized script used to trigger lookup.
		req := Request{Text: "helloक", SourceLang: "auto", TargetLang: "zh-HANS"}
		testYoudaoWebTranslationSession(t, req, map[string]string{req.Text: wordPayload}, wordTranslation)
	})
	t.Run("long input ending with one word", func(t *testing.T) {
		// The final chunk still belongs to the translation even if it is a dictionary word.
		first := strings.Repeat("x", maxChunkRunes-1) + "\n"
		const last = "classical"
		req := Request{Text: first + last, SourceLang: "auto", TargetLang: "zh-HANS"}
		testYoudaoWebTranslationSession(t, req, map[string]string{first: sentencePayload, last: wordPayload}, sentenceTranslation+"\n"+wordTranslation)
	})
}

func testYoudaoWebTranslationSession(t *testing.T, req Request, payloads map[string]string, translated string) {
	t.Helper()
	var homeRequests, keyRequests atomic.Int32
	checkSign := func(values url.Values, keyID, key string) {
		t.Helper()
		time := values.Get("mysticTime")
		want := fmt.Sprintf("%x", md5.Sum([]byte("client=fanyideskweb&mysticTime="+time+"&product=webfanyi&key="+key)))
		if time == "" || values.Get("keyid") != keyID || values.Get("sign") != want {
			t.Errorf("invalid %s request signature", keyID)
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" && r.Method == http.MethodGet {
			homeRequests.Add(1)
			http.SetCookie(w, &http.Cookie{Name: "OUTFOX_SEARCH_USER_ID", Value: "test-session", Path: "/"})
			return
		}
		cookie, err := r.Cookie("OUTFOX_SEARCH_USER_ID")
		if err != nil || cookie.Value != "test-session" {
			t.Error("web request missing session cookie")
			http.Error(w, "missing session", http.StatusBadRequest)
			return
		}
		switch {
		case r.URL.Path == "/webtranslate/key" && r.Method == http.MethodGet:
			keyRequests.Add(1)
			checkSign(r.URL.Query(), "webfanyi-key-getter", "asdjnjfenknafdfsdfsd")
			fmt.Fprint(w, `{"code":0,"data":{"secretKey":"test-key","aesKey":"response-key","aesIv":"response-iv"}}`)
		case r.URL.Path == "/webtranslate" && r.Method == http.MethodPost:
			if err := r.ParseForm(); err != nil {
				t.Error(err)
			}
			checkSign(r.PostForm, "webfanyi", "test-key")
			for name, want := range map[string]string{"from": "auto", "to": "zh-CHS", "dictResult": "false"} {
				if got := r.PostForm.Get(name); got != want {
					t.Errorf("%s = %q, want %q", name, got, want)
				}
			}
			payload, ok := payloads[r.PostForm.Get("i")]
			if !ok {
				t.Error("unexpected input sent for translation")
				http.Error(w, "unexpected source", http.StatusBadRequest)
				return
			}
			fmt.Fprint(w, payload)
		default:
			t.Errorf("unexpected translation request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	y := YoudaoWeb{client: srv.Client(), endpoint: srv.URL, homepage: srv.URL}
	cfg := config.Default()
	cfg.Services.Enabled = []string{"youdaoweb"}
	all := []Translator{y}
	for run := int32(1); run <= 2; run++ {
		var got []Result
		RunWith(t.Context(), all, cfg, nil, req, func(result Result) {
			got = append(got, result)
		})
		if want := (Result{Service: "youdaoweb", Text: translated, DetectedLang: "en"}); len(got) != 1 || got[0] != want {
			t.Fatalf("translation = %+v, want %+v", got, want)
		}
		if home, keys := homeRequests.Load(), keyRequests.Load(); home != run || keys != run {
			t.Fatalf("after run %d, homepage = %d and key = %d, want one of each per run", run, home, keys)
		}
	}
}
