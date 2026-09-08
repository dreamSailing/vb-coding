package ui

// selection.go — 鼠标框选复制（drag-to-select → copy to clipboard）。
//
// 纯逻辑（selectionCoord / wrapPlainLines / extractSelection）与 AppModel 的
// 鼠标事件接线（handleContentSelection）都放在本文件，职责单一。
//
// 坐标约定：
//   - 物理行（physical line）= 把内容按 contentWidth 换行后的行；
//   - selectionCoord.line 是全局物理行索引（含滚动偏移前的全部内容），
//     .col 是该行内的 rune 列（0 基）；
//   - 屏幕坐标到物理行的换算在 handleContentSelection 内完成。

import (
	"strings"

	"github.com/eosaios/eos/internal/i18n"

	"charm.land/bubbletea/v2"
	"github.com/atotto/clipboard"
	"github.com/mattn/go-runewidth"
)

// selDragThreshold 判定「点击 vs 拖选」的最小移动距离（单元：单元格）。
// 小于该距离视为点击（保留原有点击弹出复制），达到视为框选。
const selDragThreshold = 2

// selectionCoord 表示选中区域的一端。
type selectionCoord struct {
	line int // 物理行索引（0 基，全局）
	col  int // rune 列（0 基）
}

// normalizeSelection 返回 (start, end)，保证 start <= end（行优先排序）。
func normalizeSelection(a, b selectionCoord) (selectionCoord, selectionCoord) {
	if a.line < b.line || (a.line == b.line && a.col <= b.col) {
		return a, b
	}
	return b, a
}

// clampInt 把 v 限制在 [lo, hi]。
func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// wrapPlainLines 去掉 ANSI 转义后按 width 列宽换行，得到物理行列表。
// 空行保留为空字符串，保证行索引与视口渲染的行一一对应。
func wrapPlainLines(text string, width int) []string {
	if width < 1 {
		width = 1
	}
	raw := stripANSI(text)
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	raw = strings.ReplaceAll(raw, "\r", "\n")

	var out []string
	for _, logical := range strings.Split(raw, "\n") {
		if logical == "" {
			out = append(out, "")
			continue
		}
		line := []rune(logical)
		col := 0
		start := 0
		for i, r := range line {
			w := runewidth.RuneWidth(r)
			if w > width {
				// 单个字符超过列宽（罕见），按 1 列处理避免死循环。
				w = 1
			}
			if col+w > width {
				out = append(out, string(line[start:i]))
				start = i
				col = 0
			}
			col += w
		}
		out = append(out, string(line[start:]))
	}
	return out
}

// extractSelection 把选中区域映射回纯文本（去掉 ANSI）。
// start/end 已是全局物理行坐标；行索引越界自动裁剪到内容范围。
func extractSelection(text string, width int, start, end selectionCoord) string {
	lines := wrapPlainLines(text, width)
	if len(lines) == 0 {
		return ""
	}
	s, e := normalizeSelection(start, end)
	s.line = clampInt(s.line, 0, len(lines)-1)
	e.line = clampInt(e.line, 0, len(lines)-1)

	var b strings.Builder
	for l := s.line; l <= e.line; l++ {
		line := []rune(lines[l])
		if l == s.line && l == e.line {
			lo := clampInt(s.col, 0, len(line))
			hi := clampInt(e.col, 0, len(line))
			if lo > hi {
				lo, hi = hi, lo
			}
			b.WriteString(string(line[lo:hi]))
		} else if l == s.line {
			b.WriteString(string(line[clampInt(s.col, 0, len(line)):]))
		} else if l == e.line {
			b.WriteString(string(line[:clampInt(e.col, 0, len(line))]))
		} else {
			b.WriteString(lines[l])
		}
		if l != e.line {
			b.WriteString("\n")
		}
	}
	return b.String()
}

