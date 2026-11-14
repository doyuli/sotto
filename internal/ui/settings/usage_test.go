package settings

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"

	"sotto/internal/keys"
)

func TestSettingsUsageReusesQueryAndCancelsOnClose(t *testing.T) {
	// Repeated expansion used to launch overlapping requests and refetch a completed result.
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		calls := 0
		var pending context.Context
		v := NewView(TabServices, testSettingsConfig(), Actions{DeeplUsage: func(ctx context.Context) (DeeplUsage, error) {
			calls++
			if calls == 1 {
				<-release
				return DeeplUsage{Used: 3, Limit: 10}, nil
			}
			pending = ctx
			<-ctx.Done()
			return DeeplUsage{}, ctx.Err()
		}})
		defer v.Dispose()
		v.expanded["deepl"] = true
		v.loadDeeplUsageAsync()
		synctest.Wait()
		v.loadDeeplUsageAsync()
		synctest.Wait()
		if calls != 1 {
			t.Fatal("duplicate in-flight usage query")
		}
		close(release)
		synctest.Wait()
		v.loadDeeplUsageAsync()
		synctest.Wait()
		if calls != 1 || v.deeplUsageText != "本月已用 3 / 10 字符" {
			t.Fatal("completed usage was not reused")
		}
		v.DetachWindow()
		v.expanded["deepl"] = true
		v.loadDeeplUsageAsync()
		synctest.Wait()
		if calls != 2 {
			t.Fatal("reopened window reused an expired result")
		}
		v.DetachWindow()
		synctest.Wait()
		if pending.Err() != context.Canceled {
			t.Fatal("window closure did not cancel usage request")
		}
		v.expanded["deepl"] = true
		v.loadDeeplUsageAsync()
		synctest.Wait()
		v.Dispose()
		synctest.Wait()
		if pending.Err() != context.Canceled {
			t.Fatal("dispose did not cancel usage request")
		}
		v.loadDeeplUsageAsync()
		synctest.Wait()
		if calls != 3 {
			t.Fatal("disposed view started a query")
		}
	})
}

func TestSettingsUsageFailureCanRetry(t *testing.T) {
	// A failed query must not be cached as a successful monthly usage result.
	synctest.Test(t, func(t *testing.T) {
		calls := 0
		v := NewView(TabServices, testSettingsConfig(), Actions{DeeplUsage: func(context.Context) (DeeplUsage, error) {
			calls++
			if calls == 1 {
				return DeeplUsage{}, errors.New("offline")
			}
			return DeeplUsage{Used: 4, Limit: 10}, nil
		}})
		defer v.Dispose()
		v.expanded["deepl"] = true
		v.loadDeeplUsageAsync()
		synctest.Wait()
		if v.deeplUsageText != "额度查询失败" {
			t.Fatal("missing usage error")
		}
		v.loadDeeplUsageAsync()
		synctest.Wait()
		v.loadDeeplUsageAsync()
		synctest.Wait()
		if calls != 2 || v.deeplUsageText != "本月已用 4 / 10 字符" {
			t.Fatal("retry did not replace the failure")
		}
	})
}

func TestSettingsUsageSaveFinishingAfterCloseDoesNotQuery(t *testing.T) {
	// Persisting a key outlives its window, but must not restart a detached quota query.
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		calls := 0
		v := NewView(TabServices, testSettingsConfig(), Actions{
			SetSecret: func(string, string) error { <-release; return nil },
			Credentials: func() (CredentialStatus, error) {
				return CredentialStatus{Stored: []string{keys.DeeplKey}}, nil
			},
			DeeplUsage: func(context.Context) (DeeplUsage, error) { calls++; return DeeplUsage{}, nil },
		})
		defer v.Dispose()
		synctest.Wait()
		v.expanded["deepl"] = true
		v.serviceDrafts["deepl"] = map[string]string{keys.DeeplKey: "new-key"}
		v.saveCredentials("deepl")
		synctest.Wait()
		v.DetachWindow()
		close(release)
		synctest.Wait()
		v.loadDeeplUsageAsync()
		synctest.Wait()
		if calls != 0 {
			t.Fatal("credential save started a query after window closure")
		}
	})
}

func TestSettingsUsageKeyChangeCancelsAndRejectsOldResult(t *testing.T) {
	// Credential writes invalidate the old query before the keyring write completes.
	for _, clear := range []bool{false, true} {
		name := "save"
		if clear {
			name = "clear"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				oldRelease, writeRelease := make(chan struct{}), make(chan struct{})
				calls, stored := 0, true
				var oldContext context.Context
				v := NewView(TabServices, testSettingsConfig(), Actions{
					DeeplUsage: func(ctx context.Context) (DeeplUsage, error) {
						calls++
						if calls == 1 {
							oldContext = ctx
							<-oldRelease
							return DeeplUsage{Used: 9, Limit: 10}, nil
						}
						return DeeplUsage{Used: 2, Limit: 10}, nil
					},
					SetSecret: func(_, value string) error { <-writeRelease; stored = value != ""; return nil },
					Credentials: func() (CredentialStatus, error) {
						if stored {
							return CredentialStatus{Stored: []string{keys.DeeplKey}}, nil
						}
						return CredentialStatus{}, nil
					},
				})
				defer v.Dispose()
				synctest.Wait()
				v.expanded["deepl"] = true
				v.loadDeeplUsageAsync()
				synctest.Wait()
				if clear {
					v.clearSecret("deepl", keys.DeeplKey)
				} else {
					v.serviceDrafts["deepl"] = map[string]string{keys.DeeplKey: "new-key"}
					v.saveCredentials("deepl")
				}
				synctest.Wait()
				if oldContext.Err() != context.Canceled {
					t.Fatal("credential change did not cancel old query")
				}
				v.loadDeeplUsageAsync()
				synctest.Wait()
				if calls != 1 {
					t.Fatal("queried credentials during a pending write")
				}
				close(writeRelease)
				synctest.Wait()
				close(oldRelease)
				synctest.Wait()
				if clear {
					if calls != 1 || v.usageLoaded {
						t.Fatal("cleared credentials retained or reloaded usage")
					}
				} else if calls != 2 || v.deeplUsageText != "本月已用 2 / 10 字符" {
					t.Fatal("old key result overwrote the new query")
				}
			})
		})
	}
}
