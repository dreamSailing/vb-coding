package ui

// Copyright (c) 2026 EOSAIOS
// SPDX-License-Identifier: EOS-NCL-1.1
// 本文件基于 EOS 非商用许可证 v1.1 发布，详见 LICENSE。
// 商业使用请联系版权人获得商业授权。

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	runpkg "runtime"
	"sort"
	"strings"
	"time"

	"github.com/eosaios/eos/internal/config"
	"github.com/eosaios/eos/internal/modes"
	"github.com/eosaios/eos/internal/ui/panels"
	"github.com/eosaios/eos/pkg/coreapi"

	tea "charm.land/bubbletea/v2"
)

func (m *AppModel) localize(zh, en string) string {
	if m != nil && strings.EqualFold(m.state.Language, "en") {
		return en
	}
	return zh
}

func (m *AppModel) currentWorkspaceRoot() string {
	if m != nil && m.adapter != nil {
		if root := strings.TrimSpace(m.adapter.ActiveWorkspace(context.Background())); root != "" {
			return root
		}
	}
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return wd
}

func isSupportedExecutionModeInput(raw string) bool {
	if strings.TrimSpace(raw) == "" {
		return false
	}
	normalized := modes.NormalizeExecutionMode(raw)
	for _, item := range modes.SupportedExecutionModes() {
		if item.Name == normalized {
			return true
		}
	}
	return false
}

func (m *AppModel) executionModeUsage() string {
	return m.localize(
		"用法: /permissions [auto|plan|access <mode>|approval <mode>]",
		"Usage: /permissions [auto|plan|access <mode>|approval <mode>]",
	)
}

func isSupportedAccessModeInput(raw string) bool {
	if strings.TrimSpace(raw) == "" {
		return false
	}
	normalized := modes.NormalizeAccessMode(raw)
	for _, item := range modes.SupportedAccessModes() {
		if item.Name == normalized {
			return true
		}
	}
	return false
}

func isSupportedApprovalModeInput(raw string) bool {
	if strings.TrimSpace(raw) == "" {
		return false
	}
	normalized := modes.NormalizeApprovalMode(raw)
	for _, item := range modes.SupportedApprovalModes() {
		if item.Name == normalized {
			return true
		}
	}
	return false
}

func (m *AppModel) openModelsPanel() {
	m.activeView = "panel"
	m.activePanel = "models"
	m.shell.ClearInput()
	m.refreshModelsPanel()
}

func (m *AppModel) openContextPanel() {
	m.activeView = "panel"
	m.activePanel = "context"
	m.shell.ClearInput()
	if panel, ok := m.panels["context"].(*panels.ContextPanel); ok && panel != nil {
		panel.ResetView()
	}
	m.refreshContextPanel()
}

func (m *AppModel) openMemoryPanel() {
	m.activeView = "panel"
	m.activePanel = "memory"
	m.shell.ClearInput()
	if panel, ok := m.panels["memory"].(*panels.MemoryPanel); ok && panel != nil {
		if panel.IsEditing() {
			panel.CancelEdit()
		}
	}
	m.refreshMemoryPanel()
}

func (m *AppModel) openSettingsPanel() {
	m.activeView = "panel"
	m.activePanel = "settings"
	m.shell.ClearInput()
	m.refreshSettingsPanel()
}

func (m *AppModel) handleWorkspaceSlash(args []string) tea.Cmd {
	if len(args) == 0 || strings.EqualFold(args[0], "list") {
		m.activeView = "panel"
		m.activePanel = "workspace"
		m.shell.ClearInput()
		m.refreshWorkspacePanel()
		return nil
	}

	sub := strings.ToLower(strings.TrimSpace(args[0]))
	if len(args) < 2 && (sub == "add" || sub == "remove" || sub == "use") {
		m.appendSystem(m.localize("用法: /workspace add|remove|use <path>", "Usage: /workspace add|remove|use <path>"), "warning")
		return nil
	}

	rawPath := strings.TrimSpace(args[1])
	path, err := resolveWorkspaceInputPath(rawPath, m.state.Language)
	if err != nil {
		m.appendSystem(err.Error(), "error")
		return nil
	}

	switch sub {
	case "add":
		fi, err := os.Stat(path)
		if err != nil || !fi.IsDir() {
			m.appendSystem(m.localize("路径不是目录: ", "Path is not a directory: ")+path, "warning")
			return nil
		}
		if err := m.adapter.AddWorkspace(context.Background(), path); err != nil {
			m.appendSystem(err.Error(), "error")
			return nil
		}
		m.refreshWorkspacePanel()
		m.appendSystem(m.localize("已添加工作区: ", "Added workspace: ")+path, "success")
	case "remove":
		if err := m.adapter.RemoveWorkspace(context.Background(), path); err != nil {
			m.appendSystem(err.Error(), "error")
			return nil
		}
		m.refreshWorkspacePanel()
		m.appendSystem(m.localize("已移除工作区: ", "Removed workspace: ")+path, "success")
	case "use":
		return m.handleWorkspaceUse(path)
	default:
		m.appendSystem(m.localize("用法: /workspace add|remove|use <path> 或 /workspace list", "Usage: /workspace add|remove|use <path> or /workspace list"), "warning")
	}
	return nil
}

func (m *AppModel) handleModelSlash(args []string) tea.Cmd {
	if len(args) == 0 {
		m.openModelsPanel()
		return nil
	}

	if strings.EqualFold(args[0], "current") {
		snapshot, err := m.adapter.ModelContext(context.Background())
		if err != nil {
			m.appendSystem(err.Error(), "error")
			return nil
		}
		modelName, modelBase := m.adapter.GetModelInfo()
		modelName = strings.TrimSpace(modelName)
		modelBase = strings.TrimSpace(modelBase)
		if modelName == "" {
			modelName = m.localize("未配置", "unconfigured")
		}
		if modelBase == "" {
			modelBase = m.localize("未配置", "unconfigured")
		}
		scopeLabel := map[string]string{
			"session":   m.localize("会话", "session"),
			"workspace": m.localize("工作区", "workspace"),
			"global":    m.localize("全局", "global"),
		}[strings.ToLower(strings.TrimSpace(snapshot.ResolvedScope))]
		if scopeLabel == "" {
			scopeLabel = m.localize("未解析", "unresolved")
		}
		m.appendSystem(fmt.Sprintf("%s: %s (%s) [%s]", m.localize("当前模型", "Current model"), strings.TrimSpace(modelName), strings.TrimSpace(modelBase), scopeLabel), "info")
		return nil
	}

	name := strings.TrimSpace(strings.Join(args, " "))
	if strings.EqualFold(args[0], "use") && len(args) > 1 {
		name = strings.TrimSpace(strings.Join(args[1:], " "))
	}
	if name == "" {
		m.appendSystem(m.localize("用法: /model [use <name>]", "Usage: /model [use <name>]"), "warning")
		return nil
	}

	// 归一化解析：条目名 / 模型 ID / 套餐内模型 label（如 MiniMax M3，桌面端
	// 历史数据形态）/ preset 名都接受，空格连字符不敏感；命中套餐内具体模型
	// 时顺带切换（M3 ↔ M2.7），对齐桌面端语义。
	res, err := m.adapter.ResolveModelInput(context.Background(), name)
	if err != nil {
		m.appendSystem(err.Error(), "error")
		return nil
	}
	if res.NeedsPlanSwitch {
		if switchErr := m.adapter.SwitchPlanModel(context.Background(), res.EntryName, res.PlanModelID); switchErr != nil {
			m.appendSystem(fmt.Sprintf("%s: %v", m.localize("切换套餐内模型失败", "Failed to switch plan model"), switchErr), "error")
			return nil
		}
	}
	name = res.EntryName

	scope, err := m.adapter.SelectModelForCurrentContext(context.Background(), name)
	if err != nil {
		m.appendSystem(err.Error(), "error")
		return nil
	}

	m.refreshModelsPanel()
	scopeLabel := map[string]string{
		"session":   m.localize("会话", "session"),
		"workspace": m.localize("工作区", "workspace"),
		"global":    m.localize("全局", "global"),
	}[strings.ToLower(strings.TrimSpace(scope))]
	if scopeLabel == "" {
		scopeLabel = m.localize("当前上下文", "current context")
	}
	if res.NeedsPlanSwitch {
		m.appendSystem(fmt.Sprintf("%s: %s (%s) [%s]", m.localize("已切换模型", "Switched model"), name, res.PlanModelID, scopeLabel), "success")
		return nil
	}
	m.appendSystem(fmt.Sprintf("%s: %s [%s]", m.localize("已切换模型", "Switched model"), name, scopeLabel), "success")
	return nil
}

