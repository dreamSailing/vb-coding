package setup

// Copyright (c) 2026 EOSAIOS
// SPDX-License-Identifier: EOS-NCL-1.1
// 本文件基于 EOS 非商用许可证 v1.1 发布，详见 LICENSE。
// 商业使用请联系版权人获得商业授权。

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/eosaios/eos/internal/ai"
	"github.com/eosaios/eos/internal/config"
	"github.com/eosaios/eos/internal/i18n"
	"github.com/eosaios/eos/internal/ui/styles"
	"github.com/eosaios/eos/pkg/coreapi"

	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// ModelSetupStep 模型设置步骤
type ModelSetupStep int

const (
	ModelSetupStepProvider ModelSetupStep = iota
	ModelSetupStepModel
	ModelSetupStepConfig
	ModelSetupStepCustom
	ModelSetupStepComplete
)

// ModelSetupView 模型添加向导视图
type ModelSetupView struct {
	width            int
	height           int
	styles           *styles.Styles
	step             ModelSetupStep
	providers        []*ai.ProviderConfig
	providerTable    table.Model
	models           []*ai.ModelCatalogEntry
	modelTable       table.Model
	inputs           []textinput.Model
	focusIndex       int
	selectedProvider *ai.ProviderConfig
	selectedModel    *ai.ModelCatalogEntry
	providerFocused  bool
	modelFocused     bool
	customProvider   bool   // 是否选择自定义服务商
	customModel      bool   // 是否选择自定义模型
	apiBaseReadOnly  bool   // API Base 是否只读
	modelReadOnly    bool   // Model 名称是否只读（套餐类 preset 为 false，可从套餐模型中选择）
	errMsg           string // 保存校验错误（非空时显示在配置步骤底部）
	language         string

	editMode         bool   // 编辑已有条目（跳过向导前几步）
	editOriginalName string // 编辑时的原始条目名（upsert 键）

	existingNames map[string]bool // 已配置条目名（默认显示名去重用）
	planIndex     int             // 套餐内模型当前选中索引（选择式切换）
}

// ModelSetupConfig 模型设置配置
type ModelSetupConfig struct {
	Name    string
	APIBase string
	APIKey  string
	Model   string
}

// NewModelSetupWizard 创建新的模型添加向导视图
func NewModelSetupWizard(styles *styles.Styles, lang string) *ModelSetupView {
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

	// 创建服务商表格
	providerTable := table.New(
		table.WithHeight(15),
		table.WithStyles(s),
		table.WithFocused(true),
	)

	// 创建模型表格
	modelTable := table.New(
		table.WithHeight(15),
		table.WithStyles(s),
		table.WithFocused(true),
	)

	// 创建输入框
	inputs := make([]textinput.Model, 4)
	for i := range inputs {
		inputs[i] = textinput.New()
		inputs[i].SetWidth(50)
	}
	inputs[1].EchoMode = textinput.EchoPassword // API Key

	v := &ModelSetupView{
		styles:          styles,
		step:            ModelSetupStepProvider,
		providers:       ai.GetAllProviders(),
		providerTable:   providerTable,
		modelTable:      modelTable,
		inputs:          inputs,
		focusIndex:      0,
		providerFocused: true,
		modelFocused:    false,
		customProvider:  false,
		customModel:     false,
		apiBaseReadOnly: false,
		modelReadOnly:   false,
		language:        lang,
	}

	slog.Info("NewModelSetupWizard", "inputs_len", len(inputs))

	v.updateTableColumns()
	v.loadProviders()
	return v
}

func (v *ModelSetupView) updateTableColumns() {
	// Provider Table
	providerColumns := []table.Column{
		{Title: i18n.T("setup.col.provider", v.language), Width: 18},
		{Title: i18n.T("setup.col.website", v.language), Width: 28},
		{Title: i18n.T("setup.col.env", v.language), Width: 18},
		{Title: i18n.T("setup.col.recommend", v.language), Width: 8},
	}
	v.providerTable.SetColumns(providerColumns)

	// Model Table
	modelColumns := []table.Column{
		{Title: i18n.T("setup.col.model", v.language), Width: 32},
		{Title: i18n.T("setup.col.context", v.language), Width: 10},
		{Title: i18n.T("setup.col.capability", v.language), Width: 12},
		{Title: i18n.T("setup.col.tags", v.language), Width: 22},
	}
	v.modelTable.SetColumns(modelColumns)
}

