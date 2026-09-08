package ui

// app_panels_models.go — 模型（models）面板的刷新与增删改。
//
// 本文件包含：
//   - refreshModelsPanel：从适配器加载模型列表并更新面板
//   - handleModelSelect / handleModelDelete / handleModelSyncEnv
//   - handleModelFormComplete：模型表单完成（添加或编辑）
//   - Update 中模型相关面板消息分支的处理方法
//
// 代码原位于 app.go，仅做物理拆分，不改行为。

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/eosaios/eos/internal/config"
	"github.com/eosaios/eos/internal/i18n"
	"github.com/eosaios/eos/internal/ui/panels"
	"github.com/eosaios/eos/internal/ui/views/setup"
	"github.com/eosaios/eos/pkg/coreapi"

	tea "charm.land/bubbletea/v2"
)

func (m *AppModel) refreshModelsPanel() {
	panel, ok := m.panels["models"].(*panels.ModelsPanel)
	if !ok || panel == nil || m.adapter == nil {
		return
	}
	models, active, err := m.adapter.ModelEntries(context.Background())
	if err != nil {
		m.appendSystem(fmt.Sprintf("%s: %v", m.localize("刷新模型列表失败", "Failed to refresh models"), err), "error")
		return
	}
	if snapshot, snapErr := m.adapter.ModelContext(context.Background()); snapErr == nil && strings.TrimSpace(snapshot.ResolvedModelName) != "" {
		active = strings.TrimSpace(snapshot.ResolvedModelName)
	}
	panel.SetModels(models, active)

	// 注入目录的套餐内可选模型（M3 / M2.7 等切换选择器数据源）。
	planMap := make(map[string][]coreapi.PlanModel)
	if catalog := m.adapter.ModelCatalogSnapshot(context.Background()); catalog != nil {
		for _, preset := range catalog.Presets {
			if len(preset.PlanModels) > 1 {
				planMap[strings.TrimSpace(preset.ID)] = preset.PlanModels
			}
		}
	}
	panel.SetPlanModels(planMap)
}

// handleModelSelect 处理模型选择，根据作用域显示不同的成功消息
func (m *AppModel) handleModelSelect(msg panels.ModelSelectMsg) {
	scope, err := m.adapter.SelectModelForCurrentContext(context.Background(), msg.Name)
	if err != nil {
		m.appendSystem(fmt.Sprintf("Failed to switch model: %s", msg.Name), "error")
		return
	}
	m.refreshModelsPanel()
	m.refreshShellWelcomeInfo()
	switch scope {
	case "session":
		m.appendSystem(fmt.Sprintf("Switched current session model: %s", msg.Name), "success")
	case "workspace":
		m.appendSystem(fmt.Sprintf("Switched workspace model: %s", msg.Name), "success")
	default:
		m.appendSystem(fmt.Sprintf("Switched global default model: %s", msg.Name), "success")
	}
}

// handleModelPlanSelect 套餐内切换模型：条目不动，仅 model/save 换套餐内
// 具体模型（如 MiniMax Token Plan 的 MiniMax-M3 ↔ MiniMax-M2.7），随后重新
// 激活条目让当前会话立即生效。
func (m *AppModel) handleModelPlanSelect(msg panels.ModelPlanSelectMsg) {
	ctx := context.Background()
	if err := m.adapter.SwitchPlanModel(ctx, msg.EntryName, msg.ModelID); err != nil {
		m.appendSystem(fmt.Sprintf("%s: %v", m.localize("切换套餐内模型失败", "Failed to switch plan model"), err), "error")
		return
	}
	scope, _ := m.adapter.SelectModelForCurrentContext(ctx, msg.EntryName)
	m.refreshModelsPanel()
	m.refreshShellWelcomeInfo()
	scopeLabel := map[string]string{
		"session":   m.localize("会话", "session"),
		"workspace": m.localize("工作区", "workspace"),
	}[scope]
	if scopeLabel == "" {
		scopeLabel = m.localize("全局", "global")
	}
	m.appendSystem(fmt.Sprintf("%s: %s → %s [%s]",
		m.localize("已切换套餐内模型", "Switched plan model"), msg.EntryName, msg.ModelID, scopeLabel), "success")
}

