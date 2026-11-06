package translate

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"sotto/internal/config"
)

func TestDictionaryWord(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/entries/en/hello" {
			t.Errorf("path = %q", r.URL.Path)
		}
		fmt.Fprint(w, `[{"word":"hello","phonetic":"həˈləʊ","phonetics":[{"text":"həˈləʊ"}],
		  "meanings":[
		    {"partOfSpeech":"exclamation","definitions":[{"definition":"used as a greeting","example":"hello there"}]},
		    {"partOfSpeech":"noun","definitions":[{"definition":"an utterance of hello"}]}
		  ]}]`)
	}))
	t.Cleanup(srv.Close)
	d := Dictionary{Client: srv.Client(), Endpoint: srv.URL + "/api/v2/entries/en"}
	res, err := d.Translate(context.Background(), Request{Text: "hello", SingleWord: true}, fakeKeys{}, config.Settings{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Service != "dictionary" || res.DetectedLang != "en" {
		t.Errorf("res = %+v", res)
	}
	for _, want := range []string{"həˈləʊ", "used as a greeting", "exclamation"} {
		if !strings.Contains(res.Text, want) {
			t.Errorf("text %q missing %q", res.Text, want)
		}
	}
}

func TestDictionarySkipsPhrases(t *testing.T) {
	d := Dictionary{}
	_, err := d.Translate(context.Background(), Request{Text: "hello world", SingleWord: false}, fakeKeys{}, config.Settings{})
	if !errors.Is(err, ErrSkip) {
		t.Errorf("err = %v, want ErrSkip", err)
	}
}

func TestDictionaryNotFoundSkips(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, "Not Found")
	}))
	t.Cleanup(srv.Close)
	d := Dictionary{Client: srv.Client(), Endpoint: srv.URL}
	_, err := d.Translate(context.Background(), Request{Text: "xyzzy", SingleWord: true}, fakeKeys{}, config.Settings{})
	if !errors.Is(err, ErrSkip) {
		t.Errorf("err = %v, want ErrSkip", err)
	}
}

func TestRunWithSkipsSilently(t *testing.T) {
	all := []Translator{
		fakeTranslator{name: "a", res: Result{Service: "a", Text: "甲"}},
		skipTranslator{},
	}
	cfg := config.Default()
	cfg.Services.Enabled = []string{"a"}
	cfg.Services.Order = []string{"a"}
	var got []Result
	RunWith(context.Background(), all, cfg, fakeKeys{"a": "1"}, Request{TargetLang: "en", SingleWord: true}, func(r Result) {
		got = append(got, r)
	})
	if len(got) != 1 || got[0].Service != "a" {
		t.Errorf("results = %+v, want only a (skipped one stays off)", got)
	}
}

// skipTranslator always skips, like the dictionary outside word mode.
type skipTranslator struct{}

func (skipTranslator) Name() string { return "skipper" }

func (skipTranslator) Available(KeyStore, config.Settings) bool { return true }

func (skipTranslator) Translate(context.Context, Request, KeyStore, config.Settings) (Result, error) {
	return Result{}, ErrSkip
}

func TestUserMessageNotForSkip(t *testing.T) {
	// ErrSkip never reaches UserMessage (RunWith intercepts it), but the
	// mapping must not panic on it either.
	if UserMessage(ErrSkip) == "" {
		t.Log("ErrSkip maps to an empty message — fine, RunWith filters it")
	}
}
