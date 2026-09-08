package panels

// Copyright (c) 2026 EOSAIOS
// SPDX-License-Identifier: EOS-NCL-1.1
// 本文件基于 EOS 非商用许可证 v1.1 发布，详见 LICENSE。
// 商业使用请联系版权人获得商业授权。

import (
	"fmt"
	"strings"

	"github.com/eosaios/eos/internal/config"
	"github.com/eosaios/eos/internal/i18n"
	"github.com/eosaios/eos/internal/ui/styles"
	"github.com/eosaios/eos/pkg/coreapi"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// ModelsPanel 模型管理面板
type ModelsPanel struct {
	BasePanel
	styles       *styles.Styles
	table        table.Model
	models       []config.ModelEntry // 用户配置的模型列表
	currentModel string
	actionOps    []string
	actionIndex  int
	language     string

	// presetPlanMaps: preset_id → 套餐内可选模型（内核目录下发）。
	// 套餐类条目（如 MiniMax Token Plan）可在其中切换 MiniMax-M3 / M2.7。
	presetPlanModels map[string][]coreapi.PlanModel
	// planPickerEntry 非空时面板处于「套餐内模型选择」子模式。
	planPickerEntry *config.ModelEntry
	planModels      []coreapi.PlanModel
	planTable       table.Model
}

// NewModelsPanel 创建新的模型管理面板
func NewModelsPanel(styles *styles.Styles, lang string) *ModelsPanel {
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
		table.WithHeight(20),
		table.WithStyles(s),
		table.WithFocused(true),
	)

	// 设置表格的键位映射，确保上下键可用
	t.KeyMap.LineUp.SetKeys("up", "k")
	t.KeyMap.LineDown.SetKeys("down", "j")

	// 套餐内模型选择表格（复用同一套样式）
	planTable := table.New(
		table.WithHeight(10),
		table.WithStyles(s),
		table.WithFocused(true),
	)
	planTable.KeyMap.LineUp.SetKeys("up", "k")
	planTable.KeyMap.LineDown.SetKeys("down", "j")

	panel := &ModelsPanel{
		BasePanel:        NewBasePanel("models"),
		styles:           styles,
		table:            t,
		models:           make([]config.ModelEntry, 0),
		currentModel:     "",
		actionOps:        []string{"Use", "Model", "Add", "Edit", "Delete", "SyncEnv", "Refresh"},
		actionIndex:      0,
		language:         lang,
		presetPlanModels: make(map[string][]coreapi.PlanModel),
		planTable:        planTable,
	}

	panel.updateTableColumns()
	panel.updateTable()

	return panel
}

func (p *ModelsPanel) updateTableColumns() {
	columns := []table.Column{
		{Title: i18n.T("models.col.name", p.language), Width: 25},
		{Title: i18n.T("models.col.source", p.language), Width: 10},
		{Title: i18n.T("models.col.base", p.language), Width: 35},
		{Title: i18n.T("models.col.model", p.language), Width: 20},
	}
	p.table.SetColumns(columns)
}

func (p *ModelsPanel) SetModels(models []config.ModelEntry, current string) {
	p.models = append([]config.ModelEntry(nil), models...)
	p.currentModel = strings.TrimSpace(current)
	p.updateTable()
}

// SetCurrentModel 设置当前模型
func (p *ModelsPanel) SetCurrentModel(current string) {
	p.currentModel = current
	p.updateTable()
}

// SetPlanModels 注入内核目录的 preset_id → 套餐内可选模型映射。
func (p *ModelsPanel) SetPlanModels(m map[string][]coreapi.PlanModel) {
	p.presetPlanModels = m
	if p.planPickerEntry != nil {
		// 刷新子模式数据；条目已不存在时退出子模式。
		if !p.reloadPlanModels(p.planPickerEntry.Name) {
			p.planPickerEntry = nil
		}
	}
}

