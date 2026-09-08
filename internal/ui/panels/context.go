package panels

// Copyright (c) 2026 EOSAIOS
// SPDX-License-Identifier: EOS-NCL-1.1
// 本文件基于 EOS 非商用许可证 v1.1 发布，详见 LICENSE。
// 商业使用请联系版权人获得商业授权。

import (
	"fmt"
	"strings"

	"github.com/eosaios/eos/internal/ai"
	"github.com/eosaios/eos/internal/i18n"
	"github.com/eosaios/eos/internal/ui/styles"

	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/mattn/go-runewidth"
)

// ContextPanel 上下文面板
type ContextPanel struct {
	BasePanel
	styles       *styles.Styles
	table        table.Model
	messages     []ContextMessage
	actionOps    []string
	actionIndex  int
	language     string
	viewing      bool // 是否正在查看详情
	detail       viewport.Model
	stats        ContextStats
	previewWidth int
}

type ContextStats struct {
	ModelName          string
	ContextWindow      int
	MaxPromptTokens    int
	PinnedTokens       int
	ConversationTokens int
}

// ContextMessage 上下文消息
type ContextMessage struct {
	Role    string
	Content string
	Tokens  int
}

// NewContextPanel 创建新的上下文面板
func NewContextPanel(styles *styles.Styles, lang string) *ContextPanel {
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

	// 设置表格的键位映射
	t.KeyMap.LineUp.SetKeys("up", "k")
	t.KeyMap.LineDown.SetKeys("down", "j")

	vp := viewport.New()
	vp.MouseWheelEnabled = true
	vp.MouseWheelDelta = 3

	panel := &ContextPanel{
		BasePanel:   NewBasePanel("context"),
		styles:      styles,
		table:       t,
		messages:    make([]ContextMessage, 0),
		actionOps:   []string{"Compact", "Clear", "Export", "View"},
		actionIndex: 0,
		language:    lang,
		viewing:     false,
		detail:      vp,
	}

	panel.updateTableColumns()
	panel.updateTable()

	return panel
}

func (p *ContextPanel) updateTableColumns() {
	contentWidth := p.innerWidth()
	roleWidth := 12
	tokensWidth := 10
	minPreviewWidth := 18
	previewWidth := contentWidth - roleWidth - tokensWidth - 4
	if previewWidth < minPreviewWidth {
		roleWidth = 10
		tokensWidth = 8
		previewWidth = contentWidth - roleWidth - tokensWidth - 4
	}
	if previewWidth < minPreviewWidth {
		previewWidth = minPreviewWidth
	}
	p.previewWidth = previewWidth
	columns := []table.Column{
		{Title: i18n.T("ctx.col.role", p.language), Width: roleWidth},
		{Title: i18n.T("ctx.col.preview", p.language), Width: previewWidth},
		{Title: i18n.T("ctx.col.tokens", p.language), Width: tokensWidth},
	}
	p.table.SetColumns(columns)
}

// SetMessages 设置消息列表
func (p *ContextPanel) SetMessages(messages []ContextMessage) {
	p.messages = messages
	p.updateTable()
	p.syncLayout()
}

func (p *ContextPanel) ResetView() {
	p.viewing = false
}

func (p *ContextPanel) IsViewing() bool {
	return p.viewing
}

func (p *ContextPanel) SetStats(modelName string, maxPromptTokens int, pinnedTokens int, conversationTokens int) {
	p.stats.ModelName = strings.TrimSpace(modelName)
	p.stats.ContextWindow = ai.ContextWindowTokens(p.stats.ModelName)
	p.stats.MaxPromptTokens = maxPromptTokens
	p.stats.PinnedTokens = pinnedTokens
	p.stats.ConversationTokens = conversationTokens
	p.syncLayout()
}

// updateTable 更新表格内容
func (p *ContextPanel) updateTable() {
	rows := make([]table.Row, 0)

	if len(p.messages) == 0 {
		rows = append(rows, table.Row{i18n.T("ctx.empty", p.language), "", ""})
	} else {
		for _, msg := range p.messages {
			preview := msg.Content
			runes := []rune(preview)
			if len(runes) > 37 {
				preview = string(runes[:37]) + "..."
			}
			preview = strings.ReplaceAll(preview, "\n", " ")
			rows = append(rows, table.Row{msg.Role, preview, fmt.Sprintf("%d", msg.Tokens)})
		}
	}
	p.table.SetRows(rows)
}