func (m *AppModel) handleSessionSlash(args []string) tea.Cmd {
	if len(args) > 0 {
		switch strings.ToLower(strings.TrimSpace(args[0])) {
		case "save":
			id, err := m.adapter.SaveSessionMessages(context.Background(), "", m.sessionTranscript())
			if err != nil {
				m.appendSystem(err.Error(), "error")
				return nil
			}
			m.appendSystem(fmt.Sprintf("%s: %s", m.localize("已保存会话", "Saved session"), id), "success")
			return nil
		case "export":
			id := ""
			if len(args) >= 2 {
				id = strings.TrimSpace(args[1])
			}
			path := ""
			if len(args) >= 3 {
				path = strings.TrimSpace(args[2])
			} else if id != "" {
				path = filepath.Join(m.adapter.SessionsDir(context.Background()), id+".md")
			}
			if strings.TrimSpace(path) == "" {
				m.appendSystem(m.localize("用法: /session export <id> [outputPath]", "Usage: /session export <id> [outputPath]"), "warning")
				return nil
			}
			if err := m.adapter.ExportSessionMarkdown(context.Background(), id, path); err != nil {
				m.appendSystem(err.Error(), "error")
				return nil
			}
			m.appendSystem(fmt.Sprintf("%s: %s", m.localize("已导出会话", "Exported session"), path), "success")
			return nil
		}
	}

	metas, err := m.adapter.ListSessions(context.Background())
	if err != nil {
		m.appendSystem(err.Error(), "error")
		return nil
	}
	currentID, _ := m.adapter.CurrentSessionID(context.Background())
	if len(metas) == 0 {
		m.appendSystem(m.localize("暂无已保存会话。使用 /session save 保存当前会话。", "No saved sessions. Use /session save to persist the current session."), "info")
		return nil
	}

	lines := []string{
		m.localize("会话列表", "Sessions"),
		fmt.Sprintf("%s: %d", m.localize("总数", "Total"), len(metas)),
	}
	limit := len(metas)
	if limit > 12 {
		limit = 12
	}
	for i := 0; i < limit; i++ {
		meta := metas[i]
		ts := sessionTimestampLabel(meta)
		label := sessionLabelFromMeta(meta)
		prefix := " "
		if meta.ID == currentID {
			prefix = "*"
		}
		line := fmt.Sprintf("%s %s  %s  rounds=%d", prefix, meta.ID, ts, sessionRoundsFromMeta(meta))
		if label != "" {
			line += "  " + label
		}
		lines = append(lines, line)
	}
	lines = append(lines, m.localize("使用 /resume [id] 恢复，或 /session save 保存当前会话。", "Use /resume [id] to restore, or /session save to persist the current session."))
	m.appendSystem(strings.Join(lines, "\n"), "info")
	return nil
}

func (m *AppModel) handleResumeSlash(args []string) tea.Cmd {
	id := ""
	if len(args) > 0 {
		id = strings.TrimSpace(args[0])
	}
	if err := m.adapter.ResumeSession(context.Background(), id); err != nil {
		m.appendSystem(err.Error(), "error")
		return nil
	}

	resolvedID, _ := m.adapter.CurrentSessionID(context.Background())
	m.restoreSessionHistory(resolvedID)
	m.refreshContextPanel()
	m.refreshCostPanel()
	m.updateContextUsageUI()
	m.appendSystem(fmt.Sprintf("%s: %s", m.localize("已恢复会话", "Resumed session"), resolvedID), "success")
	return nil
}

func (m *AppModel) handlePermissionsSlash(args []string) tea.Cmd {
	if len(args) > 0 {
		switch {
		case len(args) == 1 && isSupportedExecutionModeInput(args[0]):
			mode := modes.NormalizeExecutionMode(args[0])
			if err := m.adapter.SetExecutionMode(context.Background(), mode); err != nil {
				m.appendSystem(err.Error(), "error")
				return nil
			}
			m.state.ExecutionMode = mode
			m.shell.SetExecutionMode(mode)
			m.appendSystem(fmt.Sprintf("%s %s", m.localize("执行模式已切换为", "Execution mode switched to"), m.executionModeLabel(mode)), "success")
		case len(args) >= 2 && strings.EqualFold(strings.TrimSpace(args[0]), "access") && isSupportedAccessModeInput(args[1]):
			mode := modes.NormalizeAccessMode(args[1])
			// 走 ApplyAccessMode 语义入口（danger 档 = 内核 enter_full_access
			// 双轴原子推进；其余档 = derive+set policy 真实裁决路径）——单独
			// SetAccessMode 只写 UI 快照，沙箱裁决不会跟着变。
			if err := m.adapter.ApplyAccessMode(context.Background(), mode); err != nil {
				m.appendSystem(err.Error(), "error")
				return nil
			}
			m.appendSystem(fmt.Sprintf("%s %s", m.localize("访问模式已切换为", "Access mode switched to"), mode), "success")
		case len(args) >= 2 && strings.EqualFold(strings.TrimSpace(args[0]), "approval") && isSupportedApprovalModeInput(args[1]):
			mode := modes.NormalizeApprovalMode(args[1])
			if err := m.adapter.SetApprovalMode(context.Background(), mode); err != nil {
				m.appendSystem(err.Error(), "error")
				return nil
			}
			m.appendSystem(fmt.Sprintf("%s %s", m.localize("审批模式已切换为", "Approval mode switched to"), mode), "success")
		default:
			m.appendSystem(m.executionModeUsage(), "warning")
			return nil
		}
	}

	snap, err := m.adapter.PermissionSnapshot(context.Background())
	if err != nil {
		m.appendSystem(err.Error(), "error")
		return nil
	}
	lines := []string{
		m.localize("权限与审批状态", "Permissions and approvals"),
		fmt.Sprintf("%s: %s", m.localize("执行模式", "Execution mode"), m.executionModeLabel(snap.ExecutionMode)),
		fmt.Sprintf("%s: %s", m.localize("访问模式", "Access mode"), snap.AccessMode),
		fmt.Sprintf("%s: %s", m.localize("审批模式", "Approval mode"), snap.ApprovalMode),
		fmt.Sprintf("%s: %s", m.localize("沙箱模式", "Sandbox mode"), snap.SandboxMode),
		fmt.Sprintf("%s: %t", m.localize("全局放行", "Allow all"), snap.AllowAll),
	}
	if len(snap.AllowedCategories) > 0 {
		lines = append(lines, fmt.Sprintf("%s: %s", m.localize("会话内已放行类别", "Session-allowed categories"), strings.Join(snap.AllowedCategories, ", ")))
	} else {
		lines = append(lines, fmt.Sprintf("%s: %s", m.localize("会话内已放行类别", "Session-allowed categories"), m.localize("无", "none")))
	}
	if snap.HasPendingDiff {
		target := snap.PendingDiffPath
		if strings.TrimSpace(target) == "" {
			target = m.localize("(未标记路径)", "(path unavailable)")
		}
		lines = append(lines, fmt.Sprintf("%s: %s", m.localize("待审批 diff", "Pending diff"), target))
	} else {
		lines = append(lines, fmt.Sprintf("%s: %s", m.localize("待审批 diff", "Pending diff"), m.localize("无", "none")))
	}
	if strings.TrimSpace(snap.LastAuthorization) != "" {
		lines = append(lines, fmt.Sprintf("%s: %s", m.localize("最近授权结果", "Last authorization"), snap.LastAuthorization))
		if strings.TrimSpace(snap.LastAuthorizationKind) != "" {
			lines = append(lines, fmt.Sprintf("%s: %s", m.localize("最近授权类别", "Last authorization kind"), snap.LastAuthorizationKind))
		}
		if strings.TrimSpace(snap.LastAuthorizationTarget) != "" {
			lines = append(lines, fmt.Sprintf("%s: %s", m.localize("最近升级目标", "Last escalation target"), snap.LastAuthorizationTarget))
		}
		if strings.TrimSpace(snap.LastAuthorizationNote) != "" {
			lines = append(lines, fmt.Sprintf("%s: %s", m.localize("最近授权说明", "Last authorization note"), snap.LastAuthorizationNote))
		}
	} else {
		lines = append(lines, fmt.Sprintf("%s: %s", m.localize("最近授权结果", "Last authorization"), m.localize("无", "none")))
	}
	m.appendSystem(strings.Join(lines, "\n"), "info")
	return nil
}

func (m *AppModel) handlePlanSlash(args []string) tea.Cmd {
	if len(args) > 0 && isSupportedExecutionModeInput(args[0]) {
		return m.handlePermissionsSlash(args)
	}

	items, _ := m.adapter.Todos(context.Background())
	modeSnapshot, _ := m.adapter.ModeSnapshot(context.Background())
	if strings.TrimSpace(modeSnapshot.ExecutionMode) == "" {
		modeSnapshot.ExecutionMode = m.state.ExecutionMode
	}
	lines := []string{
		m.localize("当前计划与待办", "Current plan and todos"),
		fmt.Sprintf("%s: %s", m.localize("执行模式", "Execution mode"), m.executionModeLabel(modeSnapshot.ExecutionMode)),
	}
	if len(items) == 0 {
		lines = append(lines, m.localize("暂无待办。", "No todo items."))
	} else {
		for idx, item := range items {
			line := fmt.Sprintf("%d. [%s] %s", idx+1, strings.TrimSpace(item.Status), strings.TrimSpace(item.Content))
			if item.ID != "" {
				line += " (" + item.ID + ")"
			}
			lines = append(lines, line)
		}
	}
	lines = append(lines, m.localize("使用 /plan auto|plan 可直接切换执行模式。", "Use /plan auto|plan to switch execution mode directly."))
	m.appendSystem(strings.Join(lines, "\n"), "info")
	return nil
}

