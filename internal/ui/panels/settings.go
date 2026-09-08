package panels

// Copyright (c) 2026 EOSAIOS
// SPDX-License-Identifier: EOS-NCL-1.1
// 本文件基于 EOS 非商用许可证 v1.1 发布，详见 LICENSE。
// 商业使用请联系版权人获得商业授权。

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/eosaios/eos/internal/config"
	"github.com/eosaios/eos/internal/i18n"
	"github.com/eosaios/eos/internal/pkg/settings"
	"github.com/eosaios/eos/internal/ui/styles"

	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// SettingsPanel 设置面板
type SettingsPanel struct {
	BasePanel
	styles      *styles.Styles
	table       table.Model
	manager     *settings.Manager
	settings    *settings.Settings
	actionOps   []string
	actionIndex int
	language    string
	editMode    bool
	editInput   textinput.Model
	editKey     string
	editValue   string
	// 选择模式：固定取值集合的字段（语言/主题）不用文本输入，
	// ←/→ 在 editChoices 里循环；nil = 该字段走文本输入。
	editChoices     []string
	editChoiceIndex int
	rowKeys         []string // 与 table 行同步的稳定字段 ID，供 saveEditValue 匹配

	globalPredictionEnabled bool
	// memoryInjectionEnabled 是 CLI 全局「记忆注入」开关（~/.eos.json
	// memory_injection_enabled，默认开），随每次 turn 下发 use_memory。
	memoryInjectionEnabled bool
	// diffTheme 是 diff/代码块 chroma 高亮主题（~/.eos.json diff_theme，
	// 默认 monokai），供 /diff、审批 diff 与 markdown 代码块渲染使用。
	diffTheme string
}

// settingChoices 返回字段的合法取值集合；nil 表示自由文本编辑。
func settingChoices(key string) []string {
	switch key {
	case "Language":
		return []string{"zh", "en"}
	case "Theme":
		return []string{"dark", "light", "high-contrast"}
	default:
		return nil
	}
}

// SettingItem 设置项
type SettingItem struct {
	Key   string
	Value string
	Type  string // bool, int, string
}

// NewSettingsPanel 创建新的设置面板
func NewSettingsPanel(styles *styles.Styles, mgr *settings.Manager, lang string) *SettingsPanel {
	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("#6366f1")).
		BorderBottom(true).
		Bold(true)
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("#000000")).
		Background(lipgloss.Color("#6366f1")).
		Bold(true)

	t := table.New(
		table.WithHeight(15),
		table.WithStyles(s),
		table.WithFocused(true),
	)

	// 设置表格的键位映射
	t.KeyMap.LineUp.SetKeys("up", "k")
	t.KeyMap.LineDown.SetKeys("down", "j")

	// 创建编辑输入框
	input := textinput.New()
	input.SetWidth(40)
	panel := &SettingsPanel{
		BasePanel:   NewBasePanel("settings"),
		styles:      styles,
		table:       t,
		manager:     mgr,
		settings:    nil,
		actionOps:   []string{"Edit", "Save", "Reset", "Refresh"},
		actionIndex: 0,
		language:    lang,
		editMode:    false,
		editInput:   input,
	}

	panel.updateTableColumns()

	return panel
}

func (p *SettingsPanel) updateTableColumns() {
	columns := []table.Column{
		{Title: i18n.T("settings.col.name", p.language), Width: 25},
		{Title: i18n.T("settings.col.value", p.language), Width: 40},
	}
	p.table.SetColumns(columns)
}

// LoadSettings 从管理器加载设置
func (p *SettingsPanel) LoadSettings() {
	if p.manager != nil {
		if s, err := p.manager.Load(); err == nil && s != nil {
			p.settings = s
		} else {
			p.settings = defaultPanelSettings()
		}
	} else if p.settings == nil {
		p.settings = defaultPanelSettings()
	}
	cfg, _ := config.Load()
	p.globalPredictionEnabled = config.NextMessagePredictionEnabled(&cfg)
	p.memoryInjectionEnabled = config.MemoryInjectionEnabled(&cfg)
	p.diffTheme = config.DiffHighlightTheme(&cfg)
	p.updateTable()
}

