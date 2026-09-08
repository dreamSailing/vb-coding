package hints

// Copyright (c) 2026 EOSAIOS
// SPDX-License-Identifier: EOS-NCL-1.1
// 本文件基于 EOS 非商用许可证 v1.1 发布，详见 LICENSE。
// 商业使用请联系版权人获得商业授权。

import (
	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// Hint 是提示项
type Hint struct {
	Key   string
	Desc  string
	Value string
}

// SelectMsg 选择提示消息
type SelectMsg struct {
	Value string
}

// Model 是提示组件模型
type Model struct {
	table   table.Model
	hints   []Hint
	visible bool
	width   int
	height  int
}

// New 创建新的提示模型
func New() Model {
	columns := []table.Column{
		{Title: "Command", Width: 20},
		{Title: "Description", Width: 40},
	}

	t := table.New(
		table.WithColumns(columns),
		table.WithFocused(true),
		table.WithHeight(6),
	)

	return Model{
		table:   t,
		hints:   make([]Hint, 0),
		visible: false,
	}
}

// SetSize 设置大小
func (m *Model) SetSize(width, height int) {
	m.width = width
	if height < 3 {
		height = 3
	}
	m.height = height
	m.table.SetWidth(width)
	m.table.SetHeight(height)
}

// SetStyle 设置样式（使用默认样式）
func (m *Model) SetStyle(style lipgloss.Style) {
	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("#6366f1")).
		BorderBottom(true).
		Bold(true)
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("#ffffff")).
		Background(lipgloss.Color("#6366f1")).
		Bold(false)
	m.table.SetStyles(s)
}

// SetHints 设置提示
func (m *Model) SetHints(hints []Hint) {
	m.hints = hints
	m.updateTableRows()
}

// updateTableRows 更新表格行
func (m *Model) updateTableRows() {
	rows := make([]table.Row, len(m.hints))
	for i, h := range m.hints {
		rows[i] = table.Row{h.Key, h.Desc}
	}
	m.table.SetRows(rows)
}

// AddHint 添加提示
func (m *Model) AddHint(key, desc string) {
	m.hints = append(m.hints, Hint{Key: key, Desc: desc, Value: key})
	m.updateTableRows()
}

// ClearHints 清空提示
func (m *Model) ClearHints() {
	m.hints = m.hints[:0]
	m.table.SetRows([]table.Row{})
}

// Show 显示
func (m *Model) Show() {
	m.visible = true
	m.updateTableRows()
}

// Hide 隐藏
func (m *Model) Hide() {
	m.visible = false
}

// Visible 是否可见
func (m *Model) Visible() bool {
	return m.visible && len(m.hints) > 0
}

// Selected 获取选中的值
func (m *Model) Selected() string {
	if len(m.hints) == 0 {
		return ""
	}
	idx := m.table.Cursor()
	if idx < 0 || idx >= len(m.hints) {
		return ""
	}
	return m.hints[idx].Value
}

// CursorUp 向上移动
func (m *Model) CursorUp() {
	if len(m.hints) == 0 {
		return
	}
	m.table.MoveUp(1)
}

// CursorDown 向下移动
func (m *Model) CursorDown() {
	if len(m.hints) == 0 {
		return
	}
	m.table.MoveDown(1)
}

// Init 初始化
func (m Model) Init() tea.Cmd {
	return nil
}

// Update 更新
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

// View 渲染视图
func (m Model) View() string {
	if !m.visible || len(m.hints) == 0 {
		return ""
	}

	return m.table.View()
}

// Height 获取高度
func (m Model) Height() int {
	if !m.visible || len(m.hints) == 0 {
		return 0
	}
	return m.height
}

// SetHeight 设置高度
func (m *Model) SetHeight(h int) {
	if h < 3 {
		h = 3
	}
	m.height = h
	m.table.SetHeight(h)
}
