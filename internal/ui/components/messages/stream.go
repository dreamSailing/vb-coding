package messages

// Copyright (c) 2026 EOSAIOS
// SPDX-License-Identifier: EOS-NCL-1.1
// 本文件基于 EOS 非商用许可证 v1.1 发布，详见 LICENSE。
// 商业使用请联系版权人获得商业授权。

// stream.go 提供文本流布局的渲染工具。
//
// 文本流布局：无边框、无背景填充、
// 无右对齐，仅以首行前缀 + 续行缩进表达一条消息，消息之间以空行分隔。
// 这比圆角气泡更贴合 CLI 的从上至下文本流视觉。

import (
	"fmt"
	"strings"
	"time"

	"github.com/eosaios/eos/internal/ui/styles"
)

// 文本流前缀。首行用语义前缀，续行统一两空格缩进。
const (
	streamContinueIndent = "  "
)

// prefixLines 给逻辑行的每一行加上首行/续行前缀。
// 第一行用 first，其余行用 rest。
func prefixLines(lines []string, first, rest string) []string {
	if len(lines) == 0 {
		return lines
	}
	out := make([]string, 0, len(lines))
	out = append(out, first+lines[0])
	for _, line := range lines[1:] {
		out = append(out, rest+line)
	}
	return out
}

// renderStreamLines 把已按宽度换行的逻辑行加上前缀，拼成单段文本。
// 空白行（纯空格）会被规整为空串，避免前缀污染空行。
func renderStreamLines(lines []string, firstPrefix, contPrefix string) string {
	prefixed := prefixLines(normalizeBlankLines(lines), firstPrefix, contPrefix)
	return strings.Join(prefixed, "\n")
}

// normalizeBlankLines 将纯空白行规整为空串，前缀只加在有内容的行上。
func normalizeBlankLines(lines []string) []string {
	out := make([]string, len(lines))
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			out[i] = ""
			continue
		}
		out[i] = line
	}
	return out
}

// renderUserStream 渲染用户输入文本流。
// 视觉：首行 "› "（粗体 dim），续行 "  " 缩进。
func renderUserStream(s *styles.Styles, content string, width int) string {
	wrapW := streamContentWidth(width)
	first := s.StreamUserPrefix.Render("› ") + " "
	rest := streamContinueIndent
	lines := wrapText(content, wrapW)
	return renderStreamLines(lines, first, rest)
}

// renderAIStream 渲染 AI 输出文本流。
// 视觉：首行 "• "（dim），续行 "  " 缩进。PreStyled 内容（已含 ANSI）直接按行切分换行。
func renderAIStream(s *styles.Styles, content string, preStyled bool, width int) string {
	wrapW := streamContentWidth(width)
	first := s.StreamAIPrefix.Render("• ") + " "
	rest := streamContinueIndent

	var lines []string
	if preStyled {
		lines = splitAndWrapANSI(content, wrapW)
	} else {
		lines = wrapText(content, wrapW)
	}
	return renderStreamLines(lines, first, rest)
}

// renderMetaLine 渲染完成态元信息行（tokens · duration），dim 风格。
func renderMetaLine(s *styles.Styles, tokens int, duration time.Duration) string {
	var parts []string
	if tokens > 0 {
		parts = append(parts, fmt.Sprintf("%d tokens", tokens))
	}
	if duration > 0 {
		parts = append(parts, fmt.Sprintf("%.1fs", duration.Seconds()))
	}
	if len(parts) == 0 {
		return ""
	}
	return s.StreamMeta.Render("  " + strings.Join(parts, " · "))
}

// renderAgentHeader 渲染子 Agent 文本流头部行：
// "● [event] source -> name <id> ts"
func renderAgentHeader(s *styles.Styles, event, sourceName, agentName, agentID string, ts time.Time) string {
	dot := renderAgentEventDot(s, event, false, false)
	eventLabel := renderAgentEventLabel(s, firstNonEmptyString(strings.TrimSpace(event), "result"))
	route := renderAgentRoute(s, sourceName, agentName)
	id := renderAgentID(s, agentID)

	parts := []string{dot, eventLabel, route}
	if id != "" {
		parts = append(parts, id)
	}
	if !ts.IsZero() {
		parts = append(parts, s.StreamMeta.Render(ts.Format("15:04:05")))
	}
	return strings.Join(parts, " ")
}

// renderAgentTaskStream 渲染子 Agent dispatch 文本流：头部行 + 任务文本缩进。
func renderAgentTaskStream(s *styles.Styles, event, sourceName, agentName, agentID, task string, ts time.Time, width int) string {
	var b strings.Builder
	b.WriteString(renderAgentHeader(s, event, sourceName, agentName, agentID, ts))

	taskText := strings.TrimSpace(task)
	if taskText != "" {
		wrapW := streamContentWidth(width)
		lines := wrapText(taskText, wrapW)
		b.WriteString("\n")
		b.WriteString(renderStreamLines(lines, streamContinueIndent, streamContinueIndent))
	}
	return b.String()
}