// SetSettings 直接设置设置值
func (p *SettingsPanel) SetSettings(s *settings.Settings) {
	if s != nil {
		p.settings = s
	}
	p.updateTable()
}

func (p *SettingsPanel) SetGlobalPredictionEnabled(enabled bool) {
	p.globalPredictionEnabled = enabled
	p.updateTable()
}

func (p *SettingsPanel) SetMemoryInjectionEnabled(enabled bool) {
	p.memoryInjectionEnabled = enabled
	p.updateTable()
}

func defaultPanelSettings() *settings.Settings {
	tn := true
	return &settings.Settings{
		AutoContext:          true,
		DesktopNotifications: &tn,
		MaxInjectKB:          48,
		WatchDebounceMs:      500,
		PollIntervalSec:      5,
		Language:             "zh",
		Theme:                "dark",
		PlanPromptStyle:      "concise",
	}
}

// updateTable 更新表格内容
func (p *SettingsPanel) updateTable() {
	rows := make([]table.Row, 0)
	p.rowKeys = make([]string, 0)

	if p.settings == nil {
		rows = append(rows, table.Row{i18n.T("settings.empty", p.language), ""})
	} else {
		s := p.settings
		rows = append(rows, table.Row{i18n.T("settings.row.auto_context", p.language), fmt.Sprintf("%v", s.AutoContext)})
		p.rowKeys = append(p.rowKeys, "AutoContext")
		desktopNotifications := true
		if s.DesktopNotifications != nil {
			desktopNotifications = *s.DesktopNotifications
		}
		rows = append(rows, table.Row{i18n.T("settings.row.desktop_notifications", p.language), fmt.Sprintf("%v", desktopNotifications)})
		p.rowKeys = append(p.rowKeys, "DesktopNotifications")
		gitCommitReminder := true
		if s.GitCommitReminder != nil {
			gitCommitReminder = *s.GitCommitReminder
		}
		rows = append(rows, table.Row{i18n.T("settings.row.git_commit_reminder", p.language), fmt.Sprintf("%v", gitCommitReminder)})
		p.rowKeys = append(p.rowKeys, "GitCommitReminder")
		rows = append(rows, table.Row{i18n.T("settings.row.memory_injection", p.language), fmt.Sprintf("%v", p.memoryInjectionEnabled)})
		p.rowKeys = append(p.rowKeys, "MemoryInjection(Global)")
		rows = append(rows, table.Row{i18n.T("settings.row.context_limit_kb", p.language), fmt.Sprintf("%d", s.MaxInjectKB)})
		p.rowKeys = append(p.rowKeys, "MaxInjectKB")
		rows = append(rows, table.Row{i18n.T("settings.row.language", p.language), s.Language})
		p.rowKeys = append(p.rowKeys, "Language")
		rows = append(rows, table.Row{i18n.T("settings.row.theme", p.language), s.Theme})
		p.rowKeys = append(p.rowKeys, "Theme")
		rows = append(rows, table.Row{i18n.T("settings.row.diff_theme", p.language), p.diffTheme})
		p.rowKeys = append(p.rowKeys, "DiffTheme")
		planPromptStyle := strings.TrimSpace(s.PlanPromptStyle)
		if planPromptStyle == "" {
			planPromptStyle = "concise"
		}
		rows = append(rows, table.Row{i18n.T("settings.row.plan_prompt_style", p.language), planPromptStyle})
		p.rowKeys = append(p.rowKeys, "PlanPromptStyle")
	}
	p.table.SetRows(rows)
}

// GetCurrentAction 获取当前选中的操作
func (p *SettingsPanel) GetCurrentAction() string {
	if p.actionIndex >= 0 && p.actionIndex < len(p.actionOps) {
		return p.actionOps[p.actionIndex]
	}
	return ""
}

