package styles

// Copyright (c) 2026 EOSAIOS
// SPDX-License-Identifier: EOS-NCL-1.1
// 本文件基于 EOS 非商用许可证 v1.1 发布，详见 LICENSE。
// 商业使用请联系版权人获得商业授权。

import "charm.land/lipgloss/v2"

// Styles 是应用程序中使用的所有样式的集合
type Styles struct {
	Theme *Theme // 主题引用

	// 全局样式
	App     lipgloss.Style
	Surface lipgloss.Style

	// 内容区域
	Content lipgloss.Style

	// 输入区域
	Input      lipgloss.Style
	InputFocus lipgloss.Style

	// 提示区域
	Hints lipgloss.Style

	// 状态栏
	StatusBar lipgloss.Style

	// 面板样式
	Panel        lipgloss.Style
	PanelTitle   lipgloss.Style
	PanelBody    lipgloss.Style
	ContentPanel lipgloss.Style // 对话内容区面板（纯黑背景，与终端默认背景一致）

	// 按钮样式
	Button      lipgloss.Style
	ButtonFocus lipgloss.Style

	// 表格样式
	Table       lipgloss.Style
	TableHeader lipgloss.Style
	TableCell   lipgloss.Style

	// 文本样式
	Text        lipgloss.Style
	TextMuted   lipgloss.Style
	TextSuccess lipgloss.Style
	TextError   lipgloss.Style
	TextWarning lipgloss.Style
	TextInfo    lipgloss.Style

	// 边框样式
	Border lipgloss.Border

	// Markdown 样式
	MarkdownCodeBlock lipgloss.Style
	MarkdownLink      lipgloss.Style
	MarkdownHeader    lipgloss.Style

	// 工具调用样式
	ToolCall   lipgloss.Style
	ToolResult lipgloss.Style
	Thinking   lipgloss.Style

	// 消息组件样式 (文本流布局)
	// 用户消息
	MsgUser          lipgloss.Style
	StreamUserPrefix lipgloss.Style // 用户消息首行前缀 "› "

	// AI消息
	MsgAI          lipgloss.Style
	MsgAIHeader    lipgloss.Style
	MsgAIFooter    lipgloss.Style
	StreamAIPrefix lipgloss.Style // AI 消息首行前缀 "• "
	StreamMeta     lipgloss.Style // 元信息行（tokens · duration 等）

	// 工具调用消息
	MsgTool        lipgloss.Style
	MsgToolHeader  lipgloss.Style
	MsgToolSuccess lipgloss.Style
	MsgToolError   lipgloss.Style

	// 文本流工具调用样式（bullet + 状态标题 + 缩进输出）
	StreamToolName     lipgloss.Style // 工具名（accent 加粗）
	StreamToolRunning  lipgloss.Style // running 态圆点
	StreamToolSuccess  lipgloss.Style // success 圆点/时长
	StreamToolErrorDot lipgloss.Style // error 圆点
	StreamToolDetail   lipgloss.Style // 参数/结果明细（dim）

	// 子Agent消息
	MsgAgent        lipgloss.Style
	MsgAgentHeader  lipgloss.Style
	MsgAgentRunning lipgloss.Style
	MsgAgentDone    lipgloss.Style

	// 计划消息
	MsgPlan       lipgloss.Style
	MsgPlanHeader lipgloss.Style
	MsgPlanStep   lipgloss.Style

	// 思考过程
	MsgThinking       lipgloss.Style
	MsgThinkingHeader lipgloss.Style

	// 系统消息
	MsgSystem  lipgloss.Style
	MsgError   lipgloss.Style
	MsgWarning lipgloss.Style
	MsgInfo    lipgloss.Style
}