// loadProviders 加载服务商列表
func (v *ModelSetupView) loadProviders() {
	v.providers = ai.GetAllProviders() // 确保获取最新列表
	rows := make([]table.Row, 0, len(v.providers)+1)
	for _, p := range v.providers {
		recommended := ""
		if len(p.DefaultModels) > 0 {
			recommended = "★"
		}
		rows = append(rows, table.Row{providerDisplayName(p, v.language), p.Website, p.APIKeyEnv, recommended})
	}
	// 添加"自定义"选项
	rows = append(rows, table.Row{i18n.T("setup.custom", v.language), "-", "-", ""})
	v.providerTable.SetRows(rows)
}

// providerDisplayName 把 ID 为 "custom" 的内置 provider 显示为当前语言的"自定义"标签，
// 其余 provider 直接用其原始名称。rust_catalog 在 ai 包没有 language 上下文，
// 只能在这里按当前语言渲染。
func providerDisplayName(p *ai.ProviderConfig, language string) string {
	if p != nil && p.ID == "custom" {
		return i18n.T("setup.custom", language)
	}
	return p.Name
}

// loadModels 加载指定服务商的模型列表
func (v *ModelSetupView) loadModels(provider *ai.ProviderConfig) {
	if provider == nil {
		slog.Error("loadModels called with nil provider")
		return
	}
	v.models = ai.GetModelsByProvider(provider.Type)
	rows := make([]table.Row, 0, len(v.models))
	for _, m := range v.models {
		window := ai.GetCatalogContextWindow(m)
		ctx := "-"
		if window > 0 {
			ctx = fmt.Sprintf("%d", window)
		}
		if window >= 1000 {
			ctx = fmt.Sprintf("%.1fK", float64(window)/1000)
		}
		caps := modelCapabilityLabel(m, v.language)
		tags := strings.Join(m.Tags, ", ")
		rows = append(rows, table.Row{m.Name, ctx, caps, tags})
	}
	// 添加"自定义"选项
	rows = append(rows, table.Row{i18n.T("setup.custom", v.language), "-", "-", "-"})
	v.modelTable.SetRows(rows)
	// 重置光标到首位
	v.modelTable.SetCursor(0)
	slog.Info("loadModels", "provider", provider.Name, "models_len", len(v.models), "inputs_len", len(v.inputs))
}

