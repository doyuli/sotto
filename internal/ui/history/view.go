package historyview

import (
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"

	"sotto/internal/history"
	"sotto/internal/naming"
	"sotto/internal/ui/shared"
)

const searchLabel = "搜索原文或译文"
const DefaultWidth, DefaultHeight = 720, 460
const (
	railDividerWidth   float32 = 1
	detailPaddingX     float32 = 20
	providerMenuWidth  float32 = 132
	sourceFontSize     float32 = 14
	sourceLineHeight   float32 = 1.5
	sourcePreviewLines         = 5
	resultFontSize     float32 = 14
	resultLongLines            = 10
	maxNamingTextBytes         = 120
	maxNamingWords             = 8
)

var groupLabels = [...]string{"今天", "近 3 天", "更早"}

var (
	searchIcon      = ui.MustParseSVG([]byte(`<svg viewBox="0 0 24 24"><path d="m20 20-4-4m2-5a7 7 0 1 1-14 0 7 7 0 0 1 14 0Z" stroke="currentColor" stroke-width="2" stroke-linecap="round" fill="none"/></svg>`))
	retranslateIcon = ui.MustParseSVG([]byte(`<svg viewBox="0 0 24 24"><path d="M20 9a8 8 0 1 0 .4 5.5M20 4v5h-5" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>`))
	checkIcon       = ui.MustParseSVG([]byte(`<svg viewBox="0 0 24 24"><path d="M20 6 9 17l-5-5" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>`))
	emptyIcon       = ui.MustParseSVG([]byte(`<svg viewBox="0 0 48 48" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
  <path d="M32 17v-5a5 5 0 0 0-5-5H13a5 5 0 0 0-5 5v21a5 5 0 0 0 5 5h10M14 15h12M14 21h8"/>
  <circle cx="31" cy="28" r="8"/>
  <path d="m37 34 5 5"/>
</svg>`))
)

var singleLine = strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ")

type palette struct {
	dark       bool
	panel      ui.Color
	rail       ui.Color
	field      ui.Color
	control    ui.Color
	pill       ui.Color
	hover      ui.Color
	selected   ui.Color
	line       ui.Color
	ink        ui.Color
	muted      ui.Color
	icon       ui.Color
	iconStrong ui.Color
	clearBg    ui.Color
	clearFg    ui.Color
	accent     ui.Color
	accentText ui.Color
	mark       ui.Color
	selection  ui.Color
	success    ui.Color
	error      ui.Color
	theme      *ui.Theme
}

var lightHistoryPalette = func() palette {
	p := palette{
		dark:       false,
		panel:      ui.Hex("#ffffff"),
		rail:       ui.Hex("#f6f7f9"),
		field:      ui.Hex("#f2f3f5"),
		control:    ui.Hex("#f1f1f1"),
		pill:       ui.Hex("#e7e7e7"),
		hover:      ui.Hex("#eceef1"),
		selected:   ui.Hex("#e5e7eb"),
		line:       ui.Hex("#e5e7eb"),
		ink:        ui.Hex("#202020"),
		muted:      ui.Hex("#5f5f5f"),
		icon:       ui.Hex("#4b4b4b"),
		iconStrong: ui.Hex("#202020"),
		clearBg:    ui.Hex("#c9c9c9"),
		clearFg:    ui.Hex("#4b4b4b"),
		accent:     ui.Hex("#2f7fe8"),
		accentText: ui.Hex("#1a5fae"),
		mark:       ui.RGBA(47, 127, 232, 0.16),
		selection:  ui.RGBA(31, 111, 208, 0.18),
		success:    ui.Hex("#15803d"),
		error:      ui.Hex("#c62828"),
	}
	t := ui.LightTheme()
	t.DisableAutoDimming = true
	t.Background = p.panel
	t.Surface = p.control
	t.SurfaceHover = p.hover
	t.SurfacePressed = p.pill
	t.Border = p.line
	t.Text = p.ink
	t.TextMuted = p.muted
	t.Accent = p.accent
	t.AccentText = ui.Hex("#ffffff")
	t.Selection = p.selection
	t.Scrollbar = ui.Hex("#c9c9c9")
	t.ScrollbarWidth = 6
	t.Danger = p.error
	p.theme = t
	return p
}()