// NewStyles 基于给定的主题创建新的样式集�?
func NewStyles(theme *Theme) *Styles {
	s := &Styles{Theme: theme}

	// 全局样式
	s.App = lipgloss.NewStyle().
		Background(theme.Background).
		Foreground(theme.Text)

	s.Surface = lipgloss.NewStyle().
		Background(theme.Surface).
		Foreground(theme.Text)

	// 内容区域（与 ContentPanel 一致，纯黑背景）
	s.Content = lipgloss.NewStyle().
		Background(lipgloss.Color("#000000")).
		Foreground(theme.Text).
		Padding(0, 1)

	// 输入区域
	s.Input = lipgloss.NewStyle().
		Background(theme.SurfaceAlt).
		Foreground(theme.Text).
		Border(lipgloss.NormalBorder(), true, true, true, true).
		BorderForeground(theme.Muted).
		Padding(0, 1)

	s.InputFocus = s.Input
	s.InputFocus = s.InputFocus.BorderForeground(theme.Primary)

	// 提示区域
	s.Hints = lipgloss.NewStyle().
		Background(theme.Surface).
		Foreground(theme.TextMuted).
		Padding(0, 1)

	// 状态栏
	s.StatusBar = lipgloss.NewStyle().
		Background(theme.SurfaceAlt).
		Foreground(theme.Text).
		Padding(0, 1)

	// 面板样式
	s.Panel = lipgloss.NewStyle().
		Background(theme.Surface).
		Foreground(theme.Text).
		Border(lipgloss.NormalBorder(), true, true, true, true).
		BorderForeground(theme.Muted)

	// 对话内容区面板：纯黑背景，与终端默认背景融合，
	// 避免文本行（终端默认黑）与面板背景形成色差。
	// 注意：必须用纯黑 #000000 而非 theme.Background(#0f172a 深蓝)，
	// 否则文本行透出的终端默认纯黑会与面板深蓝产生色差。
	s.ContentPanel = lipgloss.NewStyle().
		Background(lipgloss.Color("#000000")).
		Foreground(theme.Text).
		Border(lipgloss.NormalBorder(), true, true, true, true).
		BorderForeground(theme.Muted)

	s.PanelTitle = lipgloss.NewStyle().
		Background(theme.SurfaceAlt).
		Foreground(theme.Primary).
		Bold(true).
		Padding(0, 1)

	s.PanelBody = lipgloss.NewStyle().
		Background(theme.Surface).
		Foreground(theme.Text).
		Padding(1, 1)

	// 按钮样式
	s.Button = lipgloss.NewStyle().
		Background(theme.SurfaceAlt).
		Foreground(theme.Text).
		Border(lipgloss.NormalBorder(), true, true, true, true).
		BorderForeground(theme.Muted).
		Padding(0, 2)

	s.ButtonFocus = s.Button
	s.ButtonFocus = s.ButtonFocus.
		Background(theme.Primary).
		Foreground(theme.Background).
		BorderForeground(theme.Primary)

	// 表格样式
	s.Table = lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), true, true, true, true).
		BorderForeground(theme.Muted)

	s.TableHeader = theme.TableHeader

	s.TableCell = theme.TableCell

	// 文本样式
	s.Text = lipgloss.NewStyle().
		Foreground(theme.Text)

	s.TextMuted = lipgloss.NewStyle().
		Foreground(theme.TextMuted)

	s.TextSuccess = lipgloss.NewStyle().
		Foreground(theme.Success)

	s.TextError = lipgloss.NewStyle().
		Foreground(theme.Error)

	s.TextWarning = lipgloss.NewStyle().
		Foreground(theme.Warning)

	s.TextInfo = lipgloss.NewStyle().
		Foreground(theme.Info)

	// 边框样式
	s.Border = theme.Border

	// Markdown 样式
	s.MarkdownCodeBlock = lipgloss.NewStyle().
		Background(theme.SurfaceAlt).
		Foreground(theme.Text).
		Padding(0, 1).
		Border(lipgloss.NormalBorder(), true, true, true, true).
		BorderForeground(theme.Muted)

	s.MarkdownLink = lipgloss.NewStyle().
		Foreground(theme.Primary).
		Underline(true)

	s.MarkdownHeader = lipgloss.NewStyle().
		Foreground(theme.Primary).
		Bold(true)

	// 工具调用样式
	s.ToolCall = lipgloss.NewStyle().
		Background(theme.SurfaceAlt).
		Foreground(theme.Text).
		Padding(0, 1)

	s.ToolResult = lipgloss.NewStyle().
		Background(theme.SurfaceAlt).
		Foreground(theme.Text).
		Padding(0, 1)

	s.Thinking = lipgloss.NewStyle().
		Background(theme.SurfaceAlt).
		Foreground(theme.Info).
		Padding(0, 1)

	// 消息组件样式 (文本流布局)
	s.MsgUser = lipgloss.NewStyle().
		Foreground(theme.Text)

	s.StreamUserPrefix = lipgloss.NewStyle().
		Foreground(theme.TextMuted).
		Bold(true)

	// AI消息
	s.MsgAI = lipgloss.NewStyle().
		Foreground(theme.Text).
		Padding(0, 0)

	s.MsgAIHeader = lipgloss.NewStyle().
		Foreground(theme.Primary).
		Bold(true)

	s.MsgAIFooter = lipgloss.NewStyle().
		Foreground(theme.TextMuted).
		Italic(true)

	s.StreamAIPrefix = lipgloss.NewStyle().
		Foreground(theme.TextMuted)

	s.StreamMeta = lipgloss.NewStyle().
		Foreground(theme.TextMuted)

	// 工具调用消息
	s.MsgTool = lipgloss.NewStyle().
		Foreground(theme.Text).
		Padding(0, 0)

	s.MsgToolHeader = lipgloss.NewStyle().
		Foreground(theme.Warning).
		Bold(true)

	s.MsgToolSuccess = lipgloss.NewStyle().
		Foreground(theme.Success)

	s.MsgToolError = lipgloss.NewStyle().
		Foreground(theme.Error)

	// 文本流工具调用样式
	s.StreamToolName = lipgloss.NewStyle().
		Foreground(theme.Accent).
		Bold(true)

	s.StreamToolRunning = lipgloss.NewStyle().
		Foreground(theme.Warning)

	s.StreamToolSuccess = lipgloss.NewStyle().
		Foreground(theme.Success)

	s.StreamToolErrorDot = lipgloss.NewStyle().
		Foreground(theme.Error)

	s.StreamToolDetail = lipgloss.NewStyle().
		Foreground(theme.TextMuted)

	// 子Agent消息
	s.MsgAgent = lipgloss.NewStyle().
		Foreground(theme.Text).
		Padding(0, 0)

	s.MsgAgentHeader = lipgloss.NewStyle().
		Foreground(theme.Secondary).
		Bold(true)

	s.MsgAgentRunning = lipgloss.NewStyle().
		Foreground(theme.Info)

	s.MsgAgentDone = lipgloss.NewStyle().
		Foreground(theme.Success)

	// 计划消息
	s.MsgPlan = lipgloss.NewStyle().
		Foreground(theme.Text).
		Padding(0, 0)

	s.MsgPlanHeader = lipgloss.NewStyle().
		Foreground(theme.Accent).
		Bold(true)

	s.MsgPlanStep = lipgloss.NewStyle().
		Foreground(theme.Text)

	// 思考过程
	s.MsgThinking = lipgloss.NewStyle().
		Foreground(theme.Text).
		Padding(0, 0)

	s.MsgThinkingHeader = lipgloss.NewStyle().
		Foreground(theme.Info).
		Bold(true)

	// 系统消息
	s.MsgSystem = lipgloss.NewStyle().
		Foreground(theme.TextMuted).
		Italic(true)

	s.MsgError = lipgloss.NewStyle().
		Foreground(theme.Error).
		Bold(true)

	s.MsgWarning = lipgloss.NewStyle().
		Foreground(theme.Warning).
		Bold(true)

	s.MsgInfo = lipgloss.NewStyle().
		Foreground(theme.Info)

	return s
}
