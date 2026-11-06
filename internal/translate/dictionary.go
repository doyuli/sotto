package translate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"sotto/internal/config"
)

// dictionaryEndpoint is the free, keyless English dictionary.
const dictionaryEndpoint = "https://api.dictionaryapi.dev/api/v2/entries/en"

// Dictionary serves the single-word mode: an English word's phonetics
// and meanings from dictionaryapi.dev, beside the translation cards. It
// only answers when the request is marked as a single word — everything
// else reports ErrSkip, which the fan-out leaves off the bubble.
type Dictionary struct {
	Client   *http.Client // nil: the shared client
	Endpoint string       // overrides the endpoint (tests)
}

func (Dictionary) Name() string { return "dictionary" }

// Available is always true: no key, no configuration.
func (Dictionary) Available(KeyStore, config.Settings) bool { return true }

func (d Dictionary) Translate(ctx context.Context, req Request, _ KeyStore, _ config.Settings) (Result, error) {
	if !req.SingleWord {
		return Result{}, ErrSkip
	}
	endpoint := d.Endpoint
	if endpoint == "" {
		endpoint = dictionaryEndpoint
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet,
		endpoint+"/"+req.Text, nil)
	if err != nil {
		return Result{}, err
	}
	resp, err := d.client().Do(httpReq)
	if err != nil {
		return Result{}, fmt.Errorf("词典: 网络不可达")
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Result{}, err
	}
	if resp.StatusCode == http.StatusNotFound {
		// The word is not in the dictionary — a miss, not a failure;
		// quietly keep it off the bubble.
		return Result{}, ErrSkip
	}
	if resp.StatusCode != http.StatusOK {
		return Result{}, fmt.Errorf("词典: HTTP %d", resp.StatusCode)
	}
	var entries []struct {
		Word      string `json:"word"`
		Phonetic  string `json:"phonetic"`
		Phonetics []struct {
			Text  string `json:"text"`
			Audio string `json:"audio"`
		} `json:"phonetics"`
		Meanings []struct {
			PartOfSpeech string `json:"partOfSpeech"`
			Definitions  []struct {
				Definition string `json:"definition"`
				Example    string `json:"example"`
			} `json:"definitions"`
		} `json:"meanings"`
	}
	if err := json.Unmarshal(body, &entries); err != nil {
		return Result{}, fmt.Errorf("词典: %w", err)
	}
	if len(entries) == 0 {
		return Result{}, ErrSkip
	}
	var b strings.Builder
	phonetic := entries[0].Phonetic
	for _, p := range entries[0].Phonetics {
		if phonetic == "" && p.Text != "" {
			phonetic = p.Text
		}
	}
	if phonetic != "" {
		b.WriteString("/" + strings.Trim(phonetic, "/") + "/\n")
	}
	shown := 0
	for _, m := range entries[0].Meanings {
		if shown >= 4 || len(m.Definitions) == 0 {
			break
		}
		b.WriteString(m.PartOfSpeech + ". " + m.Definitions[0].Definition + "\n")
		shown++
	}
	if b.Len() == 0 {
		return Result{}, ErrSkip
	}
	return Result{
		Service:      "dictionary",
		Text:         strings.TrimRight(b.String(), "\n"),
		DetectedLang: "en",
	}, nil
}

func (d Dictionary) client() *http.Client {
	if d.Client != nil {
		return d.Client
	}
	return &http.Client{Timeout: 5 * time.Second}
}

// IsSkip reports whether a translator result should stay off the bubble.
func IsSkip(err error) bool { return errors.Is(err, ErrSkip) }