var darkHistoryPalette = func() palette {
	p := palette{
		dark:       true,
		panel:      ui.Hex("#222325"),
		rail:       ui.Hex("#1b1c1e"),
		field:      ui.Hex("#28292c"),
		control:    ui.Hex("#2c2d2f"),
		pill:       ui.Hex("#3a3b3d"),
		hover:      ui.Hex("#26282b"),
		selected:   ui.Hex("#2f3136"),
		line:       ui.Hex("#2e3035"),
		ink:        ui.Hex("#f3f4f5"),
		muted:      ui.Hex("#a0a3a7"),
		icon:       ui.Hex("#b9bcc0"),
		iconStrong: ui.Hex("#f3f4f5"),
		clearBg:    ui.Hex("#55575b"),
		clearFg:    ui.Hex("#f3f4f5"),
		accent:     ui.Hex("#7db4ff"),
		accentText: ui.Hex("#7db4ff"),
		mark:       ui.RGBA(125, 180, 255, 0.25),
		selection:  ui.RGBA(125, 180, 255, 0.28),
		success:    ui.Hex("#4ade80"),
		error:      ui.Hex("#ff8a8a"),
	}
	t := ui.DarkTheme()
	t.DisableAutoDimming = true
	t.Background = p.panel
	t.Surface = p.control
	t.SurfaceHover = p.hover
	t.SurfacePressed = p.pill
	t.Border = p.line
	t.Text = p.ink
	t.TextMuted = p.muted
	t.Accent = p.accent
	t.AccentText = ui.Hex("#0d1b2b")
	t.Selection = p.selection
	t.Scrollbar = ui.Hex("#4a4c50")
	t.ScrollbarWidth = 6
	t.Danger = p.error
	p.theme = t
	return p
}()

func colors(c *ui.Context) palette {
	if c.Theme().Dark {
		return darkHistoryPalette
	}
	return lightHistoryPalette
}

func (v *View) Render(c *ui.Context) {
	if v.life == nil {
		return
	}
	v.updateQuery(c)
	p := colors(c)
	c.SetTheme(p.theme)

	if v.clearToasts {
		v.clearToasts = false
		c.CloseToast("")
	}
	if v.pendingToast != "" {
		c.Toast(v.pendingToast)
		v.pendingToast = ""
	}
	if v.copiedKey != "" {
		if v.copiedUntil.IsZero() {
			v.copiedUntil = c.Now().Add(1500 * time.Millisecond)
		}
		if !c.Now().Before(v.copiedUntil) {
			v.copiedKey, v.copiedUntil = "", time.Time{}
		} else {
			c.After(v.copiedUntil.Sub(c.Now()))
		}
	}

	width, _ := c.Size()
	rows := v.railRows
	c.Root().Background(p.panel).Padding(0).Children(func() {
		if !v.clearOpen && !v.providerOpen {
			c.OnShortcut(0, ui.KeyEscape, v.Dismiss)
			if !v.searchField.Focused(c) {
				c.OnShortcut(0, ui.KeyUp, func() { v.move(-1) })
				c.OnShortcut(0, ui.KeyDown, func() { v.move(1) })
				c.OnShortcut(0, ui.KeyEnter, v.copySelected)
			}
		}
		ui.Column(c).Fill().Children(func() {
			v.renderSearch(c, p)
			rule(c, p)
			if len(rows) == 0 && (v.ready || v.selected == 0) {
				v.renderPlaceholder(c, p)
			} else {
				ui.Row(c).FillWidth().Grow(1).MinHeight(0).Children(func() {
					railW := max(180, min(210, width*0.29))
					ui.Column(c).Width(railW).FillHeight().Background(p.rail).Children(func() {
						v.renderRail(c, p, rows)
					})
					ui.Box(c).Width(railDividerWidth).FillHeight().Background(p.line)
					v.renderDetail(c, p, width-railW-railDividerWidth)
				})
			}
			rule(c, p)
			v.renderFooter(c, p)
		})
		if v.clearOpen {
			shared.ConfirmDialog(c.Key("confirm-clear"), &v.clearOpen, shared.ConfirmDialogOptions{
				Title:        "清空翻译历史？",
				Message:      "所有原文和译文都将删除，且无法恢复。",
				CancelLabel:  "取消",
				ConfirmLabel: "确认清空",
				Colors: shared.ConfirmDialogColors{
					Dark:   p.dark,
					Panel:  p.panel,
					Border: p.line,
					Text:   p.ink,
					Muted:  p.muted,
				},
				OnConfirm: func() { v.delete(0) },
			})
		}
	})
}

func rule(c *ui.Context, p palette) { ui.Box(c).FillWidth().Height(1).Background(p.line) }

