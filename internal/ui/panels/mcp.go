package panels

// Copyright (c) 2026 EOSAIOS
// SPDX-License-Identifier: EOS-NCL-1.1
// 本文件基于 EOS 非商用许可证 v1.1 发布，详见 LICENSE。
// 商业使用请联系版权人获得商业授权。

import (
	"fmt"
	"strings"

	"github.com/eosaios/eos/internal/i18n"
	"github.com/eosaios/eos/internal/ui/styles"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// MCPServer MCP服务器信息
type MCPServer struct {
	Name    string
	Type    string // stdio/sse
	Enabled bool
}

// MCPPanel MCP管理面板
type MCPPanel struct {
	BasePanel
	styles         *styles.Styles
	table          table.Model
	servers        []MCPServer
	language       string
	actionOps      []string
	actionIndex    int
	browserSummary BrowserSummary
}

type BrowserSummary struct {
	Running   bool
	Kind      string
	Version   string
	Profile   string
	LastError string
}

// NewMCPPanel 创建新的MCP面板
func NewMCPPanel(styles *styles.Styles, lang string) *MCPPanel {
	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("#6366f1")).
		BorderBottom(true).
		Bold(true)
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("#000000")).
		Background(lipgloss.Color("#6366f1")).
		Bold(true)

	t := table.New(
		table.WithHeight(20),
		table.WithStyles(s),
		table.WithFocused(true),
	)

	// 设置表格的键位映射，确保上下键可用
	t.KeyMap.LineUp.SetKeys("up", "k")
	t.KeyMap.LineDown.SetKeys("down", "j")

	panel := &MCPPanel{
		BasePanel:   NewBasePanel("mcp"),
		styles:      styles,
		table:       t,
		servers:     make([]MCPServer, 0),
		language:    lang,
		actionOps:   []string{"Toggle", "Browser", "Add", "Edit", "Delete", "Reload"},
		actionIndex: 0,
	}

	panel.updateTableColumns()
	return panel
}

func (p *MCPPanel) GetCurrentAction() string {
	if len(p.actionOps) > 0 && p.actionIndex >= 0 && p.actionIndex < len(p.actionOps) {
		return p.actionOps[p.actionIndex]
	}
	return ""
}

func (p *MCPPanel) updateTableColumns() {
	columns := []table.Column{
		{Title: i18n.T("mcp.col.name", p.language), Width: 25},
		{Title: i18n.T("mcp.col.type", p.language), Width: 10},
		{Title: i18n.T("mcp.col.status", p.language), Width: 10},
	}
	p.table.SetColumns(columns)
}

// SetServers 设置服务器列表
func (p *MCPPanel) SetServers(servers []MCPServer) {
	p.servers = servers
	rows := make([]table.Row, len(servers))
	for i, s := range servers {
		status := i18n.T("mcp.status.disabled", p.language)
		if s.Enabled {
			status = i18n.T("mcp.status.enabled", p.language)
		}
		// 翻译服务器类型
		serverType := s.Type
		switch s.Type {
		case "stdio":
			serverType = i18n.T("mcp.type.stdio", p.language)
		case "sse":
			serverType = i18n.T("mcp.type.sse", p.language)
		}
		rows[i] = table.Row{s.Name, serverType, status}
	}
	p.table.SetRows(rows)
}

func (p *MCPPanel) SetBrowserSummary(summary BrowserSummary) {
	p.browserSummary = summary
}

// Init 初始化
func (p *MCPPanel) Init() tea.Cmd {
	return nil
}

