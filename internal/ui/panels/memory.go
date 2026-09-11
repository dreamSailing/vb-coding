package panels

// 记忆面板：只读查看 ~/.eos/memories 的两个核心文件（memory_summary.md +
// MEMORY.md，数据来自内核 memory/snapshot），并提供「添加记忆笔记」入口
//（走内核 memory/save，语义 = 写一条 ad_hoc note）。交互为只读设计：
// 面板不编辑记忆文件本身，生成/合并由内核写管线负责。

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/eosaios/eos/internal/i18n"
	"github.com/eosaios/eos/internal/ui/components/input"
	"github.com/eosaios/eos/internal/ui/styles"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
)

// MemoryRefreshMsg 请求刷新记忆快照。
type MemoryRefreshMsg struct{}

// MemorySaveMsg 携带一条 ad_hoc 记忆笔记内容与作用域（内核 memory/save 落盘；
// Scope 为 "project" 时落当前项目分区，默认落全局分区）。
type MemorySaveMsg struct {
	Content string
	Scope   string
}

// MemoryProject 是当前活动项目分区记忆的投影（内核 snapshot.projects[0]）。
// 为 nil 表示当前没有项目工作区，面板只展示全局分区。
type MemoryProject struct {
	Key  string
	Root string
	Name string
	Docs [2]MemoryDoc
}

// MemoryDoc 是面板侧的记忆文档投影；Scope 固定为 memory_summary.md / MEMORY.md。
type MemoryDoc struct {
	Scope   string
	Path    string
	Content string
	Exists  bool
}

// memoryDocScopes 是面板展示的文档顺序（与内核 panel_snapshot 一致）。
var memoryDocScopes = [2]string{"memory_summary.md", "MEMORY.md"}

type MemoryPanel struct {
	BasePanel
	styles   *styles.Styles
	language string

	docs [2]MemoryDoc
	tab  int

	// project 为当前项目分区（可空）；scope: 0=全局 1=项目。
	project *MemoryProject
	scope   int

	view      viewport.Model
	composing bool
	editor    input.Model
}

func NewMemoryPanel(styles *styles.Styles, lang string) *MemoryPanel {
	vp := viewport.New()
	vp.MouseWheelEnabled = true
	vp.MouseWheelDelta = 3
	ed := input.New()
	p := &MemoryPanel{
		BasePanel: NewBasePanel("memory"),
		styles:    styles,
		language:  lang,
		view:      vp,
		editor:    ed,
	}
	p.updateEditorPlaceholder()
	p.updateViewContent()
	return p
}

func (p *MemoryPanel) Init() tea.Cmd { return nil }

func (p *MemoryPanel) IsEditing() bool { return p != nil && p.composing }

func (p *MemoryPanel) CancelEdit() {
	if p == nil {
		return
	}
	p.composing = false
	p.editor.Blur()
	p.updateViewContent()
}

// SetData 注入快照数据；docs 为全局分区（顺序固定 [memory_summary.md,
// MEMORY.md]，缺失文档以 Exists=false 呈现空态），project 为当前项目分区
// （可空）。
func (p *MemoryPanel) SetData(docs []MemoryDoc, project *MemoryProject) {
	for i := range p.docs {
		p.docs[i] = MemoryDoc{Scope: memoryDocScopes[i]}
	}
	for i, doc := range docs {
		if i < len(p.docs) {
			p.docs[i] = doc
		}
	}
	p.project = project
	if p.scope > 1 || (p.scope == 1 && project == nil) {
		p.scope = 0
	}
	p.updateViewContent()
}

// SelectProjectScope 切到项目作用域（无项目分区时为 no-op）。供 /memory project 直达。
func (p *MemoryPanel) SelectProjectScope() {
	if p == nil || p.project == nil {
		return
	}
	if p.scope != 1 {
		p.scope = 1
		p.tab = 0
		p.updateViewContent()
	}
}

// currentDocs 返回当前作用域下的文档。
func (p *MemoryPanel) currentDocs() *[2]MemoryDoc {
	if p.scope == 1 && p.project != nil {
		return &p.project.Docs
	}
	return &p.docs
}

func (p *MemoryPanel) currentDoc() MemoryDoc {
	docs := p.currentDocs()
	if p.tab < 0 || p.tab >= len(docs) {
		return docs[0]
	}
	return docs[p.tab]
}

func (p *MemoryPanel) updateViewContent() {
	if p == nil {
		return
	}
	doc := p.currentDoc()
	body := strings.TrimRight(doc.Content, "\n")
	if !doc.Exists || strings.TrimSpace(body) == "" {
		// 空态：记忆由内核写管线在会话提炼后生成，尚未生成时提示而非报错。
		body = i18n.T("memory.empty", p.language)
	}
	p.view.SetContent(body)
}

