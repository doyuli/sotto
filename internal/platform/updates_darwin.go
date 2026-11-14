//go:build darwin

package platform

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/egoist/mygo"

	"sotto/internal/macnative"
)

// The stock updater opens an activating NSWindow even for accessory apps.
// Keep the signed download/install engine and publish status in our tray.
type macUpdateController struct {
	sync.Mutex
	ctx             context.Context
	stop            context.CancelFunc
	automatic       context.CancelFunc
	busy, installed bool
	update          *mygo.Update
	status          string
	failed          bool
	refresh         func()
	check           func(context.Context) (*mygo.Update, error)
	install         func(context.Context, *mygo.Update) error
	changed         func()
}

var macUpdates macUpdateController

func InitUpdates(_ []byte) {
	macUpdates.check = mygo.Updater.Check
	macUpdates.install = func(ctx context.Context, up *mygo.Update) error { return up.Install(ctx, nil) }
	macUpdates.changed = refreshMacUpdateMenu
	macUpdates.ctx, macUpdates.stop = context.WithCancel(context.Background())
	mygo.App.OnQuit(func() { macUpdates.stop() })
}

func SetAutomaticUpdates(on bool) { macUpdates.setAutomatic(on && mygo.Updater.Enabled()) }

// Disabling automatic checks stops scheduling; quit cancels in-flight work.
func (u *macUpdateController) setAutomatic(on bool) {
	u.Lock()
	defer u.Unlock()
	if u.automatic != nil {
		u.automatic()
		u.automatic = nil
	}
	if !on || u.ctx == nil {
		return
	}
	ctx, cancel := context.WithCancel(u.ctx)
	u.automatic = cancel
	go func() {
		timer := time.NewTimer(10 * time.Second)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				if ctx.Err() != nil {
					return
				}
				u.run(false)
				timer.Reset(24 * time.Hour)
			}
		}
	}()
}

func UpdateMenuItem(refresh func()) *mygo.MenuItem {
	macUpdates.Lock()
	defer macUpdates.Unlock()
	macUpdates.refresh = refresh
	label := "检查更新…"
	if macUpdates.status != "" {
		label = macUpdates.status
	}
	if macUpdates.update != nil && !macUpdates.busy && !macUpdates.failed {
		label = "下载并安装 Sotto " + macUpdates.update.Version
	}
	if macUpdates.installed {
		label = "重启以完成更新"
	}
	enabled := mygo.Updater.Enabled()
	if !enabled {
		label = "当前构建未启用更新"
	}
	return &mygo.MenuItem{Label: label, Disabled: macUpdates.busy || !enabled, Click: func(*mygo.MenuItem, *mygo.Window) {
		macUpdates.Lock()
		restart := macUpdates.installed
		macUpdates.Unlock()
		if restart {
			mygo.App.Relaunch()
		} else {
			go runMacUpdate(true)
		}
	}}
}

func refreshMacUpdateMenu() {
	macUpdates.Lock()
	refresh, ctx := macUpdates.refresh, macUpdates.ctx
	macUpdates.Unlock()
	if refresh != nil && ctx != nil && ctx.Err() == nil {
		macnative.PostMain(func() {
			if ctx.Err() == nil {
				refresh()
			}
		})
	}
}

func runMacUpdate(user bool) { macUpdates.run(user) }

func (u *macUpdateController) run(user bool) {
	u.Lock()
	if u.busy || u.installed || u.ctx == nil || u.ctx.Err() != nil {
		u.Unlock()
		return
	}
	up := u.update
	// Background checks only discover updates; installation requires a click.
	if !user && up != nil {
		u.Unlock()
		return
	}
	u.busy, u.status = true, "正在检查更新…"
	if user && up != nil {
		u.status = "正在下载更新…"
	}
	root := u.ctx
	u.Unlock()
	u.notify()
	ctx, cancel := context.WithTimeout(root, 20*time.Minute)
	defer cancel()
	var err error
	install := user && up != nil
	if install {
		err = u.install(ctx, up)
	} else {
		up, err = u.check(ctx)
	}
	u.Lock()
	u.busy = false
	u.failed = err != nil
	switch {
	case err != nil:
		u.status = "更新失败，点击重试…"
	case install:
		u.installed = true
	default:
		u.update = up
		u.status = "已是最新版本，点击重新检查"
	}
	u.Unlock()
	if err != nil {
		log.Printf("update: %v", err)
	}
	u.notify()
}

func (u *macUpdateController) notify() {
	if u.changed != nil {
		u.changed()
	}
}
