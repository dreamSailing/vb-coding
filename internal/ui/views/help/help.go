package help

// Copyright (c) 2026 EOSAIOS
// SPDX-License-Identifier: EOS-NCL-1.1
// 本文件基于 EOS 非商用许可证 v1.1 发布，详见 LICENSE。
// 商业使用请联系版权人获得商业授权。

import (
	"strings"

	"github.com/eosaios/eos/internal/i18n"
	"github.com/eosaios/eos/internal/ui/features/slash"
	"github.com/eosaios/eos/internal/ui/styles"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

const (
	keyColumnWidth     = 12
	commandColumnWidth = 14
)

// HelpView 帮助视图
type HelpView struct {
	width    int
	height   int
	styles   *styles.Styles
	language string
	vp       viewport.Model
	content  string
	resetTop bool
}

// NewHelpView 创建新的帮助视图
func NewHelpView(styles *styles.Styles, lang string) *HelpView {
	vp := viewport.New()
	vp.MouseWheelEnabled = true
	vp.MouseWheelDelta = 3
	return &HelpView{
		styles:   styles,
		language: lang,
		vp:       vp,
		resetTop: true,
	}
}

// SetLanguage 设置语言
func (h *HelpView) SetLanguage(lang string) {
	h.language = lang
	h.resetTop = true
}

// SetSize 设置大小
func (h *HelpView) SetSize(width, height int) {
	h.width = width
	h.height = height
	h.relayout()
}

func (h *HelpView) ResetScroll() {
	h.resetTop = true
}

func (h *HelpView) relayout() {
	w := h.width
	hh := h.height
	if w <= 0 {
		w = 80
	}
	if hh <= 0 {
		hh = 40
	}
	innerW := w - 2 - 4
	innerH := hh - 2 - 2
	if innerW < 10 {
		innerW = 10
	}
	if innerH < 5 {
		innerH = 5
	}
	h.vp.SetWidth(innerW)
	h.vp.SetHeight(innerH)
}

// Init 初始化
func (h *HelpView) Init() tea.Cmd {
	return nil
}

// Update 更新
func (h *HelpView) Update(msg tea.Msg) (*HelpView, tea.Cmd) {
	switch msg := msg.(type) {
	case LanguageChangeMsg:
		h.language = msg.Language
		h.resetTop = true
	case tea.KeyMsg, tea.MouseMsg:
		var cmd tea.Cmd
		h.vp, cmd = h.vp.Update(msg)
		return h, cmd
	}
	return h, nil
}

// View 渲染
func (h *HelpView) View() string {
	if h.width == 0 {
		h.width = 80
	}
	if h.height == 0 {
		h.height = 40
	}
	h.relayout()

	lang := h.language
	next := h.renderContent(lang)
	if next != h.content {
		h.content = next
		h.vp.SetContent(next)
		h.resetTop = true
	}
	if h.resetTop {
		h.vp.GotoTop()
		h.resetTop = false
	}

	// 包装在面板样式中
	panelStyle := lipgloss.NewStyle().
		Width(h.width).
		Height(h.height).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#6366f1")).
		Padding(1, 2)

	return panelStyle.Render(h.vp.View())
}

func (h *HelpView) renderContent(lang string) string {
	var b strings.Builder

	// 标题
	b.WriteString(h.styles.PanelTitle.Render(i18n.T("help.title", lang)))
	b.WriteString("\n\n")

	// 全局快捷键
	b.WriteString(h.styles.TextInfo.Render(i18n.T("help.global", lang)))
	b.WriteString("\n")
	b.WriteString(h.renderKey("F2", i18n.T("help.key.f2", lang)) + "\n")
	b.WriteString(h.renderKey("Tab", i18n.T("help.key.tab", lang)) + "\n")
	b.WriteString(h.renderKey("Alt+V", i18n.T("help.key.alt_v", lang)) + "\n")
	b.WriteString(h.renderKey("→", i18n.T("help.key.right", lang)) + "\n")
	b.WriteString(h.renderKey("Ctrl+J", i18n.T("help.key.ctrl_j", lang)) + "\n")
	b.WriteString(h.renderKey("Esc", i18n.T("help.key.esc_esc", lang)) + "\n")
	b.WriteString(h.renderKey("Ctrl+C", i18n.T("help.key.ctrl_c", lang)) + "\n")
	b.WriteString(h.renderKey("PgUp/PgDn", i18n.T("help.key.pgup_pgdn", lang)) + "\n")
	b.WriteString(h.renderKey("Home / End", i18n.T("help.key.home_end", lang)) + "\n")
	b.WriteString(h.renderKey("↑/↓", i18n.T("help.key.up_down", lang)) + "\n")
	b.WriteString("\n")

	// 斜杠命令
	for _, group := range slash.GroupedVisibleCommands(lang) {
		b.WriteString(h.styles.TextInfo.Render(group.Label))
		b.WriteString("\n")
		for _, cmd := range group.Commands {
			b.WriteString(h.renderCmd(cmd.DisplayText(), cmd.Description(lang)) + "\n")
		}
		b.WriteString("\n")
	}

	b.WriteString(h.styles.TextMuted.Render(i18n.T("footer.close_help", lang)))
	return b.String()
}

// renderKey 渲染快捷键
func (h *HelpView) renderKey(key, desc string) string {
	keyStyle := h.styles.TextInfo.Bold(true)
	return h.renderAlignedRow(key, desc, keyColumnWidth, keyStyle, h.styles.TextMuted)
}

// renderCmd 渲染命令
func (h *HelpView) renderCmd(cmd, desc string) string {
	cmdStyle := h.styles.TextInfo.Bold(true)
	return h.renderAlignedRow(cmd, "- "+desc, commandColumnWidth, cmdStyle, h.styles.TextMuted)
}

func (h *HelpView) renderAlignedRow(label, desc string, labelColumnWidth int, labelStyle, descStyle lipgloss.Style) string {
	label = strings.TrimSpace(label)
	desc = strings.TrimSpace(desc)

	labelText := labelStyle.Render(label)
	descText := descStyle.Render(desc)
	labelWidth := lipgloss.Width(label)
	if labelWidth >= labelColumnWidth {
		return labelText + "\n" + strings.Repeat(" ", labelColumnWidth) + descText
	}

	return labelText + strings.Repeat(" ", labelColumnWidth-labelWidth) + descText
}

// LanguageChangeMsg 语言切换消息
type LanguageChangeMsg struct {
	Language string
}