func (p *MemoryPanel) enterCompose() {
	if p == nil {
		return
	}
	p.updateEditorPlaceholder()
	p.composing = true
	p.editor.SetValue("")
	p.editor.Focus()
}

func (p *MemoryPanel) updateEditorPlaceholder() {
	p.editor.SetPlaceholder(i18n.T("memory.note.placeholder", p.language))
}

func (p *MemoryPanel) Update(msg tea.Msg) (Panel, tea.Cmd) {
	switch msg := msg.(type) {
	case LanguageChangeMsg:
		p.language = msg.Language
		p.updateEditorPlaceholder()
		p.updateViewContent()
		return p, nil
	case tea.KeyMsg:
		if p.composing {
			switch msg.String() {
			case "ctrl+s":
				text := p.editor.Value()
				p.editor.AddToHistory(text)
				p.composing = false
				p.editor.Blur()
				p.updateViewContent()
				scope := "global"
				if p.scope == 1 && p.project != nil {
					scope = "project"
				}
				return p, func() tea.Msg { return MemorySaveMsg{Content: text, Scope: scope} }
			}
			var cmd tea.Cmd
			p.editor, cmd = p.editor.Update(msg)
			return p, cmd
		}

		switch msg.String() {
		case "tab", "right", "l":
			// currentDocs 返回 *[2]MemoryDoc，len 需解引用取实际数组长度
			p.tab = (p.tab + 1) % len(*p.currentDocs())
			p.updateViewContent()
			return p, nil
		case "shift+tab", "left", "h":
			p.tab--
			if p.tab < 0 {
				p.tab = len(*p.currentDocs()) - 1
			}
			p.updateViewContent()
			return p, nil
		case "1":
			if p.scope != 0 {
				p.scope = 0
				p.tab = 0
				p.updateViewContent()
			}
			return p, nil
		case "2":
			if p.project != nil && p.scope != 1 {
				p.scope = 1
				p.tab = 0
				p.updateViewContent()
			}
			return p, nil
		case "a":
			p.enterCompose()
			return p, p.editor.Init()
		case "r":
			return p, func() tea.Msg { return MemoryRefreshMsg{} }
		}
		var cmd tea.Cmd
		p.view, cmd = p.view.Update(msg)
		return p, cmd
	case tea.MouseMsg:
		if p.composing {
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

func (p *MemoryPanel) View() string {
	var b strings.Builder
	b.WriteString(p.styles.PanelTitle.Render("Memory"))
	b.WriteString("\n\n")

	// 作用域行：全局 / 项目（项目仅在有活动工作区时出现）。
	var scopes []string
	globalLabel := i18n.T("memory.scope.global", p.language)
	if p.scope == 0 {
		scopes = append(scopes, p.styles.TextSuccess.Render("[1 "+globalLabel+"]"))
	} else {
		scopes = append(scopes, p.styles.TextMuted.Render("1 "+globalLabel))
	}
	if p.project != nil {
		projectLabel := fmt.Sprintf("%s: %s", i18n.T("memory.scope.project", p.language), p.project.Name)
		if p.scope == 1 {
			scopes = append(scopes, p.styles.TextSuccess.Render("[2 "+projectLabel+"]"))
		} else {
			scopes = append(scopes, p.styles.TextMuted.Render("2 "+projectLabel))
		}
	}
	b.WriteString(strings.Join(scopes, "  "))
	b.WriteString("\n\n")

	var tabs []string
	for i, label := range memoryDocScopes {
		if i == p.tab {
			tabs = append(tabs, p.styles.TextSuccess.Render("["+label+"]"))
		} else {
			tabs = append(tabs, p.styles.TextMuted.Render(label))
		}
	}
	b.WriteString(strings.Join(tabs, "  "))
	b.WriteString("\n\n")

	doc := p.currentDoc()
	status := "missing"
	if doc.Exists {
		status = "ok"
	}
	b.WriteString(p.styles.TextMuted.Render(filepath.ToSlash(doc.Path) + " (" + status + ")"))
	b.WriteString("\n\n")

	if p.composing {
		b.WriteString(p.editor.View())
		b.WriteString("\n\n")
		b.WriteString(p.styles.TextMuted.Render(i18n.T("memory.footer.editing", p.language)))
	} else {
		b.WriteString(p.view.View())
		b.WriteString("\n\n")
		b.WriteString(p.styles.TextMuted.Render(i18n.T("memory.footer.browse", p.language)))
	}
	return p.RenderBorder(b.String(), "Memory Panel")
}

func (p *MemoryPanel) SetSize(width, height int) {
	p.BasePanel.SetSize(width, height)
	w := width - 6
	h := height - 10
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