func (m *AppModel) handleSkillsSlash(args []string) tea.Cmd {
	if len(args) > 0 && strings.EqualFold(args[0], "reload") {
		if err := m.adapter.ReloadSkills(context.Background()); err != nil {
			m.appendSystem(err.Error(), "error")
			return nil
		}
		m.appendSystem(m.localize("已重载 skills。", "Reloaded skills."), "success")
	}

	skills, err := m.adapter.Skills(context.Background())
	if err != nil {
		m.appendSystem(fmt.Sprintf("%s: %v", m.localize("读取 skills 失败", "Failed to read skills"), err), "error")
		return nil
	}
	sort.Slice(skills, func(i, j int) bool { return strings.ToLower(skills[i].Name) < strings.ToLower(skills[j].Name) })
	lines := []string{fmt.Sprintf("%s: %d", m.localize("Skills", "Skills"), len(skills))}
	if len(skills) == 0 {
		lines = append(lines, m.localize("暂无可用 skills。", "No skills available."))
	} else {
		for _, skill := range skills {
			prefix := " "
			if skill.Active {
				prefix = "*"
			}
			desc := strings.TrimSpace(skill.Description)
			if desc == "" {
				desc = m.localize("(无描述)", "(no description)")
			}
			origin := strings.TrimSpace(skill.Location)
			if origin == "" {
				origin = strings.TrimSpace(skill.BaseDir)
			}
			if origin == "" {
				origin = strings.TrimSpace(skill.Source)
			}
			lines = append(lines, fmt.Sprintf("%s %s [%s] - %s", prefix, skill.Name, blankFallback(origin, m.localize("unknown", "unknown")), desc))
		}
	}
	lines = append(lines, m.localize("使用 /skills reload 重新扫描并保留当前激活状态。", "Use /skills reload to rescan while preserving active skills."))
	m.appendSystem(strings.Join(lines, "\n"), "info")
	return nil
}

func (m *AppModel) handlePluginSlash(args ...string) tea.Cmd {
	// 子命令模式：install/list/remove
	if len(args) > 0 {
		switch args[0] {
		case "install":
			if len(args) < 2 {
				m.appendSystem("用法：/plugin install <本地路径或 git URL>", "warning")
				return nil
			}
			return m.pluginInstallCmd(args[1])
		case "remove":
			if len(args) < 2 {
				m.appendSystem("用法：/plugin remove <名称>", "warning")
				return nil
			}
			return m.pluginRemoveCmd(args[1])
		}
	}
	rows, err := m.adapter.Plugins(context.Background())
	if err != nil {
		m.appendSystem(fmt.Sprintf("%s: %v", m.localize("读取插件失败", "Failed to read plugins"), err), "error")
		return nil
	}
	sort.Slice(rows, func(i, j int) bool { return strings.ToLower(rows[i].Name) < strings.ToLower(rows[j].Name) })
	lines := []string{fmt.Sprintf("%s: %d", m.localize("插件", "Plugins"), len(rows))}
	if len(rows) == 0 {
		lines = append(lines, m.localize("暂无可用插件。", "No plugins available."))
	} else {
		for _, plugin := range rows {
			status := m.localize("enabled", "enabled")
			if !plugin.Enabled {
				status = m.localize("disabled", "disabled")
			}
			desc := strings.TrimSpace(plugin.Description)
			if desc == "" {
				desc = m.localize("(无描述)", "(no description)")
			}
			source := strings.TrimSpace(plugin.Source)
			if source == "" {
				source = strings.TrimSpace(plugin.Command)
			}
			lines = append(lines, fmt.Sprintf("- %s [%s, %s]: %s", plugin.Name, blankFallback(source, "plugin"), status, desc))
		}
	}
	m.appendSystem(strings.Join(lines, "\n"), "info")
	return nil
}

func (m *AppModel) handleReloadPluginsSlash() tea.Cmd {
	if err := m.adapter.Reload(); err != nil {
		m.appendSystem(fmt.Sprintf("%s: %v", m.localize("插件重载失败", "Plugin reload failed"), err), "error")
		return nil
	}
	m.refreshContextPanel()
	m.refreshMCPPanel()
	m.refreshLSPPanel()
	m.appendSystem(
		m.localize("已重载插件扩展与目录发现。", "Reloaded plugin extensions and discovery."),
		"success",
	)
	return nil
}

func (m *AppModel) handleDoctorSlash() tea.Cmd {
	modelName, modelBase := m.adapter.GetModelInfo()
	modelName = strings.TrimSpace(modelName)
	modelBase = strings.TrimSpace(modelBase)
	snap, _ := m.adapter.PermissionSnapshot(context.Background())
	sessions, _ := m.adapter.ListSessions(context.Background())
	currentSessionID, _ := m.adapter.CurrentSessionID(context.Background())
	bgTasks, _ := m.adapter.Tasks(context.Background())
	agentTasks, _ := m.adapter.Agents(context.Background())
	todos, _ := m.adapter.Todos(context.Background())
	traces, _ := m.adapter.ToolTraces(context.Background())
	stats, _ := m.adapter.ToolStats(context.Background())
	browser, _ := m.adapter.BrowserStatus(context.Background())
	skills, _ := m.adapter.Skills(context.Background())
	plugins, _ := m.adapter.Plugins(context.Background())

	lines := []string{
		m.localize("Doctor 摘要", "Doctor summary"),
		fmt.Sprintf("%s: %s", m.localize("工作区", "Workspace"), m.currentWorkspaceRoot()),
		fmt.Sprintf("%s: %s", m.localize("模型", "Model"), strings.TrimSpace(modelName)),
		fmt.Sprintf("%s: %s", m.localize("API Base", "API Base"), strings.TrimSpace(modelBase)),
		fmt.Sprintf("%s: %s", m.localize("执行模式", "Execution mode"), m.executionModeLabel(snap.ExecutionMode)),
		fmt.Sprintf("%s: %d", m.localize("保存会话数", "Saved sessions"), len(sessions)),
		fmt.Sprintf("%s: %s", m.localize("当前会话", "Current session"), blankFallback(currentSessionID, m.localize("无", "none"))),
		fmt.Sprintf("%s: %d", m.localize("后台任务", "Background tasks"), len(bgTasks)),
		fmt.Sprintf("%s: %d", m.localize("代理任务", "Agent tasks"), len(agentTasks)),
		fmt.Sprintf("%s: %d", m.localize("待办项", "Todo items"), len(todos)),
		fmt.Sprintf("%s: %d", m.localize("可用 skills", "Available skills"), len(skills)),
		fmt.Sprintf("%s: %d", m.localize("已注册插件", "Registered plugins"), len(plugins)),
		fmt.Sprintf("%s: %d", m.localize("工具追踪数", "Tool traces"), len(traces)),
		fmt.Sprintf("%s: %s", m.localize("内置浏览器", "Built-in browser"), m.browserStatusLabel(browser)),
	}
	if strings.TrimSpace(browser.LastError) != "" {
		lines = append(lines, fmt.Sprintf("%s: %s", m.localize("浏览器错误", "Browser error"), browser.LastError))
	}
	if len(stats) > 0 {
		lines = append(lines, m.localize("工具统计:", "Tool stats:"))
		sort.Slice(stats, func(i, j int) bool {
			if stats[i].TotalCalls == stats[j].TotalCalls {
				return stats[i].Tool < stats[j].Tool
			}
			return stats[i].TotalCalls > stats[j].TotalCalls
		})
		if len(stats) > 5 {
			stats = stats[:5]
		}
		for _, stat := range stats {
			lines = append(lines, fmt.Sprintf("- %s: calls=%d avg=%s", stat.Tool, stat.TotalCalls, stat.AvgDuration.Round(time.Millisecond)))
		}
	}
	if len(traces) > 0 {
		lines = append(lines, m.localize("最近工具时间线:", "Recent tool timeline:"))
		start := len(traces) - 5
		if start < 0 {
			start = 0
		}
		for _, trace := range traces[start:] {
			status := "ok"
			if !trace.Success {
				status = "error"
			}
			if trace.Cached {
				status += ",cached"
			}
			lines = append(lines, fmt.Sprintf("- %s [%s] %s", trace.Tool, status, trace.Duration.Round(time.Millisecond)))
		}
	}
	diag := strings.TrimSpace(m.adapter.LSPDiagnosticsMarkdown(context.Background()))
	if diag != "" {
		lines = append(lines, m.localize("LSP 诊断摘要:", "LSP diagnostics:"))
		lines = append(lines, truncateBlock(diag, 12, 1200))
	}
	m.appendSystem(strings.Join(lines, "\n"), "info")
	return nil
}

