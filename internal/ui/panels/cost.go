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

// CostPanel 成本面板
type CostPanel struct {
	BasePanel
	styles      *styles.Styles
	table       table.Model
	stats       []CostStats
	totalStats  TotalStats
	actionOps   []string
	actionIndex int
	language    string
}

// CostStats 按模型的成本统计
type CostStats struct {
	Model  string
	Rounds int
	Input  string
	Reply  string
	Total  string
}

// TotalStats 总统计
type TotalStats struct {
	TotalRounds   int
	TotalInput    string
	TotalReply    string
	TotalTokens   string
	TotalDuration int // ms
}

// NewCostPanel 创建新的成本面板
func NewCostPanel(styles *styles.Styles, lang string) *CostPanel {
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
		table.WithHeight(15),
		table.WithStyles(s),
		table.WithFocused(true),
	)

	// 设置表格的键位映射
	t.KeyMap.LineUp.SetKeys("up", "k")
	t.KeyMap.LineDown.SetKeys("down", "j")

	panel := &CostPanel{
		BasePanel:   NewBasePanel("cost"),
		styles:      styles,
		table:       t,
		stats:       make([]CostStats, 0),
		actionOps:   []string{"Clear", "Export", "Refresh"},
		actionIndex: 0,
		language:    lang,
	}

	panel.updateTableColumns()
	panel.updateTable()

	return panel
}

func (p *CostPanel) updateTableColumns() {
	columns := []table.Column{
		{Title: i18n.T("cost.col.model", p.language), Width: 30},
		{Title: i18n.T("cost.col.rounds", p.language), Width: 8},
		{Title: i18n.T("cost.col.input", p.language), Width: 12},
		{Title: i18n.T("cost.col.reply", p.language), Width: 12},
		{Title: i18n.T("cost.col.total", p.language), Width: 12},
	}
	p.table.SetColumns(columns)
}

// SetStats 设置统计信息
func (p *CostPanel) SetStats(stats []CostStats, total TotalStats) {
	p.stats = stats
	p.totalStats = total
	p.updateTable()
}

// updateTable 更新表格内容
func (p *CostPanel) updateTable() {
	rows := make([]table.Row, 0)

	if len(p.stats) == 0 {
		rows = append(rows, table.Row{i18n.T("cost.empty", p.language), "", "", "", ""})
	} else {
		for _, s := range p.stats {
			rows = append(rows, table.Row{
				s.Model,
				fmt.Sprintf("%d", s.Rounds),
				s.Input,
				s.Reply,
				s.Total,
			})
		}
	}
	p.table.SetRows(rows)
}

// GetCurrentAction 获取当前选中的操作
func (p *CostPanel) GetCurrentAction() string {
	if p.actionIndex >= 0 && p.actionIndex < len(p.actionOps) {
		return p.actionOps[p.actionIndex]
	}
	return ""
}

// Init 初始化
func (p *CostPanel) Init() tea.Cmd {
	return nil
}

// Update 更新
func (p *CostPanel) Update(msg tea.Msg) (Panel, tea.Cmd) {
	var cmd tea.Cmd
	p.table, cmd = p.table.Update(msg)

	switch msg := msg.(type) {
	case LanguageChangeMsg:
		p.language = msg.Language
		p.updateTableColumns()
		p.updateTable()
		return p, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "left", "h":
			// 向左切换操作
			if p.actionIndex > 0 {
				p.actionIndex--
			} else {
				p.actionIndex = len(p.actionOps) - 1
			}
			return p, nil
		case "right", "l":
			// 向右切换操作
			if p.actionIndex < len(p.actionOps)-1 {
				p.actionIndex++
			} else {
				p.actionIndex = 0
			}
			return p, nil
		case "enter":
			// 执行当前选中的操作
			action := p.GetCurrentAction()
			switch action {
			case "Clear":
				return p, func() tea.Msg {
					return CostClearMsg{}
				}
			case "Export":
				return p, func() tea.Msg {
					return CostExportMsg{}
				}
			case "Refresh":
				return p, func() tea.Msg {
					return CostRefreshMsg{}
				}
			}
		case "c":
			// 直接执行清除操作
			return p, func() tea.Msg {
				return CostClearMsg{}
			}
		case "e":
			// 直接执行导出操作
			return p, func() tea.Msg {
				return CostExportMsg{}
			}
		case "r":
			// 直接执行刷新操作
			return p, func() tea.Msg {
				return CostRefreshMsg{}
			}
		}
	}

	return p, cmd
}

// View 渲染
func (p *CostPanel) View() string {
	var content strings.Builder

	content.WriteString(p.styles.PanelTitle.Render(i18n.T("cost.list.title", p.language)))
	content.WriteString("\n\n")

	// 总计信息
	content.WriteString(p.styles.TextInfo.Render(i18n.T("cost.summary.title", p.language) + ":"))
	content.WriteString("\n")
	fmt.Fprintf(&content, "  %s: %d\n",
		i18n.T("cost.summary.rounds", p.language), p.totalStats.TotalRounds)
	fmt.Fprintf(&content, "  %s: %s\n",
		i18n.T("cost.summary.input", p.language), p.totalStats.TotalInput)
	fmt.Fprintf(&content, "  %s: %s\n",
		i18n.T("cost.summary.reply", p.language), p.totalStats.TotalReply)
	fmt.Fprintf(&content, "  %s: %s",
		i18n.T("cost.summary.total", p.language), p.totalStats.TotalTokens)
	if p.totalStats.TotalDuration > 0 && p.totalStats.TotalRounds > 0 {
		fmt.Fprintf(&content, "\n  %s: %dms",
			i18n.T("cost.summary.avg_duration", p.language),
			p.totalStats.TotalDuration/p.totalStats.TotalRounds)
	}
	content.WriteString("\n\n")

	// 模型详情表格
	content.WriteString(p.styles.TextInfo.Render(i18n.T("cost.manager.title", p.language) + ":"))
	content.WriteString("\n")
	content.WriteString(p.table.View())
	content.WriteString("\n\n")

	// 显示操作列表
	var opStrs []string
	for i, op := range p.actionOps {
		key := ""
		switch op {
		case "Clear":
			key = "cost.action.clear"
		case "Export":
			key = "cost.action.export"
		case "Refresh":
			key = "cost.action.refresh"
		}
		text := i18n.T(key, p.language)
		if i == p.actionIndex {
			opStrs = append(opStrs, p.styles.TextSuccess.Render("["+text+"]"))
		} else {
			opStrs = append(opStrs, p.styles.TextMuted.Render(text))
		}
	}
	fmt.Fprintf(&content, "%s %s\n\n",
		i18n.T("models.action", p.language),
		strings.Join(opStrs, "  "))

	content.WriteString(p.styles.TextMuted.Render(i18n.T("cost.help", p.language)))

	return p.RenderBorder(content.String(), i18n.T("cost.manager.title", p.language))
}

// SetSize 设置大小
func (p *CostPanel) SetSize(width, height int) {
	p.BasePanel.SetSize(width, height)
	p.table.SetWidth(width - 4)
	p.table.SetHeight(height - 16)
}

// CostClearMsg 清除统计消息
type CostClearMsg struct{}

// CostExportMsg 导出统计消息
type CostExportMsg struct{}

// CostRefreshMsg 刷新统计消息
type CostRefreshMsg struct{}
