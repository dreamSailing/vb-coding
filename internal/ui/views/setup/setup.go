package setup

// Copyright (c) 2026 EOSAIOS
// SPDX-License-Identifier: EOS-NCL-1.1
// 本文件基于 EOS 非商用许可证 v1.1 发布，详见 LICENSE。
// 商业使用请联系版权人获得商业授权。

import (
	"fmt"
	"strings"

	"github.com/eosaios/eos/internal/ui/styles"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// SetupStep 设置步骤
type SetupStep int

const (
	StepProvider SetupStep = iota
	StepAPIBase
	StepAPIKey
	StepModel
	StepComplete
)

// SetupView 设置向导视图
type SetupView struct {
	width      int
	height     int
	styles     *styles.Styles
	step       SetupStep
	providers  []string
	inputs     []textinput.Model
	focusIndex int
	config     SetupConfig
}

// SetupConfig 设置配置
type SetupConfig struct {
	Name     string // 模型名称（显示名称）
	Provider string // 服务商 ID
	PresetID string // 选中的内核 preset ID（自定义模型/自定义服务商为空）
	APIBase  string
	APIKey   string
	Model    string
}

// NewSetupView 创建新的设置向导视图
func NewSetupView(styles *styles.Styles) *SetupView {
	inputs := make([]textinput.Model, 4)
	for i := range inputs {
		inputs[i] = textinput.New()
		inputs[i].SetWidth(40)
	}

	// 设置输入框类型
	inputs[2].EchoMode = textinput.EchoPassword // API Key

	providers := []string{"anthropic", "openai", "ark", "openrouter"}

	v := &SetupView{
		styles:     styles,
		step:       StepProvider,
		providers:  providers,
		inputs:     inputs,
		focusIndex: 0,
	}

	v.updatePlaceholder()
	return v
}

// updatePlaceholder 根据当前步骤更新占位符
func (v *SetupView) updatePlaceholder() {
	switch v.step {
	case StepProvider:
		v.inputs[0].Placeholder = "e.g., anthropic, openai"
		v.inputs[0].Focus()
	case StepAPIBase:
		v.inputs[1].Placeholder = "e.g., https://api.anthropic.com"
		v.inputs[1].Focus()
	case StepAPIKey:
		v.inputs[2].Placeholder = "your-api-key"
		v.inputs[2].Focus()
	case StepModel:
		v.inputs[3].Placeholder = "e.g., claude-3-opus-20240229"
		v.inputs[3].Focus()
	}
}

// SetSize 设置大小
func (v *SetupView) SetSize(width, height int) {
	v.width = width
	v.height = height
	for i := range v.inputs {
		v.inputs[i].SetWidth(width - 20)
	}
}

// Init 初始化
func (v *SetupView) Init() tea.Cmd {
	return textinput.Blink
}

// Update 更新
func (v *SetupView) Update(msg tea.Msg) (*SetupView, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			return v.handleEnter()
		case "tab":
			if v.step == StepProvider {
				v.focusIndex = (v.focusIndex + 1) % len(v.providers)
				v.inputs[0].SetValue(v.providers[v.focusIndex])
			}
		case "esc":
			// 取消设置
			return v, func() tea.Msg {
				return SetupCancelMsg{}
			}
		}
	}

	// 更新当前输入框
	if v.step < StepComplete {
		var cmd tea.Cmd
		v.inputs[v.step], cmd = v.inputs[v.step].Update(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	return v, tea.Batch(cmds...)
}

// handleEnter 处理回车键
func (v *SetupView) handleEnter() (*SetupView, tea.Cmd) {
	switch v.step {
	case StepProvider:
		v.config.Provider = v.inputs[0].Value()
		if v.config.Provider == "" {
			v.config.Provider = v.providers[0]
		}
		v.step = StepAPIBase
		v.updatePlaceholder()
	case StepAPIBase:
		v.config.APIBase = v.inputs[1].Value()
		v.step = StepAPIKey
		v.updatePlaceholder()
	case StepAPIKey:
		v.config.APIKey = v.inputs[2].Value()
		v.step = StepModel
		v.updatePlaceholder()
	case StepModel:
		v.config.Model = v.inputs[3].Value()
		v.step = StepComplete
		// 完成设置
		return v, func() tea.Msg {
			return SetupCompleteMsg{Config: v.config}
		}
	}
	return v, nil
}

// View 渲染
func (v *SetupView) View() string {
	if v.width == 0 {
		v.width = 80
	}
	if v.height == 0 {
		v.height = 24
	}

	var content strings.Builder

	// 标题
	titleStyle := v.styles.PanelTitle.
		Width(v.width - 4).
		Align(lipgloss.Center)
	content.WriteString(titleStyle.Render("Welcome to EOS"))
	content.WriteString("\n\n")

	// 进度指示器
	content.WriteString(v.renderProgress())
	content.WriteString("\n\n")

	// 当前步骤内容
	switch v.step {
	case StepProvider:
		content.WriteString(v.styles.Text.Render("Select your AI provider:"))
		content.WriteString("\n\n")
		content.WriteString(v.renderProviders())
		content.WriteString("\n\n")
		content.WriteString(v.inputs[0].View())

	case StepAPIBase:
		content.WriteString(v.styles.Text.Render("Enter API Base URL (optional for some providers):"))
		content.WriteString("\n\n")
		content.WriteString(v.inputs[1].View())

	case StepAPIKey:
		content.WriteString(v.styles.Text.Render("Enter your API Key:"))
		content.WriteString("\n\n")
		content.WriteString(v.inputs[2].View())

	case StepModel:
		content.WriteString(v.styles.Text.Render("Enter the model name:"))
		content.WriteString("\n\n")
		content.WriteString(v.renderModels())
		content.WriteString("\n")
		content.WriteString(v.inputs[3].View())

	case StepComplete:
		content.WriteString(v.styles.TextSuccess.Render("✓ Setup complete!"))
		content.WriteString("\n\n")
		content.WriteString(v.styles.Text.Render("Your configuration has been saved."))
	}

	content.WriteString("\n\n")
	content.WriteString(v.styles.TextMuted.Render("Enter: continue | Esc: cancel"))

	// 包装在面板样式中
	panelStyle := lipgloss.NewStyle().
		Width(v.width-4).
		Height(v.height-2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(v.styles.Theme.Primary).
		Padding(2, 4)

	return panelStyle.Render(content.String())
}

// renderProgress 渲染进度条
func (v *SetupView) renderProgress() string {
	steps := []string{"Provider", "API Base", "API Key", "Model"}
	var parts []string

	for i, step := range steps {
		style := v.styles.TextMuted
		if i == int(v.step) {
			style = v.styles.TextInfo.Bold(true)
		} else if i < int(v.step) {
			style = v.styles.TextSuccess
		}
		parts = append(parts, style.Render(fmt.Sprintf("%d. %s", i+1, step)))
	}

	return strings.Join(parts, " → ")
}

// renderProviders 渲染提供商列表
func (v *SetupView) renderProviders() string {
	var parts []string
	for i, p := range v.providers {
		style := v.styles.TextMuted
		if i == v.focusIndex {
			style = v.styles.TextInfo.Bold(true).Background(v.styles.Theme.SurfaceAlt)
		}
		parts = append(parts, style.Render(" "+p+" "))
	}
	return strings.Join(parts, " ")
}

// renderModels 渲染模型建议
func (v *SetupView) renderModels() string {
	suggestions := map[string][]string{
		"anthropic": {"claude-3-opus-20240229", "claude-3-sonnet-20240229", "claude-3-haiku-20240307"},
		"openai":    {"gpt-4o", "gpt-4-turbo", "gpt-3.5-turbo"},
		"ark":       {"doubao-pro-32k", "doubao-lite-32k"},
	}

	models, ok := suggestions[v.config.Provider]
	if !ok {
		return ""
	}

	var parts []string
	for _, m := range models {
		parts = append(parts, v.styles.TextMuted.Render("• "+m))
	}
	return "Suggested models:\n" + strings.Join(parts, "\n")
}

// SetupCompleteMsg 设置完成消息
type SetupCompleteMsg struct {
	Config SetupConfig
}

// SetupCancelMsg 设置取消消息
type SetupCancelMsg struct{}

// ModelFormCompleteMsg 模型表单完成消息
type ModelFormCompleteMsg struct {
	Config   SetupConfig
	EditMode bool
}