// planModelsFor 返回条目对应的套餐内可选模型（需 >1 才有切换意义）。
func (p *ModelsPanel) planModelsFor(m *config.ModelEntry) []coreapi.PlanModel {
	if m == nil {
		return nil
	}
	return p.presetPlanModels[strings.TrimSpace(m.PresetID)]
}

// reloadPlanModels 重新装载指定条目的套餐模型；条目不在列表返回 false。
func (p *ModelsPanel) reloadPlanModels(entryName string) bool {
	var entry *config.ModelEntry
	for i := range p.models {
		if p.models[i].Name == entryName {
			entry = &p.models[i]
			break
		}
	}
	if entry == nil {
		return false
	}
	p.planModels = p.planModelsFor(entry)
	p.updatePlanTable()
	return true
}

// openPlanPicker 进入「套餐内模型选择」子模式。
func (p *ModelsPanel) openPlanPicker() {
	entry := p.GetSelectedModel()
	if entry == nil || len(p.planModelsFor(entry)) < 2 {
		return
	}
	p.planPickerEntry = entry
	p.reloadPlanModels(entry.Name)
}

func (p *ModelsPanel) updatePlanTable() {
	columns := []table.Column{
		{Title: i18n.T("models.col.label", p.language), Width: 24},
		{Title: i18n.T("models.col.model_id", p.language), Width: 22},
		{Title: i18n.T("models.col.ctx", p.language), Width: 12},
		{Title: i18n.T("models.col.capability", p.language), Width: 14},
	}
	p.planTable.SetColumns(columns)

	rows := make([]table.Row, 0, len(p.planModels))
	current := ""
	if p.planPickerEntry != nil {
		current = strings.TrimSpace(p.planPickerEntry.Model)
	}
	for _, pm := range p.planModels {
		mark := " "
		if strings.TrimSpace(pm.ModelID) == current {
			mark = "*"
		}
		ctx := "-"
		if pm.ContextWindow > 0 {
			ctx = fmt.Sprintf("%.0fK", float64(pm.ContextWindow)/1000)
		}
		caps := p.planCapabilityLabel(pm)
		rows = append(rows, table.Row{mark + " " + pm.Label, pm.ModelID, ctx, caps})
	}
	p.planTable.SetRows(rows)
	p.planTable.SetCursor(0)
}

// planCapabilityLabel 套餐模型能力紧凑标注：中文「视/理/工」，英文 V/R/T。
// 能力字段为 *bool（nil = 未标注），nil 视为不支持。
func (p *ModelsPanel) planCapabilityLabel(pm coreapi.PlanModel) string {
	zh := p.language == "zh"
	var parts []string
	if derefPlanCap(pm.SupportsVision) {
		if zh {
			parts = append(parts, "视")
		} else {
			parts = append(parts, "V")
		}
	}
	if derefPlanCap(pm.SupportsReasoningEffort) {
		if zh {
			parts = append(parts, "理")
		} else {
			parts = append(parts, "R")
		}
	}
	if derefPlanCap(pm.SupportsTools) {
		if zh {
			parts = append(parts, "工")
		} else {
			parts = append(parts, "T")
		}
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, "/")
}

func derefPlanCap(v *bool) bool { return v != nil && *v }

// updateTable 更新表格
func (p *ModelsPanel) updateTable() {
	rows := make([]table.Row, 0)

	if len(p.models) == 0 {
		rows = append(rows, table.Row{i18n.T("models.empty", p.language), "", "", ""})
	} else {
		for _, m := range p.models {
			status := ""
			if m.Name == p.currentModel {
				status = "* "
			}
			name := status + m.Name
			rows = append(rows, table.Row{name, m.Source, m.APIBase, m.Model})
		}
	}
	p.table.SetRows(rows)
}

// Refresh 刷新模型列表
func (p *ModelsPanel) Refresh() {
	p.updateTable()
}

