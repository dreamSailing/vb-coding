package features

// Copyright (c) 2026 EOSAIOS
// SPDX-License-Identifier: EOS-NCL-1.1
// 本文件基于 EOS 非商用许可证 v1.1 发布，详见 LICENSE。
// 商业使用请联系版权人获得商业授权。

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
)

// ThinkingManager 思考内容管理器
type ThinkingManager struct {
	content      strings.Builder
	isExpanded   bool
	isThinking   bool
	startTime    time.Time
	lastUpdate   time.Time
	width        int
	collapsedLen int // 折叠时显示的最大长度
}

// NewThinkingManager 创建新的思考内容管理器
func NewThinkingManager() *ThinkingManager {
	return &ThinkingManager{
		isExpanded:   false,
		isThinking:   false,
		collapsedLen: 100,
		width:        80,
	}
}

// StartThinking 开始思考
func (t *ThinkingManager) StartThinking() {
	t.isThinking = true
	t.isExpanded = true
	t.startTime = time.Now()
	t.lastUpdate = time.Now()
	t.content.Reset()
}

// StopThinking 停止思考
func (t *ThinkingManager) StopThinking() {
	t.isThinking = false
	t.lastUpdate = time.Now()
}

// AppendContent 追加思考内容
func (t *ThinkingManager) AppendContent(text string) {
	t.content.WriteString(text)
	t.lastUpdate = time.Now()
}

// SetContent 设置思考内容
func (t *ThinkingManager) SetContent(text string) {
	t.content.Reset()
	t.content.WriteString(text)
	t.lastUpdate = time.Now()
}

// ToggleExpand 切换展开/折叠
func (t *ThinkingManager) ToggleExpand() {
	t.isExpanded = !t.isExpanded
}

// IsExpanded 返回是否展开
func (t *ThinkingManager) IsExpanded() bool {
	return t.isExpanded
}

// IsThinking 返回是否正在思考
func (t *ThinkingManager) IsThinking() bool {
	return t.isThinking
}

// SetWidth 设置宽度
func (t *ThinkingManager) SetWidth(width int) {
	t.width = width
}

// GetDuration 获取思考持续时间
func (t *ThinkingManager) GetDuration() time.Duration {
	return time.Since(t.startTime)
}

// FormatDuration 格式化持续时间
func (t *ThinkingManager) FormatDuration() string {
	d := t.GetDuration()
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}

// Render 渲染思考内容
func (t *ThinkingManager) Render(style lipgloss.Style, infoStyle lipgloss.Style, mutedStyle lipgloss.Style) string {
	if t.content.Len() == 0 && !t.isThinking {
		return ""
	}

	var result strings.Builder

	// 头部信息
	header := "Thinking"
	if t.isThinking {
		header += " " + t.FormatDuration()
	}

	if t.isExpanded {
		header += " [-]"
	} else {
		header += " [+]"
	}

	result.WriteString(infoStyle.Render(header))
	result.WriteString("\n")

	// 内容
	content := t.content.String()
	if !t.isExpanded {
		if len(content) > t.collapsedLen {
			content = content[:t.collapsedLen] + "..."
		}
	}

	// 包装内容
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		result.WriteString(mutedStyle.Render(line))
		result.WriteString("\n")
	}

	return style.Render(result.String())
}

// Clear 清空思考内容
func (t *ThinkingManager) Clear() {
	t.content.Reset()
	t.isThinking = false
	t.isExpanded = false
}