func (v *View) renderSearch(c *ui.Context, p palette) {
	ui.Row(c).FillWidth().Height(44).Padding(0, 10, 0, 14).AlignItems(ui.Center).Gap(8).DragWindow().Children(func() {
		ui.Icon(c, searchIcon).Size(15, 15).TextColor(p.muted)
		ui.TextInputBase(c.Key("history-search"), &v.search).Bind(&v.searchField).
			Label(searchLabel).Placeholder(searchLabel).PlaceholderColor(p.muted.Alpha(0.75)).
			Grow(1).FillHeight().MinWidth(0).FontSize(14).TextColor(p.ink).
			CaretColor(p.accent).SelectionColor(p.selection).FocusRing(false).
			OnChange(func() { v.searchChanged(c.Now()) }).
			OnSubmit(v.copySelected).
			OnTextKey(func(k ui.TextKey) bool {
				if k.Mods != 0 {
					return false
				}
				if strings.TrimSpace(k.Text()) != v.query {
					return k.Key == ui.KeyUp || k.Key == ui.KeyDown
				}
				switch k.Key {
				case ui.KeyUp:
					v.move(-1)
				case ui.KeyDown:
					v.move(1)
				default:
					return false
				}
				return true
			})
		if v.focus {
			v.searchField.Focus(c)
			v.focus = false
		}
		if v.search != "" {
			clearBtn := ui.ButtonBase(c.Key("clear-search")).Label("清空搜索").Tooltip("清空搜索").
				Cursor(ui.CursorPointer).Size(18, 18).Radius(9).Background(p.clearBg).Center().
				OnClick(func() {
					v.search = ""
					v.searchChanged(c.Now())
					v.searchField.Focus(c)
				})
			if clearBtn.Hovered() {
				clearBtn.Background(p.icon)
			}
			clearBtn.Children(func() {
				fg := p.clearFg
				if clearBtn.Hovered() && !p.dark {
					fg = p.panel
				}
				ui.Icon(c, shared.CloseIcon).Size(10, 10).TextColor(fg)
			})
		}
		closeBtn := ui.ButtonBase(c.Key("close-history")).Label("关闭翻译历史").Tooltip("关闭翻译历史 (Esc)").
			Size(26, 26).Radius(6).Cursor(ui.CursorPointer).FocusRing(true).Center().OnClick(v.Dismiss)
		color := p.muted
		if closeBtn.Hovered() {
			closeBtn.Background(p.hover)
			color = p.ink
		}
		closeBtn.Children(func() { ui.Icon(c, shared.CloseIcon).Size(15, 15).TextColor(color) })
	})
}

func (v *View) renderPlaceholder(c *ui.Context, p palette) {
	ui.Column(c).FillWidth().Grow(1).MinHeight(0).Padding(16, 24).Center().Gap(10).Children(func() {
		switch {
		case v.loadError:
			ui.Text(c, "无法读取翻译历史").FontSize(14).TextColor(p.ink)
			ui.Button(c, "重试").OnClick(func() { v.reset(false) })
		case !v.ready:
			ui.Text(c, "正在读取…").FontSize(13).TextColor(p.muted)
		default:
			title, hint := "还没有翻译记录", "开启记录后，翻译结果会保存在这里。"
			if v.query != "" {
				title, hint = "没找到匹配的记录", "换个关键词，或试试更短的词。"
			}
			ui.Box(c).Size(56, 56).Radius(14).Background(p.field).Center().Children(func() {
				ui.Icon(c, emptyIcon).Size(38, 38).TextColor(p.muted).AccessHidden(true)
			})
			ui.Column(c).Gap(4).AlignItems(ui.Center).Children(func() {
				ui.Text(c, title).FontSize(15).FontWeight(600).TextColor(p.ink).TextAlign(ui.Center)
				ui.Text(c, hint).FontSize(13).LineHeight(1.55).TextColor(p.muted).TextAlign(ui.Center)
			})
		}
	})
}

func (v *View) renderRail(c *ui.Context, p palette, rows []railRow) {
	v.rail.NearEnd = 3
	v.rail.EndReached = func(int) {
		if v.canExtend() {
			v.loadPage()
		}
	}
	ui.List(c, &v.rail, len(rows)).Fill().Padding(6, 6, 8, 6).
		ItemKey(func(i int) any { return rows[i].key() }).
		Rows(func(row ui.ListRow) {
			c, r := row.Context, rows[row.Index]
			if r.entry == nil {
				v.renderGroup(c, p, r, row.Index == 0)
				return
			}
			entry := r.entry
			selected := v.selected == entry.ID
			button := ui.ButtonBase(c).FillWidth().Padding(6, 8).Radius(6).Label(entry.Text).Cursor(ui.CursorPointer)
			if selected {
				button.Background(p.selected)
			} else if button.Hovered() {
				button.Background(p.hover)
			}
			button.OnClick(func() { v.selectEntry(entry.ID) })
			// Enter copies the current selection even if an arrow key moved it past this focused row.
			button.OnShortcut(0, ui.KeyEnter, v.copySelected)
			if button.DoubleClicked() {
				v.copy(entry.ID, "", false)
			}
			button.Children(func() {
				textColor := p.ink.Alpha(0.88)
				if selected {
					textColor = p.ink
				}
				ui.Column(c).FillWidth().Gap(3).Children(func() {
					v.marked(c, p, singleLine.Replace(entry.Text)).FillWidth().MaxLines(2).Ellipsis("…").
						FontSize(13).FontWeight(400).LineHeight(1.38).TextColor(textColor)
					if entry.Snippet != "" {
						spans := []ui.Span{{Text: providerName(entry.SnippetProvider) + " · "}}
						spans = append(spans, searchSpans(singleLine.Replace(entry.Snippet), v.query, p.mark, ui.Span{})...)
						ui.RichText(c, spans...).FillWidth().SingleLine().Ellipsis("…").FontSize(11.5).LineHeight(1.35).TextColor(p.muted)
					}
				})
			})
		})
}