// handleModelDelete 处理模型删除
func (m *AppModel) handleModelDelete(msg panels.ModelDeleteMsg) {
	if err := m.adapter.DeleteModel(context.Background(), msg.Name); err != nil {
		m.appendSystem(fmt.Sprintf("Failed to delete model: %s (may be env model or active model)", msg.Name), "error")
		return
	}
	m.refreshModelsPanel()
	m.appendSystem(fmt.Sprintf("Deleted model: %s", msg.Name), "success")
}

// handleModelSyncEnv 处理环境变量同步
func (m *AppModel) handleModelSyncEnv() {
	if err := m.adapter.SyncEnvModel(context.Background()); err != nil {
		m.appendSystem(i18n.T("model.sync_env_failed", m.state.Language), "error")
		return
	}
	m.refreshModelsPanel()
	m.appendSystem(i18n.T("model.sync_env_ok", m.state.Language), "success")
}

// handleModelFormComplete 处理模型表单完成事件
// 根据编辑模式决定是更新还是添加模型
func (m *AppModel) handleModelFormComplete(msg setup.ModelFormCompleteMsg) {
	m.activeView = "shell"
	m.shell.FocusInput()
	// 初始设置流程中不显示成功消息
	suppressSuccessMessage := m.initialSetupFlow && len(m.history) == 0 && !msg.EditMode

	// 生成模型名称
	name := msg.Config.Name
	if name == "" {
		name = fmt.Sprintf("model-%d", time.Now().Unix()%100000)
	}

	if msg.EditMode {
		// 编辑模式：更新现有模型（旧 upsert 通道，向导暂无编辑入口）
		entry := config.ModelEntry{
			Name:    name,
			APIBase: msg.Config.APIBase,
			APIKey:  msg.Config.APIKey,
			Model:   msg.Config.Model,
			Source:  "user",
		}
		if err := m.adapter.UpsertModelEntry(context.Background(), entry); err != nil {
			m.appendSystem(fmt.Sprintf("Failed to update model: %s", name), "error")
		} else {
			m.appendSystem(fmt.Sprintf("Updated model: %s", name), "success")
		}
		m.refreshModelsPanel()
		return
	}

	// 添加模式：走内核 model/save（保留 provider/preset 关联，套餐类
	// preset 由内核按 (plan, format) 解析端点，对齐桌面端添加流程）。
	req := coreapi.ModelSaveRequest{
		Name:    name,
		APIKey:  strings.TrimSpace(msg.Config.APIKey),
		APIBase: strings.TrimSpace(msg.Config.APIBase),
		Model:   strings.TrimSpace(msg.Config.Model),
	}
	switch {
	case msg.Config.PresetID != "":
		req.Mode = "preset"
		req.ProviderID = msg.Config.Provider
		req.PresetID = msg.Config.PresetID
	case msg.Config.Provider == "custom":
		req.Mode = "custom_provider"
	default:
		req.Mode = "custom_model"
		req.ProviderID = msg.Config.Provider
	}

	if err := m.adapter.SaveModel(context.Background(), req); err != nil {
		m.appendSystem(fmt.Sprintf("Failed to add model: %s (%v)", name, err), "error")
	} else {
		scope, _ := m.adapter.SelectModelForCurrentContext(context.Background(), name)
		if scope == "session" {
			// 会话内新增仅写了 session 绑定；同步 workspace 默认，
			// 让后续新会话继承「最近添加」的模型（对齐桌面端语义）。
			_ = m.adapter.SelectWorkspaceModel(context.Background(), name)
		}
		m.refreshShellWelcomeInfo()
		if !suppressSuccessMessage {
			m.appendSystem(fmt.Sprintf("Added and selected model: %s", name), "success")
		}
	}
	m.initialSetupFlow = false

	// 刷新模型列表面板
	m.refreshModelsPanel()
}

