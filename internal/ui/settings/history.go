package settings

import (
	"errors"
	"fmt"

	"github.com/egoist/mygo/ui"

	"sotto/internal/config"
	"sotto/internal/history"
	"sotto/internal/ui/shared"
	"sotto/internal/usermessage"
)

type historyConfirmation struct {
	next  config.HistorySettings
	count int64
	clear bool
}

func retentionLabel(days int) string {
	if days == 0 {
		return "不限"
	}
	return fmt.Sprintf("%d 天", days)
}

func (v *View) renderHistorySettings(c *ui.Context, p settingsPalette, cfg config.HistorySettings) {
	if v.actions.SaveHistory == nil {
		return
	}
	v.mu.Lock()
	busy := v.historyBusy
	v.mu.Unlock()

	ui.Text(c, "翻译历史").FontSize(16).FontWeight(600).TextColor(p.fg)
	ui.Column(c).FillWidth().Radius(12).Background(p.card).Border(1, p.border).Children(func() {
		enabled := cfg.Enabled
		v.renderSettingRow(c, p, "记录翻译历史", "原文和译文保存在本机。关闭后仍可查看已有记录。", true, func() {
			ui.Switch(c, &enabled).Label("切换记录翻译历史").Disabled(busy).OnClick(func() {
				next := cfg
				next.Enabled = enabled
				v.saveHistory(next, 0)
			})
		})
		v.renderSettingRow(c, p, "保留期限", "从每条原文最近一次翻译起算，过期记录自动删除。", true, func() {
			selected := cfg.RetentionDays
			parts := ui.SelectBase(c, &selected)
			trigger := parts.Trigger.Label("选择历史保留期限").Disabled(busy).MinWidth(100).
				Cursor(ui.CursorPointer).Padding(6, 10).Radius(8).Background(p.field).Border(1, p.border)
			trigger.OnChange(func() {
				next := cfg
				next.RetentionDays = selected
				v.saveHistory(next, 0)
			})
			trigger.Children(func() {
				ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
					ui.Text(c, retentionLabel(selected)).Grow(1).FontSize(12).TextColor(p.fg)
					ui.Icon(c, shared.ChevronDown).Size(12, 12).TextColor(p.muted)
				})
			})
			parts.Popup(func(panel ui.Element) {
				panel.Width(110).Padding(4).Margin(4, 0, 0, 0).Radius(8).Background(p.panel).Border(1, p.border)
				for _, days := range []int{7, 14, 30, 0} {
					item := parts.Item(days).Height(30).Padding(0, 8).Radius(5)
					if item.Highlighted() || days == selected {
						item.Background(p.hover)
					}
					item.Children(func() { ui.Text(c, retentionLabel(days)).FontSize(12).TextColor(p.fg) })
				}
			})
		})
		v.renderSettingRow(c, p, "清空历史", "删除所有已保存的原文和译文。", false, func() {
			v.renderSecondaryButtonDisabled(c, p, "清空历史", "清空翻译历史", busy || v.actions.ClearHistory == nil, func() {
				v.mu.Lock()
				v.historyPrompt = &historyConfirmation{clear: true}
				v.mu.Unlock()
			})
		})
	})
}

func (v *View) renderHistoryConfirmation(c *ui.Context, p settingsPalette) {
	v.mu.Lock()
	prompt := v.historyPrompt
	v.mu.Unlock()
	if prompt == nil {
		return
	}
	open := true
	cancel := func() {
		v.mu.Lock()
		if v.historyPrompt == prompt {
			v.historyPrompt = nil
		}
		v.mu.Unlock()
	}
	title, message, commit := "缩短保留期限？", fmt.Sprintf("将删除 %d 条原文记录及其译文，且无法恢复。", prompt.count), "确认更改"
	if prompt.clear {
		title, message, commit = "清空翻译历史？", "所有原文和译文都将删除，且无法恢复。", "确认清空"
	}
	shared.ConfirmDialog(c.Key("history-confirmation"), &open, shared.ConfirmDialogOptions{
		Title:        title,
		Message:      message,
		CancelLabel:  "取消",
		ConfirmLabel: commit,
		Colors: shared.ConfirmDialogColors{
			Dark:   p.dark,
			Panel:  p.panel,
			Border: p.border,
			Text:   p.fg,
			Muted:  p.muted,
		},
		OnCancel: cancel,
		OnConfirm: func() {
			cancel()
			if prompt.clear {
				v.runHistoryChange(config.HistorySettings{}, v.actions.ClearHistory, true)
			} else {
				v.saveHistory(prompt.next, prompt.count)
			}
		},
	})
}

func (v *View) saveHistory(next config.HistorySettings, approved int64) {
	v.runHistoryChange(next, func() error { return v.actions.SaveHistory(next, approved) }, false)
}

func (v *View) runHistoryChange(next config.HistorySettings, action func() error, clearHistory bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.disposed || v.historyBusy {
		return
	}
	v.historyBusy = true
	v.pendingWrites++
	generation := v.windowGeneration
	v.enqueueTaskLocked(func() {
		err := action()
		v.mu.Lock()
		v.historyBusy = false
		v.pendingWrites--
		if v.pendingWrites == 0 && !v.disposed && v.actions.GetSettings != nil {
			v.setSettingsLocked(v.actions.GetSettings())
		} else if !clearHistory {
			if v.actions.GetSettings != nil {
				v.settings.History = v.actions.GetSettings().History
			} else if err == nil {
				v.settings.History = next
			}
		}
		if !v.disposed && generation == v.windowGeneration {
			var confirmation *history.Confirmation
			switch {
			case errors.As(err, &confirmation) && v.activeTab == TabGeneral:
				v.historyPrompt = &historyConfirmation{next: next, count: confirmation.Count}
			case errors.As(err, &confirmation):
				// The preferences were left before the deletion could be confirmed.
			case err != nil && clearHistory:
				v.pendingToast = usermessage.Format("清空翻译历史失败", err)
			case err != nil:
				v.pendingToast = usermessage.Format("保存历史设置失败", err)
			case clearHistory:
				v.pendingToast = "已清空翻译历史"
			}
		}
		disposed := v.disposed
		v.mu.Unlock()
		if !disposed {
			v.invalidate()
		}
	})
}
