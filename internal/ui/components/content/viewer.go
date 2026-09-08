package content

// Copyright (c) 2026 EOSAIOS
// SPDX-License-Identifier: EOS-NCL-1.1
// 本文件基于 EOS 非商用许可证 v1.1 发布，详见 LICENSE。
// 商业使用请联系版权人获得商业授权。

import (
	"strings"

	"charm.land/bubbles/v2/viewport"
	"charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// Model 是内容查看器组件模型
type Model struct {
	viewport viewport.Model
	content  strings.Builder
	width    int
	height   int

	// 样式
	style lipgloss.Style
}

// New 创建新的内容查看器模型
func New(width, height int) Model {
	vp := viewport.New(viewport.WithWidth(width), viewport.WithHeight(height))
	vp.SetContent("")
	vp.MouseWheelEnabled = true
	vp.MouseWheelDelta = 3

	return Model{
		viewport: vp,
		width:    width,
		height:   height,
		style:    lipgloss.NewStyle(),
	}
}

// SetSize 设置大小
func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
	m.viewport.SetWidth(width)
	m.viewport.SetHeight(height)
}

// SetStyle 设置样式
func (m *Model) SetStyle(style lipgloss.Style) {
	m.style = style
}

// Append 追加内容
func (m *Model) Append(text string) {
	atBottom := m.viewport.AtBottom()
	m.content.WriteString(text)
	m.viewport.SetContent(m.content.String())
	if atBottom {
		m.viewport.GotoBottom()
	}
}

// AppendLine 追加一行
func (m *Model) AppendLine(line string) {
	atBottom := m.viewport.AtBottom()
	m.content.WriteString(line)
	m.content.WriteString("\n")
	m.viewport.SetContent(m.content.String())
	if atBottom {
		m.viewport.GotoBottom()
	}
}

// Clear 清空内容
func (m *Model) Clear() {
	m.content.Reset()
	m.viewport.SetContent("")
}

// SetContent 设置内容
func (m *Model) SetContent(text string) {
	m.content.Reset()
	m.content.WriteString(text)
	m.viewport.SetContent(text)
	m.viewport.GotoBottom()
}

func (m *Model) SetContentPreserveOffset(text string) {
	atBottom := m.viewport.AtBottom()
	old := m.viewport.YOffset()
	m.content.Reset()
	m.content.WriteString(text)
	m.viewport.SetContent(text)
	if atBottom {
		m.viewport.GotoBottom()
		return
	}
	lineCount := 1
	if text != "" {
		lineCount = strings.Count(text, "\n") + 1
	}
	maxOffset := 0
	if lineCount > m.viewport.Height() {
		maxOffset = lineCount - m.viewport.Height()
	}
	if old < 0 {
		old = 0
	}
	if old > maxOffset {
		old = maxOffset
	}
	m.viewport.SetYOffset(old)
}

// Content 获取内容
func (m *Model) Content() string {
	return m.content.String()
}

func (m *Model) YOffset() int {
	return m.viewport.YOffset()
}

func (m *Model) Height() int {
	return m.viewport.Height()
}

func (m *Model) LineCount() int {
	s := m.content.String()
	if s == "" {
		return 1
	}
	return strings.Count(s, "\n") + 1
}

// Init 初始化
func (m Model) Init() tea.Cmd {
	return nil
}

// Update 更新
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

// View 渲染视图
func (m Model) View() string {
	return m.style.Render(m.viewport.View())
}

// ScrollPercent 返回滚动百分比
func (m Model) ScrollPercent() float64 {
	return m.viewport.ScrollPercent()
}

// AtBottom 是否在底部
func (m Model) AtBottom() bool {
	return m.viewport.AtBottom()
}

// GotoBottom 滚动到底部
func (m *Model) GotoBottom() {
	m.viewport.GotoBottom()
}

// GotoTop 滚动到顶部
func (m *Model) GotoTop() {
	m.viewport.GotoTop()
}

// LineDown 向下滚动一行
func (m *Model) LineDown() {
	m.viewport.ScrollDown(1)
}

// LineUp 向上滚动一行
func (m *Model) LineUp() {
	m.viewport.ScrollUp(1)
}

// HalfViewDown 向下滚动半页
func (m *Model) HalfViewDown() {
	m.viewport.HalfPageDown()
}

// HalfViewUp 向上滚动半页
func (m *Model) HalfViewUp() {
	m.viewport.HalfPageUp()
}
