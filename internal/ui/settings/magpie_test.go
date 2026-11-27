package settings

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"testing"
	"testing/synctest"
	"time"

	"github.com/egoist/mygo/ui"

	"sotto/internal/config"
	"sotto/internal/translate"
)

func TestSettingsMagpieSelectsAndEnablesModel(t *testing.T) {
	// A locked keyring must not hide a keyless service. Several models still require a choice.
	cfg := config.Default()
	cfg.Services.Order = []string{"openai", "magpie"}
	cfg.Services.Enabled = []string{"openai"}
	invalidated := make(chan struct{}, 1)
	saved := make(chan config.Settings, 1)
	v := NewView(TabServices, cfg, Actions{
		GetSettings: func() config.Settings { return cfg },
		SaveSettings: func(s config.Settings) error {
			cfg = s
			saved <- s
			return nil
		},
		Credentials: func(string) (CredentialStatus, error) { return CredentialStatus{}, errors.New("keyring locked") },
		DiscoverMagpie: func(_ context.Context, port int) (translate.MagpieGateway, error) {
			if port == 0 {
				port = 3425
			}
			return translate.MagpieGateway{Port: port, Models: []string{"workbuddy/auto", "group/translation"}}, nil
		},
		SaveService: func(name string, changes ServiceChanges) (ServiceSaveResult, error) {
			if name != "magpie" {
				t.Errorf("saving unexpected service: %q", name)
			}
			if changes.Port != nil {
				cfg.Services.Magpie.Port, _ = strconv.Atoi(*changes.Port)
			}
			if changes.Model != nil {
				cfg.Services.Magpie.Model = *changes.Model
			}
			return ServiceSaveResult{}, nil
		},
		Invalidate: func() {
			select {
			case invalidated <- struct{}{}:
			default:
			}
		},
	})
	defer v.Dispose()
	deadline, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	await := func(ready func() bool) {
		t.Helper()
		for {
			v.mu.Lock()
			ok := ready()
			v.mu.Unlock()
			if ok {
				return
			}
			select {
			case <-invalidated:
			case <-deadline.Done():
				t.Fatal("Magpie settings did not settle")
			}
		}
	}
	await(func() bool { return v.credentialErrors[""] != "" && v.magpie.gateway.Port == 3425 })
	tt := ui.NewTester(v.Render, 660, 600)
	tt.Frame()
	// An unreadable keyring leaves the enabled OpenAI service unconfirmed.
	if tt.HasText("还没有启用的服务，可在下方启用或配置。") {
		t.Fatal("reported no enabled services while credentials were unknown")
	}
	if _, ok := tt.Find("启用Magpie"); ok {
		t.Fatal("Magpie can be enabled without choosing among several models")
	}
	settingsClick(t, tt, "配置Magpie")
	settingsClick(t, tt, "编辑端口")
	tt.Type("4425")
	settingsClick(t, tt, "保存配置")
	await(func() bool { return v.magpie.gateway.Port == 4425 && !v.serviceSaving["magpie"] })
	tt.Frame()
	settingsClick(t, tt, "选择 Magpie 模型")
	settingsClick(t, tt, "group/translation")
	settingsClick(t, tt, "保存配置")
	await(func() bool { return v.settings.Services.Magpie.Model == "group/translation" })
	tt.Frame()
	settingsClick(t, tt, "启用Magpie")
	select {
	case got := <-saved:
		if got.Services.Magpie != (config.MagpieOptions{Port: 4425, Model: "group/translation"}) ||
			!slices.Contains(got.Services.Enabled, "magpie") {
			t.Fatalf("saved Magpie options: %+v; enabled: %v", got.Services.Magpie, got.Services.Enabled)
		}
	case <-deadline.Done():
		t.Fatal("Magpie enablement was not saved")
	}
}

func TestSettingsMagpieRefreshesPortAfterQueuedWrite(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// A queued settings write used to defer the port readback past its only refresh.
		cfg := config.Default()
		ports := make(chan int, 3)
		saving, release := make(chan struct{}), make(chan struct{})
		v := NewView(TabServices, cfg, Actions{
			GetSettings: func() config.Settings { return cfg },
			SaveService: func(_ string, changes ServiceChanges) (ServiceSaveResult, error) {
				close(saving)
				<-release
				cfg.Services.Magpie.Port, _ = strconv.Atoi(*changes.Port)
				return ServiceSaveResult{}, nil
			},
			SaveSettings: func(next config.Settings) error {
				cfg = next
				return nil
			},
			DiscoverMagpie: func(_ context.Context, port int) (translate.MagpieGateway, error) {
				ports <- port
				return translate.MagpieGateway{Port: port, Models: []string{"group/translation"}}, nil
			},
		})
		defer v.Dispose()
		synctest.Wait()
		if len(ports) != 1 || <-ports != 0 {
			t.Fatal("expected an initial probe of the automatic port")
		}
		v.editMagpiePort("4425")
		v.saveService("magpie")
		<-saving
		v.mu.Lock()
		next := cloneSettings(v.settings)
		next.TargetLanguage = "ja"
		v.enqueueSaveLocked(next)
		v.mu.Unlock()
		close(release)
		synctest.Wait()

		if len(ports) != 1 || <-ports != 4425 {
			t.Fatal("expected one probe of the newly committed port")
		}
		v.mu.Lock()
		defer v.mu.Unlock()
		if !v.magpieReadyLocked() || v.settings.TargetLanguage != "ja" {
			t.Fatalf("queued saves were not applied: port=%d, target=%s", v.magpie.gateway.Port, v.settings.TargetLanguage)
		}
	})
}
