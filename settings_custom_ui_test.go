package main

import (
	"errors"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/egoist/mygo/ui"

	"sotto/internal/config"
	"sotto/internal/keys"
)

func TestSettingsCustomDrawerClearAndDeleteConfirmation(t *testing.T) {
	current := testSettingsConfig()
	service := config.CustomService{ID: "custom:1", Name: "Local", BaseURL: "http://localhost/v1", Model: "m"}
	current.Services.Custom = []config.CustomService{service}
	current.Services.Order = []string{service.ID}
	current.Services.Enabled = nil
	var mu sync.Mutex
	secret := "secret"
	v := newSettingsView(tabServices, current, settingsActions{
		getSettings: func() config.Settings { return current },
		setSecret: func(k, value string) error {
			if k != keys.CustomAPIKey(service.ID) {
				return errors.New("unexpected credential")
			}
			mu.Lock()
			secret = value
			mu.Unlock()
			return nil
		},
		credentials: func() (CredentialStatus, error) {
			mu.Lock()
			defer mu.Unlock()
			status := CredentialStatus{Configured: []string{service.ID}}
			if secret != "" {
				status.Stored = []string{keys.CustomAPIKey(service.ID)}
			}
			return status, nil
		},
	})
	defer v.dispose()
	waitSettingsIdle(t, v)
	v.mu.Lock()
	v.expanded[service.ID] = true
	v.mu.Unlock()
	tt := ui.NewTester(v.render, 780, 900)
	settingsClick(t, tt, "编辑API Key")
	tt.Type("replacement-key")
	settingsClick(t, tt, "清除API Key")
	waitSettingsIdle(t, v)
	tt.Frame()
	v.mu.Lock()
	keyDraft := v.customDrafts[service.ID].apiKey
	stored := slices.Contains(v.credentials.Stored, keys.CustomAPIKey(service.ID))
	v.mu.Unlock()
	if keyDraft != "" || stored || !tt.HasText("凭证已清除") {
		t.Fatal("clearing the key did not clear the input and stored credential status")
	}
	settingsClick(t, tt, "删除服务")
	if !tt.HasText("确认删除") {
		t.Fatal("delete did not request confirmation")
	}
	settingsClick(t, tt, "取消删除")
	if tt.HasText("确认删除") {
		t.Fatal("cancel did not clear deletion confirmation")
	}
	settingsClick(t, tt, "删除服务")
	v.mu.Lock()
	draft := v.customDrafts[service.ID]
	draft.deleteUntil = time.Now().Add(-time.Second)
	v.customDrafts[service.ID] = draft
	v.mu.Unlock()
	tt.Frame()
	if tt.HasText("确认删除") || !tt.HasText("Local") {
		t.Fatal("expired confirmation did not return to the service drawer")
	}
}

func TestSettingsCustomMutationsFinishBeforeQuit(t *testing.T) {
	for _, operation := range []string{"save", "delete"} {
		for _, phase := range []string{"queued", "writing"} {
			t.Run(operation+"/"+phase, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					current := testSettingsConfig()
					service := config.CustomService{ID: "custom:1", Name: "Local", BaseURL: "http://localhost/v1", Model: "m"}
					if operation == "delete" {
						current.Services.Custom = []config.CustomService{service}
						current.Services.Enabled = append(current.Services.Enabled, service.ID)
						current.Services.Order = append(current.Services.Order, service.ID)
					}
					release := make(chan struct{})
					writtenSecrets := map[string]string{keys.CustomAPIKey(service.ID): "old-key"}
					retries := 0
					v := newSettingsView(tabServices, current, settingsActions{
						getSettings: func() config.Settings { return current },
						saveCustomSettings: func(s config.Settings, id string, credential *string) error {
							if phase == "writing" {
								<-release
							}
							value := ""
							if credential != nil {
								value = *credential
							}
							writtenSecrets[keys.CustomAPIKey(id)] = value
							current = s
							return nil
						},
					})
					defer v.dispose()
					if phase == "queued" {
						v.enqueueTask(func() { <-release })
					}
					if operation == "save" {
						v.mu.Lock()
						v.customDrafts["new"] = customDraft{service: config.CustomService{Name: service.Name, BaseURL: service.BaseURL, Model: service.Model}, apiKey: "new-key"}
						v.mu.Unlock()
						v.saveCustomService("new")
					} else {
						v.deleteCustomService(service.ID)
					}
					synctest.Wait()
					retry := func() { retries++ }
					if v.prepareQuit(retry) || v.prepareQuit(retry) {
						t.Fatal("quit proceeded before the mutation finished")
					}
					close(release)
					synctest.Wait()
					if retries != 1 || !v.prepareQuit(retry) {
						t.Fatal("quit did not drain the mutation and retry exactly once")
					}
					if operation == "save" {
						if len(current.Services.Custom) != 1 {
							t.Fatal("quit discarded the service settings")
						}
						got := current.Services.Custom[0]
						if got.Name != service.Name || got.BaseURL != service.BaseURL || got.Model != service.Model ||
							writtenSecrets[keys.CustomAPIKey(got.ID)] != "new-key" || !slices.Contains(current.Services.Order, got.ID) {
							t.Fatalf("incomplete save: service=%+v secrets=%v", got, writtenSecrets)
						}
					} else if len(current.Services.Custom) != 0 || slices.Contains(current.Services.Enabled, service.ID) ||
						slices.Contains(current.Services.Order, service.ID) || writtenSecrets[keys.CustomAPIKey(service.ID)] != "" {
						t.Fatalf("incomplete deletion: settings=%+v secrets=%v", current.Services, writtenSecrets)
					}
				})
			})
		}
	}
}

