package panels

// Copyright (c) 2026 EOSAIOS
// SPDX-License-Identifier: EOS-NCL-1.1
// 本文件基于 EOS 非商用许可证 v1.1 发布，详见 LICENSE。
// 商业使用请联系版权人获得商业授权。

import (
	"path/filepath"
	"strings"

	"github.com/eosaios/eos/internal/i18n"
	"github.com/eosaios/eos/internal/ui/styles"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
)

// Workspace 工作区定义
type Workspace struct {
	Name string
	Path string
}

// WorkspaceItem 工作区列表项
type WorkspaceItem struct {
	workspace Workspace
	active    bool
}

func (w WorkspaceItem) FilterValue() string { return w.workspace.Name + " " + w.workspace.Path }
func (w WorkspaceItem) Title() string {
	if w.workspace.Name != "" {
		return w.workspace.Name
	}
	return filepath.Base(w.workspace.Path)
}
func (w WorkspaceItem) Description() string {
	if w.active {
		return "* " + w.workspace.Path
	}
	return w.workspace.Path
}

// WorkspacePanel 工作区面板
type WorkspacePanel struct {
	BasePanel
	styles     *styles.Styles
	language   string
	list       list.Model
	workspaces []Workspace
	activeIdx  int
}

// NewWorkspacePanel 创建新的工作区面板
func NewWorkspacePanel(styles *styles.Styles, lang string) *WorkspacePanel {
	items := make([]list.Item, 0)
	l := list.New(items, list.NewDefaultDelegate(), 60, 20)
	l.Title = ""
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowPagination(false)
	l.SetShowHelp(false)
	l.SetFilteringEnabled(false)

	return &WorkspacePanel{
		BasePanel:  NewBasePanel("workspace"),
		styles:     styles,
		language:   lang,
		list:       l,
		workspaces: make([]Workspace, 0),
		activeIdx:  -1,
	}
}

// SetWorkspaces 设置工作区列表
func (p *WorkspacePanel) SetWorkspaces(workspaces []Workspace, activePath string) {
	p.workspaces = workspaces
	items := make([]list.Item, len(workspaces))
	for i, ws := range workspaces {
		items[i] = WorkspaceItem{
			workspace: ws,
			active:    ws.Path == activePath,
		}
		if ws.Path == activePath {
			p.activeIdx = i
		}
	}
	p.list.SetItems(items)
}

// Init 初始化
func (p *WorkspacePanel) Init() tea.Cmd {
	return nil
}

// Update 更新
func (p *WorkspacePanel) Update(msg tea.Msg) (Panel, tea.Cmd) {
	var cmd tea.Cmd
	p.list, cmd = p.list.Update(msg)

	switch msg := msg.(type) {
	case LanguageChangeMsg:
		p.language = msg.Language
		return p, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "enter", "u":
			// 设置活跃工作区
			if item, ok := p.list.SelectedItem().(WorkspaceItem); ok {
				return p, func() tea.Msg {
					return WorkspaceSelectMsg{Path: item.workspace.Path}
				}
			}
		case "a":
			// 添加工作区
			return p, func() tea.Msg {
				return WorkspaceAddMsg{}
			}
		case "d":
			// 删除工作区
			if item, ok := p.list.SelectedItem().(WorkspaceItem); ok {
				return p, func() tea.Msg {
					return WorkspaceDeleteMsg{Path: item.workspace.Path}
				}
			}
		}
	}

	return p, cmd
}

// View 渲染
func (p *WorkspacePanel) View() string {
	var content strings.Builder

	content.WriteString(p.styles.PanelTitle.Render(i18n.T("workspace.title", p.language)))
	content.WriteString("\n\n")
	content.WriteString(p.list.View())
	content.WriteString("\n\n")
	content.WriteString(p.styles.TextMuted.Render(i18n.T("workspace.help", p.language)))

	return p.RenderBorder(content.String(), "Workspace Panel")
}

// SetSize 设置大小
func (p *WorkspacePanel) SetSize(width, height int) {
	p.BasePanel.SetSize(width, height)
	p.list.SetSize(width-6, height-10)
}

// WorkspaceSelectMsg 选择工作区消息
type WorkspaceSelectMsg struct {
	Path string
}

// WorkspaceAddMsg 添加工作区消息
type WorkspaceAddMsg struct{}

// WorkspaceDeleteMsg 删除工作区消息
type WorkspaceDeleteMsg struct {
	Path string
}
