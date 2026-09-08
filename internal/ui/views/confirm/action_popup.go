package confirm

// Copyright (c) 2026 EOSAIOS
// SPDX-License-Identifier: EOS-NCL-1.1
// 本文件基于 EOS 非商用许可证 v1.1 发布，详见 LICENSE。
// 商业使用请联系版权人获得商业授权。

// action_popup.go 是一个可复用的纯 UI 操作选择弹框组件。
//
// 当用户点击某条消息文本时，由 app 层构造 ActionRequest 弹出本组件，
// 让用户在「复制」「下载」等动作中选择。本组件只负责 UI 交互并发出
// ActionResultMsg，不执行任何剪贴板/文件等副作用——执行由 app 层决定，
// 从而保持 UI 层的高内聚、低耦合。

import (
	"strings"

	"github.com/eosaios/eos/internal/i18n"
	"github.com/eosaios/eos/internal/ui/styles"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// ActionItem 描述弹框中一个可选动作。
type ActionItem struct {
	Kind  string // "copy"、"download" 等业务约定的动作标识
	Label string // 展示文案（如「复制」「下载」）
}

// ActionRequest 构造弹框所需的输入。
type ActionRequest struct {
	Title   string       // 弹框标题；为空时取 i18n 的 action.popup.title
	Actions []ActionItem // 可选动作列表（至少一个）
	Payload string       // 待操作文本（复制/下载的内容）
	Index   int          // 对应历史记录索引，便于 app 回填状态
}

// ActionResultMsg 用户选择动作后发出，由 app 层处理。
type ActionResultMsg struct {
	Kind    string // 取消时为 "cancel"，否则为所选 ActionItem.Kind
	Action  string // 所选动作的展示文案（取消时为空）
	Payload string // 原样回传
	Index   int    // 原样回传
}

// ActionPopup 操作选择弹框。
type ActionPopup struct {
	width    int
	height   int
	styles   *styles.Styles
	language string

	req      ActionRequest
	selected int

	panel lipgloss.Style
	title lipgloss.Style
	muted lipgloss.Style
	opt   lipgloss.Style
	sel   lipgloss.Style
}

// NewActionPopup 创建操作弹框。
func NewActionPopup(s *styles.Styles, lang string, req ActionRequest) *ActionPopup {
	m := &ActionPopup{
		styles:   s,
		language: lang,
		req:      req,
	}
	if m.selected < 0 || m.selected >= len(req.Actions) {
		m.selected = 0
	}
	m.panel = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(s.Theme.Primary).
		Padding(1, 2)
	m.title = lipgloss.NewStyle().Bold(true).Foreground(s.Theme.Primary)
	m.muted = lipgloss.NewStyle().Foreground(s.Theme.TextMuted)
	m.opt = lipgloss.NewStyle().Padding(0, 2)
	m.sel = lipgloss.NewStyle().
		Background(s.Theme.Primary).
		Foreground(s.Theme.Background).
		Padding(0, 2)
	return m
}

// SetSize 设置可用尺寸，用于居中定位。
func (m *ActionPopup) SetSize(w, h int) {
	m.width, m.height = w, h
}

// Init 启动命令。
func (m *ActionPopup) Init() tea.Cmd { return nil }

// Update 处理按键：Esc 取消、↑↓ 选择、Enter 确认、1..9 快选。
func (m *ActionPopup) Update(msg tea.Msg) (*ActionPopup, tea.Cmd) {
	kmsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch kmsg.String() {
	case "esc":
		return m, func() tea.Msg {
			return ActionResultMsg{Kind: "cancel", Index: m.req.Index}
		}
	case "up":
		if m.selected > 0 {
			m.selected--
		}
		return m, nil
	case "down":
		if m.selected < len(m.req.Actions)-1 {
			m.selected++
		}
		return m, nil
	case "enter":
		if m.selected < 0 || m.selected >= len(m.req.Actions) {
			return m, nil
		}
		chosen := m.req.Actions[m.selected]
		return m, func() tea.Msg {
			return ActionResultMsg{
				Kind:    chosen.Kind,
				Action:  chosen.Label,
				Payload: m.req.Payload,
				Index:   m.req.Index,
			}
		}
	default:
		// 1..9 快速选择
		if s := kmsg.String(); len(s) == 1 {
			k := s[0]
			if k >= '1' && k <= '9' {
				idx := int(k - '1')
				if idx >= 0 && idx < len(m.req.Actions) {
					m.selected = idx
				}
			}
		}
	}
	return m, nil
}

// View 渲染弹框内容（不含居中定位，由调用方 overlay）。
func (m *ActionPopup) View() string {
	title := strings.TrimSpace(m.req.Title)
	if title == "" {
		title = i18n.T("action.popup.title", m.language)
	}

	var b strings.Builder
	b.WriteString(m.title.Render(title))
	b.WriteString("\n\n")

	for i, item := range m.req.Actions {
		style := m.opt
		if i == m.selected {
			style = m.sel
		}
		label := item.Label
		if label == "" {
			label = item.Kind
		}
		b.WriteString(style.Render(itoa(i+1) + ". " + label))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(m.muted.Render(i18n.T("action.popup.help", m.language)))

	return m.panel.Render(b.String())
}

// itoa 轻量整数转字符串，避免引入 strconv 仅为此一处。
func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := false
	if v < 0 {
		neg = true
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