func (v *View) marked(c *ui.Context, p palette, text string) ui.Element {
	return ui.RichText(c, searchSpans(text, v.query, p.mark, ui.Span{})...)
}

func asciiFold(r rune) rune {
	if r >= 'A' && r <= 'Z' {
		return r + ('a' - 'A')
	}
	return r
}

func firstFoldedMatch(text, query string) (int, int, bool) {
	if query == "" {
		return 0, 0, false
	}
	folded, word := strings.Map(asciiFold, text), strings.Map(asciiFold, query)
	offset := strings.Index(folded, word)
	if offset < 0 {
		return 0, 0, false
	}
	return offset, offset + len(word), true
}

func foldedContains(text, query string) bool {
	_, _, ok := firstFoldedMatch(text, query)
	return ok
}

func searchSpans(text, query string, mark ui.Color, style ui.Span) []ui.Span {
	style.Text = text
	if query == "" {
		return []ui.Span{style}
	}
	// SQLite's ASCII fold preserves byte offsets. Limit spans on long
	// translations so a one-letter query cannot dominate layout.
	folded, word := strings.Map(asciiFold, text), strings.Map(asciiFold, query)
	var spans []ui.Span
	start := 0
	for range 128 {
		offset := strings.Index(folded[start:], word)
		if offset < 0 {
			break
		}
		match := start + offset
		if offset > 0 {
			style.Text = text[start:match]
			spans = append(spans, style)
		}
		start = match + len(query)
		highlight := style
		highlight.Text, highlight.Background, highlight.Radius = text[match:start], mark, 2
		spans = append(spans, highlight)
	}
	if start < len(text) || len(spans) == 0 {
		style.Text = text[start:]
		spans = append(spans, style)
	}
	return spans
}

func (v *View) renderGroup(c *ui.Context, p palette, row railRow, first bool) {
	label := groupLabels[row.group]
	topMargin := float32(10)
	if first {
		topMargin = 3
	}
	content := func() {
		ui.Text(c, label).FontSize(11.5).FontWeight(500).TextColor(p.muted.Alpha(0.85))
		ui.Box(c).Grow(1)
		if row.count != 0 {
			ui.Text(c, fmt.Sprint(row.count)).FontSize(11.5).TextColor(p.muted.Alpha(0.75))
		}
	}
	if row.group != 2 || v.query != "" {
		ui.Row(c).FillWidth().Padding(topMargin, 8, 4, 8).AlignItems(ui.Center).Children(content)
		return
	}
	button := ui.ButtonBase(c).FillWidth().Justify(ui.Start).Margin(topMargin-3, 0, 0, 0).Padding(4, 8).Radius(6).
		Label(label).Cursor(ui.CursorPointer).OnClick(v.toggleOlder)
	if button.Hovered() {
		button.Background(p.hover)
	}
	button.Children(func() {
		ui.Row(c).FillWidth().Gap(4).AlignItems(ui.Center).Children(func() {
			icon := ui.Icon(c, shared.ChevronDown).Size(11, 11).TextColor(p.muted.Alpha(0.85))
			if !v.older {
				icon.Rotate(-90)
			}
			content()
		})
	})
}

