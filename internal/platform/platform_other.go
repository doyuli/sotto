//go:build !darwin

package platform

import (
	"time"

	"github.com/egoist/mygo"
)

func CaptureDelay() time.Duration { return 120 * time.Millisecond }

func Init()                                                  {}
func WorkspaceGeneration() uint64                            { return 0 }
func TrayOptions(o mygo.TrayOptions) mygo.TrayOptions        { return o }
func SelectionPermissionMessage(error) string                { return "" }
func SettingsWindow(o mygo.WindowOptions) mygo.WindowOptions { return o }
func ShowBubbleWindow(w *mygo.Window, trigger string)        { w.Show() }
func UpdateDescription() string {
	return "后台检查新版本，可在更新窗口中确认安装"
}
