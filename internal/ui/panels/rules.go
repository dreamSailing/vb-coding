package panels

// Copyright (c) 2026 EOSAIOS
// SPDX-License-Identifier: EOS-NCL-1.1
// 本文件基于 EOS 非商用许可证 v1.1 发布，详见 LICENSE。
// 商业使用请联系版权人获得商业授权。

import (
	"path/filepath"
	"strings"

	"github.com/eosaios/eos/internal/i18n"
	uinput "github.com/eosaios/eos/internal/ui/components/input"
	"github.com/eosaios/eos/internal/ui/styles"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
)

type RulesRefreshMsg struct{}

type RulesSaveMsg struct {
	Scope   string
	Content string
}

type RulesPanel struct {
	BasePanel
	styles   *styles.Styles
	language string

	activeRoot string

	projectPath    string
	projectContent string
	projectExists  bool

	globalPath    string
	globalContent string
	globalExists  bool

	scope   int
	view    viewport.Model
	editing bool
	editor  uinput.Model
}

func NewRulesPanel(styles *styles.Styles, lang string) *RulesPanel {
	vp := viewport.New()
	vp.MouseWheelEnabled = true
	vp.MouseWheelDelta = 3
	ed := uinput.New()
	ed.SetPlaceholder("Edit EOS.md here...")
	p := &RulesPanel{
		BasePanel: NewBasePanel("rules"),
		styles:    styles,
		language:  lang,
		view:      vp,
		editor:    ed,
	}
	p.updateViewContent()
	return p
}

func (p *RulesPanel) Init() tea.Cmd { return nil }

func (p *RulesPanel) IsEditing() bool { return p != nil && p.editing }

func (p *RulesPanel) CancelEdit() {
	if p == nil {
		return
	}
	p.editing = false
	p.editor.Blur()
	p.updateViewContent()
}

func (p *RulesPanel) SetData(activeRoot string, projectPath string, projectContent string, projectExists bool, globalPath string, globalContent string, globalExists bool) {
	p.activeRoot = strings.TrimSpace(activeRoot)
	p.projectPath = strings.TrimSpace(projectPath)
	p.projectContent = projectContent
	p.projectExists = projectExists
	p.globalPath = strings.TrimSpace(globalPath)
	p.globalContent = globalContent
	p.globalExists = globalExists
	p.updateViewContent()
}

func (p *RulesPanel) scopeLabel() string {
	if p.scope == 1 {
		return "Global"
	}
	return "Project"
}

func (p *RulesPanel) scopePath() (string, bool) {
	if p.scope == 1 {
		return p.globalPath, p.globalExists
	}
	return p.projectPath, p.projectExists
}

func (p *RulesPanel) scopeContent() string {
	if p.scope == 1 {
		return p.globalContent
	}
	return p.projectContent
}

func (p *RulesPanel) setScopeContent(v string) {
	if p.scope == 1 {
		p.globalContent = v
		p.globalExists = true
		return
	}
	p.projectContent = v
	p.projectExists = true
}

func (p *RulesPanel) updateViewContent() {
	if p == nil {
		return
	}
	body := strings.TrimRight(p.scopeContent(), "\n")
	if strings.TrimSpace(body) == "" {
		path, exists := p.scopePath()
		if strings.TrimSpace(path) == "" {
			body = "(empty)"
		} else if !exists {
			body = "(file not found)"
		} else {
			body = "(empty)"
		}
	}
	p.view.SetContent(body)
}

func (p *RulesPanel) enterEdit() {
	if p == nil {
		return
	}
	p.editing = true
	p.editor.SetValue(p.scopeContent())
	p.editor.Focus()
}

func (p *RulesPanel) Update(msg tea.Msg) (Panel, tea.Cmd) {
	switch msg := msg.(type) {
	case LanguageChangeMsg:
		p.language = msg.Language
		return p, nil
	case tea.KeyMsg:
		if p.editing {
			switch msg.String() {
			case "ctrl+s":
				text := p.editor.Value()
				p.editor.AddToHistory(text)
				scope := "project"
				if p.scope == 1 {
					scope = "global"
				}
				p.setScopeContent(text)
				p.editing = false
				p.editor.Blur()
				p.updateViewContent()
				return p, func() tea.Msg { return RulesSaveMsg{Scope: scope, Content: text} }
			}
			var cmd tea.Cmd
			p.editor, cmd = p.editor.Update(msg)
			return p, cmd
		}

		switch msg.String() {
		case "tab", "right", "l":
			p.scope = (p.scope + 1) % 2
			p.updateViewContent()
			return p, nil
		case "shift+tab", "left", "h":
			p.scope--
			if p.scope < 0 {
				p.scope = 1
			}
			p.updateViewContent()
			return p, nil
		case "e":
			p.enterEdit()
			return p, p.editor.Init()
		case "r":
			return p, func() tea.Msg { return RulesRefreshMsg{} }
		}
		var cmd tea.Cmd
		p.view, cmd = p.view.Update(msg)
		return p, cmd
	case tea.MouseMsg:
		if p.editing {
			var cmd tea.Cmd
			p.editor, cmd = p.editor.Update(msg)
			return p, cmd
		}
		var cmd tea.Cmd
		p.view, cmd = p.view.Update(msg)
		return p, cmd
	}
	return p, nil
}

func (p *RulesPanel) View() string {
	var b strings.Builder
	b.WriteString(p.styles.PanelTitle.Render("Rules"))
	b.WriteString("\n\n")

	active := strings.TrimSpace(p.activeRoot)
	if active != "" {
		b.WriteString(p.styles.TextMuted.Render("Active workspace: " + filepath.ToSlash(active)))
		b.WriteString("\n")
	}

	path, exists := p.scopePath()
	scope := p.scopeLabel()
	status := "missing"
	if exists {
		status = "ok"
	}
	b.WriteString(p.styles.TextMuted.Render(scope + ": " + filepath.ToSlash(path) + " (" + status + ")"))
	b.WriteString("\n\n")

	if p.editing {
		b.WriteString(p.editor.View())
		b.WriteString("\n\n")
		b.WriteString(p.styles.TextMuted.Render(i18n.T("rules.footer.editing", p.language)))
	} else {
		b.WriteString(p.view.View())
		b.WriteString("\n\n")
		b.WriteString(p.styles.TextMuted.Render(i18n.T("rules.footer.browse", p.language)))
	}

	return p.RenderBorder(b.String(), "Rules Panel")
}

func (p *RulesPanel) SetSize(width, height int) {
	p.BasePanel.SetSize(width, height)
	w := width - 6
	h := height - 9
	if w < 0 {
		w = 0
	}
	if h < 0 {
		h = 0
	}
	p.view.SetWidth(w)
	p.view.SetHeight(h)
	p.editor.SetSize(w, h)
	p.updateViewContent()
}
