package panels

// Copyright (c) 2026 EOSAIOS
// SPDX-License-Identifier: EOS-NCL-1.1
// 本文件基于 EOS 非商用许可证 v1.1 发布，详见 LICENSE。
// 商业使用请联系版权人获得商业授权。

import (
	"strings"

	"charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// LanguageChangeMsg 语言切换消息
type LanguageChangeMsg struct {
	Language string
}

// Panel 是面板的通用接口
type Panel interface {
	Init() tea.Cmd
	Update(tea.Msg) (Panel, tea.Cmd)
	View() string
	SetSize(width, height int)
	GetName() string
	IsActive() bool
	SetActive(active bool)
}

// BasePanel 是所有面板的基类
type BasePanel struct {
	name   string
	width  int
	height int
	active bool
	style  lipgloss.Style
}

// NewBasePanel 创建新的基础面板
func NewBasePanel(name string) BasePanel {
	return BasePanel{
		name: name,
		style: lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			Padding(1, 2),
	}
}

// GetName 返回面板名称
func (p *BasePanel) GetName() string {
	return p.name
}

// SetSize 设置面板大小
func (p *BasePanel) SetSize(width, height int) {
	p.width = width
	p.height = height
}

// IsActive 返回面板是否处于活动状态
func (p *BasePanel) IsActive() bool {
	return p.active
}

// SetActive 设置面板活动状态
func (p *BasePanel) SetActive(active bool) {
	p.active = active
}

// GetSize 返回面板尺寸
func (p *BasePanel) GetSize() (int, int) {
	return p.width, p.height
}

// RenderBorder 渲染带标题的边框
func (p *BasePanel) RenderBorder(content string, title string) string {
	var result strings.Builder

	// 渲染标题
	if title != "" {
		titleStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#6366f1")).
			Bold(true)
		if p.width > 0 {
			titleStyle = titleStyle.Width(p.width)
		}
		result.WriteString(titleStyle.Render(title))
		result.WriteString("\n")
	}

	// 渲染内容
	h := p.height
	if title != "" && h > 0 {
		h--
	}
	if h < 0 {
		h = 0
	}
	style := p.style.Width(p.width).Height(h)
	result.WriteString(style.Render(content))

	return result.String()
}
