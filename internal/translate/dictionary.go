package translate

import (
	"bytes"
	"cmp"
	"compress/gzip"
	"context"
	_ "embed"
	"encoding/csv"
	"fmt"
	"io"
	"strings"
	"sync"
	"unicode"

	"sotto/internal/config"
	"sotto/internal/usermessage"
)

//go:embed dictionarydata/ecdict.csv.gz
var bundledDictionary []byte

// Load the index on the first eligible lookup, off the UI thread.
var dictionaryEntries = sync.OnceValues(loadDictionary)

type dictionaryRecord struct {
	phonetic string
	meanings string
}

// DictionaryEntry is the result shared by all dictionary providers.
type DictionaryEntry struct {
	Word       string `json:"word"`
	Meanings   string `json:"meanings"`
	Phonetic   string `json:"phonetic,omitempty"` // accent unspecified
	UKPhonetic string `json:"ukPhonetic,omitempty"`
	USPhonetic string `json:"usPhonetic,omitempty"`
}

// CopyText is the same complete plain text used by the dictionary card.
func (e *DictionaryEntry) CopyText(text string) string {
	return e.Word + "\n" + text
}

func (e DictionaryEntry) text() string {
	// Plain text retains a single pronunciation and prefers US when available.
	ipa := cmp.Or(e.USPhonetic, e.UKPhonetic, e.Phonetic)
	if ipa == "" {
		return e.Meanings
	}
	return "/" + ipa + "/\n" + e.Meanings
}

// AllowsEnglishLookup reports whether text and sourceLang qualify for a
// single-word English dictionary lookup.
func AllowsEnglishLookup(text, sourceLang string) bool {
	return Request{Text: text, SourceLang: sourceLang}.allowsEnglishLookup()
}

func (r Request) allowsEnglishLookup() bool {
	if r.TranslationOnly || (r.SourceLang != "" && r.SourceLang != "auto" && r.SourceLang != "en") {
		return false
	}
	// The bundled and web English dictionaries share the bare-word rule.
	if r.Text == "" || len(r.Text) > 45 {
		return false
	}
	for _, ch := range r.Text {
		if !unicode.IsLetter(ch) || !unicode.Is(unicode.Latin, ch) {
			return false
		}
	}
	return true
}

// Dictionary looks up single English words in the bundled ECDICT data.
type Dictionary struct{}

func (Dictionary) Name() string { return "dictionary" }

// Available is always true because the dictionary is bundled with the app.
func (Dictionary) Available(KeyStore, config.Settings) bool { return true }

func (d Dictionary) Translate(ctx context.Context, req Request, _ KeyStore, _ config.Settings) (Result, error) {
	if !req.allowsEnglishLookup() {
		return Result{}, ErrSkip
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	entries, err := dictionaryEntries()
	if err != nil {
		return Result{}, usermessage.Explain(err, "无法读取离线词典")
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	word := strings.ToLower(req.Text)
	entry, ok := entries[word]
	if !ok {
		return Result{}, ErrSkip
	}
	// An English spelling match cannot identify the source language.
	result := &DictionaryEntry{Word: word, Meanings: entry.meanings, Phonetic: entry.phonetic}
	return Result{Dictionary: result, Service: d.Name(), Text: result.text()}, nil
}

func loadDictionary() (map[string]dictionaryRecord, error) {
	archive, err := gzip.NewReader(bytes.NewReader(bundledDictionary))
	if err != nil {
		return nil, fmt.Errorf("open ECDICT data: %w", err)
	}
	defer archive.Close()
	reader := csv.NewReader(archive)
	reader.Comment = '#'
	reader.FieldsPerRecord = 3
	reader.ReuseRecord = true
	entries := make(map[string]dictionaryRecord)
	for {
		record, err := reader.Read()
		if err == io.EOF {
			return entries, nil
		}
		if err != nil {
			return nil, fmt.Errorf("read ECDICT record: %w", err)
		}
		entries[record[0]] = dictionaryRecord{phonetic: record[1], meanings: record[2]}
	}
}