// modelCapabilityLabel 把模型能力渲染成紧凑的列单元格文本。
// 中文用「视/理/工」，英文用「V/R/T」，按 视觉→推理→工具 顺序拼接，无能力则显示“-”。
func modelCapabilityLabel(m *ai.ModelCatalogEntry, language string) string {
	var parts []string
	zh := language == "zh"
	if m.SupportsVision {
		if zh {
			parts = append(parts, "视")
		} else {
			parts = append(parts, "V")
		}
	}
	if m.SupportsReasoningEffort {
		if zh {
			parts = append(parts, "理")
		} else {
			parts = append(parts, "R")
		}
	}
	if m.SupportsTools {
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

// SetSize 设置大小
func (v *ModelSetupView) SetSize(width, height int) {
	v.width = width
	v.height = height

	providerWidth := width - 12
	modelWidth := width - 12
	if providerWidth < 20 {
		providerWidth = 20
	}
	if modelWidth < 20 {
		modelWidth = 20
	}

	v.providerTable.SetWidth(providerWidth)
	v.modelTable.SetWidth(modelWidth)

	for i := range v.inputs {
		v.inputs[i].SetWidth(width - 20)
	}
}

// SetExistingNames 注入已配置条目名列表：进入配置步骤时默认显示名与其
// 冲突会自动加序号后缀，避免「已存在同名模型」保存失败（用户重添套餐时
// 曾反复撞名）。
func (v *ModelSetupView) SetExistingNames(names []string) {
	v.existingNames = make(map[string]bool, len(names))
	for _, n := range names {
		v.existingNames[strings.TrimSpace(n)] = true
	}
}

// dedupeName 确保 base 在 existingNames 中不冲突，冲突则追加 -2/-3…。
func (v *ModelSetupView) dedupeName(base string) string {
	if v.existingNames == nil || !v.existingNames[base] {
		return base
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s-%d", base, i)
		if !v.existingNames[candidate] {
			return candidate
		}
	}
}

// hasPlanChoice 当前选中 preset 是否提供套餐内多模型选择。
func (v *ModelSetupView) hasPlanChoice() bool {
	return v.selectedModel != nil && len(v.selectedModel.PlanModels) > 1
}

// currentPlanModel 返回套餐内当前选中的模型（无套餐选择时 nil）。
func (v *ModelSetupView) currentPlanModel() *coreapi.PlanModel {
	if !v.hasPlanChoice() || v.planIndex < 0 || v.planIndex >= len(v.selectedModel.PlanModels) {
		return nil
	}
	return &v.selectedModel.PlanModels[v.planIndex]
}

// cyclePlanModel 左右切换套餐内模型并同步到表单值（handleSave 直接读 inputs[3]）。
func (v *ModelSetupView) cyclePlanModel(delta int) {
	if !v.hasPlanChoice() {
		return
	}
	n := len(v.selectedModel.PlanModels)
	v.planIndex = ((v.planIndex+delta)%n + n) % n
	if pm := v.currentPlanModel(); pm != nil {
		v.inputs[3].SetValue(pm.ModelID)
	}
}

// LoadForEdit 进入编辑模式：预填 API Base / Model，API Key 留空表示保持不变。
func (v *ModelSetupView) LoadForEdit(entry config.ModelEntry) {
	v.editMode = true
	v.editOriginalName = strings.TrimSpace(entry.Name)
	v.step = ModelSetupStepCustom
	v.customProvider = true
	v.customModel = false
	v.apiBaseReadOnly = false
	v.modelReadOnly = false
	v.errMsg = ""

	v.inputs[0].SetValue(v.editOriginalName)
	v.inputs[0].Placeholder = i18n.T("setup.label.display_name", v.language)
	v.inputs[1].SetValue(strings.TrimSpace(entry.APIBase))
	v.inputs[1].Placeholder = i18n.T("setup.label.api_base", v.language)
	v.inputs[2].SetValue("")
	v.inputs[2].Placeholder = i18n.T("setup.label.api_key_keep", v.language)
	v.inputs[3].SetValue(strings.TrimSpace(entry.Model))
	v.inputs[3].Placeholder = i18n.T("setup.label.model_name", v.language)
	v.focusInput(1)
}

// configFocusOrder 返回配置步骤的焦点循环顺序（只含当前模式的可编辑字段）：
// 自定义服务商四个字段全可编辑；其余模式 API Base 只读（跳过 index 1）；
// 套餐类 preset 的模型为选择式（含 index 3），普通 preset 模型只读（不含 index 3）。
func (v *ModelSetupView) configFocusOrder() []int {
	switch {
	case v.customProvider && v.editMode:
		return []int{1, 2, 3}
	case v.customProvider:
		return []int{0, 1, 2, 3}
	case v.customModel || v.hasPlanChoice():
		return []int{0, 2, 3}
	default:
		return []int{0, 2}
	}
}

// focusInput 聚焦第 idx 个输入框并使其余输入框失焦。
// 进入/切换焦点的唯一入口，保证 focusIndex 与实际焦点一致。
func (v *ModelSetupView) focusInput(idx int) {
	for i := range v.inputs {
		if i == idx {
			v.inputs[i].Focus()
		} else {
			v.inputs[i].Blur()
		}
	}
	v.focusIndex = idx
}

// cycleConfigFocus 在焦点循环里按 delta（+1/-1）双向循环移动：
// Tab 在末尾回开头，Shift+Tab 在开头回末尾。
func (v *ModelSetupView) cycleConfigFocus(delta int) {
	order := v.configFocusOrder()
	pos := 0
	for i, idx := range order {
		if idx == v.focusIndex {
			pos = i
			break
		}
	}
	v.focusInput(order[(pos+delta+len(order))%len(order)])
}

// Init 初始化
func (v *ModelSetupView) Init() tea.Cmd {
	return textinput.Blink
}

// Update 更新
func (v *ModelSetupView) Update(msg tea.Msg) (*ModelSetupView, tea.Cmd) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("model_wizard.Update panic recovered",
				"step", v.step,
				"customProvider", v.customProvider,
				"customModel", v.customModel,
				"selectedProvider", v.selectedProvider,
				"selectedModel", v.selectedModel,
				"models_len", len(v.models),
				"inputs_len", len(v.inputs),
				"recover", r,
			)
		}
	}()

	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			switch v.step {
			case ModelSetupStepProvider:
				return v, func() tea.Msg { return SetupCancelMsg{} }
			case ModelSetupStepModel:
				// 返回服务商选择
				v.step = ModelSetupStepProvider
				v.providerFocused = true
				v.modelFocused = false
				// 重置模型表格光标到首位
				v.modelTable.SetCursor(0)
				return v, nil
			case ModelSetupStepConfig:
				// 返回模型选择
				v.step = ModelSetupStepModel
				v.modelFocused = true
				v.providerFocused = false
				return v, nil
			case ModelSetupStepCustom:
				if v.editMode {
					return v, func() tea.Msg { return SetupCancelMsg{} }
				}
				// 返回服务商选择
				v.step = ModelSetupStepProvider
				v.providerFocused = true
				v.modelFocused = false
				// 重置模型表格光标到首位
				v.modelTable.SetCursor(0)
				return v, nil
			}

		case "enter":
			switch v.step {
			case ModelSetupStepProvider:
				row := v.providerTable.Cursor()
				providers := ai.GetAllProviders()
				if row >= 0 && row < len(providers) {
					// 选择内置服务商
					v.selectedProvider = providers[row]
					v.step = ModelSetupStepModel
					v.loadModels(v.selectedProvider)
					v.providerFocused = false
					v.modelFocused = true
					v.customProvider = false
				} else if row == len(providers) {
					// 选择自定义服务商
					v.step = ModelSetupStepCustom
					v.customProvider = true
					v.customModel = false
					v.apiBaseReadOnly = false
					v.modelReadOnly = false
					if len(v.inputs) > 0 {
						v.inputs[0].Placeholder = i18n.T("setup.label.display_name", v.language)
					}
					if len(v.inputs) > 1 {
						v.inputs[1].Placeholder = i18n.T("setup.label.api_base", v.language)
					}
					if len(v.inputs) > 2 {
						v.inputs[2].Placeholder = i18n.T("setup.label.api_key", v.language)
					}
					if len(v.inputs) > 3 {
						v.inputs[3].Placeholder = i18n.T("setup.label.model_name", v.language)
					}
					v.focusInput(0)
					return v, nil
				}

			case ModelSetupStepModel:
				row := v.modelTable.Cursor()
				slog.Debug("ModelSetupStepModel enter",
					"row", row,
					"models_len", len(v.models),
					"selectedProvider", v.selectedProvider,
				)
				if row >= 0 && row < len(v.models) {
					// 选择内置模型
					model := v.models[row]
					if model == nil {
						slog.Error("Selected model is nil", "row", row)
						return v, nil
					}
					v.selectedModel = model
					v.step = ModelSetupStepConfig
					v.modelFocused = false
					v.customModel = false
					v.apiBaseReadOnly = true
					// 套餐类 preset（如 MiniMax Token Plan）内含多个可选模型，
					// 模型字段为选择式（←/→ 切换，见 updatePlanPickerUI）；
					// 普通 preset 固定不可改。
					v.modelReadOnly = len(model.PlanModels) == 0

					// 默认显示名：preset ID，与已有条目冲突时自动加序号，
					// 避免重复添加同一套餐时「已存在同名模型」保存失败。
					v.inputs[0].SetValue(v.dedupeName(v.selectedModel.ID))
					v.inputs[0].Placeholder = i18n.T("setup.label.display_name", v.language) + " (e.g., " + v.selectedModel.Name + ")"

					// 设置 API Base：按 (plan, format) 在服务商端点表里查，
					// 与内核 resolve_api_base 同一套规则（旧枚举特判只做回落）。
					base := v.selectedProvider.ResolveAPIBase(model.Plan, model.PlanFormat)
					if base == "" {
						base = ai.GetAPIBase(v.selectedProvider.Type, model.APIType, "")
					}
					v.inputs[1].SetValue(base)
					v.inputs[1].Placeholder = i18n.T("setup.label.api_base", v.language) + " (fixed)"

					v.inputs[2].SetValue("")
					v.inputs[2].Placeholder = v.selectedProvider.APIKeyEnv

					if len(model.PlanModels) > 0 {
						// 套餐类：默认选 preset 默认模型（选择式切换，非手输）。
						v.planIndex = 0
						defaultID := strings.TrimSpace(model.ModelName)
						for i, pm := range model.PlanModels {
							if strings.TrimSpace(pm.ModelID) == defaultID {
								v.planIndex = i
								break
							}
						}
						v.inputs[3].SetValue(model.PlanModels[v.planIndex].ModelID)
					} else {
						v.inputs[3].SetValue(v.selectedModel.ModelName)
						v.inputs[3].Placeholder = i18n.T("setup.label.model_name", v.language) + " (fixed)"
					}

					v.focusInput(0)
				} else if row == len(v.models) {
					// 选择自定义模型
					v.step = ModelSetupStepConfig
					v.customModel = true
					v.apiBaseReadOnly = true // API Base 固定为服务商的默认值
					v.modelReadOnly = false  // 模型名称可编辑

					// 设置显示名称
					v.inputs[0].SetValue("")
					v.inputs[0].Placeholder = i18n.T("setup.label.display_name", v.language)

					// 设置 API Base（固定为服务商默认值）
					base := v.selectedProvider.DefaultAPIBase
					v.inputs[1].SetValue(base)
					v.inputs[1].Placeholder = i18n.T("setup.label.api_base", v.language) + " (fixed)"

					v.inputs[2].SetValue("")
					v.inputs[2].Placeholder = v.selectedProvider.APIKeyEnv

					v.inputs[3].SetValue("")
					v.inputs[3].Placeholder = i18n.T("setup.label.model_name", v.language)

					v.focusInput(0)
				}

			case ModelSetupStepConfig, ModelSetupStepCustom:
				// 保存配置
				return v, v.handleSave()
			}
		}
	}

	// 根据当前步骤更新相应的组件
	switch v.step {
	case ModelSetupStepProvider:
		if v.providerFocused {
			var cmd tea.Cmd
			v.providerTable, cmd = v.providerTable.Update(msg)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}

	case ModelSetupStepModel:
		if v.modelFocused {
			var cmd tea.Cmd
			v.modelTable, cmd = v.modelTable.Update(msg)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}

	case ModelSetupStepConfig, ModelSetupStepCustom:
		// 套餐内模型：选择式（←/→ 或 h/l 切换），不转发按键给输入框；
		// Tab/Shift+Tab 仍走焦点循环，保证能从模型选择切回上面的字段。
		if !v.customProvider && !v.customModel && v.hasPlanChoice() && v.focusIndex == 3 {
			if keyMsg, ok := msg.(tea.KeyMsg); ok {
				switch keyMsg.String() {
				case "left", "h":
					v.cyclePlanModel(-1)
				case "right", "l":
					v.cyclePlanModel(1)
				case "tab":
					v.cycleConfigFocus(1)
				case "shift+tab":
					v.cycleConfigFocus(-1)
				}
			}
			return v, tea.Batch(cmds...)
		}

		var cmd tea.Cmd
		v.inputs[v.focusIndex], cmd = v.inputs[v.focusIndex].Update(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}

		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			switch keyMsg.String() {
			case "tab":
				v.cycleConfigFocus(1)
			case "shift+tab":
				v.cycleConfigFocus(-1)
			}
		}
	}

	return v, tea.Batch(cmds...)
}

