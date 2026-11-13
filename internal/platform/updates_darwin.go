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
var macUpdates struct {
	sync.Mutex
	ctx             context.Context
	stop            context.CancelFunc
	automatic       context.CancelFunc
	busy, installed bool
	update          *mygo.Update
	status          string
	failed          bool
	refresh         func()
}

func InitUpdates(_ []byte) {
	macUpdates.ctx, macUpdates.stop = context.WithCancel(context.Background())
	mygo.App.OnQuit(func() { macUpdates.stop() })
}

func SetAutomaticUpdates(on bool) {
	macUpdates.Lock()
	defer macUpdates.Unlock()
	if macUpdates.automatic != nil {
		macUpdates.automatic()
		macUpdates.automatic = nil
	}
	if !on || macUpdates.ctx == nil || !mygo.Updater.Enabled() {
		return
	}
	ctx, cancel := context.WithCancel(macUpdates.ctx)
	macUpdates.automatic = cancel
	go func() {
		timer := time.NewTimer(10 * time.Second)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				runMacUpdate(false)
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

func runMacUpdate(user bool) {
	macUpdates.Lock()
	if macUpdates.busy || macUpdates.installed || macUpdates.ctx.Err() != nil {
		macUpdates.Unlock()
		return
	}
	up := macUpdates.update
	// Background checks only discover updates; installation requires a click.
	if !user && up != nil {
		macUpdates.Unlock()
		return
	}
	macUpdates.busy, macUpdates.status = true, "正在检查更新…"
	if user && up != nil {
		macUpdates.status = "正在下载更新…"
	}
	root := macUpdates.ctx
	macUpdates.Unlock()
	refreshMacUpdateMenu()
	ctx, cancel := context.WithTimeout(root, 20*time.Minute)
	defer cancel()
	var err error
	install := user && up != nil
	if install {
		err = up.Install(ctx, nil)
	} else {
		up, err = mygo.Updater.Check(ctx)
	}
	macUpdates.Lock()
	macUpdates.busy = false
	macUpdates.failed = err != nil
	switch {
	case err != nil:
		macUpdates.status = "更新失败，点击重试…"
	case install:
		macUpdates.installed = true
	default:
		macUpdates.update = up
		macUpdates.status = "已是最新版本，点击重新检查"
	}
	macUpdates.Unlock()
	if err != nil {
		log.Printf("update: %v", err)
	}
	refreshMacUpdateMenu()
}