// GetCurrentAction 获取当前选中的操作
func (p *ContextPanel) GetCurrentAction() string {
	if p.actionIndex >= 0 && p.actionIndex < len(p.actionOps) {
		return p.actionOps[p.actionIndex]
	}
	return ""
}

// GetSelectedMessage 获取选中的消息
func (p *ContextPanel) GetSelectedMessage() *ContextMessage {
	i := p.table.Cursor()
	if i >= 0 && i < len(p.messages) {
		return &p.messages[i]
	}
	return nil
}

// Init 初始化
func (p *ContextPanel) Init() tea.Cmd {
	return nil
}

// Update 更新
func (p *ContextPanel) Update(msg tea.Msg) (Panel, tea.Cmd) {
	switch msg := msg.(type) {
	case LanguageChangeMsg:
		p.language = msg.Language
		p.updateTableColumns()
		p.updateTable()
		p.syncLayout()
		return p, nil

	case tea.KeyMsg:
		if p.viewing {
			if msg.String() == "esc" {
				p.viewing = false
				return p, nil
			}
			var cmd tea.Cmd
			p.detail, cmd = p.detail.Update(msg)
			return p, cmd
		}

		var cmd tea.Cmd
		p.table, cmd = p.table.Update(msg)
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
			case "Compact":
				return p, func() tea.Msg {
					return ContextCompactMsg{}
				}
			case "Clear":
				return p, func() tea.Msg {
					return ContextClearMsg{}
				}
			case "Export":
				return p, func() tea.Msg {
					return ContextExportMsg{}
				}
			case "View":
				// 进入查看详情模式
				if m := p.GetSelectedMessage(); m != nil {
					p.viewing = true
					contentStr := strings.TrimRight(m.Content, "\n")
					if strings.TrimSpace(contentStr) == "" {
						contentStr = "(empty)"
					}
					p.detail.SetContent(contentStr)
					p.detail.GotoTop()
				}
				return p, nil
			}
		case "c":
			// 直接执行压缩操作
			return p, func() tea.Msg {
				return ContextCompactMsg{}
			}
		case "x":
			// 直接执行清空操作
			return p, func() tea.Msg {
				return ContextClearMsg{}
			}
		case "e":
			// 直接执行导出操作
			return p, func() tea.Msg {
				return ContextExportMsg{}
			}
		case "v":
			// 直接执行查看操作
			if m := p.GetSelectedMessage(); m != nil {
				p.viewing = true
				contentStr := strings.TrimRight(m.Content, "\n")
				if strings.TrimSpace(contentStr) == "" {
					contentStr = "(empty)"
				}
				p.detail.SetContent(contentStr)
				p.detail.GotoTop()
			}
			return p, nil
		}
		return p, cmd
	case tea.MouseMsg:
		if p.viewing {
			var cmd tea.Cmd
			p.detail, cmd = p.detail.Update(msg)
			return p, cmd
		}
		var cmd tea.Cmd
		p.table, cmd = p.table.Update(msg)
		return p, cmd
	}
	var cmd tea.Cmd
	p.table, cmd = p.table.Update(msg)
	return p, cmd
}

// View 渲染
func (p *ContextPanel) View() string {
	p.syncLayout()

	// 如果在查看详情模式，显示详情
	if p.viewing {
		return p.viewDetail()
	}

	var content strings.Builder

	if p.stats.MaxPromptTokens > 0 {
		model := p.stats.ModelName
		if model == "" {
			model = "(none)"
		}
		pinnedPct := 0
		if p.stats.MaxPromptTokens > 0 {
			pinnedPct = p.stats.PinnedTokens * 100 / p.stats.MaxPromptTokens
		}
		line := fmt.Sprintf("Model: %s  Win: %d  Prompt: %d  Pinned: %d (%d%%)  Conv: %d",
			model, p.stats.ContextWindow, p.stats.MaxPromptTokens, p.stats.PinnedTokens, pinnedPct, p.stats.ConversationTokens)
		content.WriteString(p.styles.TextMuted.Render(truncateForWidth(line, p.innerWidth())))
		content.WriteString("\n")
	}

	content.WriteString(p.table.View())
	content.WriteString("\n")

	// 显示当前选中的消息信息
	selected := p.GetSelectedMessage()
	if selected != nil {
		selectedText := fmt.Sprintf("%s %s · Tokens: %d",
			i18n.T("ctx.selected", p.language),
			selected.Role,
			selected.Tokens)
		content.WriteString(renderFixedWidthLine(p.styles.TextMuted.Render(selectedText), p.innerWidth()))
		content.WriteString("\n")
	}

	// 显示操作列表
	var opStrs []string
	for i, op := range p.actionOps {
		key := ""
		switch op {
		case "Compact":
			key = "ctx.action.compact"
		case "Clear":
			key = "ctx.action.clear"
		case "Export":
			key = "ctx.action.export"
		case "View":
			key = "ctx.action.view"
		}
		text := i18n.T(key, p.language)
		if i == p.actionIndex {
			opStrs = append(opStrs, p.styles.TextSuccess.Render("["+text+"]"))
		} else {
			opStrs = append(opStrs, p.styles.TextMuted.Render(text))
		}
	}
	content.WriteString(renderFixedWidthLine(fmt.Sprintf("%s %s",
		i18n.T("models.action", p.language),
		strings.Join(opStrs, "  ")), p.innerWidth()))
	content.WriteString("\n")

	content.WriteString(p.styles.TextMuted.Render(truncateForWidth(i18n.T("ctx.help", p.language), p.innerWidth())))

	return p.RenderBorder(content.String(), i18n.T("context.manager.title", p.language))
}