// handleSave 处理保存操作
func (v *ModelSetupView) handleSave() tea.Cmd {
	displayName := v.inputs[0].Value()
	apiBase := v.inputs[1].Value()
	apiKey := v.inputs[2].Value()
	model := v.inputs[3].Value()

	// 套餐类 preset：模型必须是其 plan_models 白名单内的 ModelID
	//（内核 model/save 同样按白名单校验，这里前置拦截并提示）。
	if !v.customProvider && !v.customModel && v.selectedModel != nil && len(v.selectedModel.PlanModels) > 0 {
		valid := false
		for _, pm := range v.selectedModel.PlanModels {
			if strings.TrimSpace(model) == strings.TrimSpace(pm.ModelID) {
				valid = true
				break
			}
		}
		if !valid {
			ids := make([]string, 0, len(v.selectedModel.PlanModels))
			for _, pm := range v.selectedModel.PlanModels {
				ids = append(ids, pm.ModelID)
			}
			v.errMsg = fmt.Sprintf("%s: %s", i18n.T("setup.plan_model.invalid", v.language), strings.Join(ids, " / "))
			return nil
		}
	}
	v.errMsg = ""

	// 如果没有设置显示名称，使用默认值
	if displayName == "" {
		if v.editMode {
			displayName = v.editOriginalName
		} else if v.customProvider {
			displayName = fmt.Sprintf("model-%d", time.Now().Unix()%100000)
		} else if v.customModel {
			// 自定义模型：使用模型名称作为显示名称
			displayName = model
			if displayName == "" {
				displayName = fmt.Sprintf("custom-model-%d", time.Now().Unix()%100000)
			}
		} else {
			// 固定模型：使用选中的模型名称
			if v.selectedModel != nil {
				displayName = v.selectedModel.Name
			} else {
				displayName = fmt.Sprintf("model-%d", time.Now().Unix()%100000)
			}
		}
	}

	// Provider 字段：内置服务商使用服务商 ID，自定义使用"custom"
	providerID := ""
	presetID := ""
	if v.customProvider {
		providerID = "custom"
	} else {
		providerID = v.selectedProvider.ID
		if !v.customModel && v.selectedModel != nil {
			presetID = v.selectedModel.ID
		}
	}

	return func() tea.Msg {
		name := displayName
		if v.editMode {
			name = v.editOriginalName
		}
		return ModelFormCompleteMsg{
			Config: SetupConfig{
				Name:     name,
				Provider: providerID,
				PresetID: presetID,
				APIBase:  apiBase,
				APIKey:   apiKey,
				Model:    model,
			},
			EditMode: v.editMode,
		}
	}
}