// handleModelSelectMsg 处理 panels.ModelSelectMsg（Update 分支提取）。fall-through。
func (m *AppModel) handleModelSelectMsg(msg panels.ModelSelectMsg) (tea.Model, tea.Cmd) {
	m.handleModelSelect(msg)
	return m, m.finalizeUpdate(nil)
}

// handleModelPlanSelectMsg 处理 panels.ModelPlanSelectMsg（Update 分支提取）。fall-through。
func (m *AppModel) handleModelPlanSelectMsg(msg panels.ModelPlanSelectMsg) (tea.Model, tea.Cmd) {
	m.handleModelPlanSelect(msg)
	return m, m.finalizeUpdate(nil)
}

// handleModelEdit 打开模型编辑向导（预填现有配置）。
func (m *AppModel) handleModelEdit(msg panels.ModelEditMsg) {
	ctx := context.Background()
	entries, _, err := m.adapter.ModelEntries(ctx)
	if err != nil {
		m.appendSystem(fmt.Sprintf("%s: %v", m.localize("加载模型失败", "Failed to load models"), err), "error")
		return
	}
	var entry *config.ModelEntry
	for _, e := range entries {
		if e.Name == msg.Name {
			e2 := e
			entry = &e2
			break
		}
	}
	if entry == nil {
		m.appendSystem(fmt.Sprintf("%s: %s", m.localize("未找到模型", "Model not found"), msg.Name), "warning")
		return
	}
	if strings.TrimSpace(entry.Source) == "env" {
		m.appendSystem(i18n.T("models.edit.forbidden", m.state.Language), "warning")
		return
	}
	m.activeView = "setup"
	wizard := setup.NewModelSetupWizard(m.styles, m.state.Language)
	wizard.SetSize(m.width, m.height)
	wizard.LoadForEdit(*entry)
	m.setupView = wizard
}

// handleModelAddMsg 处理 panels.ModelAddMsg（Update 分支提取）。fall-through。
func (m *AppModel) handleModelAddMsg(_ panels.ModelAddMsg) (tea.Model, tea.Cmd) {
	// 添加模型 - 打开向导视图
	m.activeView = "setup"
	wizard := setup.NewModelSetupWizard(m.styles, m.state.Language)
	wizard.SetSize(m.width, m.height)
	if entries, _, err := m.adapter.ModelEntries(context.Background()); err == nil {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name)
		}
		wizard.SetExistingNames(names)
	}
	m.setupView = wizard
	return m, m.finalizeUpdate(nil)
}

// handleModelEditMsg 处理 panels.ModelEditMsg（Update 分支提取）。fall-through。
func (m *AppModel) handleModelEditMsg(msg panels.ModelEditMsg) (tea.Model, tea.Cmd) {
	m.handleModelEdit(msg)
	return m, m.finalizeUpdate(nil)
}

// handleModelDeleteMsg 处理 panels.ModelDeleteMsg（Update 分支提取）。fall-through。
func (m *AppModel) handleModelDeleteMsg(msg panels.ModelDeleteMsg) (tea.Model, tea.Cmd) {
	m.handleModelDelete(msg)
	return m, m.finalizeUpdate(nil)
}

// handleModelSyncMsg 处理 panels.ModelSyncMsg（Update 分支提取）。fall-through。
func (m *AppModel) handleModelSyncMsg(_ panels.ModelSyncMsg) (tea.Model, tea.Cmd) {
	m.handleModelSyncEnv()
	return m, m.finalizeUpdate(nil)
}

// handleModelRefreshMsg 处理 panels.ModelRefreshMsg（Update 分支提取）。fall-through。
func (m *AppModel) handleModelRefreshMsg(_ panels.ModelRefreshMsg) (tea.Model, tea.Cmd) {
	m.refreshModelsPanel()
	return m, m.finalizeUpdate(nil)
}

// handleModelFormCompleteMsg 处理 setup.ModelFormCompleteMsg（Update 分支提取）。fall-through。
func (m *AppModel) handleModelFormCompleteMsg(msg setup.ModelFormCompleteMsg) (tea.Model, tea.Cmd) {
	m.handleModelFormComplete(msg)
	return m, m.finalizeUpdate(nil)
}