// handleContentSelection 处理内容区的鼠标框选/点击。返回 true 表示事件已被消耗，
// 不应再交给 shell/viewport 处理。
//
// 判定规则：
//   - 按下：在内容区（非滚动条列）记录锚点并消耗事件，等 release 区分点击/拖选；
//   - 拖动：移动超过 selDragThreshold 即进入框选并更新高亮；
//   - 释放：拖选→复制选中文本到剪贴板；点击→走原有 tryHandleBubbleActionAt
//     （AI 消息点击弹出复制/下载）。
//   - 滚轮（Wheel*）不拦截，交给 viewport 滚动。
func (m *AppModel) handleContentSelection(msg tea.MouseMsg) bool {
	if m.shell == nil {
		return false
	}
	ox, oy := m.shell.ContentOrigin()
	cw := m.shell.ContentWidth()
	ch := m.shell.ContentHeight()
	yOff := m.shell.ContentYOffset()

	mo := msg.Mouse()

	inArea := mo.X >= ox && mo.Y >= oy && mo.Y < oy+ch

	toCoord := func() selectionCoord {
		col := mo.X - ox
		if col < 0 {
			col = 0
		}
		if col >= cw-1 {
			col = cw - 1 // 最后一列是滚动条，clamp 到其左侧
		}
		return selectionCoord{line: yOff + (mo.Y - oy), col: col}
	}

	// v2：按具体鼠标事件类型分流；滚轮（MouseWheelMsg）不拦截，交给 viewport 滚动。
	switch msg.(type) {
	case tea.MouseClickMsg:
		if !inArea {
			return false
		}
		if mo.X-ox >= cw-1 {
			// 滚动条列：不框选，交给 viewport 处理拖动滚动。
			return false
		}
		coord := toCoord()
		m.selAnchor = &coord
		m.selStart = coord
		m.selEnd = coord
		m.selActive = false
		m.shell.ClearSelectionHighlight()
		return true

	case tea.MouseMotionMsg:
		if m.selAnchor == nil {
			return false
		}
		coord := toCoord()
		m.selEnd = coord
		if m.selDistance(m.selStart, coord) >= selDragThreshold {
			m.selActive = true
			m.applySelectionHighlight()
		}
		return true

	case tea.MouseReleaseMsg:
		if m.selAnchor == nil {
			return false
		}
		m.selAnchor = nil
		if !m.selActive {
			// 无拖动的点击：保留原有「点击消息文本弹出复制/下载」。
			m.shell.ClearSelectionHighlight()
			m.tryHandleBubbleActionAt(mo.X, mo.Y)
			return true
		}
		coord := toCoord()
		m.selEnd = coord
		m.copySelectedText()
		return true
	}
	return false
}

// selDistance 返回两个坐标间的最大单轴距离（切比雪夫距离）。
func (m *AppModel) selDistance(a, b selectionCoord) int {
	dl := a.line - b.line
	if dl < 0 {
		dl = -dl
	}
	dc := a.col - b.col
	if dc < 0 {
		dc = -dc
	}
	if dl > dc {
		return dl
	}
	return dc
}

// applySelectionHighlight 根据当前选中区域设置内容区高亮。
func (m *AppModel) applySelectionHighlight() {
	if m.shell == nil {
		return
	}
	start, end := normalizeSelection(m.selStart, m.selEnd)
	m.shell.SetSelectionHighlight(start.line, end.line)
}

// copySelectedText 把选中文本写入剪贴板。
func (m *AppModel) copySelectedText() {
	if m.shell == nil {
		return
	}
	text := extractSelection(m.shell.Content(), m.shell.ContentWidth(), m.selStart, m.selEnd)
	if strings.TrimSpace(text) == "" {
		m.shell.ClearSelectionHighlight()
		return
	}
	if err := clipboard.WriteAll(text); err != nil {
		m.appendSystem(i18n.T("tool.error.copy_error", m.state.Language, err), "error")
		return
	}
	m.appendSystem(i18n.T("clipboard.copied", m.state.Language), "success")
}

// clearSelection 清除所有框选状态与高亮。
func (m *AppModel) clearSelection() {
	m.selAnchor = nil
	m.selActive = false
	if m.shell != nil {
		m.shell.ClearSelectionHighlight()
	}
}
