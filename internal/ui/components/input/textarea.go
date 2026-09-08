package input

// Copyright (c) 2026 EOSAIOS
// SPDX-License-Identifier: EOS-NCL-1.1
// 本文件基于 EOS 非商用许可证 v1.1 发布，详见 LICENSE。
// 商业使用请联系版权人获得商业授权。

import (
	"strings"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/mattn/go-runewidth"
)

// Model 是文本输入组件模型
type Model struct {
	textarea textarea.Model
	width    int
	height   int
	focused  bool
	maxLines int

	// 样式
	style      lipgloss.Style
	focusStyle lipgloss.Style

	// 历史
	history            []string
	historyIdx         int
	historyMax         int
	historyDraft       string
	historyDraftActive bool

	// 占位符
	basePlaceholder string
	prediction      string
}

// New 创建新的输入模型
func New() Model {
	ta := textarea.New()
	ta.Placeholder = "Type your message..."
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	ta.SetHeight(1)
	ta.Focus()

	return Model{
		textarea:   ta,
		height:     1,
		maxLines:   8,
		history:    make([]string, 0),
		historyMax: 100,
		focused:    true,
	}
}

// SetSize 设置大小
func (m *Model) SetSize(width, height int) {
	m.width = width
	if m.maxLines <= 0 {
		m.maxLines = 8
	}
	w := width - 2
	if w < 1 {
		w = 1
	}
	m.textarea.SetWidth(w)
	if height > 0 {
		m.height = height
		m.textarea.SetHeight(height)
		return
	}
	m.adjustHeight()
}

// SetStyle 设置样式
func (m *Model) SetStyle(style, focusStyle lipgloss.Style) {
	m.style = style
	m.focusStyle = focusStyle
}

// SetPlaceholder 设置占位符
func (m *Model) SetPlaceholder(text string) {
	m.basePlaceholder = text
	m.refreshPlaceholder()
}

func (m *Model) SetPrediction(text string) {
	m.prediction = strings.TrimSpace(text)
	m.refreshPlaceholder()
}

func (m *Model) ClearPrediction() {
	if m.prediction == "" {
		m.refreshPlaceholder()
		return
	}
	m.prediction = ""
	m.refreshPlaceholder()
}

func (m *Model) HasPrediction() bool {
	return m.prediction != ""
}

func (m *Model) Prediction() string {
	return m.prediction
}

func (m *Model) PredictionSuffix() string {
	value := m.textarea.Value()
	if strings.TrimSpace(value) == "" {
		return m.prediction
	}
	if m.prediction == "" {
		return ""
	}
	if !strings.HasPrefix(m.prediction, value) {
		return ""
	}
	return m.prediction[len(value):]
}

func (m *Model) CanAcceptPrediction() bool {
	return strings.TrimSpace(m.PredictionSuffix()) != ""
}

func (m *Model) AcceptPrediction() bool {
	suffix := strings.TrimSpace(m.PredictionSuffix())
	if suffix == "" {
		return false
	}
	if strings.TrimSpace(m.textarea.Value()) == "" {
		m.textarea.SetValue(m.prediction)
	} else {
		m.textarea.SetValue(m.textarea.Value() + m.PredictionSuffix())
	}
	m.prediction = ""
	m.refreshPlaceholder()
	m.adjustHeight()
	return true
}

// Focus 聚焦
func (m *Model) Focus() {
	m.focused = true
	m.textarea.Focus()
}

// Blur 失焦
func (m *Model) Blur() {
	m.focused = false
	m.textarea.Blur()
}

// Focused 是否聚焦
func (m *Model) Focused() bool {
	return m.focused
}

// Value 获取输入值
func (m *Model) Value() string {
	return m.textarea.Value()
}

// SetValue 设置输入值
func (m *Model) SetValue(text string) {
	m.textarea.SetValue(text)
	m.refreshPlaceholder()
}

// Clear 清空输入
func (m *Model) Clear() {
	m.textarea.SetValue("")
	m.prediction = ""
	m.historyDraft = ""
	m.historyDraftActive = false
	m.refreshPlaceholder()
	m.adjustHeight()
}

// InsertNewline 插入换行
func (m *Model) InsertNewline() {
	m.textarea.InsertString("\n")
	m.adjustHeight()
}