func TestSettingsCustomFailedSaveKeepsDraftForRetry(t *testing.T) {
	for _, isNew := range []bool{true, false} {
		t.Run(map[bool]string{true: "new", false: "existing"}[isNew], func(t *testing.T) {
			current := testSettingsConfig()
			id := "new"
			if !isNew {
				id = "custom:1"
				current.Services.Custom = []config.CustomService{{ID: id, Name: "Old", BaseURL: "http://old/v1", Model: "old"}}
				current.Services.Order = append(current.Services.Order, id)
			}
			fail := true
			writtenKeys := []string{}
			v := newSettingsView(tabServices, current, settingsActions{
				getSettings: func() config.Settings { return current },
				saveCustomSettings: func(s config.Settings, id string, credential *string) error {
					writtenKeys = append(writtenKeys, keys.CustomAPIKey(id))
					if fail {
						return errors.New("disk full")
					}
					current = s
					return nil
				},
				credentials: func() (CredentialStatus, error) { return CredentialStatus{}, nil },
			})
			defer v.dispose()
			waitSettingsIdle(t, v)
			v.mu.Lock()
			draft := customDraft{service: config.CustomService{ID: id, Name: "Local", BaseURL: "http://localhost/v1", Model: "m"}, apiKey: "secret"}
			v.customDrafts[id] = draft
			v.expanded[id] = true
			v.mu.Unlock()
			v.saveCustomService(id)
			waitSettingsIdle(t, v)
			v.mu.Lock()
			got, ok := v.customDrafts[id]
			status := v.testStatuses[id]
			busy := v.credentialBusy[id]
			v.mu.Unlock()
			if !ok || got.service.Name != draft.service.Name || got.service.BaseURL != draft.service.BaseURL || got.service.Model != draft.service.Model || got.apiKey != draft.apiKey ||
				busy || !status.isError || status.result != "配置保存失败：disk full" {
				t.Fatalf("failed save discarded its retry state: draft=%+v status=%+v busy=%v", got, status, busy)
			}
			if _, exists := current.Services.FindCustom(got.service.ID); exists == isNew {
				t.Fatal("failed save changed persisted services")
			}
			tt := ui.NewTester(v.render, 780, 900)
			if !tt.HasText("配置保存失败：disk full") {
				t.Fatal("failed save did not leave its drawer visible")
			}
			// Editing after failure must retain the allocated ID for the retry.
			settingsClick(t, tt, "编辑服务名称")
			tt.Type(" Updated")
			tt.Frame()
			time.Sleep(time.Millisecond)
			fail = false
			settingsClick(t, tt, "保存")
			waitSettingsIdle(t, v)
			tt.Frame()
			tt.Frame()
			if len(current.Services.Custom) != 1 || current.Services.Custom[0].Name != "Local Updated" ||
				len(writtenKeys) != 2 || writtenKeys[0] != writtenKeys[1] {
				t.Fatalf("retry did not save the retained draft with the same credential: services=%+v keys=%v", current.Services.Custom, writtenKeys)
			}
			if !tt.HasText("配置已保存") || tt.HasText("配置保存失败：disk full") {
				t.Fatal("retry did not update the success status")
			}
		})
	}
}

func TestSettingsCustomDeleteFailureCanBeRetried(t *testing.T) {
	for _, failure := range []string{"secret", "settings"} {
		t.Run(failure, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				current := testSettingsConfig()
				id := "custom:1"
				current.Services.Custom = []config.CustomService{{ID: id, Name: "Local", BaseURL: "http://localhost/v1", Model: "m"}}
				current.Services.Enabled = append(current.Services.Enabled, id)
				current.Services.Order = append(current.Services.Order, id)
				before := cloneSettings(current)
				fail := true
				secret := "secret"
				saves := 0
				v := newSettingsView(tabServices, current, settingsActions{
					getSettings: func() config.Settings { return current },
					saveCustomSettings: func(s config.Settings, id string, credential *string) error {
						if fail && failure == "secret" {
							return errors.New("keyring unavailable")
						}
						saves++
						if fail {
							return errors.New("disk full")
						}
						secret = ""
						current = s
						return nil
					},
				})
				defer v.dispose()
				v.mu.Lock()
				v.expanded[id] = true
				v.customDrafts[id] = customDraft{service: config.CustomService{ID: id, Name: "Local", BaseURL: "http://localhost/v1", Model: "m"}}
				v.mu.Unlock()
				v.deleteCustomService(id)
				synctest.Wait()
				v.mu.Lock()
				status := v.testStatuses[id]
				expanded := v.expanded[id]
				busy := v.credentialBusy[id]
				_, hasDraft := v.customDrafts[id]
				v.mu.Unlock()
				if !slices.Equal(current.Services.Custom, before.Services.Custom) || !slices.Equal(current.Services.Enabled, before.Services.Enabled) ||
					!slices.Equal(current.Services.Order, before.Services.Order) || !status.isError || !expanded || busy || !hasDraft {
					t.Fatalf("failed deletion lost its retry state: services=%+v status=%+v", current.Services, status)
				}
				if secret != "secret" || (failure == "secret" && saves != 0) {
					t.Fatal("credential deletion failure still changed persistent state")
				}
				fail = false
				v.deleteCustomService(id)
				synctest.Wait()
				if len(current.Services.Custom) != 0 || slices.Contains(current.Services.Enabled, id) || slices.Contains(current.Services.Order, id) || secret != "" {
					t.Fatal("deletion retry did not remove the service and credential")
				}
			})
		})
	}
}
