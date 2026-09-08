package shell

// Copyright (c) 2026 EOSAIOS
// SPDX-License-Identifier: EOS-NCL-1.1
// 本文件基于 EOS 非商用许可证 v1.1 发布，详见 LICENSE。
// 商业使用请联系版权人获得商业授权。

import (
	"math"
	"math/rand"
	"os"
	"strings"
	"time"

	"github.com/eosaios/eos/internal/i18n"
	"github.com/eosaios/eos/internal/ui/styles"
	"github.com/eosaios/eos/internal/update"
	"github.com/eosaios/eos/internal/version"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// WelcomeCard 欢迎卡片组件
type WelcomeCard struct {
	width      int
	height     int
	styles     *styles.Styles
	language   string
	modelName  string
	apiInfo    string
	workDir    string
	appVersion string
	updateInfo *update.CheckResult

	// 动画状态
	frame     int
	particles []particle
	shinePos  int
}

// particle 粒子结构
type particle struct {
	x      float64
	y      float64
	vx     float64
	vy     float64
	char   string
	bright float64
}

// NewWelcomeCard 创建新的欢迎卡片
func NewWelcomeCard(s *styles.Styles, lang string) *WelcomeCard {
	wd, _ := os.Getwd()
	w := &WelcomeCard{
		width:      80,
		height:     24,
		styles:     s,
		language:   lang,
		modelName:  "AI Assistant",
		apiInfo:    "Ready",
		workDir:    wd,
		appVersion: version.AppVersion,
	}
	w.initParticles()
	return w
}

// SetLanguage 设置欢迎卡片语言
func (w *WelcomeCard) SetLanguage(lang string) {
	w.language = lang
}

// initParticles 初始化粒子系统
func (w *WelcomeCard) initParticles() {
	w.particles = make([]particle, 0, 60)
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	starChars := []string{"·", "✦", "✧", "⋆", "∗"}

	for i := 0; i < 60; i++ {
		p := particle{
			x:      rng.Float64() * float64(w.width),
			y:      rng.Float64() * float64(w.height),
			vx:     (rng.Float64() - 0.5) * 0.2,
			vy:     (rng.Float64() - 0.5) * 0.1,
			char:   starChars[rng.Intn(len(starChars))],
			bright: 0.3 + rng.Float64()*0.7,
		}
		w.particles = append(w.particles, p)
	}
}

// Tick 动画帧更新
func (w *WelcomeCard) Tick() {
	w.frame++
	w.shinePos = (w.frame * 3) % (w.width + 60)

	for i := range w.particles {
		p := &w.particles[i]
		p.x += p.vx
		p.y += p.vy

		if p.x < 0 {
			p.x = 0
			p.vx = -p.vx
		}
		if p.x >= float64(w.width) {
			p.x = float64(w.width) - 1
			p.vx = -p.vx
		}
		if p.y < 0 {
			p.y = 0
			p.vy = -p.vy
		}
		if p.y >= float64(w.height) {
			p.y = float64(w.height) - 1
			p.vy = -p.vy
		}

		p.bright = 0.3 + 0.4*math.Sin(float64(w.frame)*0.04+float64(i)*0.8)
	}
}

// WelcomeTickMsg 动画帧消息
type WelcomeTickMsg struct{}

// WelcomeTickCmd 返回动画 tick 命令
func WelcomeTickCmd() tea.Cmd {
	return tea.Tick(80*time.Millisecond, func(time.Time) tea.Msg {
		return WelcomeTickMsg{}
	})
}

// SetSize 设置大小
func (w *WelcomeCard) SetSize(width, height int) {
	w.width = width
	w.height = height
}

// SetInfo 设置信息（空值不覆盖）
func (w *WelcomeCard) SetInfo(modelName, apiInfo, workDir string) {
	if modelName != "" {
		w.modelName = modelName
	}
	if apiInfo != "" {
		w.apiInfo = apiInfo
	}
	if workDir != "" {
		w.workDir = workDir
	}
}

// SetUpdateInfo 设置版本更新信息
func (w *WelcomeCard) SetUpdateInfo(info *update.CheckResult) {
	w.updateInfo = info
}

// View 渲染欢迎卡片
func (w *WelcomeCard) View() string {
	if w.width == 0 {
		w.width = 80
	}
	if w.height == 0 {
		w.height = 24
	}

	contentWidth := w.width - 2
	contentHeight := w.height - 2

	// Logo - 大号块字符 EOS
	logoLines := []string{
		"  ███████  ██████  ███████ ",
		"  ██       ██  ██  ██      ",
		"  █████    ██  ██  ███████ ",
		"  ██       ██  ██       ██ ",
		"  ███████  ██████  ███████ ",
	}

	// 计算布局
	logoStartY := (contentHeight - 12) / 2
	if logoStartY < 1 {
		logoStartY = 1
	}

	// 构建每一行
	var lines []string

	// 上方空白
	for i := 0; i < logoStartY; i++ {
		lines = append(lines, "")
	}

	// Logo 行
	lines = append(lines, logoLines...)

	// 副标题行
	lines = append(lines, "")
	lines = append(lines, i18n.T("welcome.tagline", w.language))
	lines = append(lines, version.AppName+" "+w.appVersion)

	// 中间空白（填充到倒数3行）
	for len(lines) < contentHeight-3 {
		lines = append(lines, "")
	}

	// 提示行
	lines = append(lines, i18n.T("welcome.tip", w.language))

	// 快捷键行
	lines = append(lines, i18n.T("welcome.hotkeys", w.language))

	// 补齐到 contentHeight
	for len(lines) < contentHeight {
		lines = append(lines, "")
	}

	// 渲染
	var builder strings.Builder
	for i := 0; i < contentHeight && i < len(lines); i++ {
		line := lines[i]
		if line == "" {
			// 空行：渲染粒子背景
			row := w.renderParticleRow(i, contentWidth)
			builder.WriteString(row)
		} else if i == logoStartY || (i >= logoStartY && i < logoStartY+len(logoLines)) {
			// Logo 行：居中并渲染
			logoIdx := i - logoStartY
			padded := padCenter(line, contentWidth)
			logoStr := w.renderLogoLine(padded, logoIdx)
			builder.WriteString(logoStr)
		} else if strings.Contains(line, "EOSAIOS") || strings.Contains(line, "EOS") {
			// 副标题
			padded := padCenter(line, contentWidth)
			builder.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#94a3b8")).Render(padded))
		} else if strings.HasPrefix(line, "●") {
			// 提示行
			padded := padCenter(line, contentWidth)
			rendered := w.renderTipLine(padded)
			builder.WriteString(rendered)
		} else if strings.HasPrefix(line, "tab") {
			// 快捷键行
			padded := padCenter(line, contentWidth)
			builder.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#475569")).Render(padded))
		} else {
			padded := padCenter(line, contentWidth)
			builder.WriteString(padded)
		}
		if i < contentHeight-1 {
			builder.WriteString("\n")
		}
	}

	return builder.String()
}

// renderLogoLine 渲染 Logo 单行（带炫光效果）
func (w *WelcomeCard) renderLogoLine(line string, lineIdx int) string {
	var builder strings.Builder
	for j, ch := range line {
		if ch == ' ' {
			builder.WriteString(string(ch))
			continue
		}

		// 炫光效果
		distToShine := math.Abs(float64(j-w.shinePos)) / 15.0
		if distToShine < 1.0 {
			builder.WriteString(lipgloss.NewStyle().
				Foreground(lipgloss.Color("#ffd700")).
				Bold(true).
				Render(string(ch)))
		} else {
			builder.WriteString(lipgloss.NewStyle().
				Foreground(lipgloss.Color("#f59e0b")).
				Bold(true).
				Render(string(ch)))
		}
	}
	return builder.String()
}

// renderTipLine 渲染提示行
func (w *WelcomeCard) renderTipLine(line string) string {
	var builder strings.Builder
	for _, ch := range line {
		switch ch {
		case '●':
			builder.WriteString(lipgloss.NewStyle().
				Foreground(lipgloss.Color("#6366f1")).
				Bold(true).
				Render(string(ch)))
		case ' ':
			builder.WriteString(" ")
		default:
			builder.WriteString(lipgloss.NewStyle().
				Foreground(lipgloss.Color("#64748b")).
				Render(string(ch)))
		}
	}
	return builder.String()
}

// renderParticleRow 渲染粒子行
func (w *WelcomeCard) renderParticleRow(row int, width int) string {
	// 创建行缓冲区
	rowChars := make([]rune, width)
	for i := range rowChars {
		rowChars[i] = ' '
	}

	// 绘制粒子
	for _, p := range w.particles {
		x := int(p.x)
		y := int(p.y)
		if y == row && x >= 0 && x < width {
			rowChars[x] = []rune(p.char)[0]
		}
	}

	// 渲染
	var builder strings.Builder
	for i, ch := range rowChars {
		if ch == ' ' {
			builder.WriteString(" ")
		} else {
			// 查找对应粒子获取亮度
			bright := 0.5
			for _, p := range w.particles {
				if int(p.x) == i && int(p.y) == row {
					bright = p.bright
					break
				}
			}
			var color string
			if bright > 0.7 {
				color = "#94a3b8"
			} else if bright > 0.4 {
				color = "#64748b"
			} else {
				color = "#334155"
			}
			builder.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(string(ch)))
		}
	}
	return builder.String()
}

// padCenter 居中填充
func padCenter(s string, width int) string {
	sLen := lipgloss.Width(s)
	if sLen >= width {
		return s
	}
	leftPad := (width - sLen) / 2
	rightPad := width - sLen - leftPad
	return strings.Repeat(" ", leftPad) + s + strings.Repeat(" ", rightPad)
}