func (m *AppModel) handleDiffSlash(args []string) tea.Cmd {
	if len(args) == 0 {
		review, _ := m.adapter.PendingReview(context.Background())
		if strings.TrimSpace(review.Diff) != "" {
			target := strings.TrimSpace(review.Path)
			if target == "" {
				target = m.localize("(当前待审批改动)", "(current pending edit)")
			}
			m.appendSystemStyled(fmt.Sprintf("%s: %s\n%s", m.localize("待审批 diff", "Pending diff"), target, m.highlightDiffBlock(review.Diff, 40, 5000)), "info")
			return nil
		}
		changes, err := m.adapter.GitStatus(context.Background())
		if err != nil {
			m.appendSystem(err.Error(), "error")
			return nil
		}
		if len(changes) == 0 {
			m.appendSystem(m.localize("当前没有检测到 Git 改动。", "No Git changes detected."), "info")
			return nil
		}
		lines := []string{m.localize("当前改动文件", "Changed files")}
		for _, change := range changes {
			lines = append(lines, fmt.Sprintf("- [%s] %s", change.State, change.Path))
		}
		lines = append(lines, m.localize("使用 /diff <path> 查看某个文件的统一 diff。", "Use /diff <path> to inspect a file diff."))
		m.appendSystem(strings.Join(lines, "\n"), "info")
		return nil
	}

	path := strings.TrimSpace(args[0])
	diff, err := m.adapter.GitDiff(context.Background(), path)
	if err != nil {
		m.appendSystem(err.Error(), "error")
		return nil
	}
	if strings.TrimSpace(diff) == "" {
		m.appendSystem(fmt.Sprintf("%s: %s", m.localize("该文件没有差异", "No diff for file"), path), "info")
		return nil
	}
	m.appendSystemStyled(fmt.Sprintf("%s: %s\n%s", m.localize("文件 diff", "File diff"), path, m.highlightDiffBlock(diff, 80, 7000)), "info")
	return nil
}

func (m *AppModel) handleReviewSlash(args []string) tea.Cmd {
	lines := []string{m.localize("审查摘要", "Review summary")}
	if len(args) > 0 {
		path := strings.TrimSpace(args[0])
		if diff, err := m.adapter.GitDiff(context.Background(), path); err == nil && strings.TrimSpace(diff) != "" {
			lines = append(lines, fmt.Sprintf("%s: %s", m.localize("目标文件", "Target file"), path))
			lines = append(lines, m.highlightDiffBlock(diff, 40, 5000))
		} else if err != nil {
			lines = append(lines, fmt.Sprintf("%s: %v", m.localize("读取 diff 失败", "Failed to read diff"), err))
		}
	} else if review, _ := m.adapter.PendingReview(context.Background()); strings.TrimSpace(review.Diff) != "" {
		target := blankFallback(review.Path, m.localize("(当前待审批改动)", "(current pending edit)"))
		lines = append(lines, fmt.Sprintf("%s: %s", m.localize("待审改动", "Pending change"), target))
		lines = append(lines, m.highlightDiffBlock(review.Diff, 40, 5000))
	} else {
		changes, err := m.adapter.GitStatus(context.Background())
		if err == nil && len(changes) > 0 {
			lines = append(lines, m.localize("当前 Git 改动:", "Current Git changes:"))
			for _, change := range changes {
				lines = append(lines, fmt.Sprintf("- [%s] %s", change.State, change.Path))
			}
		}
	}

	diag := strings.TrimSpace(m.adapter.LSPDiagnosticsMarkdown(context.Background()))
	if diag != "" {
		lines = append(lines, m.localize("诊断:", "Diagnostics:"))
		lines = append(lines, truncateBlock(diag, 12, 1200))
	}

	traces, _ := m.adapter.ToolTraces(context.Background())
	if len(traces) > 0 {
		lines = append(lines, m.localize("最近工具调用:", "Recent tool calls:"))
		start := len(traces) - 5
		if start < 0 {
			start = 0
		}
		for _, trace := range traces[start:] {
			result := "ok"
			if !trace.Success {
				result = "error"
			}
			lines = append(lines, fmt.Sprintf("- %s [%s] %s", trace.Tool, result, trace.Duration.Round(time.Millisecond)))
		}
	}

	lines = append(lines, m.localize("建议：先看 /diff，再结合 /doctor 与 /tasks 判断是否需要继续审查。", "Tip: inspect /diff first, then combine /doctor and /tasks to continue the review flow."))
	// 含高亮 diff（ANSI），走 preStyled 通道避免宽度折行破坏转义序列。
	m.appendSystemStyled(strings.Join(lines, "\n"), "info")
	return nil
}

func (m *AppModel) handleGitSlash(args []string) tea.Cmd {
	if len(args) == 0 {
		args = []string{"status"}
	}

	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "status":
		changes, err := m.adapter.GitStatus(context.Background())
		if err != nil {
			m.appendSystem(err.Error(), "error")
			return nil
		}
		if len(changes) == 0 {
			m.appendSystem(m.localize("Git 工作区干净。", "Git working tree is clean."), "info")
			return nil
		}
		lines := []string{m.localize("Git 状态", "Git status")}
		for _, change := range changes {
			lines = append(lines, fmt.Sprintf("- [%s] %s", change.State, change.Path))
		}
		m.appendSystem(strings.Join(lines, "\n"), "info")
	case "branches":
		out, err := m.adapter.GitBranches(context.Background(), m.currentWorkspaceRoot())
		if err != nil {
			m.appendSystem(err.Error(), "error")
			return nil
		}
		lines := []string{fmt.Sprintf("%s: %s", m.localize("当前分支", "Current branch"), out.Current)}
		for _, branch := range out.Branches {
			lines = append(lines, "- "+branch)
		}
		m.appendSystem(strings.Join(lines, "\n"), "info")
	case "log":
		out, err := m.adapter.GitLog(context.Background(), coreapi.GitLogRequest{Limit: 20, Oneline: true})
		if err != nil {
			m.appendSystem(err.Error(), "error")
			return nil
		}
		m.appendSystem(fmt.Sprintf("%s\n%s", m.localize("Git 日志", "Git log"), truncateBlock(out.Text, 30, 3000)), "info")
	case "show":
		revision := "HEAD"
		path := ""
		if len(args) > 1 {
			revision = strings.TrimSpace(args[1])
		}
		if len(args) > 2 {
			path = strings.TrimSpace(args[2])
		}
		out, err := m.adapter.GitShow(context.Background(), coreapi.GitShowRequest{Revision: revision, Path: path})
		if err != nil {
			m.appendSystem(err.Error(), "error")
			return nil
		}
		m.appendSystem(fmt.Sprintf("%s %s\n%s", m.localize("Git 显示", "Git show"), revision, truncateBlock(out.Text, 40, 5000)), "info")
	case "diff":
		if len(args) < 2 {
			return m.handleDiffSlash(nil)
		}
		return m.handleDiffSlash(args[1:])
	default:
		m.appendSystem(m.localize("暂支持: /git status|branches|log|show|diff", "Supported: /git status|branches|log|show|diff"), "warning")
	}
	return nil
}

func (m *AppModel) sessionTranscript() []coreapi.SessionMessage {
	out := make([]coreapi.SessionMessage, 0, len(m.history))
	for _, item := range m.history {
		content := strings.TrimSpace(item.content)
		if content == "" {
			continue
		}
		msg := coreapi.SessionMessage{
			Content: content,
			Time:    item.timestamp,
		}
		switch item.kind {
		case "user":
			msg.Role = "user"
			msg.Type = "user"
		case "ai", "agent.final":
			msg.Role = "assistant"
			msg.Type = "assistant"
		case "system":
			msg.Role = "system"
			msg.Type = "system"
		default:
			continue
		}
		out = append(out, msg)
	}
	return out
}