// GetSelectedSetting 获取选中的设置项（返回稳定字段 ID + 当前值）
func (p *SettingsPanel) GetSelectedSetting() (string, string) {
	i := p.table.Cursor()
	rows := p.table.Rows()
	if i >= 0 && i < len(rows) && i < len(p.rowKeys) {
		return p.rowKeys[i], rows[i][1]
	}
	return "", ""
}

// editKeyLabel 把稳定字段 ID 映射回当前语言的显示标签，供编辑模式顶部展示。
func (p *SettingsPanel) editKeyLabel() string {
	switch p.editKey {
	case "AutoContext":
		return i18n.T("settings.row.auto_context", p.language)
	case "DesktopNotifications":
		return i18n.T("settings.row.desktop_notifications", p.language)
	case "GitCommitReminder":
		return i18n.T("settings.row.git_commit_reminder", p.language)
	case "MemoryInjection(Global)":
		return i18n.T("settings.row.memory_injection", p.language)
	case "MaxInjectKB":
		return i18n.T("settings.row.context_limit_kb", p.language)
	case "Language":
		return i18n.T("settings.row.language", p.language)
	case "Theme":
		return i18n.T("settings.row.theme", p.language)
	case "DiffTheme":
		return i18n.T("settings.row.diff_theme", p.language)
	case "PlanPromptStyle":
		return i18n.T("settings.row.plan_prompt_style", p.language)
	default:
		return p.editKey
	}
}

// Init 初始化
func (p *SettingsPanel) Init() tea.Cmd {
	p.LoadSettings()
	return nil
}

// Update 更新
func (p *SettingsPanel) Update(msg tea.Msg) (Panel, tea.Cmd) {
	// 如果在编辑模式，处理编辑输入
	if p.editMode {
		return p.handleEditMode(msg)
	}

	var cmd tea.Cmd
	p.table, cmd = p.table.Update(msg)

	switch msg := msg.(type) {
	case LanguageChangeMsg:
		p.language = msg.Language
		p.updateTableColumns()
		p.updateTable()
		return p, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "left", "h":
			// 向左切换操作
			if p.actionIndex > 0 {
				p.actionIndex--
			} else {
				p.actionIndex = len(p.actionOps) - 1
			}
			return p, nil
		case "right", "l":
			// 向右切换操作
			if p.actionIndex < len(p.actionOps)-1 {
				p.actionIndex++
			} else {
				p.actionIndex = 0
			}
			return p, nil
		case "enter":
			return p.handleAction()
		case "e":
			// 直接执行编辑操作
			return p.enterEditMode()
		case "s":
			// 直接执行保存操作
			enabled := p.globalPredictionEnabled
			memoryInjection := p.memoryInjectionEnabled
			return p, func() tea.Msg {
				return SettingsSaveMsg{Settings: p.settings, GlobalPredictionEnabled: &enabled, MemoryInjectionEnabled: &memoryInjection, DiffTheme: p.diffTheme}
			}
		case "r":
			// 直接执行重置操作
			p.LoadSettings()
			return p, func() tea.Msg {
				return SettingsResetMsg{}
			}
		case "f5":
			// 刷新设置
			p.LoadSettings()
		}
	}

	return p, cmd
}

// handleAction 处理当前选中的操作
func (p *SettingsPanel) handleAction() (Panel, tea.Cmd) {
	action := p.GetCurrentAction()
	switch action {
	case "Edit":
		return p.enterEditMode()
	case "Save":
		enabled := p.globalPredictionEnabled
		memoryInjection := p.memoryInjectionEnabled
		return p, func() tea.Msg {
			return SettingsSaveMsg{Settings: p.settings, GlobalPredictionEnabled: &enabled, MemoryInjectionEnabled: &memoryInjection, DiffTheme: p.diffTheme}
		}
	case "Reset":
		p.LoadSettings()
		return p, func() tea.Msg {
			return SettingsResetMsg{}
		}
	case "Refresh":
		p.LoadSettings()
	}
	return p, nil
}

