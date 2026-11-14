package settings

import (
	"runtime"

	"github.com/egoist/mygo/ui"
	"sotto/internal/hotkey"
	"sotto/internal/ui/shared"
)

func (v *View) renderHotkeys(c *ui.Context, p settingsPalette) {
	v.mu.Lock()
	cfg := v.settings.Hotkeys
	listening, pending, editor := v.hotkeyListening, v.hotkeyPending, v.hotkeyEditor
	conflicts := make(map[string]string, len(v.hotkeyConflicts))
	for id, message := range v.hotkeyConflicts {
		conflicts[id] = message
	}
	draft := v.hotkeyDraft
	v.mu.Unlock()
	if v.actions.HotkeyErrors != nil {
		for id, message := range v.actions.HotkeyErrors() {
			if conflicts[id] == "" && listening != id && pending != id {
				conflicts[id] = "未生效：" + message
			}
		}
	}

	ui.Text(c, "快捷键").FontSize(18).FontWeight(600).TextColor(p.fg)
	ui.Text(c, "点击组合键开始录制，按 Esc 取消。设置成功后立即生效，失败时保留原快捷键。").FontSize(12).LineHeight(1.6).TextColor(p.muted)
	ui.Column(c).FillWidth().Radius(12).Background(p.card).Border(1, p.border).Children(func() {
		for i, item := range settingsHotkeys {
			current := hotkeyValue(cfg, item.id)
			recording := listening == item.id
			ui.Column(c.Key(item.id)).FillWidth().Padding(14, 16).Gap(8).Children(func() {
				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(10).Children(func() {
					ui.Text(c, item.label).Grow(1).FontSize(13).FontWeight(500).TextColor(p.fg)
					text := displayHotkey(current)
					if recording {
						text = "按下组合键…"
					} else if pending == item.id {
						text = "正在保存…"
					}
					btn := ui.ButtonBase(c.Key("recorder")).Bind(v.hotkeyRecorders[item.id]).
						HandleInput(func(ev ui.InputEvent) bool { return v.captureHotkey(item.id, ev) }).
						Label(item.label+"快捷键").Tooltip("点击后按下新组合键").Disabled(pending != "").
						Cursor(ui.CursorPointer).MinWidth(132).Padding(7, 12).Radius(8).Center()
					if recording {
						btn.Background(p.accentSoft).Border(1, p.accent)
					} else if btn.Hovered() {
						btn.Background(p.hover).Border(1, p.border)
					} else {
						btn.Background(p.field).Border(1, p.border)
					}
					if btn.Clicked() {
						v.mu.Lock()
						v.hotkeyListening = item.id
						v.hotkeyEditor = ""
						v.hotkeyConflicts[item.id] = ""
						v.mu.Unlock()
						v.hotkeyRecorders[item.id].Focus(c)
						c.AnimationFrame()
					}
					btn.Children(func() { ui.Text(c, text).FontSize(12).TextColor(p.fg).SingleLine() })
					v.renderSecondaryButtonDisabled(c, p, "手动设置", item.label+"手动设置", pending != "", func() {
						parsed, err := hotkey.Parse(current, runtime.GOOS)
						if err != nil {
							parsed, _ = hotkey.Parse("Alt+A", runtime.GOOS)
						}
						v.mu.Lock()
						v.hotkeyListening = ""
						v.hotkeyEditor = item.id
						v.hotkeyDraft = parsed
						v.hotkeyConflicts[item.id] = ""
						v.mu.Unlock()
					})
				})
				if message := conflicts[item.id]; message != "" {
					ui.Text(c, message).FillWidth().FontSize(12).LineHeight(1.6).TextColor(p.error)
				}
				if recording {
					ui.Text(c, "按住修饰键，再按主键；没有反应时可改用手动设置。").FontSize(12).TextColor(p.muted)
				}
				if editor == item.id {
					v.renderHotkeyPicker(c, p, item, draft, pending != "")
				}
			})
			if i < len(settingsHotkeys)-1 {
				ui.Box(c).FillWidth().Height(1).Background(p.border)
			}
		}
	})
	ui.Text(c, "组合键没有被识别？使用“手动设置”选择键位。系统或其他应用可能会优先处理某些组合键。").FontSize(12).LineHeight(1.6).TextColor(p.muted)
}