func (v *View) renderDetail(c *ui.Context, p palette, width float32) {
	ui.Scroll(c).Grow(1).MinWidth(0).FillHeight().TrackScroll(&v.detailScroll).Children(func() {
		ui.Column(c.Key(v.selected)).FillWidth().Padding(16, detailPaddingX).Gap(8).Children(func() {
			if v.detailError {
				ui.Text(c, "无法读取这条译文").FontSize(13).TextColor(p.muted)
				ui.Button(c, "重新读取").OnClick(func() { v.loadDetail(v.selected) })
				return
			}
			entry := v.entry
			if entry.ID == 0 {
				return
			}
			textW := max(1, width-2*detailPaddingX)
			measured := v.measureDetail(c, entry, textW)
			sourceExpanded := v.sourceExpanded
			if !v.sourceManual && measured.sourceMatchHidden {
				sourceExpanded = true
			}

			direction := shared.LanguageName(entry.SourceLang) + " → " + shared.LanguageName(entry.TargetLang)
			ui.Row(c).FillWidth().Gap(2).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "原文 · "+direction).Grow(1).MinWidth(0).FontSize(12).TextColor(p.muted).SingleLine().Ellipsis("…")
				v.renderSpeakButton(c, p, entry.ID, "朗读原文", "朗读原文", entry.Text, entry.SourceLang)
				v.renderCopyButton(c, p, "复制原文", "复制原文", "source", false, v.copySource)
			})
			sourceText := v.marked(c, p, entry.Text).FillWidth().Selectable().SelectionColor(p.selection).
				FontSize(sourceFontSize).FontWeight(600).LineHeight(sourceLineHeight).TextColor(p.ink)
			if measured.sourceOverflow && !sourceExpanded {
				sourceText.MaxLines(sourcePreviewLines).Ellipsis("…")
			}
			if measured.sourceOverflow {
				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
					toggleLabel := "展开全文"
					if sourceExpanded {
						toggleLabel = "收起原文"
					}
					linkButton(c.Key("source-disclosure"), p, toggleLabel, sourceExpanded, func() {
						v.sourceExpanded = !sourceExpanded
						v.sourceManual = true
					})
					ui.Text(c, fmt.Sprintf("共 %d 字", utf8.RuneCountInString(entry.Text))).
						FontSize(12).TextColor(p.muted.Alpha(0.8))
				})
			}
			translated := time.UnixMilli(entry.TranslatedMS).In(v.now().Location())
			ui.Text(c, "最近翻译 "+translated.Format("2006-01-02 15:04")).FontSize(12).TextColor(p.muted)

			providers, formats := v.actions.Options()
			retry := v.retries[entry.ID]
			ui.Row(c).FillWidth().MinHeight(28).Margin(6, 0, 0, 0).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "译文").FontSize(14).FontWeight(600).TextColor(p.ink)
				ui.Text(c, fmt.Sprintf("· %d 个源", len(v.results))).FontSize(12).TextColor(p.muted)
				ui.Box(c).Grow(1)
				v.renderProviders(c, p, entry, providers, retry.pending)
			})
			v.renderRetryStatus(c, p, entry, retry, slices.Contains(providers, retry.provider))

			for _, result := range v.results {
				v.renderResult(c, p, entry, result, providers, formats, false, measured.defaultCollapsed[result.Provider])
			}
			if unsaved := retry.result.Text; unsaved != "" {
				result := history.Result{Provider: retry.resultProvider, Text: unsaved, Type: retry.result.Type}
				v.renderResult(c, p, entry, result, providers, formats, true, false)
			}
		})
	})
}

func (v *View) renderResult(c *ui.Context, p palette, entry history.Entry, result history.Result, providers, formats []string, unsaved, defaultCollapsed bool) {
	provider, text, resultType := result.Provider, result.Text, result.Type
	name := providerName(provider)
	title, speechName := name, name
	collapseKey, copyFeedbackKey, copyLabel := provider, "provider:"+provider, "复制 "+name+" 译文"
	if unsaved {
		title += " · 本次译文（未保存）"
		speechName += "（未保存）"
		collapseKey = "unsaved:" + provider
		copyFeedbackKey, copyLabel = collapseKey, "复制未保存的译文"
	}
	retry := v.retries[entry.ID]
	canRetry := slices.Contains(providers, provider) && v.providerEligible(provider) && (unsaved || retry.result.Text == "" || retry.resultProvider != provider)
	rowKey := "result:" + collapseKey
	collapsed, manual := v.manualCollapse[collapseKey]
	if !manual {
		collapsed = defaultCollapsed
	}
	c.Key(rowKey + ":rule")
	rule(c, p)
	ui.Column(c.Key(rowKey)).FillWidth().Padding(8, 0).Gap(6).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(2).Children(func() {
			toggleBtn := ui.ButtonBase(c.Key("toggle")).
				Label("折叠 "+title).
				Expanded(!collapsed).
				Grow(1).
				MinWidth(0).
				Justify(ui.Start).
				AlignItems(ui.Center).
				Padding(2, 4, 2, 0).
				Radius(5).
				Cursor(ui.CursorPointer).
				OnClick(func() {
					v.manualCollapse[collapseKey] = !collapsed
				})
			toggleBtn.Children(func() {
				color := p.ink
				if collapsed && !toggleBtn.Hovered() {
					color = p.muted
				}
				ui.Text(c, title).FontSize(13).FontWeight(600).TextColor(color).SingleLine().Ellipsis("…")
			})

			v.renderResultSpeakButton(c, p, entry, "朗读译文 · "+speechName, "朗读单词 · "+speechName, resultType, text)
			if canRetry {
				iconButton(c, p, retranslateIcon, "重译 "+title+" 译文", "重译", retry.pending, func() { v.retry(entry, provider) })
			}
			v.renderCopyButton(c, p, copyLabel, "复制", copyFeedbackKey, false, func() {
				if !v.detailUsable() {
					return
				}
				if unsaved {
					v.copyCustom(entry.ID, "", text, "已复制译文", copyFeedbackKey, false)
				} else {
					v.copy(entry.ID, provider, false)
				}
			})
		})

		if collapsed {
			return
		}

		v.renderResultText(c, p, resultType, text)
		if entry.TargetLang == "en" && resultType != history.ResultDictionary {
			v.renderNamingChips(c, p, entry, collapseKey, text, formats)
		}
	})
}

