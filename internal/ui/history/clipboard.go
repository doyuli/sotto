package historyview

import (
	"errors"
	"time"

	"sotto/internal/history"
)

func (v *View) copySelected() {
	v.copy(v.navigationID(), "", true)
}

func (v *View) copySource() {
	if v.detailUsable() && v.entry.ID != 0 {
		v.copyCustom(v.entry.ID, "", v.entry.Text, "已复制原文", "source", false)
	}
}

func (v *View) copy(id int64, provider string, closeAfter bool) {
	v.copyCustom(id, provider, "", "已复制译文", "provider:"+provider, closeAfter)
}

func (v *View) copyCustom(id int64, provider, overrideText, toastMsg, feedbackKey string, closeAfter bool) {
	if !v.pageUsable() || id == 0 {
		return
	}
	v.copySerial++
	ctx, serial, removals := v.life, v.copySerial, v.pageRemovals
	finish := func(text string, err error) {
		if ctx.Err() != nil || serial != v.copySerial {
			return
		}
		// Reject snapshots predating a removal, not unrelated incoming translations.
		if v.store.RemovalRevision() != removals || errors.Is(err, history.ErrMissing) {
			v.Refresh()
			v.status = "历史记录已变更，请重新选择。"
			return
		}
		if err != nil {
			v.status = "无法读取译文，请重试。"
			return
		}
		if err := v.actions.Copy(text); err != nil {
			v.status = "无法写入剪贴板，请重试。"
			return
		}
		if closeAfter {
			v.Dismiss()
			return
		}
		v.status, v.pendingToast = "", toastMsg
		v.copiedKey, v.copiedUntil = feedbackKey, time.Time{}
	}
	if overrideText != "" {
		finish(overrideText, nil)
		return
	}
	v.actions.Run(func() {
		text, err := v.store.CopyText(ctx, id, provider)
		v.actions.Post(func() { finish(text, err) })
	})
}
