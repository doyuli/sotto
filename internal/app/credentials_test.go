package app

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"sotto/internal/config"
	"sotto/internal/keys"
	"sotto/internal/ui/bubble"
	"testing"
)

func TestDeeplUsageRejectsCanceledRequestBeforeReadingCredentials(t *testing.T) {
	// A query canceled while queued must not start keyring or network work.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	a := &application{keys: &unusedKeyring{t: t}}
	if _, err := (ConfigSvc{a}).DeeplUsage(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled usage: %v", err)
	}
}

type unusedKeyring struct {
	t       *testing.T
	deleted string
}

func (k *unusedKeyring) Snapshot([]string) keys.Snapshot {
	k.t.Fatal("settings mutation read the keyring")
	return keys.Snapshot{}
}
func (k *unusedKeyring) Set(name, value string) error {
	if value == "" {
		k.deleted = name
	}
	return nil
}

func TestSettingsPreserveServicesWithoutReadingKeyring(t *testing.T) {
	// Saving an unrelated setting must not erase enabled services on keyring failure.
	cfg := config.Default()
	cfg.Services.Enabled = []string{"openai", "deepl", "azure"}
	keyring := &unusedKeyring{t: t}
	a := &application{cfg: cfg, cfgPath: filepath.Join(t.TempDir(), "settings.json"), keys: keyring, bubbles: bubble.NewManager(nil, bubble.Actions{})}
	changed := cfg
	changed.TargetLanguage = "ja"
	if err := a.updateSettings(changed); err != nil {
		t.Fatal(err)
	}
	saved, err := config.Load(a.cfgPath)
	if err != nil || !slices.Equal(saved.Services.Enabled, cfg.Services.Enabled) {
		t.Fatalf("saved=%v err=%v", saved.Services.Enabled, err)
	}
	if err := (ConfigSvc{a}).SetSecret(keys.OpenAIKey, ""); err != nil {
		t.Fatal(err)
	}
	saved, err = config.Load(a.cfgPath)
	if err != nil || keyring.deleted != keys.OpenAIKey || !slices.Equal(saved.Services.Enabled, []string{"deepl", "azure"}) {
		t.Fatalf("delete changed unrelated services: %v, %v", saved.Services.Enabled, err)
	}
}
