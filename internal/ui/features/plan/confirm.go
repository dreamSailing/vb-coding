package plan

// Copyright (c) 2026 EOSAIOS
// SPDX-License-Identifier: EOS-NCL-1.1
// 本文件基于 EOS 非商用许可证 v1.1 发布，详见 LICENSE。
// 商业使用请联系版权人获得商业授权。

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// ConfirmMode 确认模式
type ConfirmMode int

const (
	ModeAuto ConfirmMode = iota
	ModeEdit
)

// ConfirmModel 计划确认模型
type ConfirmModel struct {
	width     int
	height    int
	plan      string
	mode      ConfirmMode
	selected  int
	onConfirm func(ConfirmMode)
	onCancel  func()
	styles    *ConfirmStyles
}

// ConfirmStyles 确认面板样式
type ConfirmStyles struct {
	Title       lipgloss.Style
	Plan        lipgloss.Style
	Option      lipgloss.Style
	Selected    lipgloss.Style
	Description lipgloss.Style
	Panel       lipgloss.Style
}

// NewConfirmStyles 创建确认面板样式
func NewConfirmStyles() *ConfirmStyles {
	return &ConfirmStyles{
		Title: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#f59e0b")).
			Padding(0, 1),
		Plan: lipgloss.NewStyle().
			Background(lipgloss.Color("#1e293b")).
			Foreground(lipgloss.Color("#f1f5f9")).
			Padding(1, 2).
			Margin(1, 0),
		Option: lipgloss.NewStyle().
			Padding(0, 2),
		Selected: lipgloss.NewStyle().
			Background(lipgloss.Color("#6366f1")).
			Foreground(lipgloss.Color("#ffffff")).
			Padding(0, 2),
		Description: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#94a3b8")),
		Panel: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#f59e0b")).
			Padding(2, 4),
	}
}

// NewConfirmModel 创建计划确认模型
func NewConfirmModel(plan string) *ConfirmModel {
	return &ConfirmModel{
		plan:     plan,
		mode:     ModeAuto,
		selected: 0,
		styles:   NewConfirmStyles(),
	}
}

// SetSize 设置大小
func (m *ConfirmModel) SetSize(width, height int) {
	m.width = width
	m.height = height
}

// Init 初始化
func (m *ConfirmModel) Init() tea.Cmd {
	return nil
}

// Update 更新
func (m *ConfirmModel) Update(msg tea.Msg) (*ConfirmModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "1":
			m.selected = 0
			m.mode = ModeAuto
		case "2":
			m.selected = 1
			m.mode = ModeEdit
		case "up":
			if m.selected > 0 {
				m.selected--
			}
			m.mode = ConfirmMode(m.selected)
		case "down":
			if m.selected < 1 {
				m.selected++
			}
			m.mode = ConfirmMode(m.selected)
		case "enter":
			if m.onConfirm != nil {
				m.onConfirm(m.mode)
			}
			return m, func() tea.Msg {
				return ConfirmResultMsg{Mode: m.mode, Confirmed: true}
			}
		case "esc":
			if m.onCancel != nil {
				m.onCancel()
			}
			return m, func() tea.Msg {
				return ConfirmResultMsg{Mode: m.mode, Confirmed: false}
			}
		}
	}
	return m, nil
}

// View 渲染
func (m *ConfirmModel) View() string {
	if m.width == 0 {
		m.width = 80
	}

	var content strings.Builder

	// 标题
	content.WriteString(m.styles.Title.Render("⚠ Execution Plan Review"))
	content.WriteString("\n\n")

	// 计划内容
	planText := m.plan
	if len(planText) > m.width-10 {
		planText = planText[:m.width-13] + "..."
	}
	content.WriteString(m.styles.Plan.Render(planText))
	content.WriteString("\n\n")

	// 选项
	options := []struct {
		key  string
		name string
		desc string
	}{
		{"1", "Auto", "Execute all steps automatically"},
		{"2", "Edit", "Revise the plan before execution"},
	}

	for i, opt := range options {
		style := m.styles.Option
		if i == m.selected {
			style = m.styles.Selected
		}
		content.WriteString(style.Render(opt.key + ". " + opt.name))
		content.WriteString(" ")
		content.WriteString(m.styles.Description.Render(opt.desc))
		content.WriteString("\n")
	}

	content.WriteString("\n")
	content.WriteString(m.styles.Description.Render(
		"Enter: confirm | Esc: cancel | ↑/↓: select | 1/2: quick select"))

	return m.styles.Panel.Render(content.String())
}

// SetCallbacks 设置回调函数
func (m *ConfirmModel) SetCallbacks(onConfirm func(ConfirmMode), onCancel func()) {
	m.onConfirm = onConfirm
	m.onCancel = onCancel
}

// ConfirmResultMsg 确认结果消息
type ConfirmResultMsg struct {
	Mode      ConfirmMode
	Confirmed bool
}