// HistoryUp 历史向上
func (m *Model) HistoryUp() {
	if len(m.history) == 0 {
		return
	}
	if m.historyIdx == len(m.history) {
		m.historyDraft = m.textarea.Value()
		m.historyDraftActive = true
	}
	if m.historyIdx > 0 {
		m.historyIdx--
		m.textarea.SetValue(m.history[m.historyIdx])
		m.adjustHeight()
	}
}

// HistoryDown 历史向下
func (m *Model) HistoryDown() {
	if len(m.history) == 0 {
		return
	}
	if m.historyIdx < len(m.history)-1 {
		m.historyIdx++
		m.textarea.SetValue(m.history[m.historyIdx])
		m.adjustHeight()
	} else {
		m.historyIdx = len(m.history)
		if m.historyDraftActive {
			m.textarea.SetValue(m.historyDraft)
			m.historyDraft = ""
			m.historyDraftActive = false
		} else {
			m.textarea.SetValue("")
		}
		m.adjustHeight()
	}
}

// AddToHistory 添加到历史
func (m *Model) AddToHistory(text string) {
	if text == "" {
		return
	}
	// 检查是否和最后一个重复
	if len(m.history) > 0 && m.history[len(m.history)-1] == text {
		return
	}
	m.history = append(m.history, text)
	if len(m.history) > m.historyMax {
		m.history = m.history[len(m.history)-m.historyMax:]
	}
	m.historyIdx = len(m.history)
	m.historyDraft = ""
	m.historyDraftActive = false
}

// GetHistory 获取历史
func (m *Model) GetHistory() []string {
	return m.history
}

// SetHistory 设置历史
func (m *Model) SetHistory(history []string) {
	m.history = history
	m.historyIdx = len(history)
	m.historyDraft = ""
	m.historyDraftActive = false
}

// Init 初始化
func (m Model) Init() tea.Cmd {
	return textarea.Blink
}

// Update 更新
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	var cmd tea.Cmd
	before := m.textarea.Value()
	m.textarea, cmd = m.textarea.Update(msg)
	after := m.textarea.Value()
	if m.prediction != "" && after != "" && !strings.HasPrefix(m.prediction, after) {
		m.prediction = ""
	}
	if before != after && after == "" && strings.TrimSpace(m.prediction) == "" {
		m.prediction = ""
	}
	m.refreshPlaceholder()
	m.adjustHeight()
	return m, cmd
}

func (m *Model) refreshPlaceholder() {
	if strings.TrimSpace(m.textarea.Value()) == "" && m.prediction != "" {
		m.textarea.Placeholder = m.prediction
		return
	}
	m.textarea.Placeholder = m.basePlaceholder
}

func (m *Model) adjustHeight() {
	w := m.width - 2
	if w < 1 {
		w = 1
	}
	lines := m.visualLines(w)
	if lines < 1 {
		lines = 1
	}
	max := m.maxLines
	if max < 1 {
		max = 1
	}
	if lines > max {
		lines = max
	}
	if lines != m.height {
		m.height = lines
		m.textarea.SetHeight(lines)
	}
}

func (m *Model) visualLines(w int) int {
	v := m.textarea.Value()
	if v == "" {
		return 1
	}
	total := 0
	for _, raw := range strings.Split(v, "\n") {
		if raw == "" {
			total++
			continue
		}
		width := runewidth.StringWidth(raw)
		if width <= w {
			total++
			continue
		}
		n := (width + w - 1) / w
		if n < 1 {
			n = 1
		}
		total += n
	}
	return total
}

func (m *Model) IsMultiLine() bool {
	w := m.width - 2
	if w < 1 {
		w = 1
	}
	return m.visualLines(w) > 1
}

func (m Model) ViewHeight() int {
	return lipgloss.Height(m.View())
}

// View 渲染视图
func (m Model) View() string {
	style := m.style
	if m.focused {
		style = m.focusStyle
	}
	view := m.textarea.View()
	if suffix := m.PredictionSuffix(); strings.TrimSpace(m.textarea.Value()) != "" && strings.TrimSpace(suffix) != "" {
		view += lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(suffix)
	}
	return style.Render(view)
}
