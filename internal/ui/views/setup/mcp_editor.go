package setup

// Copyright (c) 2026 EOSAIOS
// SPDX-License-Identifier: EOS-NCL-1.1
// 本文件基于 EOS 非商用许可证 v1.1 发布，详见 LICENSE。
// 商业使用请联系版权人获得商业授权。

import (
	"strings"

	"github.com/eosaios/eos/internal/i18n"
	uinput "github.com/eosaios/eos/internal/ui/components/input"
	"github.com/eosaios/eos/internal/ui/styles"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type MCPConfigEditorView struct {
	width        int
	height       int
	styles       *styles.Styles
	language     string
	textarea     uinput.Model
	edit         bool
	originalName string
}

type MCPConfigSubmitMsg struct {
	Text         string
	Edit         bool
	OriginalName string
}

type MCPConfigCancelMsg struct{}

func NewMCPConfigEditorView(styles *styles.Styles, lang string, initial string, edit bool, originalName string) *MCPConfigEditorView {
	ta := uinput.New()
	ta.SetPlaceholder(i18n.T("mcp.editor.placeholder", lang))
	ta.SetValue(initial)
	ta.Focus()

	return &MCPConfigEditorView{
		styles:       styles,
		language:     lang,
		textarea:     ta,
		edit:         edit,
		originalName: originalName,
	}
}

func (v *MCPConfigEditorView) SetSize(width, height int) {
	v.width = width
	v.height = height
	v.textarea.SetSize(width-6, height-10)
}

func (v *MCPConfigEditorView) Init() tea.Cmd {
	return v.textarea.Init()
}

func (v *MCPConfigEditorView) Update(msg tea.Msg) (*MCPConfigEditorView, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			return v, func() tea.Msg { return MCPConfigCancelMsg{} }
		case "ctrl+s":
			text := v.textarea.Value()
			v.textarea.AddToHistory(text)
			return v, func() tea.Msg {
				return MCPConfigSubmitMsg{
					Text:         text,
					Edit:         v.edit,
					OriginalName: v.originalName,
				}
			}
		}
	}

	var cmd tea.Cmd
	v.textarea, cmd = v.textarea.Update(msg)
	return v, cmd
}

func (v *MCPConfigEditorView) View() string {
	if v.width == 0 {
		v.width = 80
	}
	if v.height == 0 {
		v.height = 24
	}

	title := i18n.T("mcp.editor.title.add", v.language)
	if v.edit {
		title = i18n.T("mcp.editor.title.edit", v.language)
	}
	titleStyle := v.styles.PanelTitle.
		Width(v.width - 4).
		Align(lipgloss.Center)

	var content strings.Builder
	content.WriteString(titleStyle.Render(title))
	content.WriteString("\n\n")
	content.WriteString(v.styles.Text.Render(i18n.T("mcp.editor.desc", v.language)))
	content.WriteString("\n\n")
	content.WriteString(v.textarea.View())
	content.WriteString("\n\n")
	content.WriteString(v.styles.TextMuted.Render(i18n.T("mcp.editor.help", v.language)))

	return content.String()
}