func (m *AppModel) restoreSessionHistory(id string) {
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	m.cancelProcessingUI()
	m.history = m.history[:0]
	m.actionHits = nil
	m.shell.ClearContent()
	m.shell.ClearLive()

	messages, err := m.adapter.LoadSessionMessages(context.Background(), id)
	if err != nil {
		m.appendSystem(err.Error(), "error")
		return
	}

	// 按 metadata.turn_id 分组重建历史。内核把一个 turn 的多个 TurnItem 存成多条
	// SessionMessage（每条带 metadata.turn_id + metadata.kind）。同一 turn 的
	// assistant 消息合并成一个 ai entry（正文+思考），tool 消息作为 tool entry。
	// 无 turn_id 的消息（user/system）各成独立 entry。
	var pendingTurnID string
	var pendingTexts []string
	var pendingTime time.Time

	flushPending := func() {
		if len(pendingTexts) == 0 {
			pendingTurnID = ""
			return
		}
		entry := historyEntry{
			kind:      "ai",
			content:   strings.Join(pendingTexts, "\n\n"),
			timestamp: pendingTime,
		}
		if entry.timestamp.IsZero() {
			entry.timestamp = time.Now()
		}
		m.appendHistory(entry)
		pendingTexts = pendingTexts[:0]
		pendingTurnID = ""
	}

	for _, msg := range messages {
		turnID := sessionMessageTurnID(msg.Metadata)
		role := strings.ToLower(strings.TrimSpace(msg.Role))

		if turnID == "" {
			// 无 turn_id：user/system 独立 entry（和旧行为一致）。
			flushPending()
			content := strings.TrimSpace(msg.Content)
			if content == "" {
				continue
			}
			entry := historyEntry{content: content, timestamp: msg.Time}
			if entry.timestamp.IsZero() {
				entry.timestamp = time.Now()
			}
			switch role {
			case "assistant":
				entry.kind = "ai"
			case "system", "tool":
				entry.kind = "system"
				entry.level = "info"
			default:
				entry.kind = "user"
			}
			m.appendHistory(entry)
			continue
		}

		if turnID != pendingTurnID {
			flushPending()
			pendingTurnID = turnID
			pendingTime = msg.Time
		}

		kind := sessionMessageKind(msg.Metadata)
		switch {
		case role == "tool":
			// tool 结果：作为 tool entry 跟在 ai entry 后面。
			content := strings.TrimSpace(msg.Content)
			if content == "" {
				continue
			}
			toolName := sessionMessageToolName(msg.Metadata)
			entry := historyEntry{
				kind:      "tool",
				content:   content,
				toolName:  toolName,
				timestamp: msg.Time,
			}
			if entry.timestamp.IsZero() {
				entry.timestamp = time.Now()
			}
			m.appendHistory(entry)
		case kind == "reasoning":
			// 思考内容：归档为暗色摘要一行（与实时完成归档一致，同
			// eos-app 的「思考过程」折叠块）。renderHistoryEntry 会把
			// reasoning kind 渲染成 "💭 Thinking · Xs" + 末行摘要。
			content := strings.TrimSpace(msg.Content)
			if content == "" {
				continue
			}
			entry := historyEntry{
				kind:      "reasoning",
				content:   content,
				timestamp: msg.Time,
			}
			if entry.timestamp.IsZero() {
				entry.timestamp = time.Now()
			}
			m.appendHistory(entry)
		case kind == "status":
			// 状态提示（取消/失败等）：作为 system entry。
			content := strings.TrimSpace(msg.Content)
			if content == "" {
				continue
			}
			entry := historyEntry{
				kind:      "system",
				content:   content,
				timestamp: msg.Time,
			}
			if entry.timestamp.IsZero() {
				entry.timestamp = time.Now()
			}
			m.appendHistory(entry)
		case kind == "plan":
			// 计划文本：并入 ai entry。
			if content := strings.TrimSpace(msg.Content); content != "" {
				pendingTexts = append(pendingTexts, content)
			}
		default:
			// agent_message（无 kind）或 tool_call 的 assistant 占位消息。
			// tool_call 的 content 为空（工具名在 metadata.tool_call），跳过空 content。
			if content := strings.TrimSpace(msg.Content); content != "" {
				pendingTexts = append(pendingTexts, content)
			}
		}
	}
	flushPending()
}