// enterEditMode 进入编辑模式（枚举字段走选择模式，其余走文本输入）
func (p *SettingsPanel) enterEditMode() (Panel, tea.Cmd) {
	key, value := p.GetSelectedSetting()
	if key == "" {
		return p, nil
	}

	p.editKey = key
	p.editValue = value
	if choices := settingChoices(key); choices != nil {
		p.editChoices = choices
		p.editChoiceIndex = 0
		for i, choice := range choices {
			if choice == value {
				p.editChoiceIndex = i
				break
			}
		}
		p.editMode = true
		return p, nil
	}
	p.editChoices = nil
	p.editInput.SetValue(value)
	p.editInput.Focus()
	p.editMode = true

	return p, textinput.Blink
}

// exitEditMode 退出编辑模式（清掉两种编辑态的残留）
func (p *SettingsPanel) exitEditMode() {
	p.editMode = false
	p.editChoices = nil
	p.editInput.Blur()
}

// handleEditMode 处理编辑模式下的输入
func (p *SettingsPanel) handleEditMode(msg tea.Msg) (Panel, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "esc":
			p.exitEditMode()
			return p, nil
		case "enter":
			// 保存编辑值
			p.saveEditValue()
			p.exitEditMode()
			enabled := p.globalPredictionEnabled
			memoryInjection := p.memoryInjectionEnabled
			return p, func() tea.Msg {
				return SettingsSaveMsg{Settings: p.settings, GlobalPredictionEnabled: &enabled, MemoryInjectionEnabled: &memoryInjection, DiffTheme: p.diffTheme}
			}
		}
		if p.editChoices != nil {
			switch keyMsg.String() {
			case "left", "h":
				p.editChoiceIndex = (p.editChoiceIndex + len(p.editChoices) - 1) % len(p.editChoices)
			case "right", "l":
				p.editChoiceIndex = (p.editChoiceIndex + 1) % len(p.editChoices)
			}
			return p, nil
		}
	}

	var cmd tea.Cmd
	p.editInput, cmd = p.editInput.Update(msg)
	return p, cmd
}

// saveEditValue 保存编辑的值到设置
func (p *SettingsPanel) saveEditValue() {
	if p.settings == nil {
		return
	}

	var value string
	if p.editChoices != nil {
		value = p.editChoices[p.editChoiceIndex]
	} else {
		value = p.editInput.Value()
	}
	switch p.editKey {
	case "AutoContext":
		p.settings.AutoContext = value == "true" || value == "True" || value == "1"
	case "DesktopNotifications":
		v := value == "true" || value == "True" || value == "1"
		p.settings.DesktopNotifications = &v
	case "GitCommitReminder":
		v := value == "true" || value == "True" || value == "1"
		p.settings.GitCommitReminder = &v
	case "MaxInjectKB":
		if v, err := strconv.Atoi(value); err == nil {
			p.settings.MaxInjectKB = v
		}
	case "WatchDebounceMs":
		if v, err := strconv.Atoi(value); err == nil {
			p.settings.WatchDebounceMs = v
		}
	case "PollIntervalSec":
		if v, err := strconv.Atoi(value); err == nil {
			p.settings.PollIntervalSec = v
		}
	case "Language":
		p.settings.Language = value
	case "Theme":
		p.settings.Theme = value
	case "NextMessagePrediction(Global)":
		p.globalPredictionEnabled = value == "true" || value == "True" || value == "1"
	case "MemoryInjection(Global)":
		p.memoryInjectionEnabled = value == "true" || value == "True" || value == "1"
	case "PlanPromptStyle":
		p.settings.PlanPromptStyle = value
	case "DiffTheme":
		// 原样保存；非法主题名由渲染层 NormalizeChromaTheme 回退默认。
		p.diffTheme = strings.TrimSpace(value)
	case "PlanBubbleColor":
		p.settings.PlanBubbleColor = value
	}

	p.updateTable()
}