// GetCurrentAction 获取当前选中的操作
func (p *ModelsPanel) GetCurrentAction() string {
	if p.actionIndex >= 0 && p.actionIndex < len(p.actionOps) {
		return p.actionOps[p.actionIndex]
	}
	return ""
}

// GetSelectedModel 获取选中的模型
func (p *ModelsPanel) GetSelectedModel() *config.ModelEntry {
	i := p.table.Cursor()
	if i >= 0 && i < len(p.models) {
		return &p.models[i]
	}
	return nil
}

// Init 初始化
func (p *ModelsPanel) Init() tea.Cmd {
	return nil
}

// Update 更新
func (p *ModelsPanel) Update(msg tea.Msg) (Panel, tea.Cmd) {
	switch msg := msg.(type) {
	case LanguageChangeMsg:
		p.language = msg.Language
		p.updateTableColumns()
		p.updateTable()
		return p, nil

	case tea.KeyMsg:
		// 套餐内模型选择子模式：Enter 确认 / Esc 返回，其余交给表格导航。
		if p.planPickerEntry != nil {
			switch msg.String() {
			case "enter":
				idx := p.planTable.Cursor()
				if idx >= 0 && idx < len(p.planModels) {
					entryName := p.planPickerEntry.Name
					modelID := strings.TrimSpace(p.planModels[idx].ModelID)
					p.planPickerEntry = nil
					return p, func() tea.Msg {
						return ModelPlanSelectMsg{EntryName: entryName, ModelID: modelID}
					}
				}
				return p, nil
			case "esc":
				p.planPickerEntry = nil
				return p, nil
			}
			var cmd tea.Cmd
			p.planTable, cmd = p.planTable.Update(msg)
			return p, cmd
		}

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
			// 执行当前选中的操作
			action := p.GetCurrentAction()
			switch action {
			case "Use":
				// 设置当前模型
				if m := p.GetSelectedModel(); m != nil && m.Name != "" {
					return p, func() tea.Msg {
						return ModelSelectMsg{Name: m.Name}
					}
				}
			case "Model":
				// 套餐内切换模型（如 MiniMax Token Plan 的 M3 / M2.7）
				p.openPlanPicker()
				return p, nil
			case "Add":
				// 添加模型
				return p, func() tea.Msg {
					return ModelAddMsg{}
				}
			case "Edit":
				// 编辑模型
				if m := p.GetSelectedModel(); m != nil && m.Name != "" {
					return p, func() tea.Msg {
						return ModelEditMsg{Name: m.Name}
					}
				}
			case "Delete":
				// 删除模型
				if m := p.GetSelectedModel(); m != nil && m.Name != "" {
					return p, func() tea.Msg {
						return ModelDeleteMsg{Name: m.Name}
					}
				}
			case "SyncEnv":
				// 同步环境变量
				return p, func() tea.Msg {
					return ModelSyncMsg{}
				}
			case "Refresh":
				// 刷新模型列表
				return p, func() tea.Msg {
					return ModelRefreshMsg{}
				}
			}
		case "u", "U":
			// 直接执行使用操作
			if m := p.GetSelectedModel(); m != nil && m.Name != "" {
				return p, func() tea.Msg {
					return ModelSelectMsg{Name: m.Name}
				}
			}
		case "m", "M":
			// 套餐内切换模型
			p.openPlanPicker()
			return p, nil
		case "a", "A":
			// 直接执行新增操作
			return p, func() tea.Msg {
				return ModelAddMsg{}
			}
		case "e", "E":
			// 直接执行编辑操作
			if m := p.GetSelectedModel(); m != nil && m.Name != "" {
				return p, func() tea.Msg {
					return ModelEditMsg{Name: m.Name}
				}
			}
		case "d", "D":
			// 直接执行删除操作
			if m := p.GetSelectedModel(); m != nil && m.Name != "" {
				return p, func() tea.Msg {
					return ModelDeleteMsg{Name: m.Name}
				}
			}
		case "r", "R":
			// 刷新模型列表
			return p, func() tea.Msg {
				return ModelRefreshMsg{}
			}
		case "s", "S":
			// 同步模型环境变量（对齐 help 的 S: sync；此前仅有 Enter 的 SyncEnv action）
			return p, func() tea.Msg {
				return ModelSyncMsg{}
			}
		}
	}

	var cmd tea.Cmd
	p.table, cmd = p.table.Update(msg)
	return p, cmd
}