func (v *View) renderHotkeyPicker(c *ui.Context, p settingsPalette, item hotkeyItem, draft hotkey.Shortcut, busy bool) {
	ui.Column(c).FillWidth().Padding(12).Gap(10).Radius(8).Background(p.field).Children(func() {
		ui.Text(c, "选择修饰键和主键，无需同时按下组合键。").FontSize(12).TextColor(p.muted)
		ui.Row(c).Gap(6).Children(func() {
			modifiers := []struct {
				bit   hotkey.Modifiers
				label string
			}{{hotkey.Control, "Ctrl"}, {hotkey.Alt, "Alt"}, {hotkey.Shift, "Shift"}, {hotkey.Win, "Win"}}
			if runtime.GOOS == "darwin" {
				modifiers = []struct {
					bit   hotkey.Modifiers
					label string
				}{{hotkey.Control, "Control"}, {hotkey.Option, "Option"}, {hotkey.Shift, "Shift"}, {hotkey.Command, "Command"}}
			}
			for _, mod := range modifiers {
				chosen := draft.Mods&mod.bit != 0
				btn := ui.ButtonBase(c.Key(mod.label)).Label(item.label+"修饰键"+mod.label).Disabled(busy).Padding(6, 10).Radius(6).Border(1, p.border).Cursor(ui.CursorPointer)
				if chosen {
					btn.Background(p.accentSoft).Border(1, p.accent)
				} else if btn.Hovered() {
					btn.Background(p.hover)
				} else {
					btn.Background(p.card)
				}
				btn.OnClick(func() { v.mu.Lock(); v.hotkeyDraft.Mods ^= mod.bit; v.mu.Unlock() })
				btn.Children(func() { ui.Text(c, mod.label).FontSize(12).TextColor(p.fg) })
			}
		})
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
			selected := draft.Key
			parts := ui.SelectBase(c, &selected)
			trigger := parts.Trigger.Label(item.label+"主键").Disabled(busy).MinWidth(100).Padding(6, 10).Radius(6).Background(p.card).Border(1, p.border)
			trigger.OnChange(func() { v.mu.Lock(); v.hotkeyDraft.Key = selected; v.mu.Unlock() })
			trigger.Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, selected).Grow(1).FontSize(12).TextColor(p.fg)
					ui.Icon(c, shared.ChevronDown).Size(12, 12).TextColor(p.muted)
				})
			})
			parts.Popup(func(panel ui.Element) {
				panel.Width(176).Padding(4).Margin(4, 0, 0, 0).Radius(8).Background(p.panel).Border(1, p.border)
				ui.Scroll(c).Height(180).Children(func() {
					for _, key := range hotkey.Keys(runtime.GOOS) {
						row := parts.Item(key).Height(28).Padding(0, 8).Radius(4)
						if row.Highlighted() {
							row.Background(p.hover)
						}
						row.Children(func() { ui.Text(c, key).FontSize(12).TextColor(p.fg) })
					}
				})
			})
			ui.Box(c).Grow(1)
			v.renderSecondaryButtonDisabled(c, p, "取消", item.label+"取消手动设置", busy, func() { v.mu.Lock(); v.hotkeyEditor = ""; v.hotkeyConflicts[item.id] = ""; v.mu.Unlock() })
			v.renderSecondaryButtonDisabled(c, p, "保存", item.label+"保存快捷键", busy, func() { v.mu.Lock(); v.submitHotkeyLocked(item.id, v.hotkeyDraft.String()); v.mu.Unlock() })
		})
	})
}