func (v *View) renderNamingChips(c *ui.Context, p palette, entry history.Entry, cardKey, text string, formats []string) {
	if len(formats) == 0 || len(text) > maxNamingTextBytes || strings.ContainsAny(text, "\r\n") {
		return
	}
	words := naming.SplitWords(text)
	if len(words) == 0 || len(words) > maxNamingWords {
		return
	}
	ui.Row(c).FillWidth().AlignItems(ui.Center).Wrap().Gap(4).Children(func() {
		for _, style := range formats {
			val := naming.Format(words, style)
			if val == "" {
				continue
			}
			label := naming.Labels[style]
			tag := naming.ChipTag(style)
			chipKey := "chip:" + cardKey + ":" + style
			copied := v.copiedKey == chipKey
			tip := label + "：" + val
			if copied {
				tip = "已复制 " + label
			}
			chip := ui.ButtonBase(c.Key(style)).
				Label(label+"："+val).
				Tooltip(tip).
				Cursor(ui.CursorPointer).
				MinWidth(24).
				Height(22).
				Padding(0, 6).
				Radius(6).
				Center().
				OnClick(func() {
					if v.detailUsable() {
						v.copyCustom(entry.ID, "", val, "已复制 "+label, chipKey, false)
					}
				})
			if copied {
				chip.Background(p.hover)
			} else if chip.Hovered() {
				chip.Background(p.hover)
			}
			chip.Children(func() {
				color := p.icon
				if copied {
					color = p.success
				} else if chip.Hovered() {
					color = p.iconStrong
				}
				ui.Text(c, tag).
					Font("monospace").
					FontSize(10).
					LetterSpacing(-0.2).
					TextColor(color).
					SingleLine()
			})
		}
	})
}

func (v *View) renderResultSpeakButton(c *ui.Context, p palette, entry history.Entry, transLabel, wordLabel string, resultType history.ResultType, text string) {
	label, lang := transLabel, entry.TargetLang
	if resultType == history.ResultDictionary {
		label, lang = wordLabel, "en"
		text, _, _ = strings.Cut(text, "\n")
	}
	v.renderSpeakButton(c, p, entry.ID, label, "朗读译文", text, lang)
}

func (v *View) renderSpeakButton(c *ui.Context, p palette, id int64, label, tip, text, lang string) {
	if v.actions.Speak != nil {
		iconButton(c, p, shared.SpeakIcon, label, tip, false, func() { v.speak(id, text, lang) })
	}
}

func (v *View) renderCopyButton(c *ui.Context, p palette, label, tip, feedbackKey string, disabled bool, fn func()) {
	copied := feedbackKey != "" && v.copiedKey == feedbackKey
	icon := shared.CopyIcon
	if copied {
		icon = checkIcon
		tip = "已复制"
	}
	button := ui.ButtonBase(c.Key(label)).Label(label).Tooltip(tip).Disabled(disabled).FocusRing(true).
		Size(24, 24).Radius(6).Cursor(ui.CursorPointer).OnClick(fn)
	color := p.icon
	switch {
	case disabled:
		color = p.muted.Alpha(0.45)
		button.Cursor(ui.CursorDefault)
	case copied:
		color = p.success
	case button.Hovered():
		button.Background(p.hover)
		color = p.iconStrong
	}
	button.Children(func() { ui.Icon(c, icon).Size(14, 14).TextColor(color) })
}

func (v *View) renderResultText(c *ui.Context, p palette, resultType history.ResultType, text string) {
	ui.RichText(c, resultSpans(resultType, text, v.query, p)...).
		FillWidth().Selectable().SelectionColor(p.selection).FontSize(resultFontSize).LineHeight(1.6).TextColor(p.ink)
}

func (v *View) measureDetail(c *ui.Context, entry history.Entry, textW float32) detailMeasurement {
	m := v.detailMeasure
	if m.entryID == entry.ID && m.width == textW && m.query == v.query && m.resultCount == len(v.results) {
		return m
	}
	m = detailMeasurement{
		entryID:        entry.ID,
		width:          textW,
		query:          v.query,
		resultCount:    len(v.results),
		sourceOverflow: exceedsLines(c, entry.Text, textW, sourceFontSize, 600, sourcePreviewLines),
	}
	if m.sourceOverflow && v.query != "" {
		if _, end, ok := firstFoldedMatch(entry.Text, v.query); ok {
			// Treat matches reaching the ellipsis line as hidden so they expand cleanly.
			m.sourceMatchHidden = exceedsLines(c, entry.Text[:end], textW, sourceFontSize, 600, sourcePreviewLines-1)
		}
	}
	if len(v.results) > 1 && slices.ContainsFunc(v.results, func(r history.Result) bool {
		return exceedsLines(c, r.Text, textW, resultFontSize, 400, resultLongLines)
	}) {
		// Recompute defaults with the layout; only explicit toggles persist across refreshes.
		m.defaultCollapsed = make(map[string]bool)
		for _, r := range v.results[1:] {
			if !foldedContains(r.Text, v.query) {
				m.defaultCollapsed[r.Provider] = true
			}
		}
	}
	v.detailMeasure = m
	return m
}