// sessionMessageTurnID 从 SessionMessage.metadata 提取 turn_id。
func sessionMessageTurnID(metadata map[string]any) string {
	if len(metadata) == 0 {
		return ""
	}
	if v, ok := metadata["turn_id"]; ok {
		if s, ok := v.(string); ok {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

// sessionMessageKind 从 SessionMessage.metadata 提取 kind（reasoning/plan/status）。
func sessionMessageKind(metadata map[string]any) string {
	if len(metadata) == 0 {
		return ""
	}
	if v, ok := metadata["kind"]; ok {
		if s, ok := v.(string); ok {
			return strings.TrimSpace(strings.ToLower(s))
		}
	}
	return ""
}

// sessionMessageToolName 从 SessionMessage.metadata.tool_call 提取工具名。
func sessionMessageToolName(metadata map[string]any) string {
	if len(metadata) == 0 {
		return ""
	}
	tc, ok := metadata["tool_call"].(map[string]any)
	if !ok {
		return ""
	}
	if name, ok := tc["name"].(string); ok {
		return strings.TrimSpace(name)
	}
	return ""
}

func (m *AppModel) executionModeLabel(mode string) string {
	switch modes.NormalizeExecutionMode(mode) {
	case "plan":
		return "plan"
	default:
		return "auto"
	}
}

func normalizePlanPromptStyle(raw string) string {
	style := strings.TrimSpace(raw)
	if style == "" {
		return "concise"
	}
	lower := strings.ToLower(style)
	switch lower {
	case "concise", "detailed":
		return lower
	}
	if strings.HasPrefix(lower, "custom:") {
		body := strings.TrimSpace(style[len("custom:"):])
		if body == "" {
			return "concise"
		}
		return "custom:" + body
	}
	return "custom:" + style
}

func truncateBlock(text string, maxLines int, maxBytes int) string {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n"))
	text = strings.ReplaceAll(text, "\r", "\n")
	if maxBytes > 0 {
		runes := []rune(text)
		if len(runes) > maxBytes {
			text = string(runes[:maxBytes]) + "\n[truncated]"
		}
	}
	lines := strings.Split(text, "\n")
	if maxLines > 0 && len(lines) > maxLines {
		lines = append(lines[:maxLines], "[truncated]")
	}
	return strings.Join(lines, "\n")
}

func blankFallback(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func optionalIntText(value *int, fallback string) string {
	if value == nil {
		return fallback
	}
	return fmt.Sprintf("%d", *value)
}

func optionalFloatText(value *float64, fallback string) string {
	if value == nil {
		return fallback
	}
	return fmt.Sprintf("$%.6f", *value)
}

func (m *AppModel) handleStatusSlash() tea.Cmd {
	modelName, modelBase := m.adapter.GetModelInfo()
	modelName = strings.TrimSpace(modelName)
	modelBase = strings.TrimSpace(modelBase)
	snap, _ := m.adapter.PermissionSnapshot(context.Background())
	currentSessionID, _ := m.adapter.CurrentSessionID(context.Background())
	browser, _ := m.adapter.BrowserStatus(context.Background())

	lines := []string{
		m.localize("当前状态", "Status"),
		fmt.Sprintf("%s: %s", m.localize("工作区", "Workspace"), m.currentWorkspaceRoot()),
		fmt.Sprintf("%s: %s (%s)", m.localize("模型", "Model"), strings.TrimSpace(modelName), strings.TrimSpace(modelBase)),
		fmt.Sprintf("%s: %s", m.localize("执行模式", "Mode"), m.executionModeLabel(snap.ExecutionMode)),
		fmt.Sprintf("%s: %s", m.localize("访问模式", "Access mode"), snap.AccessMode),
		fmt.Sprintf("%s: %s", m.localize("审批模式", "Approval mode"), snap.ApprovalMode),
		fmt.Sprintf("%s: %s", m.localize("沙箱模式", "Sandbox mode"), snap.SandboxMode),
		fmt.Sprintf("%s: %s", m.localize("当前会话", "Session"), blankFallback(currentSessionID, m.localize("无", "none"))),
		fmt.Sprintf("%s: %s", m.localize("内置浏览器", "Built-in browser"), m.browserStatusLabel(browser)),
	}
	if strings.TrimSpace(snap.LastAuthorization) != "" {
		lines = append(lines, fmt.Sprintf("%s: %s", m.localize("最近授权", "Last authorization"), snap.LastAuthorization))
	}
	if strings.TrimSpace(snap.LastAuthorizationTarget) != "" {
		lines = append(lines, fmt.Sprintf("%s: %s", m.localize("最近升级目标", "Last escalation target"), snap.LastAuthorizationTarget))
	}
	if strings.TrimSpace(snap.LastAuthorizationNote) != "" {
		lines = append(lines, fmt.Sprintf("%s: %s", m.localize("最近授权说明", "Last authorization note"), snap.LastAuthorizationNote))
	}
	if remote, ok, _ := m.adapter.CurrentRemoteRepo(context.Background()); ok {
		lines = append(lines,
			fmt.Sprintf("%s: %s/%s", m.localize("远程仓库", "Remote repo"), remote.Owner, remote.Repo),
			fmt.Sprintf("%s: %s", m.localize("远程分支", "Remote branch"), blankFallback(remote.WorkingBranch, remote.DefaultBranch)),
			fmt.Sprintf("%s: %s", m.localize("远程目录", "Remote path"), remote.LocalPath),
		)
	}
	if strings.TrimSpace(browser.LastError) != "" {
		lines = append(lines, fmt.Sprintf("%s: %s", m.localize("浏览器错误", "Browser error"), browser.LastError))
	}

	// Context usage
	ctxWindowTokens, _ := m.adapter.ContextWindowTokens(context.Background())
	if ctxWindowTokens > 0 {
		lines = append(lines, fmt.Sprintf("%s: %d tokens", m.localize("上下文窗口", "Context window"), ctxWindowTokens))
	}

	m.appendSystem(strings.Join(lines, "\n"), "info")
	return nil
}

func (m *AppModel) handleRemoteSlash(args []string) tea.Cmd {
	_ = args
	remote, ok, err := m.adapter.CurrentRemoteRepo(context.Background())
	if err != nil {
		m.appendSystem(err.Error(), "error")
		return nil
	}
	if !ok {
		m.appendSystem(m.localize("当前没有活跃的远程仓库上下文。", "No active remote repository context."), "info")
		return nil
	}
	lines := []string{
		m.localize("远程仓库上下文", "Remote repository context"),
		fmt.Sprintf("%s: %s", m.localize("平台", "Platform"), remote.Platform),
		fmt.Sprintf("%s: %s/%s", m.localize("仓库", "Repository"), remote.Owner, remote.Repo),
		fmt.Sprintf("%s: %s", m.localize("地址", "URL"), remote.RepoURL),
		fmt.Sprintf("%s: %s", m.localize("当前分支", "Current branch"), blankFallback(remote.WorkingBranch, remote.DefaultBranch)),
		fmt.Sprintf("%s: %s", m.localize("本地目录", "Local path"), remote.LocalPath),
	}
	if strings.TrimSpace(remote.AccountLogin) != "" || strings.TrimSpace(remote.AccountName) != "" {
		lines = append(lines, fmt.Sprintf("%s: %s", m.localize("账号", "Account"), blankFallback(remote.AccountLogin, remote.AccountName)))
	}
	m.appendSystem(strings.Join(lines, "\n"), "info")
	return nil
}

func (m *AppModel) browserStatusLabel(status coreapi.BrowserRuntimeStatus) string {
	switch {
	case status.Running:
		return m.localize("运行中", fmt.Sprintf("running (%s)", status.BrowserKind))
	case status.LastError != "":
		return m.localize("异常", "error")
	default:
		return m.localize("未启动", "not running")
	}
}

func (m *AppModel) handleFastSlash() tea.Cmd {
	cfg, _ := config.Load()
	if cfg.FastModel == "" {
		m.appendSystem(m.localize("快速模型未配置。请在配置文件中设置 fast_model。", "Fast model not configured. Set fast_model in config."), "warning")
		return nil
	}

	snapshot, err := m.adapter.ModelContext(context.Background())
	if err != nil {
		m.appendSystem(err.Error(), "error")
		return nil
	}

	// Toggle fast mode by switching the current context, not the global active flag.
	if strings.TrimSpace(snapshot.ResolvedModelName) == strings.TrimSpace(cfg.FastModel) {
		target := strings.TrimSpace(cfg.Active)
		if target == "" || strings.EqualFold(target, cfg.FastModel) {
			for _, candidate := range []string{snapshot.WorkspaceModelName, snapshot.GlobalDefaultName} {
				candidate = strings.TrimSpace(candidate)
				if candidate != "" && !strings.EqualFold(candidate, cfg.FastModel) {
					target = candidate
					break
				}
			}
		}
		if target == "" || strings.EqualFold(target, cfg.FastModel) {
			m.appendSystem(m.localize("无法切换回标准模型：未找到 fast_model 之外的默认模型。", "Cannot switch back: no non-fast default model found."), "warning")
			return nil
		}
		if _, err := m.adapter.SelectModelForCurrentContext(context.Background(), target); err != nil {
			m.appendSystem(err.Error(), "error")
			return nil
		}
		m.refreshModelsPanel()
		m.refreshShellWelcomeInfo()
		m.appendSystem(fmt.Sprintf("%s: %s", m.localize("已切换回标准模型", "Switched back to standard model"), target), "success")
		return nil
	}
	if _, err := m.adapter.SelectModelForCurrentContext(context.Background(), cfg.FastModel); err != nil {
		m.appendSystem(err.Error(), "error")
		return nil
	}
	m.refreshModelsPanel()
	m.refreshShellWelcomeInfo()
	m.appendSystem(fmt.Sprintf("%s: %s", m.localize("已切换到快速模型", "Switched to fast model"), cfg.FastModel), "success")
	return nil
}

func (m *AppModel) handleExportSlash(args []string) tea.Cmd {
	format := "markdown"
	path := ""

	if len(args) > 0 {
		f := strings.ToLower(strings.TrimSpace(args[0]))
		if f == "json" || f == "markdown" || f == "md" {
			format = f
			if format == "md" {
				format = "markdown"
			}
		}
	}
	if len(args) > 1 {
		path = strings.TrimSpace(args[1])
	}

	currentID, _ := m.adapter.CurrentSessionID(context.Background())
	if currentID == "" {
		m.appendSystem(m.localize("没有当前会话可导出。", "No current session to export."), "warning")
		return nil
	}

	if path == "" {
		ext := ".md"
		if format == "json" {
			ext = ".json"
		}
		path = filepath.Join(m.adapter.SessionsDir(context.Background()), currentID+ext)
	}

	if format == "json" {
		// Export as JSON
		messages := m.sessionTranscript()
		data, err := json.MarshalIndent(messages, "", "  ")
		if err != nil {
			m.appendSystem(fmt.Sprintf("%s: %v", m.localize("导出失败", "Export failed"), err), "error")
			return nil
		}
		if err := os.WriteFile(path, data, 0644); err != nil {
			m.appendSystem(fmt.Sprintf("%s: %v", m.localize("导出失败", "Export failed"), err), "error")
			return nil
		}
	} else {
		if err := m.adapter.ExportSessionMarkdown(context.Background(), currentID, path); err != nil {
			m.appendSystem(fmt.Sprintf("%s: %v", m.localize("导出失败", "Export failed"), err), "error")
			return nil
		}
	}

	m.appendSystem(fmt.Sprintf("%s: %s (%s)", m.localize("已导出会话", "Exported session"), path, format), "success")
	return nil
}

func (m *AppModel) handleThemeSlash(args []string) tea.Cmd {
	if len(args) == 0 {
		// Show current theme
		s, err := m.adapter.Settings(context.Background())
		if err != nil {
			m.appendSystem(fmt.Sprintf("%s: %v", m.localize("读取主题失败", "Failed to read theme"), err), "error")
			return nil
		}
		current := s.Theme
		if current == "" {
			current = "dark"
		}
		m.appendSystem(fmt.Sprintf("%s: %s", m.localize("当前主题", "Current theme"), current), "info")
		return nil
	}

	theme := strings.ToLower(strings.TrimSpace(args[0]))
	s, err := m.adapter.Settings(context.Background())
	if err != nil {
		m.appendSystem(fmt.Sprintf("%s: %v", m.localize("读取主题失败", "Failed to read theme"), err), "error")
		return nil
	}
	s.Theme = theme
	if err := m.adapter.SaveSettings(context.Background(), s); err != nil {
		m.appendSystem(fmt.Sprintf("%s: %v", m.localize("保存主题失败", "Failed to save theme"), err), "error")
		return nil
	}
	m.state.Theme = theme
	m.applyTheme(theme)
	m.refreshSettingsPanel()
	m.appendSystem(fmt.Sprintf("%s: %s", m.localize("已切换主题", "Switched theme to"), theme), "success")
	return nil
}

func (m *AppModel) handlePlanStyleSlash(args []string) tea.Cmd {
	s, err := m.adapter.Settings(context.Background())
	if err != nil {
		m.appendSystem(fmt.Sprintf("%s: %v", m.localize("读取计划提示风格失败", "Failed to read plan prompt style"), err), "error")
		return nil
	}
	current := normalizePlanPromptStyle(s.PlanPromptStyle)
	if len(args) == 0 {
		m.appendSystem(fmt.Sprintf("%s: %s", m.localize("当前计划提示风格", "Current plan prompt style"), current), "info")
		return nil
	}

	raw := strings.TrimSpace(strings.Join(args, " "))
	if strings.EqualFold(strings.TrimSpace(args[0]), "custom") {
		if len(args) == 1 {
			m.appendSystem(m.localize("用法: /plan-style [concise|detailed|custom:<text>]", "Usage: /plan-style [concise|detailed|custom:<text>]"), "warning")
			return nil
		}
		raw = "custom:" + strings.TrimSpace(strings.Join(args[1:], " "))
	}
	normalized := normalizePlanPromptStyle(raw)
	s.PlanPromptStyle = normalized

	root := m.currentWorkspaceRoot()
	if strings.TrimSpace(root) == "" {
		m.appendSystem(m.localize("没有可用工作区，无法保存计划提示风格。", "No workspace is available; cannot save plan prompt style."), "warning")
		return nil
	}
	if err := m.adapter.SaveSettings(context.Background(), s); err != nil {
		m.appendSystem(fmt.Sprintf("%s: %v", m.localize("保存计划提示风格失败", "Failed to save plan prompt style"), err), "error")
		return nil
	}
	if settingsPanel, ok := m.panels["settings"].(*panels.SettingsPanel); ok && settingsPanel != nil {
		settingsPanel.SetSettings(&s)
	}

	m.appendSystem(fmt.Sprintf("%s: %s", m.localize("已设置计划提示风格", "Set plan prompt style to"), normalized), "success")
	return nil
}

func (m *AppModel) handleStatsSlash() tea.Cmd {
	stats, err := m.adapter.UsageSummary(context.Background())
	if err != nil {
		m.appendSystem(fmt.Sprintf("%s: %v", m.localize("读取统计失败", "Failed to read statistics"), err), "error")
		return nil
	}
	toolStats, err := m.adapter.ToolStats(context.Background())
	if err != nil {
		m.appendSystem(fmt.Sprintf("%s: %v", m.localize("读取工具统计失败", "Failed to read tool stats"), err), "error")
		return nil
	}

	lines := []string{
		m.localize("统计信息", "Statistics"),
		fmt.Sprintf("%s: %d", m.localize("对话轮数", "Rounds"), stats.Rounds),
		fmt.Sprintf("%s: %s", m.localize("输入 Tokens", "Input tokens"), optionalIntText(stats.InputTokens, m.localize("未知", "unknown"))),
		fmt.Sprintf("%s: %s", m.localize("输出 Tokens", "Reply tokens"), optionalIntText(stats.ReplyTokens, m.localize("未知", "unknown"))),
		fmt.Sprintf("%s: %s", m.localize("总 Tokens", "Total tokens"), optionalIntText(stats.TotalTokens, m.localize("未知", "unknown"))),
		fmt.Sprintf("%s: %s", m.localize("总成本", "Total cost"), optionalFloatText(stats.CostUSD, m.localize("未知", "unknown"))),
	}

	if len(toolStats) > 0 {
		lines = append(lines, m.localize("工具调用统计:", "Tool call stats:"))
		sort.Slice(toolStats, func(i, j int) bool { return toolStats[i].TotalCalls > toolStats[j].TotalCalls })
		for _, stat := range toolStats {
			lines = append(lines, fmt.Sprintf("  - %s: %d calls, avg %s", stat.Tool, stat.TotalCalls, stat.AvgDuration.Round(time.Millisecond)))
		}
	}

	m.appendSystem(strings.Join(lines, "\n"), "info")
	return nil
}

func (m *AppModel) handleRenameSlash(args []string) tea.Cmd {
	if len(args) == 0 {
		m.appendSystem(m.localize("用法: /rename <title>", "Usage: /rename <title>"), "warning")
		return nil
	}
	title := strings.TrimSpace(strings.Join(args, " "))
	currentID, _ := m.adapter.CurrentSessionID(context.Background())
	if currentID == "" {
		m.appendSystem(m.localize("没有当前会话。", "No current session."), "warning")
		return nil
	}
	if err := m.adapter.RenameSession(context.Background(), currentID, title); err != nil {
		m.appendSystem(err.Error(), "error")
		return nil
	}
	m.appendSystem(fmt.Sprintf("%s: %s", m.localize("已重命名会话", "Renamed session to"), title), "success")
	return nil
}

func (m *AppModel) handleShareSlash() tea.Cmd {
	currentID, _ := m.adapter.CurrentSessionID(context.Background())
	if currentID == "" {
		m.appendSystem(m.localize("没有当前会话可分享。", "No current session to share."), "warning")
		return nil
	}

	messages := m.sessionTranscript()
	var sb strings.Builder
	sb.WriteString("# Session: ")
	sb.WriteString(currentID)
	sb.WriteString("\n\n")
	for _, msg := range messages {
		role := strings.TrimSpace(msg.Role)
		if role == "" {
			role = "unknown"
		}
		fmt.Fprintf(&sb, "**%s**: %s\n\n", role, strings.TrimSpace(msg.Content))
	}

	content := sb.String()

	// Try to copy to clipboard
	if err := copyToClipboard(content); err != nil {
		// Fallback: save to file
		path := filepath.Join(m.adapter.SessionsDir(context.Background()), currentID+"_shared.md")
		_ = os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			m.appendSystem(fmt.Sprintf("%s: %v", m.localize("分享失败", "Share failed"), err), "error")
			return nil
		}
		m.appendSystem(fmt.Sprintf("%s: %s", m.localize("已保存到文件", "Saved to file"), path), "success")
		return nil
	}

	m.appendSystem(m.localize("已复制会话到剪贴板。", "Session copied to clipboard."), "success")
	return nil
}

func copyToClipboard(text string) error {
	// Try using clip command on Windows, pbcopy on macOS, xclip on Linux
	var cmd *exec.Cmd
	switch runpkg.GOOS {
	case "windows":
		cmd = exec.Command("clip")
	case "darwin":
		cmd = exec.Command("pbcopy")
	default:
		cmd = exec.Command("xclip", "-selection", "clipboard")
	}
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}

// applyTheme applies a theme by name
func (m *AppModel) applyTheme(name string) {
	// Theme is stored and will be applied on next render
	// The actual theme application happens in styles package
	m.updateContextUsageUI()
}

// sessionTimestampLabel 从 coreapi.Session.Metadata 提取保存时间；缺省回退 UpdatedAt。
func sessionTimestampLabel(meta coreapi.Session) string {
	if v := mapString(meta.Metadata, "saved_at"); v != "" {
		return v
	}
	if !meta.UpdatedAt.IsZero() {
		return meta.UpdatedAt.Format("2006-01-02 15:04")
	}
	return ""
}

// sessionLabelFromMeta 从 coreapi.Session.Metadata 中按 title→preview→summary 顺序取首。
func sessionLabelFromMeta(meta coreapi.Session) string {
	for _, key := range []string{"title", "preview", "summary"} {
		if v := strings.TrimSpace(mapString(meta.Metadata, key)); v != "" {
			return v
		}
	}
	return ""
}

// sessionRoundsFromMeta 从 coreapi.Session.Metadata 读 rounds，缺省 0。
func sessionRoundsFromMeta(meta coreapi.Session) int {
	return mapInt(meta.Metadata, "rounds")
}

func mapString(metadata map[string]any, key string) string {
	if len(metadata) == 0 {
		return ""
	}
	value, _ := metadata[key].(string)
	return strings.TrimSpace(value)
}

func mapInt(metadata map[string]any, key string) int {
	if len(metadata) == 0 {
		return 0
	}
	switch value := metadata[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	default:
		return 0
	}
}

func (m *AppModel) pluginInstallCmd(source string) tea.Cmd {
	go func() {
		var out struct {
			Name            string   `json:"name"`
			Version         string   `json:"version"`
			McpRegistered   bool     `json:"mcp_registered"`
			SkillsInstalled []string `json:"skills_installed"`
			NeedsConfirm    bool     `json:"needs_confirm"`
			Permissions     []string `json:"permissions"`
		}
		err := m.adapter.CallCore(
			context.Background(),
			"plugin/install",
			map[string]interface{}{"source": source},
			&out,
		)
		if err != nil {
			m.appendSystem(fmt.Sprintf("安装失败: %v", err), "error")
			return
		}

		// Phase 2 权限确认流：首次返回 needs_confirm=true，展示权限后
		// 用户在 TUI 按 y 确认 → 二次调用带 confirm_permissions=true
		if out.NeedsConfirm && len(out.Permissions) > 0 {
			var sb strings.Builder
			fmt.Fprintf(&sb, "🔒 插件 %s v%s 需要以下权限：\n", out.Name, out.Version)
			for _, perm := range out.Permissions {
				fmt.Fprintf(&sb, "  • %s\n", perm)
			}
			sb.WriteString("\n按 y 确认安装，其他键取消")
			m.appendSystem(sb.String(), "warning")
			m.setPendingPluginConfirm(source)
			return
		}

		if out.NeedsConfirm {
			// 无权限声明但 needs_confirm（不应发生）——直接装
			m.pluginInstallConfirm(source)
			return
		}

		msg := fmt.Sprintf("✅ 插件 %s v%s 已安装", out.Name, out.Version)
		if out.McpRegistered {
			msg += "（MCP 已注册）"
		}
		if len(out.SkillsInstalled) > 0 {
			msg += fmt.Sprintf("（技能: %s）", strings.Join(out.SkillsInstalled, ", "))
		}
		m.appendSystem(msg, "info")
	}()
	return nil
}

func (m *AppModel) pluginInstallConfirm(source string) {
	var out struct {
		Name            string   `json:"name"`
		Version         string   `json:"version"`
		McpRegistered   bool     `json:"mcp_registered"`
		SkillsInstalled []string `json:"skills_installed"`
	}
	err := m.adapter.CallCore(
		context.Background(),
		"plugin/install",
		map[string]interface{}{"source": source, "confirm_permissions": true},
		&out,
	)
	if err != nil {
		m.appendSystem(fmt.Sprintf("确认安装失败: %v", err), "error")
		return
	}
	msg := fmt.Sprintf("✅ 插件 %s v%s 已安装（权限已确认）", out.Name, out.Version)
	if out.McpRegistered {
		msg += "（MCP 已注册）"
	}
	m.appendSystem(msg, "info")
}

func (m *AppModel) pluginRemoveCmd(name string) tea.Cmd {
	go func() {
		err := m.adapter.CallCore(
			context.Background(),
			"plugin/remove",
			map[string]interface{}{"name": name},
			&struct{}{},
		)
		if err != nil {
			m.appendSystem(fmt.Sprintf("卸载失败: %v", err), "error")
			return
		}
		m.appendSystem(fmt.Sprintf("🗑️ 插件 %s 已卸载", name), "info")
	}()
	return nil
}

func (m *AppModel) setPendingPluginConfirm(source string) {
	m.pendingPluginConfirm = source
}

func (m *AppModel) clearPendingPluginConfirm() string {
	s := m.pendingPluginConfirm
	m.pendingPluginConfirm = ""
	return s
}

func (m *AppModel) pluginSearchCmd(query string) tea.Cmd {
	go func() {
		var out struct {
			Results []struct {
				Name        string   `json:"name"`
				Description string   `json:"description"`
				Version     string   `json:"version"`
				Author      string   `json:"author"`
				Permissions []string `json:"permissions"`
			} `json:"results"`
			Total int    `json:"total"`
			Index string `json:"index_url"`
		}
		params := map[string]interface{}{"query": query}
		err := m.adapter.CallCore(
			context.Background(),
			"plugin/search",
			params,
			&out,
		)
		if err != nil {
			m.appendSystem(fmt.Sprintf("搜索失败: %v", err), "error")
			return
		}
		if out.Total == 0 {
			m.appendSystem(fmt.Sprintf("未找到匹配「%s」的插件", query), "info")
			return
		}
		var sb strings.Builder
		fmt.Fprintf(&sb, "🔍 找到 %d 个插件：\n", out.Total)
		for _, p := range out.Results {
			perms := strings.Join(p.Permissions, ", ")
			if perms != "" {
				perms = fmt.Sprintf(" [权限: %s]", perms)
			}
			fmt.Fprintf(&sb, "  📦 %s v%s — %s（by %s）%s\n", p.Name, p.Version, p.Description, p.Author, perms)
		}
		sb.WriteString("\n用 /plugin install <名称> 安装")
		m.appendSystem(sb.String(), "info")
	}()
	return nil
}

// === /goal 目标模式 ===

// goalStatusLabel 把内核 goal 状态映射为双语标签。
func (m *AppModel) goalStatusLabel(status string) string {
	switch status {
	case "active":
		return m.localize("进行中（自驱）", "active (self-driving)")
	case "paused":
		return m.localize("已暂停", "paused")
	case "blocked":
		return m.localize("已阻塞（连续阻塞后停止）", "blocked")
	case "usageLimited":
		return m.localize("用量受限", "usage-limited")
	case "budgetLimited":
		return m.localize("预算耗尽（终态）", "budget-limited (terminal)")
	case "complete":
		return m.localize("已完成", "complete")
	default:
		return status
	}
}

// goalUsageText 拼目标用量行（预算存在时附剩余）。
func (m *AppModel) goalUsageText(goal coreapi.ThreadGoal) string {
	used := fmt.Sprintf("%d tokens / %ds", goal.TokensUsed, goal.TimeUsedSeconds)
	if goal.TokenBudget != nil {
		remaining := *goal.TokenBudget - goal.TokensUsed
		if remaining < 0 {
			remaining = 0
		}
		return fmt.Sprintf("%s / %s (≈%d)", used, fmt.Sprintf("%d", *goal.TokenBudget), remaining)
	}
	return used
}

// handleGoalSlash /goal set <目标...> [budget=N] | get | pause | resume | clear。
//
// set 后目标进入 active，agent 空闲自驱持续朝目标工作，直到完成（模型调
// update_goal complete）、阻塞、预算耗尽或用户暂停/清除。
func (m *AppModel) handleGoalSlash(args []string) tea.Cmd {
	if len(args) == 0 {
		return m.runGoalGet()
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "set":
		rest := strings.TrimSpace(strings.Join(args[1:], " "))
		if rest == "" {
			m.appendSystem(m.localize("用法：/goal set <目标文本> [budget=token数]", "Usage: /goal set <objective> [budget=tokens]"), "warning")
			return nil
		}
		var budget *int64
		// 末尾 budget=N（或 budget:N）显式预算语法；目标文本里的普通词不受影响。
		if idx := strings.LastIndex(rest, "budget="); idx >= 0 {
			raw := strings.TrimSpace(rest[idx+len("budget="):])
			if n, err := strconvParseInt64(raw); err == nil && n > 0 {
				budget = &n
				rest = strings.TrimSpace(rest[:idx])
			}
		}
		if rest == "" {
			m.appendSystem(m.localize("目标文本不能为空", "objective must not be empty"), "warning")
			return nil
		}
		goal, err := m.adapter.SetGoal(context.Background(), rest, budget)
		if err != nil {
			m.appendSystem(fmt.Sprintf("%s: %v", m.localize("设定目标失败", "Failed to set goal"), err), "error")
			return nil
		}
		m.appendSystem(fmt.Sprintf(
			"%s\n%s: %s\n%s",
			m.localize("🎯 目标已设定，agent 将持续工作直到完成（/goal pause 暂停，/goal clear 清除）", "Goal set; the agent will keep working until complete (/goal pause, /goal clear)"),
			m.localize("目标", "Objective"), goal.Objective,
			m.goalUsageText(goal),
		), "success")
		return nil
	case "get", "":
		return m.runGoalGet()
	case "pause":
		goal, err := m.adapter.PauseGoal(context.Background())
		if err != nil {
			m.appendSystem(fmt.Sprintf("%s: %v", m.localize("暂停目标失败", "Failed to pause goal"), err), "error")
			return nil
		}
		m.appendSystem(fmt.Sprintf("%s: %s", m.localize("目标已暂停，停止自驱", "Goal paused; self-driving stopped"), m.goalStatusLabel(goal.Status)), "success")
		return nil
	case "resume":
		goal, err := m.adapter.ResumeGoal(context.Background())
		if err != nil {
			m.appendSystem(fmt.Sprintf("%s: %v", m.localize("恢复目标失败", "Failed to resume goal"), err), "error")
			return nil
		}
		m.appendSystem(fmt.Sprintf("%s: %s", m.localize("目标已恢复，agent 继续朝目标工作", "Goal resumed; the agent continues toward the goal"), m.goalStatusLabel(goal.Status)), "success")
		return nil
	case "clear":
		if err := m.adapter.ClearGoal(context.Background()); err != nil {
			m.appendSystem(fmt.Sprintf("%s: %v", m.localize("清除目标失败", "Failed to clear goal"), err), "error")
			return nil
		}
		m.appendSystem(m.localize("目标已清除", "Goal cleared"), "success")
		return nil
	default:
		m.appendSystem(m.localize("用法：/goal [set <目标文本> [budget=token数]|get|pause|resume|clear]", "Usage: /goal [set <objective> [budget=tokens]|get|pause|resume|clear]"), "warning")
		return nil
	}
}

// runGoalGet 拉取并展示当前目标状态。
func (m *AppModel) runGoalGet() tea.Cmd {
	go func() {
		resp, err := m.adapter.GetGoal(context.Background())
		if err != nil {
			m.appendSystem(fmt.Sprintf("%s: %v", m.localize("查询目标失败", "Failed to get goal"), err), "error")
			return
		}
		if resp.Goal == nil {
			m.appendSystem(m.localize("当前会话没有目标。用 /goal set <目标文本> 开启目标模式。", "No goal set. Use /goal set <objective> to start goal mode."), "info")
			return
		}
		goal := *resp.Goal
		m.appendSystem(fmt.Sprintf(
			"%s\n%s: %s\n%s: %s\n%s",
			m.localize("🎯 当前提问目标", "Current goal"),
			m.localize("目标", "Objective"), goal.Objective,
			m.localize("状态", "Status"), m.goalStatusLabel(goal.Status),
			m.goalUsageText(goal),
		), "info")
	}()
	return nil
}

// strconvParseInt64 独立小函数（避免在 handler 里散落 strconv 引用）。
func strconvParseInt64(raw string) (int64, error) {
	var n int64
	if _, err := fmt.Sscanf(raw, "%d", &n); err != nil {
		return 0, err
	}
	return n, nil
}
