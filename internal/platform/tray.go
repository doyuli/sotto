package platform

import (
	"runtime"

	"github.com/egoist/mygo"

	"sotto/resources"
)

func TrayOptions(minimal bool) mygo.TrayOptions {
	opts := mygo.TrayOptions{Icon: resources.TrayFilled, ToolTip: "Sotto · 气泡模式"}
	if runtime.GOOS == "darwin" {
		opts.ID, opts.IconIsTemplate = "main", true
	}
	if minimal {
		opts.Icon, opts.ToolTip = resources.TrayOutline, "Sotto · 简约模式"
	}
	return opts
}
