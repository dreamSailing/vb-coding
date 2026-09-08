package render

// Copyright (c) 2026 EOSAIOS
// SPDX-License-Identifier: EOS-NCL-1.1
// 本文件基于 EOS 非商用许可证 v1.1 发布，详见 LICENSE。
// 商业使用请联系版权人获得商业授权。

import (
	"fmt"
	"strings"
	"sync"

	uistyles "github.com/eosaios/eos/internal/ui/styles"

	"github.com/charmbracelet/glamour"
	gansi "github.com/charmbracelet/glamour/ansi"
	glstyles "github.com/charmbracelet/glamour/styles"
	"charm.land/lipgloss/v2"
	"github.com/muesli/termenv"
)

// MarkdownRenderer Markdown渲染器
type MarkdownRenderer struct {
	width       int
	styles      *RenderStyles
	chromaTheme string // 代码块 chroma 主题（空 = DefaultChromaTheme）

	mu   sync.Mutex
	tr   *glamour.TermRenderer
	trWW int
}

// RenderStyles 渲染样式
type RenderStyles struct {
	Header      lipgloss.Style
	CodeBlock   lipgloss.Style
	InlineCode  lipgloss.Style
	Link        lipgloss.Style
	Bold        lipgloss.Style
	Italic      lipgloss.Style
	Strike      lipgloss.Style
	List        lipgloss.Style
	Quote       lipgloss.Style
	Table       lipgloss.Style
	TableHeader lipgloss.Style
	TableCell   lipgloss.Style
	Normal      lipgloss.Style
}

// NewRenderStyles 创建新的渲染样式
func NewRenderStyles() *RenderStyles {
	return &RenderStyles{
		Header: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#6366f1")),
		CodeBlock: lipgloss.NewStyle().
			Background(lipgloss.Color("#1e293b")).
			Foreground(lipgloss.Color("#f1f5f9")).
			Padding(1, 2),
		InlineCode: lipgloss.NewStyle().
			Background(lipgloss.Color("#334155")).
			Foreground(lipgloss.Color("#f1f5f9")),
		Link: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#3b82f6")).
			Underline(true),
		Bold: lipgloss.NewStyle().
			Bold(true),
		Italic: lipgloss.NewStyle().
			Italic(true),
		Strike: lipgloss.NewStyle().
			Strikethrough(true),
		List: lipgloss.NewStyle().
			MarginLeft(2),
		Quote: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#94a3b8")).
			BorderLeft(true).
			BorderForeground(lipgloss.Color("#64748b")).
			PaddingLeft(1),
		Table: lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()),
		TableHeader: lipgloss.NewStyle().
			Bold(true).
			Background(lipgloss.Color("#334155")),
		TableCell: lipgloss.NewStyle().
			Padding(0, 1),
		Normal: lipgloss.NewStyle(),
	}
}

func NewPlainRenderStyles() *RenderStyles {
	return &RenderStyles{
		Header:      lipgloss.NewStyle(),
		CodeBlock:   lipgloss.NewStyle().MarginLeft(2),
		InlineCode:  lipgloss.NewStyle(),
		Link:        lipgloss.NewStyle(),
		Bold:        lipgloss.NewStyle(),
		Italic:      lipgloss.NewStyle(),
		Strike:      lipgloss.NewStyle(),
		List:        lipgloss.NewStyle().MarginLeft(2),
		Quote:       lipgloss.NewStyle().MarginLeft(1),
		Table:       lipgloss.NewStyle(),
		TableHeader: lipgloss.NewStyle(),
		TableCell:   lipgloss.NewStyle(),
		Normal:      lipgloss.NewStyle(),
	}
}

func NewThemeRenderStyles(s *uistyles.Styles) *RenderStyles {
	if s == nil || s.Theme == nil {
		return NewPlainRenderStyles()
	}
	t := s.Theme
	headerStyle := s.MarkdownHeader
	linkStyle := s.MarkdownLink
	return &RenderStyles{
		Header: headerStyle,
		CodeBlock: lipgloss.NewStyle().
			Background(t.SurfaceAlt).
			BorderLeft(true).
			BorderForeground(t.Primary).
			Padding(0, 1).
			PaddingLeft(1),
		InlineCode: lipgloss.NewStyle().
			Background(t.SurfaceAlt).
			Foreground(t.Text),
		Link: linkStyle,
		Bold: lipgloss.NewStyle().Bold(true),
		Italic: lipgloss.NewStyle().
			Italic(true),
		Strike: lipgloss.NewStyle().Strikethrough(true),
		List: lipgloss.NewStyle().
			Foreground(t.Text),
		Quote: lipgloss.NewStyle().
			Foreground(t.TextMuted).
			BorderLeft(true).
			BorderForeground(t.Muted).
			PaddingLeft(1),
		Table:       lipgloss.NewStyle().Foreground(t.Text),
		TableHeader: lipgloss.NewStyle().Bold(true).Foreground(t.Text),
		TableCell:   lipgloss.NewStyle().Foreground(t.Text),
		Normal:      lipgloss.NewStyle().Foreground(t.Text),
	}
}