// Update 更新
func (p *MCPPanel) Update(msg tea.Msg) (Panel, tea.Cmd) {
	var cmd tea.Cmd
	p.table, cmd = p.table.Update(msg)

	switch msg := msg.(type) {
	case LanguageChangeMsg:
		p.language = msg.Language
		p.updateTableColumns()
		p.SetServers(p.servers) // 重新设置服务器列表以更新翻译
		return p, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "left", "h":
			if p.actionIndex > 0 {
				p.actionIndex--
			} else {
				p.actionIndex = len(p.actionOps) - 1
			}
			return p, nil
		case "right", "l":
			if p.actionIndex < len(p.actionOps)-1 {
				p.actionIndex++
			} else {
				p.actionIndex = 0
			}
			return p, nil
		case "enter":
			action := p.GetCurrentAction()
			switch action {
			case "Toggle":
				if i := p.table.Cursor(); i >= 0 && i < len(p.servers) {
					return p, func() tea.Msg {
						return MCPToggleMsg{Name: p.servers[i].Name}
					}
				}
			case "Add":
				return p, func() tea.Msg {
					return MCPAddMsg{}
				}
			case "Browser":
				return p, func() tea.Msg {
					return MCPAddBrowserMsg{}
				}
			case "Edit":
				if i := p.table.Cursor(); i >= 0 && i < len(p.servers) {
					return p, func() tea.Msg {
						return MCPEditMsg{Name: p.servers[i].Name}
					}
				}
			case "Delete":
				if i := p.table.Cursor(); i >= 0 && i < len(p.servers) {
					return p, func() tea.Msg {
						return MCPDeleteMsg{Name: p.servers[i].Name}
					}
				}
			case "Reload":
				return p, func() tea.Msg {
					return MCPSaveMsg{}
				}
			}
		case "a":
			// 添加服务器
			return p, func() tea.Msg {
				return MCPAddMsg{}
			}
		case "b":
			return p, func() tea.Msg {
				return MCPAddBrowserMsg{}
			}
		case "t", "space", " ":
			// 快捷切换启用/禁用
			if i := p.table.Cursor(); i >= 0 && i < len(p.servers) {
				return p, func() tea.Msg {
					return MCPToggleMsg{Name: p.servers[i].Name}
				}
			}
		case "e":
			// 编辑服务器
			if i := p.table.Cursor(); i >= 0 && i < len(p.servers) {
				return p, func() tea.Msg {
					return MCPEditMsg{Name: p.servers[i].Name}
				}
			}
		case "d":
			// 删除服务器
			if i := p.table.Cursor(); i >= 0 && i < len(p.servers) {
				return p, func() tea.Msg {
					return MCPDeleteMsg{Name: p.servers[i].Name}
				}
			}
		case "r":
			// 重载配置
			return p, func() tea.Msg {
				return MCPSaveMsg{}
			}
		}
	}

	return p, cmd
}

// View 渲染
func (p *MCPPanel) View() string {
	var content strings.Builder

	content.WriteString(p.styles.PanelTitle.Render(i18n.T("mcp.manager.title", p.language)))
	content.WriteString("\n\n")
	fmt.Fprintf(&content, "%s: %d %s\n\n",
		i18n.T("cmd.mcp", p.language),
		len(p.servers),
		i18n.T("mcp.header", p.language))

	if len(p.servers) == 0 {
		content.WriteString(i18n.T("mcp.empty", p.language))
		content.WriteString("\n\n")
	} else {
		content.WriteString(p.table.View())
		content.WriteString("\n\n")
	}

	statusLine := i18n.T("mcp.browser.idle", p.language)
	switch {
	case p.browserSummary.Running:
		kind := blankOr(p.browserSummary.Kind, "chrome")
		if p.browserSummary.Version != "" {
			kind = fmt.Sprintf("%s %s", kind, p.browserSummary.Version)
		}
		statusLine = fmt.Sprintf(i18n.T("mcp.browser.ready", p.language), kind)
	case p.browserSummary.LastError != "":
		statusLine = fmt.Sprintf(i18n.T("mcp.browser.error", p.language), p.browserSummary.LastError)
	}
	content.WriteString(statusLine)
	content.WriteString("\n\n")

	var opStrs []string
	for i, op := range p.actionOps {
		key := ""
		switch op {
		case "Toggle":
			key = "mcp.action.toggle"
		case "Add":
			key = "mcp.action.add"
		case "Browser":
			key = "mcp.action.browser"
		case "Edit":
			key = "mcp.action.edit"
		case "Delete":
			key = "mcp.action.delete"
		case "Reload":
			key = "mcp.action.reload"
		}
		text := i18n.T(key, p.language)
		if i == p.actionIndex {
			opStrs = append(opStrs, p.styles.TextSuccess.Render("["+text+"]"))
		} else {
			opStrs = append(opStrs, p.styles.TextMuted.Render(text))
		}
	}
	fmt.Fprintf(&content, "%s %s\n\n",
		i18n.T("mcp.action", p.language),
		strings.Join(opStrs, "  "))

	content.WriteString(p.styles.TextMuted.Render(
		i18n.T("mcp.help", p.language)))

	return p.RenderBorder(content.String(), "MCP Panel")
}

// SetSize 设置大小
func (p *MCPPanel) SetSize(width, height int) {
	p.BasePanel.SetSize(width, height)
	p.table.SetWidth(width - 4)
	p.table.SetHeight(height - 10)
}

// MCPToggleMsg 切换MCP服务器状态消息
type MCPToggleMsg struct {
	Name string
}

// MCPAddMsg 添加MCP服务器消息
type MCPAddMsg struct{}

// MCPAddBrowserMsg 添加浏览器 MCP 预设消息
type MCPAddBrowserMsg struct{}

// MCPEditMsg 编辑MCP服务器消息
type MCPEditMsg struct {
	Name string
}

// MCPDeleteMsg 删除MCP服务器消息
type MCPDeleteMsg struct {
	Name string
}

// MCPSaveMsg 保存MCP配置消息
type MCPSaveMsg struct{}

func blankOr(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}