// View 渲染
func (p *ModelsPanel) View() string {
	var content strings.Builder

	// 套餐内模型选择子模式
	if p.planPickerEntry != nil {
		content.WriteString(p.styles.PanelTitle.Render(
			fmt.Sprintf("%s — %s", i18n.T("models.plan.title", p.language), p.planPickerEntry.Name)))
		content.WriteString("\n\n")
		content.WriteString(p.planTable.View())
		content.WriteString("\n\n")
		content.WriteString(p.styles.TextMuted.Render(i18n.T("models.plan.help", p.language)))
		return p.RenderBorder(content.String(), "Plan Models")
	}

	content.WriteString(p.styles.PanelTitle.Render(i18n.T("models.list.title", p.language)))
	content.WriteString("\n\n")

	if p.currentModel != "" {
		fmt.Fprintf(&content, "%s %s\n\n",
			i18n.T("models.current", p.language),
			p.styles.TextSuccess.Render(p.currentModel))
	}

	content.WriteString(p.table.View())
	content.WriteString("\n\n")

	// 显示当前选中的模型信息
	selected := p.GetSelectedModel()
	if selected != nil && selected.Name != "" {
		selectedText := fmt.Sprintf("%s [%s] %s (%s) - %s",
			i18n.T("models.selected", p.language),
			p.styles.TextInfo.Render(selected.Name),
			p.styles.TextMuted.Render(selected.Source),
			p.styles.TextMuted.Render(selected.APIBase),
			p.styles.TextMuted.Render(selected.Model))
		content.WriteString(selectedText)
		content.WriteString("\n\n")
	}

	// 显示操作列表
	var opStrs []string
	for i, op := range p.actionOps {
		key := ""
		switch op {
		case "Use":
			key = "models.action.use"
		case "Model":
			key = "models.action.model"
		case "Add":
			key = "models.action.add"
		case "Edit":
			key = "models.action.edit"
		case "Delete":
			key = "models.action.delete"
		case "SyncEnv":
			key = "models.action.sync_env"
		case "Refresh":
			key = "models.action.refresh"
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

	content.WriteString(p.styles.TextMuted.Render(i18n.T("models.help", p.language)))

	return p.RenderBorder(content.String(), "Models Panel")
}

// SetSize 设置大小
func (p *ModelsPanel) SetSize(width, height int) {
	p.BasePanel.SetSize(width, height)
	p.table.SetWidth(width - 4)
	p.table.SetHeight(height - 12)
}

// ModelSelectMsg 选择模型消息
type ModelSelectMsg struct {
	Name string
}

// ModelPlanSelectMsg 套餐内切换模型消息（条目不变，仅换套餐内具体模型）
type ModelPlanSelectMsg struct {
	EntryName string
	ModelID   string
}

// ModelAddMsg 添加模型消息
type ModelAddMsg struct{}

// ModelEditMsg 编辑模型消息
type ModelEditMsg struct {
	Name string
}

// ModelDeleteMsg 删除模型消息
type ModelDeleteMsg struct {
	Name string
}

// ModelSyncMsg 同步模型消息
type ModelSyncMsg struct{}

// ModelRefreshMsg 刷新模型列表消息
type ModelRefreshMsg struct{}

// ModelFormMsg 添加/编辑模型表单消息
type ModelFormMsg struct {
	Name    string
	APIBase string
	APIKey  string
	Model   string
}