func exceedsLines(c *ui.Context, text string, width, size float32, weight, maxLines int) bool {
	if text == "" || maxLines <= 0 {
		return false
	}
	if strings.Count(text, "\n") >= maxLines {
		return true
	}
	_, lineH := c.MeasureText(0, ui.Span{Text: "M", Size: size, Weight: weight})
	if lineH <= 0 {
		return false
	}
	_, fullH := c.MeasureText(width, ui.Span{Text: text, Size: size, Weight: weight})
	return fullH > lineH*float32(maxLines)+0.5
}

func resultSpans(resultType history.ResultType, text, query string, p palette) []ui.Span {
	if resultType != history.ResultDictionary {
		return searchSpans(text, query, p.mark, ui.Span{})
	}
	// History stores the complete plain-text dictionary entry. Style its
	// headword and optional pronunciation without changing copy or selection.
	word, definition, multiline := strings.Cut(text, "\n")
	if !multiline {
		return searchSpans(word, query, p.mark, ui.Span{Weight: 600})
	}
	spans := searchSpans(word+"\n", query, p.mark, ui.Span{Weight: 600})
	ipa, rest, separate := strings.Cut(definition, "\n")
	unlabeled := strings.TrimPrefix(strings.TrimPrefix(ipa, "英 "), "美 ")
	if separate && strings.HasPrefix(unlabeled, "/") && strings.HasSuffix(unlabeled, "/") {
		spans = append(spans, searchSpans(ipa+"\n", query, p.mark, ui.Span{Color: p.muted, Size: 13})...)
		definition = rest
	}
	return append(spans, searchSpans(definition, query, p.mark, ui.Span{})...)
}

func providerName(id string) string { return shared.ServiceName(id) }

func (v *View) renderProviders(c *ui.Context, p palette, entry history.Entry, providers []string, pending bool) {
	if len(providers) == 0 {
		v.providerOpen = false
		quietButton(c, p, "启用翻译源", "启用翻译源", v.actions.OpenServices == nil, v.actions.OpenServices)
		return
	}
	available := make([]string, 0, len(providers))
	for _, provider := range providers {
		if !v.hasTranslation(entry.ID, provider) && v.providerEligible(provider) {
			available = append(available, provider)
		}
	}
	if len(available) == 0 {
		v.providerOpen = false
		return
	}
	menu := ui.MenuBase(c.Key(entry.ID), &v.providerOpen)
	hoverBg := p.hover
	if p.dark {
		hoverBg = p.selected
	}
	trigger := menu.Trigger.Label("换源翻译").Width(providerMenuWidth).Padding(5, 10).Radius(8).
		Background(p.field).Border(1, p.line).Disabled(pending).Cursor(ui.CursorPointer)
	if pending {
		trigger.Cursor(ui.CursorDefault)
	} else if trigger.Hovered() || menu.Open() {
		trigger.Background(hoverBg)
	}
	labelColor, iconColor := p.ink, p.muted
	if pending {
		labelColor, iconColor = p.muted.Alpha(0.45), p.muted.Alpha(0.45)
	}
	trigger.Children(func() {
		ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "换源翻译").Grow(1).MinWidth(0).SingleLine().FontSize(12).TextColor(labelColor)
			ui.Icon(c, shared.ChevronDown).Size(12, 12).TextColor(iconColor)
		})
	})
	menu.Popup(func(panel ui.Element) {
		panel.Label("换源翻译选项").Width(providerMenuWidth).Padding(4).Margin(4, 0, 0, 0).Gap(2).Radius(10).
			Background(p.panel).Border(1, p.line).Shadow(0, 4, 12, 0, ui.RGBA(0, 0, 0, 0.14))
		for _, provider := range available {
			name := providerName(provider)
			c.Key(provider)
			item := menu.Item().FillWidth().MinHeight(30).Padding(5, 8).Radius(6).
				Label(name + " · 新增译文").Cursor(ui.CursorPointer).OnClick(func() {
				if !v.hasTranslation(entry.ID, provider) {
					v.retry(entry, provider)
				}
			})
			if item.Highlighted() {
				item.Background(hoverBg)
			}
			item.Children(func() {
				ui.Text(c, name).Grow(1).MinWidth(0).SingleLine().Ellipsis("…").FontSize(12).TextColor(p.ink)
			})
		}
	})
}