// NewMarkdownRenderer 创建新的Markdown渲染器
func NewMarkdownRenderer(width int) *MarkdownRenderer {
	r := &MarkdownRenderer{
		width:  width,
		styles: NewRenderStyles(),
	}
	r.rebuild()
	return r
}

// SetWidth 设置宽度
func (r *MarkdownRenderer) SetWidth(width int) {
	r.width = width
	r.rebuild()
}

func (r *MarkdownRenderer) SetStyles(styles *RenderStyles) {
	if styles != nil {
		r.styles = styles
	}
	r.rebuild()
}

// SetChromaTheme 设置代码块 chroma 主题并强制重建 TermRenderer
//（rebuild 只按宽度缓存，主题变更必须显式失效）。
func (r *MarkdownRenderer) SetChromaTheme(theme string) {
	r.chromaTheme = NormalizeChromaTheme(theme)
	r.mu.Lock()
	r.tr = nil
	r.mu.Unlock()
	r.rebuild()
}

// Render 渲染Markdown文本
func (r *MarkdownRenderer) Render(text string) string {
	if text == "" {
		return ""
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = unwrapMarkdownFences(text)

	r.mu.Lock()
	tr := r.tr
	r.mu.Unlock()
	if tr == nil {
		return strings.TrimRight(text, "\n")
	}
	out, err := tr.Render(text)
	if err != nil {
		return strings.TrimRight(text, "\n")
	}
	return strings.TrimRight(out, "\n")
}

// RenderStreaming applies a lightweight, line-by-line markdown styling that
// is cheap enough to run on every streaming delta. It deliberately avoids the
// full glamour AST pass (which can reflow/flicker on half-written fences or
// tables) and only styles constructs that are safe on partial input:
// headings, inline code, bold, italic, links, blockquotes and list markers.
// Fenced code blocks (``` / ~~~) are rendered verbatim and get the full
// glamour treatment once the segment completes (Render with done=true).
func (r *MarkdownRenderer) RenderStreaming(text string) string {
	if text == "" {
		return ""
	}
	styles := r.styles
	if styles == nil {
		styles = NewRenderStyles()
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	inFence := false
	for _, line := range lines {
		trim := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trim, "```") || strings.HasPrefix(trim, "~~~") {
			inFence = !inFence
			out = append(out, styles.CodeBlock.Render(line))
			continue
		}
		if inFence {
			out = append(out, styles.CodeBlock.Render(line))
			continue
		}
		out = append(out, renderStreamingLine(line, styles))
	}
	return strings.TrimRight(strings.Join(out, "\n"), "\n")
}

// renderStreamingLine styles one non-fenced markdown line.
func renderStreamingLine(line string, s *RenderStyles) string {
	if h, ok := matchHeading(line); ok {
		return s.Header.Render(h)
	}
	trim := strings.TrimLeft(line, " \t")
	if strings.HasPrefix(trim, ">") {
		rest := strings.TrimSpace(strings.TrimPrefix(trim, ">"))
		if rest == "" {
			return s.Quote.Render("")
		}
		return s.Quote.Render(renderInlineMarkdown(rest, s))
	}
	if len(trim) >= 2 && (trim[0] == '-' || trim[0] == '*' || trim[0] == '+') && trim[1] == ' ' {
		return s.List.Render(string(trim[0])) + " " + renderInlineMarkdown(trim[2:], s)
	}
	if body, ok := matchOrderedListItem(trim); ok {
		sep := strings.IndexAny(body, ".)")
		return s.List.Render(body[:sep+1]) + " " + renderInlineMarkdown(body[sep+1:], s)
	}
	return renderInlineMarkdown(line, s)
}

func matchHeading(line string) (string, bool) {
	trim := strings.TrimLeft(line, " \t")
	if !strings.HasPrefix(trim, "#") {
		return "", false
	}
	level := 0
	for level < len(trim) && trim[level] == '#' && level < 6 {
		level++
	}
	rest := trim[level:]
	if rest == "" {
		return "", false
	}
	if rest[0] != ' ' && rest[0] != '\t' {
		return "", false
	}
	return strings.TrimSpace(rest), true
}

func matchOrderedListItem(trim string) (string, bool) {
	i := 0
	for i < len(trim) && trim[i] >= '0' && trim[i] <= '9' {
		i++
	}
	if i == 0 || i >= len(trim) {
		return "", false
	}
	if trim[i] != '.' && trim[i] != ')' {
		return "", false
	}
	if i+1 >= len(trim) || trim[i+1] != ' ' {
		return "", false
	}
	return trim, true
}

func renderInlineMarkdown(text string, s *RenderStyles) string {
	if text == "" {
		return ""
	}
	text = applyPaired(text, "`", func(v string) string { return s.InlineCode.Render(v) })
	text = applyPaired(text, "**", func(v string) string { return s.Bold.Render(v) })
	text = applyPaired(text, "__", func(v string) string { return s.Bold.Render(v) })
	text = applyPaired(text, "*", func(v string) string { return s.Italic.Render(v) })
	text = applyPaired(text, "_", func(v string) string { return s.Italic.Render(v) })
	text = applyLinks(text, s)
	return text
}

func applyPaired(text, marker string, wrap func(string) string) string {
	if !strings.Contains(text, marker) {
		return text
	}
	var b strings.Builder
	rest := text
	for {
		idx := strings.Index(rest, marker)
		if idx < 0 {
			b.WriteString(rest)
			break
		}
		b.WriteString(rest[:idx])
		after := rest[idx+len(marker):]
		end := strings.Index(after, marker)
		if end < 0 {
			b.WriteString(marker)
			b.WriteString(after)
			break
		}
		b.WriteString(wrap(after[:end]))
		rest = after[end+len(marker):]
	}
	return b.String()
}

func applyLinks(text string, s *RenderStyles) string {
	for {
		open := strings.IndexByte(text, '[')
		if open < 0 {
			break
		}
		close := strings.IndexByte(text[open:], ']')
		if close < 0 {
			break
		}
		labelEnd := open + close
		if labelEnd+1 >= len(text) || text[labelEnd+1] != '(' {
			break
		}
		urlStart := labelEnd + 2
		urlEnd := strings.IndexByte(text[urlStart:], ')')
		if urlEnd < 0 {
			break
		}
		label := text[open+1 : labelEnd]
		url := text[urlStart : urlStart+urlEnd]
		rendered := s.Link.Render(label)
		_ = url
		text = text[:open] + rendered + text[urlStart+urlEnd+1:]
	}
	return text
}

func (r *MarkdownRenderer) rebuild() {
	ww := r.width
	if ww < 20 {
		ww = 20
	}

	profile := termenv.EnvColorProfile()
	chromaFmt := "terminal16"
	switch profile {
	case termenv.ANSI256:
		chromaFmt = "terminal256"
	case termenv.TrueColor:
		chromaFmt = "terminal16m"
	}

	cfg := glstyles.TokyoNightStyleConfig
	cfg.Heading.Prefix = ""
	cfg.H1.Prefix = ""
	cfg.H2.Prefix = ""
	cfg.H3.Prefix = ""
	cfg.H4.Prefix = ""
	cfg.H5.Prefix = ""
	cfg.H6.Prefix = ""
	cfg.CodeBlock.Theme = NormalizeChromaTheme(r.chromaTheme)
	codeBG := "#1e293b"
	codeFG := "#e5e7eb"
	indentToken := "│ "
	margin := uint(1)
	indent := uint(1)
	cfg.CodeBlock.BackgroundColor = &codeBG
	cfg.CodeBlock.Color = &codeFG
	cfg.CodeBlock.Margin = &margin
	cfg.CodeBlock.Indent = &indent
	cfg.CodeBlock.IndentToken = &indentToken
	_ = gansi.TemplateFuncMap

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.tr != nil && r.trWW == ww {
		return
	}
	tr, err := glamour.NewTermRenderer(
		glamour.WithStyles(cfg),
		glamour.WithWordWrap(ww),
		glamour.WithPreservedNewLines(),
		glamour.WithColorProfile(profile),
		glamour.WithChromaFormatter(chromaFmt),
	)
	if err != nil {
		r.tr = nil
		r.trWW = ww
		return
	}
	r.tr = tr
	r.trWW = ww
}

func unwrapMarkdownFences(text string) string {
	lines := strings.Split(text, "\n")
	var out []string
	in := false
	fence := ""
	for _, line := range lines {
		trim := strings.TrimLeft(line, " \t")
		if !in {
			if strings.HasPrefix(trim, "```") || strings.HasPrefix(trim, "~~~") {
				fence = "```"
				if strings.HasPrefix(trim, "~~~") {
					fence = "~~~"
				}
				lang := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(trim, fence)))
				if lang == "markdown" || lang == "md" {
					in = true
					continue
				}
			}
			out = append(out, line)
			continue
		}
		if strings.HasPrefix(trim, fence) {
			in = false
			fence = ""
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// RenderToolCall 渲染工具调用
func (r *MarkdownRenderer) RenderToolCall(name string, params map[string]any) string {
	var result strings.Builder
	result.WriteString("▶ ")
	result.WriteString(name)
	if len(params) > 0 {
		result.WriteString("(")
		var parts []string
		for k, v := range params {
			parts = append(parts, fmt.Sprintf("%s=%v", k, v))
		}
		result.WriteString(strings.Join(parts, ", "))
		result.WriteString(")")
	}
	return result.String()
}

// RenderToolResult 渲染工具结果
func (r *MarkdownRenderer) RenderToolResult(status, output string) string {
	icon := "✓"
	if status != "success" {
		icon = "✗"
	}
	return fmt.Sprintf("%s %s", icon, output)
}