// renderAgentFinalStream 渲染子 Agent 最终输出文本流：
// 头部行 + 内容缩进 + 可选元信息行。
func renderAgentFinalStream(s *styles.Styles, event, sourceName, agentName, agentID string, content string, preStyled bool, tokens int, duration time.Duration, done bool, ts time.Time, width int) string {
	var b strings.Builder
	b.WriteString(renderAgentHeader(s, event, sourceName, agentName, agentID, ts))

	if content != "" {
		wrapW := streamContentWidth(width)
		var lines []string
		if preStyled {
			lines = splitAndWrapANSI(content, wrapW)
		} else {
			lines = wrapText(content, wrapW)
		}
		b.WriteString("\n")
		b.WriteString(renderStreamLines(lines, streamContinueIndent, streamContinueIndent))
	}

	if done {
		if meta := renderMetaLine(s, tokens, duration); meta != "" {
			b.WriteString("\n")
			b.WriteString(meta)
		}
	}
	return b.String()
}

// streamContentWidth 计算文本流内容的可用换行宽度。
// 文本流每行有 2 列前缀/缩进，需预留；极窄终端下退化为可用宽度。
func streamContentWidth(width int) int {
	w := bubbleWidth(width) - 2
	if w < 10 {
		w = width - 2
	}
	if w < 1 {
		w = 1
	}
	return w
}

// toolDetailBlock 是工具调用的一行明细（参数键值或结果摘要）。
type toolDetailBlock struct {
	label string
	value string
	kind  string // "command", "path", "pattern", "meta"
}

// renderToolStream 渲染工具调用文本流（bullet + 状态标题 + 缩进输出的视觉）：
//
//	首行：状态圆点 + 工具名(加粗) + 调用摘要(inline)，running 时附 spinner 文案
//	明细：参数键值缩进于 "  └ " 树前缀下
//	结果：输出缩进于 "  └ " 树前缀下，dim 风格
//	尾行：状态时长（success/error）
//
// 无边框、无背景填充，与 user/ai 文本流保持一致。
func renderToolStream(s *styles.Styles, name string, params map[string]any, status, result string, duration time.Duration, width int) string {
	w := streamContentWidth(width)
	var b strings.Builder

	// 状态圆点 + 标题词
	var dot, title string
	switch status {
	case "running":
		dot = s.StreamToolRunning.Render("●")
		title = "Running"
	case "success":
		dot = s.StreamToolSuccess.Render("●")
		title = "Ran"
	case "error", "canceled":
		dot = s.StreamToolErrorDot.Render("●")
		if status == "canceled" {
			title = "Canceled"
		} else {
			title = "Failed"
		}
	default:
		dot = s.StreamToolRunning.Render("●")
		title = "Tool"
	}

	toolName := s.StreamToolName.Render(displayToolName(name))

	// 首行：圆点 标题 工具名  <调用摘要>
	summary := toolInvocationSummary(name, params)
	if summary != "" {
		summary = truncateInline(summary, w-4)
	}
	headFirst := dot + " " + title + " " + toolName
	if summary != "" {
		headFirst += " " + s.StreamToolDetail.Render(summary)
	}
	b.WriteString(headFirst)

	// 明细块（参数键值）
	blocks := buildToolDetailBlocks(name, params)
	detailPrefix := s.StreamToolDetail.Render("  └ ")
	contPrefix := "    "
	hasDetail := len(blocks) > 0
	if hasDetail {
		for _, blk := range blocks {
			val := strings.TrimSpace(blk.value)
			if blk.kind == "command" && !strings.Contains(val, "\n") && val != "" {
				val = "$ " + val
			}
			val = truncateBlockValue(val, 800)
			labelLine := s.StreamToolDetail.Render(blk.label + ":")
			lines := wrapText(val, w-4)
			if len(lines) == 0 {
				lines = []string{""}
			}
			b.WriteString("\n")
			b.WriteString(detailPrefix + labelLine + " " + lines[0])
			for _, ln := range lines[1:] {
				b.WriteString("\n")
				b.WriteString(contPrefix + "  " + ln)
			}
		}
	}

	// 结果输出
	if out := strings.TrimSpace(result); out != "" {
		out = strings.ReplaceAll(out, "\r\n", "\n")
		out = strings.ReplaceAll(out, "\r", "")
		out = strings.ReplaceAll(out, "\t", "    ")
		limit := 1200
		if strings.ToLower(strings.TrimSpace(name)) == "bash" {
			limit = 200000
		}
		out = truncateBlockValue(out, limit)
		resLines := wrapText(out, w-4)
		b.WriteString("\n")
		b.WriteString(detailPrefix + strings.Join(resLines, "\n"+contPrefix))
	}

	// 状态尾行（完成态显示时长）
	if status == "success" || status == "error" || status == "canceled" {
		var tail string
		switch status {
		case "success":
			tail = s.StreamToolSuccess.Render(fmt.Sprintf("✓ %s", formatDuration(duration)))
		case "error":
			tail = s.StreamToolErrorDot.Render(fmt.Sprintf("✗ %s", formatDuration(duration)))
		case "canceled":
			tail = s.StreamToolErrorDot.Render("canceled")
		}
		if tail != "" {
			b.WriteString("\n")
			b.WriteString("  " + tail)
		}
	}

	return b.String()
}

// toolInvocationSummary 生成首行的调用摘要（如 path 或 command 的首行）。
func toolInvocationSummary(name string, params map[string]any) string {
	blocks := buildToolDetailBlocks(name, params)
	for _, b := range blocks {
		switch b.kind {
		case "command", "path", "pattern":
			s := strings.TrimSpace(b.value)
			if i := strings.IndexByte(s, '\n'); i >= 0 {
				s = s[:i]
			}
			return s
		}
	}
	return ""
}
