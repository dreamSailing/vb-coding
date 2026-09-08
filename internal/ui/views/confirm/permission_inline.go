package confirm

import (
	"strings"

	"github.com/eosaios/eos/internal/i18n"
	"github.com/eosaios/eos/internal/ui/render"
	"github.com/eosaios/eos/internal/ui/styles"

	"charm.land/lipgloss/v2"
)

// RenderInlinePermission renders a lightweight permission prompt intended to
// appear above the shell status bar without taking over the full screen.
// diffTheme 是 diff chroma 高亮主题（空 = 默认 monokai）。
func RenderInlinePermission(s *styles.Styles, lang, diffTheme string, req Request, selected int, width int) string {
	if s == nil {
		return ""
	}

	// 自适应终端宽度，不再封顶 100 列，避免宽终端右侧留大段空白。
	panelW := width - 2
	if panelW < 32 {
		panelW = 32
	}

	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = i18n.T("permission.inline.title", lang)
	}

	panel := lipgloss.NewStyle().
		Width(panelW).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#f59e0b")).
		Background(s.Theme.Surface).
		Padding(0, 1)

	questionBox := lipgloss.NewStyle().
		Background(s.Theme.SurfaceAlt).
		Foreground(s.Theme.Text).
		Padding(0, 1).
		Width(max(panelW-6, 24))

	diffPathStyle := lipgloss.NewStyle().Foreground(s.Theme.Warning).Bold(true)
	diffStyle := lipgloss.NewStyle().
		Foreground(s.Theme.TextMuted).
		Background(s.Theme.SurfaceAlt).
		Padding(0, 1).
		Width(max(panelW-6, 24))

	optStyle := lipgloss.NewStyle().
		Foreground(s.Theme.Text).
		Padding(0, 1)
	selectedStyle := lipgloss.NewStyle().
		Foreground(s.Theme.Background).
		Background(s.Theme.Primary).
		Bold(true).
		Padding(0, 1)

	var body []string
	body = append(body, s.PanelTitle.Render(title))

	if q := strings.TrimSpace(req.Question); q != "" {
		body = append(body, questionBox.Render(q))
	}

	if strings.TrimSpace(req.Diff) != "" {
		if p := strings.TrimSpace(req.DiffPath); p != "" {
			body = append(body, diffPathStyle.Render(p))
		}
		// 先截断再高亮，避免把 ANSI 序列拦腰截断。
		body = append(body, diffStyle.Render(render.HighlightDiffANSI(truncateInlinePermissionDiff(req.Diff), diffTheme)))
	}

	for i, opt := range req.Options {
		label := permissionOptionLabel(lang, opt, i)
		style := optStyle
		if i == selected {
			style = selectedStyle
		}
		body = append(body, style.Render(label))
	}

	body = append(body, s.TextMuted.Render(i18n.T("permission.inline.help", lang)))

	return panel.Render(strings.Join(body, "\n"))
}

func permissionOptionLabel(lang, opt string, idx int) string {
	switch opt {
	case "accept":
		return strconvItoa(idx+1) + ". " + i18n.T("approval.accept", lang)
	case "acceptForSession":
		return strconvItoa(idx+1) + ". " + i18n.T("approval.acceptForSession", lang)
	case "decline":
		return strconvItoa(idx+1) + ". " + i18n.T("approval.decline", lang)
	case "cancel":
		return strconvItoa(idx+1) + ". " + i18n.T("approval.cancel", lang)
	default:
		return strconvItoa(idx+1) + ". " + opt
	}
}

func truncateInlinePermissionDiff(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	lines := strings.Split(raw, "\n")
	if len(lines) > 8 {
		lines = append(lines[:8], "...")
	}
	out := strings.Join(lines, "\n")
	runes := []rune(out)
	if len(runes) > 800 {
		return string(runes[:800]) + "..."
	}
	return out
}