// View 渲染
func (v *ModelSetupView) View() string {
	if v.width == 0 {
		v.width = 80
	}
	if v.height == 0 {
		v.height = 24
	}

	var content strings.Builder

	// 标题
	var title string
	switch v.step {
	case ModelSetupStepProvider:
		title = i18n.T("setup.step.provider", v.language)
	case ModelSetupStepModel:
		if v.selectedProvider != nil {
			title = fmt.Sprintf(i18n.T("setup.step.model", v.language), providerDisplayName(v.selectedProvider, v.language))
		} else {
			title = i18n.T("setup.step.provider", v.language)
		}
	case ModelSetupStepConfig:
		title = i18n.T("setup.step.config", v.language)
	case ModelSetupStepCustom:
		if v.editMode {
			title = i18n.T("setup.step.edit", v.language)
		} else {
			title = i18n.T("setup.step.custom", v.language)
		}
	default:
		title = i18n.T("models.action.add", v.language)
	}

	titleStyle := v.styles.PanelTitle.
		Width(v.width - 12).
		Align(lipgloss.Center)
	content.WriteString(titleStyle.Render(title))
	content.WriteString("\n\n")

	// 根据步骤渲染内容
	switch v.step {
	case ModelSetupStepProvider:
		content.WriteString(v.styles.Text.Render(i18n.T("setup.provider.select", v.language)))
		content.WriteString("\n\n")
		content.WriteString(v.providerTable.View())

	case ModelSetupStepModel:
		if v.selectedProvider != nil {
			content.WriteString(v.styles.Text.Render(fmt.Sprintf(i18n.T("setup.model.select", v.language), providerDisplayName(v.selectedProvider, v.language))))
			content.WriteString("\n\n")
			content.WriteString(v.modelTable.View())
		} else {
			content.WriteString(v.styles.Text.Render("Error: Provider not selected"))
		}

	case ModelSetupStepConfig, ModelSetupStepCustom:
		// 根据选择情况渲染不同的输入框
		if v.customProvider {
			if v.editMode {
				content.WriteString(v.styles.TextMuted.Render(i18n.T("setup.label.display_name", v.language) + " " + v.editOriginalName))
				content.WriteString("\n\n")
				labels := []string{
					i18n.T("setup.label.api_base", v.language),
					i18n.T("setup.label.api_key", v.language),
					i18n.T("setup.label.model_name", v.language),
				}
				for i, label := range labels {
					content.WriteString(v.styles.Text.Render(label))
					content.WriteString("\n")
					content.WriteString(v.inputs[i+1].View())
					content.WriteString("\n\n")
				}
			} else {
				// 自定义服务商：显示所有字段
				labels := []string{
					i18n.T("setup.label.display_name", v.language),
					i18n.T("setup.label.api_base", v.language),
					i18n.T("setup.label.api_key", v.language),
					i18n.T("setup.label.model_name", v.language),
				}
				for i, label := range labels {
					content.WriteString(v.styles.Text.Render(label))
					content.WriteString("\n")
					content.WriteString(v.inputs[i].View())
					content.WriteString("\n\n")
				}
			}
		} else if v.customModel {
			// 选择服务商 + 自定义模型：显示名称、API Key、模型名（API Base 固定）
			content.WriteString(v.styles.Text.Render(i18n.T("setup.label.display_name", v.language)))
			content.WriteString("\n")
			content.WriteString(v.inputs[0].View())
			content.WriteString("\n\n")

			content.WriteString(v.styles.TextMuted.Render(i18n.T("setup.label.api_base", v.language) + " " + v.inputs[1].Value() + " (fixed)"))
			content.WriteString("\n\n")

			content.WriteString(v.styles.Text.Render(i18n.T("setup.label.api_key", v.language)))
			content.WriteString("\n")
			content.WriteString(v.inputs[2].View())
			content.WriteString("\n\n")

			content.WriteString(v.styles.Text.Render(i18n.T("setup.label.model_name", v.language)))
			content.WriteString("\n")
			content.WriteString(v.inputs[3].View())
			content.WriteString("\n\n")
		} else {
			// 选择服务商 + 固定/套餐模型
			content.WriteString(v.styles.Text.Render(i18n.T("setup.label.display_name", v.language)))
			content.WriteString("\n")
			content.WriteString(v.inputs[0].View())
			content.WriteString("\n\n")

			content.WriteString(v.styles.TextMuted.Render(i18n.T("setup.label.api_base", v.language) + " " + v.inputs[1].Value() + " (fixed)"))
			content.WriteString("\n\n")

			content.WriteString(v.styles.Text.Render(i18n.T("setup.label.api_key", v.language)))
			content.WriteString("\n")
			content.WriteString(v.inputs[2].View())
			content.WriteString("\n\n")

			if v.modelReadOnly {
				// 普通 preset：模型只读
				content.WriteString(v.styles.TextMuted.Render(i18n.T("setup.label.model_name", v.language) + " " + v.inputs[3].Value() + " (fixed)"))
			} else if v.hasPlanChoice() {
				// 套餐类 preset：模型选择式切换（←/→），与桌面端选择器对齐。
				content.WriteString(v.styles.Text.Render(i18n.T("setup.plan_model.picker_label", v.language)))
				content.WriteString("\n")
				if pm := v.currentPlanModel(); pm != nil {
					ctx := "-"
					if pm.ContextWindow > 0 {
						ctx = fmt.Sprintf("%.0fK", float64(pm.ContextWindow)/1000)
					}
					content.WriteString(v.styles.TextInfo.Render(fmt.Sprintf("‹ %s ›  %s · %s", pm.ModelID, pm.Label, ctx)))
				}
				content.WriteString("\n")
				content.WriteString(v.styles.TextMuted.Render(i18n.T("setup.plan_model.picker_help", v.language)))
			} else {
				// 单一可选模型的套餐：展示当前值即可。
				content.WriteString(v.styles.TextMuted.Render(i18n.T("setup.label.model_name", v.language) + " " + v.inputs[3].Value() + " (fixed)"))
			}
			content.WriteString("\n\n")

			if v.errMsg != "" {
				content.WriteString(v.styles.TextError.Render(v.errMsg))
				content.WriteString("\n\n")
			}
		}
	}

	// 底部提示
	var hint string
	switch v.step {
	case ModelSetupStepProvider, ModelSetupStepModel:
		hint = i18n.T("setup.hint.provider", v.language)
	case ModelSetupStepConfig, ModelSetupStepCustom:
		if v.customProvider {
			hint = i18n.T("setup.hint.config", v.language)
		} else if v.customModel {
			hint = i18n.T("setup.hint.config", v.language)
		} else {
			hint = i18n.T("setup.hint.config", v.language)
		}
	}
	content.WriteString(v.styles.TextMuted.Render(hint))

	// 包装在面板样式中
	panelStyle := lipgloss.NewStyle().
		Width(v.width-4).
		Height(v.height-2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(v.styles.Theme.Primary).
		Padding(2, 4)

	return panelStyle.Render(content.String())
}