// View 渲染
func (p *SettingsPanel) View() string {
	// 如果在编辑模式，显示编辑界面
	if p.editMode {
		return p.viewEditMode()
	}

	var content strings.Builder

	content.WriteString(p.styles.PanelTitle.Render(i18n.T("settings.list.title", p.language)))
	content.WriteString("\n\n")

	content.WriteString(p.table.View())
	content.WriteString("\n\n")

	// 显示当前选中的设置项
	key, value := p.GetSelectedSetting()
	if key != "" {
		selectedText := fmt.Sprintf("%s [%s] = %s",
			i18n.T("settings.col.name", p.language),
			p.styles.TextInfo.Render(key),
			p.styles.TextMuted.Render(value))
		content.WriteString(selectedText)
		content.WriteString("\n\n")
	}

	// 显示操作列表
	var opStrs []string
	for i, op := range p.actionOps {
		key := ""
		switch op {
		case "Edit":
			key = "settings.action.edit"
		case "Save":
			key = "settings.action.save"
		case "Reset":
			key = "settings.action.reset"
		case "Refresh":
			key = "settings.action.refresh"
		}
		text := i18n.T(key, p.language)
		if i == p.actionIndex {
			opStrs = append(opStrs, p.styles.TextSuccess.Render("["+text+"]"))
		} else {
			opStrs = append(opStrs, p.styles.TextMuted.Render(text))
		}
	}
	fmt.Fprintf(&content, "%s %s\n\n",
		i18n.T("models.action", p.language),
		strings.Join(opStrs, "  "))

	content.WriteString(p.styles.TextMuted.Render(i18n.T("settings.help", p.language)))

	return p.RenderBorder(content.String(), i18n.T("settings.list.title", p.language))
}

// viewEditMode 编辑模式的视图（选择模式渲染候选列表，文本模式渲染输入框）
func (p *SettingsPanel) viewEditMode() string {
	var content strings.Builder

	content.WriteString(p.styles.PanelTitle.Render(i18n.T("settings.edit.title", p.language)))
	content.WriteString("\n\n")

	fmt.Fprintf(&content, "%s: %s\n\n",
		i18n.T("settings.col.name", p.language),
		p.styles.TextInfo.Render(p.editKeyLabel()))

	if p.editChoices != nil {
		var opts []string
		for i, choice := range p.editChoices {
			label := fmt.Sprintf("%s (%s)", i18n.T("settings.choice."+choiceLabelKey(choice), p.language), choice)
			if i == p.editChoiceIndex {
				opts = append(opts, p.styles.TextSuccess.Render("["+label+"]"))
			} else {
				opts = append(opts, p.styles.TextMuted.Render(label))
			}
		}
		content.WriteString(strings.Join(opts, "  "))
		content.WriteString("\n\n")
		content.WriteString(p.styles.TextMuted.Render(i18n.T("settings.edit.choice_hint", p.language)))
	} else {
		content.WriteString(p.editInput.View())
		content.WriteString("\n\n")
		content.WriteString(p.styles.TextMuted.Render(i18n.T("settings.edit.hint", p.language)))
	}

	return p.RenderBorder(content.String(), i18n.T("settings.edit.title", p.language))
}

// choiceLabelKey 取值 → 本地化标签的 i18n key 片段
func choiceLabelKey(choice string) string {
	switch choice {
	case "high-contrast":
		return "high_contrast"
	default:
		return choice
	}
}

// SetSize 设置大小
func (p *SettingsPanel) SetSize(width, height int) {
	p.BasePanel.SetSize(width, height)
	p.table.SetWidth(width - 4)
	p.table.SetHeight(height - 12)
	p.editInput.SetWidth(width - 10)
}

// SettingsSaveMsg 保存设置消息
type SettingsSaveMsg struct {
	Settings                *settings.Settings
	GlobalPredictionEnabled *bool
	MemoryInjectionEnabled  *bool
	// DiffTheme 是 diff/代码块高亮主题（~/.eos.json diff_theme，全局）。
	DiffTheme string
}

// SettingsResetMsg 重置设置消息
type SettingsResetMsg struct{}