func (v *View) renderRetryStatus(c *ui.Context, p palette, entry history.Entry, retry retryState, enabled bool) {
	if retry.provider == "" {
		return
	}
	name := providerName(retry.provider)
	switch {
	case retry.err != nil || retry.pending && retry.lastErr != nil:
		title, color, err := name+" 翻译失败", p.error, retry.err
		if retry.pending {
			title, color, err = name+" 重试中…", p.ink, retry.lastErr
		}
		ui.Row(c.Key("retry-card")).FillWidth().Padding(6, 10).Gap(8).Radius(8).AlignItems(ui.Center).Background(p.field).Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
				ui.Text(c, title).FontSize(13).FontWeight(600).TextColor(color).SingleLine().Ellipsis("…")
				ui.Text(c, err.Error()).FillWidth().FontSize(12).TextColor(p.muted)
			})
			if enabled {
				quietButton(c, p, "重试", "重试 "+name+" 翻译", retry.pending, func() { v.retry(entry, retry.provider) })
			}
		})
	case retry.pending:
		ui.Text(c.Key("retry-pending"), "正在用 "+name+" 翻译…").FontSize(12).TextColor(p.muted)
	case retry.result.Notice != "":
		ui.Text(c.Key("retry-notice"), retry.result.Notice).FontSize(12).TextColor(p.muted)
	default:
		ui.Text(c.Key("retry-saved"), "已保存 "+name+" 译文").FontSize(12).TextColor(p.muted)
	}
}

func (v *View) renderFooter(c *ui.Context, p palette) {
	ui.Row(c).FillWidth().Padding(4, 12).MinHeight(32).AlignItems(ui.Center).Gap(8).Children(func() {
		message := v.status
		hint := ""
		switch {
		case message != "":
		case v.loadError && len(v.entries) != 0:
			message = "无法继续读取历史"
			quietButton(c, p, "重试", "重试加载历史", false, func() { v.reset(false) })
		case v.loading && len(v.entries) != 0:
			message = "正在加载更多…"
		case v.more && len(v.entries) != 0:
			if v.hasGroupCounts {
				message = fmt.Sprintf("已加载 %d / 共 %d 条", len(v.entries), v.totalCount())
			} else {
				message = fmt.Sprintf("已加载 %d+ 条", len(v.entries))
			}
			hint = "↑↓ 切换 · Enter 复制"
		case v.ready && !v.loadError && len(v.entries) != 0:
			message = fmt.Sprintf("%d 条记录", len(v.entries))
			hint = "↑↓ 切换 · Enter 复制"
		}
		ui.Row(c).Grow(1).MinWidth(0).AlignItems(ui.Center).Gap(6).Children(func() {
			ui.Text(c, message).FontSize(12).TextColor(p.muted).SingleLine().Ellipsis("…")
			if hint != "" {
				ui.Text(c, "· "+hint).FontSize(12).TextColor(p.muted.Alpha(0.75)).SingleLine().Ellipsis("…")
			}
		})
		hasEntry := v.entry.ID != 0
		if hasEntry {
			quietButton(c, p, "删除此条", "删除此条历史", v.deleting, func() { v.delete(v.entry.ID) })
		}
		showClear := v.loadError || !v.ready || len(v.entries) != 0 || v.query != ""
		if showClear {
			if hasEntry {
				ui.Box(c).Size(1, 12).Background(p.line)
			}
			quietButton(c, p, "清空历史", "清空历史", v.deleting || v.loadError || !v.ready || len(v.entries) == 0, func() {
				v.clearOpen = true
			})
		}
	})
}

func linkButton(c *ui.Context, p palette, label string, expanded bool, fn func()) {
	button := ui.ButtonBase(c).Label(label).Expanded(expanded).Padding(2, 2).Radius(4).FocusRing(true).
		Cursor(ui.CursorPointer).OnClick(fn)
	button.Children(func() {
		txt := ui.Text(c, label).FontSize(12).TextColor(p.accentText).SingleLine()
		if button.Hovered() {
			txt.Underline()
		}
	})
}

func quietButton(c *ui.Context, p palette, text, label string, disabled bool, fn func()) {
	button := ui.ButtonBase(c.Key(label)).Label(label).Disabled(disabled).Radius(5).Padding(4, 7).
		Cursor(ui.CursorPointer).OnClick(fn)
	color := p.muted
	switch {
	case disabled:
		color = p.muted.Alpha(0.45)
		button.Cursor(ui.CursorDefault)
	case button.Hovered():
		button.Background(p.hover)
		color = p.ink
	}
	button.Children(func() { ui.Text(c, text).FontSize(12).TextColor(color).SingleLine() })
}

func iconButton(c *ui.Context, p palette, icon *ui.SVG, label, tip string, disabled bool, fn func()) {
	button := ui.ButtonBase(c.Key(label)).Label(label).Tooltip(tip).Disabled(disabled).FocusRing(true).
		Size(24, 24).Radius(6).Cursor(ui.CursorPointer).OnClick(fn)
	color := p.icon
	switch {
	case disabled:
		color = p.muted.Alpha(0.45)
		button.Cursor(ui.CursorDefault)
	case button.Hovered():
		button.Background(p.hover)
		color = p.iconStrong
	}
	button.Children(func() { ui.Icon(c, icon).Size(14, 14).TextColor(color) })
}