// viewDetail 显示消息详情
func (p *ContextPanel) viewDetail() string {
	selected := p.GetSelectedMessage()
	if selected == nil {
		p.viewing = false
		return p.View()
	}

	var content strings.Builder

	content.WriteString(p.styles.PanelTitle.Render(i18n.T("ctx.detail.title", p.language)))
	content.WriteString("\n\n")

	// 消息元信息
	fmt.Fprintf(&content, "%s: %s\n", i18n.T("ctx.col.role", p.language), p.styles.TextInfo.Render(selected.Role))
	fmt.Fprintf(&content, "%s: %d\n", i18n.T("ctx.col.tokens", p.language), selected.Tokens)
	content.WriteString("\n")

	// 消息内容
	content.WriteString(p.detail.View())
	content.WriteString("\n\n")

	content.WriteString(p.styles.TextMuted.Render(i18n.T("ctx.detail.help", p.language)))

	return p.RenderBorder(content.String(), i18n.T("ctx.detail.title", p.language))
}

// SetSize 设置大小
func (p *ContextPanel) SetSize(width, height int) {
	p.BasePanel.SetSize(width, height)
	p.syncLayout()
}

func (p *ContextPanel) syncLayout() {
	w := p.innerWidth()
	h := p.innerHeight()
	if h < 3 {
		h = 3
	}

	reservedLines := 3 // selected + actions + help
	if p.stats.MaxPromptTokens > 0 {
		reservedLines++
	}
	if p.GetSelectedMessage() != nil {
		reservedLines++
	}
	tableHeight := h - reservedLines
	if tableHeight < 3 {
		tableHeight = 3
	}

	p.table.SetWidth(w)
	p.table.SetHeight(tableHeight)
	p.detail.SetWidth(w)
	p.detail.SetHeight(h)
	p.updateTableColumns()
}

func (p *ContextPanel) innerWidth() int {
	w := p.width - 6
	if w < 24 {
		return 24
	}
	return w
}

func (p *ContextPanel) innerHeight() int {
	h := p.height - 8
	if h < 3 {
		return 3
	}
	return h
}

func renderFixedWidthLine(line string, width int) string {
	if width < 1 {
		return line
	}
	return lipgloss.NewStyle().Width(width).Render(line)
}

func truncateForWidth(s string, width int) string {
	if width <= 0 {
		return ""
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	s = strings.TrimSpace(strings.Join(strings.Fields(s), " "))
	if s == "" {
		return ""
	}
	if runewidth.StringWidth(s) <= width {
		return s
	}
	if width == 1 {
		return "…"
	}
	var b strings.Builder
	cur := 0
	for _, r := range s {
		rw := runewidth.RuneWidth(r)
		if rw == 0 {
			rw = 1
		}
		if cur+rw > width-1 {
			break
		}
		b.WriteRune(r)
		cur += rw
	}
	out := strings.TrimSpace(b.String())
	if out == "" {
		return "…"
	}
	return out + "…"
}

// ContextCompactMsg 压缩上下文消息
type ContextCompactMsg struct{}

// ContextClearMsg 清空上下文消息
type ContextClearMsg struct{}

// ContextExportMsg 导出上下文消息
type ContextExportMsg struct{}
