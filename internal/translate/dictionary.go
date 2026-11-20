package translate

import (
	"bytes"
	"compress/gzip"
	"context"
	_ "embed"
	"encoding/csv"
	"fmt"
	"io"
	"strings"
	"sync"

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

// DictionaryEntry describes the parts of an English dictionary result.
type DictionaryEntry struct {
	Word     string `json:"word"`
	Phonetic string `json:"phonetic"`
	Meanings string `json:"meanings"`
}

// Dictionary looks up single English words in the bundled ECDICT data.
type Dictionary struct{}

func (Dictionary) Name() string { return "dictionary" }

// Available is always true because the dictionary is bundled with the app.
func (Dictionary) Available(KeyStore, config.Settings) bool { return true }

func (d Dictionary) Translate(ctx context.Context, req Request, _ KeyStore, _ config.Settings) (Result, error) {
	if !req.SingleWord || (req.SourceLang != "" && req.SourceLang != "auto" && req.SourceLang != "en") {
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
	return Result{
		Dictionary: &DictionaryEntry{Word: word, Phonetic: entry.phonetic, Meanings: entry.meanings},
		Service:    d.Name(),
		Text:       "/" + entry.phonetic + "/\n" + entry.meanings,
	}, nil
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
