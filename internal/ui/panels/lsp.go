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
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type LSPRefreshMsg struct{}

type LSPPanel struct {
	BasePanel
	styles   *styles.Styles
	language string

	table   table.Model
	servers []LSPServerRow

	summary LSPPanelSummary
	viewing bool
	detail  viewport.Model
}

type LSPServerRow struct {
	Language string
	Command  string
	Found    bool
}

type LSPPanelSummary struct {
	Enabled          bool
	AutoDetect       bool
	ConfigFile       string
	Workspace        string
	DetectedLanguage string
	ActiveLanguage   string
	ActiveServer     string
	ActiveRoot       string
	Message          string
}

func NewLSPPanel(styles *styles.Styles, lang string) *LSPPanel {
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
	t.KeyMap.LineUp.SetKeys("up", "k")
	t.KeyMap.LineDown.SetKeys("down", "j")

	vp := viewport.New()
	vp.MouseWheelEnabled = true
	vp.MouseWheelDelta = 3

	p := &LSPPanel{
		BasePanel: NewBasePanel("lsp"),
		styles:    styles,
		language:  lang,
		table:     t,
		detail:    vp,
	}
	p.updateTableColumns()
	p.SetStatus(LSPPanelSummary{}, nil)
	return p
}

func (p *LSPPanel) updateTableColumns() {
	cols := []table.Column{
		{Title: "Language", Width: 16},
		{Title: "Server", Width: 70},
	}
	p.table.SetColumns(cols)
}

func (p *LSPPanel) updateTable() {
	rows := make([]table.Row, 0, len(p.servers))
	active := strings.TrimSpace(p.summary.ActiveLanguage)
	for _, it := range p.servers {
		name := it.Language
		if active != "" && strings.EqualFold(active, it.Language) {
			name = "* " + name
		}
		cmd := strings.TrimSpace(it.Command)
		if cmd == "" {
			if it.Found {
				cmd = "found"
			} else {
				cmd = "not found"
			}
		}
		rows = append(rows, table.Row{name, cmd})
	}
	if len(rows) == 0 {
		rows = append(rows, table.Row{"(empty)", ""})
	}
	p.table.SetRows(rows)
}

func (p *LSPPanel) SetStatus(summary LSPPanelSummary, servers []LSPServerRow) {
	p.summary = summary
	p.servers = servers
	p.updateTable()
}

func (p *LSPPanel) Init() tea.Cmd { return nil }

func (p *LSPPanel) Update(msg tea.Msg) (Panel, tea.Cmd) {
	switch msg := msg.(type) {
	case LanguageChangeMsg:
		p.language = msg.Language
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
		switch msg.String() {
		case "r":
			return p, func() tea.Msg { return LSPRefreshMsg{} }
		case "enter":
			i := p.table.Cursor()
			if i < 0 || i >= len(p.servers) {
				return p, nil
			}
			it := p.servers[i]
			var b strings.Builder
			b.WriteString("Language: " + it.Language + "\n")
			b.WriteString("Found: " + fmt.Sprintf("%v", it.Found) + "\n")
			b.WriteString("Command:\n")
			if strings.TrimSpace(it.Command) == "" {
				b.WriteString("(empty)\n")
			} else {
				b.WriteString(strings.TrimRight(it.Command, "\n"))
				b.WriteString("\n")
			}
			if strings.TrimSpace(p.summary.ActiveLanguage) != "" {
				b.WriteString("\nActive:\n")
				b.WriteString("- Language: " + strings.TrimSpace(p.summary.ActiveLanguage) + "\n")
				if strings.TrimSpace(p.summary.ActiveServer) != "" {
					b.WriteString("- Server: " + strings.TrimSpace(p.summary.ActiveServer) + "\n")
				}
				if strings.TrimSpace(p.summary.ActiveRoot) != "" {
					b.WriteString("- Root: " + strings.TrimSpace(p.summary.ActiveRoot) + "\n")
				}
			}
			p.viewing = true
			p.detail.SetContent(strings.TrimRight(b.String(), "\n"))
			p.detail.GotoTop()
			return p, nil
		}
		var cmd tea.Cmd
		p.table, cmd = p.table.Update(msg)
		return p, cmd
	case tea.MouseMsg:
		var cmd tea.Cmd
		if p.viewing {
			p.detail, cmd = p.detail.Update(msg)
			return p, cmd
		}
		p.table, cmd = p.table.Update(msg)
		return p, cmd
	}
	return p, nil
}

func (p *LSPPanel) View() string {
	var b strings.Builder
	b.WriteString(p.styles.PanelTitle.Render("LSP"))
	b.WriteString("\n\n")

	if strings.TrimSpace(p.summary.Message) != "" {
		b.WriteString(p.styles.TextMuted.Render("Note: " + strings.TrimSpace(p.summary.Message)))
		b.WriteString("\n\n")
	}
	b.WriteString(p.styles.TextMuted.Render(fmt.Sprintf("Config: enabled=%v auto_detect=%v", p.summary.Enabled, p.summary.AutoDetect)))
	b.WriteString("\n")
	if strings.TrimSpace(p.summary.ConfigFile) != "" {
		b.WriteString(p.styles.TextMuted.Render("Config file: " + strings.TrimSpace(p.summary.ConfigFile)))
		b.WriteString("\n")
	}
	if strings.TrimSpace(p.summary.Workspace) != "" {
		b.WriteString(p.styles.TextMuted.Render("Workspace: " + strings.TrimSpace(p.summary.Workspace)))
		b.WriteString("\n")
	}
	detected := strings.TrimSpace(p.summary.DetectedLanguage)
	if detected == "" {
		detected = "(unknown)"
	}
	b.WriteString(p.styles.TextMuted.Render("Detected: " + detected))
	b.WriteString("\n")
	active := strings.TrimSpace(p.summary.ActiveLanguage)
	if active == "" {
		active = "(not running)"
	}
	b.WriteString(p.styles.TextMuted.Render("Active: " + active))
	b.WriteString("\n\n")

	if p.viewing {
		b.WriteString(p.detail.View())
		b.WriteString("\n\n")
		b.WriteString(p.styles.TextMuted.Render(i18n.T("lsp.footer.detail", p.language)))
		return p.RenderBorder(b.String(), "LSP Panel")
	}

	b.WriteString(p.table.View())
	b.WriteString("\n\n")

	i := p.table.Cursor()
	if i >= 0 && i < len(p.servers) {
		it := p.servers[i]
		desc := it.Command
		if strings.TrimSpace(desc) == "" {
			if it.Found {
				desc = "found"
			} else {
				desc = "not found"
			}
		}
		b.WriteString(p.styles.TextMuted.Render("Selected: " + it.Language + " · " + desc))
		b.WriteString("\n\n")
	}

	b.WriteString(p.styles.TextMuted.Render(i18n.T("lsp.footer.list", p.language)))
	return p.RenderBorder(b.String(), "LSP Panel")
}

func (p *LSPPanel) SetSize(width, height int) {
	p.BasePanel.SetSize(width, height)
	w := width - 6
	h := height - 12
	if w < 0 {
		w = 0
	}
	if h < 0 {
		h = 0
	}
	p.table.SetWidth(w)
	p.table.SetHeight(h)
	p.detail.SetWidth(w)
	p.detail.SetHeight(h)
}
