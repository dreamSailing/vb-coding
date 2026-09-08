package ui

// app_panels.go — 面板协调层（一）：context / memory / rules / settings / cost / lsp
// 面板的刷新函数，以及 Update 中各类面板消息分支的处理方法；还有
// ctxUsageTick / updateContextUsageUI / updateBGTaskCountUI 等面板相关的
// 定时刷新与辅助函数，rulesSnapshotDocument / memorySnapshotDocument /
// parseContextPreviewLine / estimateDisplayTokens / overlayCenter / handlePanelMsg
// 等工具函数，以及 LanguageChange / TasksTick / TaskToast / LSPRefresh /
// RulesRefresh / Cost / Settings / Context / Memory 等 Update 分支处理。
//
// 另一半面板逻辑（models / mcp / versions）见 app_panels_models.go、
// app_panels_mcp.go、app_panels_versions.go。
//
// 代码原位于 app.go，仅做物理拆分，不改行为。

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/eosaios/eos/internal/ai"
	"github.com/eosaios/eos/internal/config"
	"github.com/eosaios/eos/internal/i18n"
	"github.com/eosaios/eos/internal/pkg/settings"
	"github.com/eosaios/eos/internal/ui/panels"
	"github.com/eosaios/eos/pkg/coreapi"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// ctxUsageTickMsg 上下文使用率定时刷新消息
type ctxUsageTickMsg struct{}

// ctxUsageTickMsg_PanelsUnused 避免删除时误判。
var _ = ctxUsageTickMsg{}

// gitBranchRefreshInterval 是状态栏 git 概览刷新节流间隔（工作区变化时立即刷新）。
const gitBranchRefreshInterval = 15 * time.Second

// GitSummaryMsg 携带当前工作区所在 git 仓库概览（Branch 空 = 非 git 工作区）。
// 一次 git/summary 同时取分支、ahead/behind 与未提交数，供状态栏标记使用。
type GitSummaryMsg struct {
	Branch string
	Dirty  int
	Ahead  int
}

// maybeRefreshGitSummary 按节流策略发起 git 概览查询：工作区变化立即查，
// 否则距上次查询超过 gitBranchRefreshInterval 才查（避免每 tick 起子进程）。
func (m *AppModel) maybeRefreshGitSummary() tea.Cmd {
	if m == nil || m.adapter == nil {
		return nil
	}
	root := strings.TrimSpace(m.currentWorkspaceRoot())
	now := time.Now()
	if root == m.gitSummaryRoot && now.Sub(m.gitSummaryCheckedAt) < gitBranchRefreshInterval {
		return nil
	}
	m.gitSummaryRoot = root
	m.gitSummaryCheckedAt = now
	adapter := m.adapter
	return func() tea.Msg {
		result, err := adapter.GitSummary(context.Background(), root)
		if err != nil {
			// 非 git 工作区 / git 不可用：按 Codex 惯例省略显示，不报错。
			return GitSummaryMsg{Branch: ""}
		}
		return GitSummaryMsg{Branch: result.Branch, Dirty: len(result.Changes), Ahead: int(result.Ahead)}
	}
}

// handleGitSummaryMsg 把 git 概览写入状态栏。
func (m *AppModel) handleGitSummaryMsg(msg GitSummaryMsg) (tea.Model, tea.Cmd) {
	if m.shell != nil {
		m.shell.SetGitSummary(msg.Branch, msg.Dirty, msg.Ahead)
	}
	return m, m.finalizeUpdate(nil)
}

// ctxUsageTick 每 900ms 触发一次上下文使用率刷新
func (m *AppModel) ctxUsageTick() tea.Cmd {
	return tea.Tick(900*time.Millisecond, func(time.Time) tea.Msg { return ctxUsageTickMsg{} })
}

// updateContextUsageUI 从适配器获取当前上下文使用情况并更新 shell 显示
func (m *AppModel) updateContextUsageUI() {
	if m == nil || m.shell == nil || m.adapter == nil {
		return
	}
	// 有历史记录或正在处理时才显示上下文信息
	if len(m.history) > 0 || m.state.Processing {
		m.shell.SetContextVisible(true)
	}
	tokens, ratio, err := m.adapter.CurrentContextUsage(context.Background())
	if err != nil {
		return
	}
	m.shell.SetContextUsage(tokens, ratio)
}

// updateBGTaskCountUI 刷新后台任务数量显示
func (m *AppModel) updateBGTaskCountUI() {
	if m == nil || m.shell == nil || m.adapter == nil {
		return
	}
	tasks, err := m.adapter.Tasks(context.Background())
	if err != nil {
		return
	}
	m.shell.SetBGTaskCount(len(tasks))
}

// handlePanelMsg 处理面板消息
func (m *AppModel) handlePanelMsg(cmd tea.Cmd) tea.Cmd {
	if cmd == nil {
		return nil
	}
	return cmd
}

// refreshContextPanel 刷新上下文面板的数据
func (m *AppModel) refreshContextPanel() {
	if m == nil || m.adapter == nil {
		return
	}
	panel, ok := m.panels["context"].(*panels.ContextPanel)
	if !ok || panel == nil {
		return
	}

	ctx := context.Background()
	preview, err := m.adapter.ContextPreview(ctx)
	if err != nil {
		m.appendSystem(fmt.Sprintf("%s: %v", m.localize("刷新上下文失败", "Failed to refresh context"), err), "error")
		return
	}
	stats, err := m.adapter.ContextStats(ctx)
	if err != nil {
		m.appendSystem(fmt.Sprintf("%s: %v", m.localize("刷新上下文统计失败", "Failed to refresh context stats"), err), "error")
		return
	}

	model, _ := m.adapter.GetModelInfo()
	panel.SetStats(model, ai.ContextWindowTokens(model), 0, stats.Estimated)

	msgs := make([]panels.ContextMessage, 0, len(preview))
	for _, line := range preview {
		role, content := parseContextPreviewLine(line)
		if strings.TrimSpace(content) == "" {
			continue
		}
		msgs = append(msgs, panels.ContextMessage{
			Role:    role,
			Content: content,
			Tokens:  estimateDisplayTokens(content),
		})
	}
	panel.SetMessages(msgs)
}

// parseContextPreviewLine 解析上下文预览行，格式为 "role: content"
func parseContextPreviewLine(line string) (string, string) {
	line = strings.TrimSpace(line)
	role, content, ok := strings.Cut(line, ":")
	if !ok {
		return "message", line
	}
	role = strings.TrimSpace(role)
	if role == "" {
		role = "message"
	}
	return role, strings.TrimSpace(content)
}

// estimateDisplayTokens 估算内容的 token 数量（约 4 个字符一个 token）
func estimateDisplayTokens(content string) int {
	content = strings.TrimSpace(content)
	if content == "" {
		return 0
	}
	runes := len([]rune(content))
	tokens := (runes + 3) / 4
	if tokens < 1 {
		return 1
	}
	return tokens
}

// rulesSnapshotDocument 从规则快照中获取指定作用域的文档
func rulesSnapshotDocument(snapshot coreapi.RulesSnapshot, scope string) coreapi.RuleDocument {
	scope = strings.ToLower(strings.TrimSpace(scope))
	for _, doc := range snapshot.Documents {
		if strings.EqualFold(strings.TrimSpace(doc.Scope), scope) {
			return doc
		}
	}
	return coreapi.RuleDocument{Scope: scope}
}

// memorySnapshotDocument 从记忆快照中获取指定 scope 的文档（memory_summary.md /
// MEMORY.md），缺失时返回仅含 scope 的零值文档。
func memorySnapshotDocument(snapshot coreapi.MemorySnapshot, scopes ...string) coreapi.MemoryDocument {
	for _, scope := range scopes {
		scope = strings.ToLower(strings.TrimSpace(scope))
		for _, doc := range snapshot.Documents {
			if strings.EqualFold(strings.TrimSpace(doc.Scope), scope) {
				return doc
			}
		}
	}
	if len(scopes) > 0 {
		return coreapi.MemoryDocument{Scope: strings.ToLower(strings.TrimSpace(scopes[0]))}
	}
	return coreapi.MemoryDocument{}
}

// panelMemoryDoc 把 coreapi 文档投影为面板文档。
func panelMemoryDoc(doc coreapi.MemoryDocument) panels.MemoryDoc {
	return panels.MemoryDoc{
		Scope:   doc.Scope,
		Path:    doc.Path,
		Content: doc.Content,
		Exists:  doc.Exists,
	}
}

// refreshMemoryPanel 刷新记忆面板：memory_summary.md + MEMORY.md 只读快照。
func (m *AppModel) refreshMemoryPanel() {
	if m == nil || m.adapter == nil {
		return
	}
	panel, ok := m.panels["memory"].(*panels.MemoryPanel)
	if !ok || panel == nil {
		return
	}
	snap, err := m.adapter.MemorySnapshot(context.Background())
	if err != nil {
		panel.SetData(nil, nil)
		return
	}
	summary := memorySnapshotDocument(snap, "memory_summary.md")
	handbook := memorySnapshotDocument(snap, "MEMORY.md")
	panel.SetData([]panels.MemoryDoc{panelMemoryDoc(summary), panelMemoryDoc(handbook)}, panelProjectMemory(snap))
}

// panelProjectMemory 投影当前活动项目分区（snapshot.projects 至多一项）。
func panelProjectMemory(snapshot coreapi.MemorySnapshot) *panels.MemoryProject {
	if len(snapshot.Projects) == 0 {
		return nil
	}
	project := snapshot.Projects[0]
	var docs [2]panels.MemoryDoc
	for i, scope := range memoryDocScopesList() {
		docs[i] = panelMemoryDoc(memorySnapshotDocumentOf(project.Documents, scope))
	}
	return &panels.MemoryProject{
		Key:  project.Key,
		Root: project.Root,
		Name: project.Name,
		Docs: docs,
	}
}

// memoryDocScopesList 与内核 panel_snapshot 的文档顺序一致。
func memoryDocScopesList() [2]string {
	return [2]string{"memory_summary.md", "MEMORY.md"}
}

// memorySnapshotDocumentOf 从指定文档集合中取 scope 对应文档（缺失返回零值）。
func memorySnapshotDocumentOf(documents []coreapi.MemoryDocument, scope string) coreapi.MemoryDocument {
	scope = strings.ToLower(strings.TrimSpace(scope))
	for _, doc := range documents {
		if strings.EqualFold(strings.TrimSpace(doc.Scope), scope) {
			return doc
		}
	}
	return coreapi.MemoryDocument{Scope: scope}
}

// overlayCenter 将弹框内容居中叠加到底层视图之上。
// 采用 lipgloss.Place 按尺寸居中，背景仍透出底层 shell 文本流。
func overlayCenter(width, height int, background, overlay string) string {
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, overlay,
		lipgloss.WithWhitespaceChars(" "),
	)
}

func (m *AppModel) refreshLSPPanel() {
	p, ok := m.panels["lsp"].(*panels.LSPPanel)
	if !ok || p == nil {
		return
	}
	if m.adapter == nil {
		p.SetStatus(panels.LSPPanelSummary{Message: "no core"}, nil)
		return
	}
	servers, err := m.adapter.LSPServers(context.Background())
	if err != nil {
		p.SetStatus(panels.LSPPanelSummary{Message: err.Error()}, nil)
		return
	}
	sum := panels.LSPPanelSummary{
		Enabled:    true,
		AutoDetect: true,
		Workspace:  m.currentWorkspaceRoot(),
		Message:    "via JSON-RPC",
	}
	rows := make([]panels.LSPServerRow, 0, len(servers))
	for _, it := range servers {
		found := !strings.EqualFold(strings.TrimSpace(it.Status), "not_found")
		if strings.EqualFold(strings.TrimSpace(it.Status), "running") {
			sum.ActiveLanguage = strings.TrimSpace(it.Language)
			sum.ActiveServer = strings.TrimSpace(it.Command)
			sum.ActiveRoot = sum.Workspace
		}
		rows = append(rows, panels.LSPServerRow{
			Language: it.Language,
			Command:  it.Command,
			Found:    found,
		})
	}
	p.SetStatus(sum, rows)
}

func (m *AppModel) refreshRulesPanel() {
	p, ok := m.panels["rules"].(*panels.RulesPanel)
	if !ok || p == nil {
		return
	}
	if m.adapter == nil {
		p.SetData("", "", "", false, "", "", false)
		return
	}
	snapshot, err := m.adapter.RulesSnapshot(context.Background())
	if err != nil {
		p.SetData("", "", "", false, "", "", false)
		return
	}
	project := rulesSnapshotDocument(snapshot, "project")
	global := rulesSnapshotDocument(snapshot, "global")
	p.SetData(snapshot.ActiveRoot, project.Path, project.Content, project.Exists, global.Path, global.Content, global.Exists)
}

func (m *AppModel) refreshSettingsPanel() {
	if m == nil || m.adapter == nil {
		return
	}
	p, ok := m.panels["settings"].(*panels.SettingsPanel)
	if !ok || p == nil {
		return
	}
	settingsData, err := m.adapter.Settings(context.Background())
	if err == nil {
		p.SetSettings(&settingsData)
	}
	cfg, _ := config.Load()
	p.SetGlobalPredictionEnabled(config.NextMessagePredictionEnabled(&cfg))
	p.SetMemoryInjectionEnabled(config.MemoryInjectionEnabled(&cfg))
}

func (m *AppModel) handleRulesSave(msg panels.RulesSaveMsg) {
	if m == nil || m.adapter == nil {
		return
	}
	scope := strings.ToLower(strings.TrimSpace(msg.Scope))
	if scope == "" {
		scope = "project"
	}
	if err := m.adapter.SaveRules(context.Background(), scope, msg.Content); err != nil {
		m.appendSystem(fmt.Sprintf(i18n.T("toast.rules_save_failed", m.state.Language), err), "error")
		return
	}

	if scope == "global" {
		m.appendSystem(i18n.T("rules.saved_global", m.state.Language), "success")
	} else {
		m.appendSystem(i18n.T("rules.saved_project", m.state.Language), "success")
	}
	m.refreshRulesPanel()
}

// handleMemorySave 把「添加记忆笔记」内容经内核 memory/save 落为 ad_hoc note。
func (m *AppModel) handleMemorySave(msg panels.MemorySaveMsg) {
	if m == nil || m.adapter == nil {
		return
	}
	content := strings.TrimSpace(msg.Content)
	if content == "" {
		m.appendSystem(i18n.T("memory.note_empty", m.state.Language), "warning")
		return
	}
	if err := m.adapter.SaveMemory(context.Background(), msg.Scope, content); err != nil {
		m.appendSystem(fmt.Sprintf(i18n.T("toast.memory_save_failed", m.state.Language), err), "error")
		return
	}
	m.appendSystem(i18n.T("memory.saved", m.state.Language), "success")
}

// refreshCostPanel 刷新成本统计面板
// 获取成本明细和使用汇总，按模型聚合后更新面板
func (m *AppModel) refreshCostPanel() {
	if costPanel, ok := m.panels["cost"].(*panels.CostPanel); ok {
		ctx := context.Background()
		items, err := m.adapter.CostItems(ctx)
		if err != nil {
			m.appendSystem(fmt.Sprintf("%s: %v", m.localize("刷新成本明细失败", "Failed to refresh cost items"), err), "error")
			return
		}
		total, err := m.adapter.UsageSummary(ctx)
		if err != nil {
			m.appendSystem(fmt.Sprintf("%s: %v", m.localize("刷新成本统计失败", "Failed to refresh usage summary"), err), "error")
			return
		}

		// 按模型聚合成本数据
		modelStats := aggregateCostItemsByModel(items)
		stats := make([]panels.CostStats, 0, len(modelStats))
		for _, s := range modelStats {
			stats = append(stats, panels.CostStats{
				Model:  s.Model,
				Rounds: s.Rounds,
				Input:  m.optionalIntLabel(s.Input),
				Reply:  m.optionalIntLabel(s.Reply),
				Total:  m.optionalIntLabel(s.Total),
			})
		}

		costPanel.SetStats(stats, panels.TotalStats{
			TotalRounds: total.Rounds,
			TotalInput:  m.optionalIntLabel(total.InputTokens),
			TotalReply:  m.optionalIntLabel(total.ReplyTokens),
			TotalTokens: m.optionalIntLabel(total.TotalTokens),
		})
	}
}

// costModelAggregate 按模型聚合的成本统计
type costModelAggregate struct {
	Model  string // 模型名称
	Rounds int    // 调用轮次
	Input  *int   // 输入 token 数（可能为 nil）
	Reply  *int   // 回复 token 数（可能为 nil）
	Total  *int   // 总 token 数（可能为 nil）
}

// aggregateCostItemsByModel 按模型聚合成本统计数据
func aggregateCostItemsByModel(items []coreapi.CostItem) []costModelAggregate {
	byModel := map[string]*costModelAggregate{}
	for _, item := range items {
		model := strings.TrimSpace(item.Model)
		if model == "" {
			model = "unknown"
		}
		agg := byModel[model]
		if agg == nil {
			agg = &costModelAggregate{Model: model}
			byModel[model] = agg
		}
		agg.Rounds++
		agg.Input = addOptionalInt(agg.Input, item.InputTokens)
		agg.Reply = addOptionalInt(agg.Reply, item.ReplyTokens)
		agg.Total = addOptionalInt(agg.Total, item.TotalTokens)
	}
	out := make([]costModelAggregate, 0, len(byModel))
	for _, item := range byModel {
		out = append(out, *item)
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Model) < strings.ToLower(out[j].Model)
	})
	return out
}

// addOptionalInt 累加两个可选整数
func addOptionalInt(total *int, value *int) *int {
	if value == nil {
		return total
	}
	next := *value
	if total != nil {
		next += *total
	}
	return &next
}

// optionalIntLabel 将可选整数转换为显示标签
func (m *AppModel) optionalIntLabel(value *int) string {
	if value == nil {
		return m.localize("未知", "unknown")
	}
	return fmt.Sprintf("%d", *value)
}

// handleSettingsSave 处理设置保存
// 1. 保存配置到本地文件
// 2. 保存工作区设置到核心
// 3. 处理语言切换
// 4. 更新预测功能状态 / 记忆注入开关 / diff 高亮主题
func (m *AppModel) handleSettingsSave(settings *settings.Settings, globalPredictionEnabled *bool, memoryInjectionEnabled *bool, diffTheme string) {
	if settings == nil {
		return
	}

	// 检查语言是否改变
	langChanged := settings.Language != m.state.Language

	// 保存配置到本地文件
	cfg, path := config.Load()
	if path != "" {
		cfg.Language = settings.Language
		if globalPredictionEnabled != nil {
			enabled := *globalPredictionEnabled
			cfg.NextMessagePredictionEnabled = &enabled
		}
		if memoryInjectionEnabled != nil {
			enabled := *memoryInjectionEnabled
			cfg.MemoryInjectionEnabled = &enabled
		}
		cfg.DiffTheme = strings.TrimSpace(diffTheme)
		cfg.GitCommitReminder = settings.GitCommitReminder
		if err := config.Save(cfg, path); err != nil {
			m.appendSystem(fmt.Sprintf("Failed to save settings: %v", err), "error")
			return
		}
	}

	// 保存工作区设置到核心
	if err := m.adapter.SaveSettings(context.Background(), *settings); err != nil {
		m.appendSystem(fmt.Sprintf("Failed to save workspace settings: %v", err), "error")
		return
	}

	// 处理语言切换
	if langChanged {
		m.state.Language = settings.Language
		m.shell.SetLanguage(settings.Language)
		m.Update(panels.LanguageChangeMsg{Language: settings.Language})
	}
	// 更新预测功能状态
	if globalPredictionEnabled != nil {
		m.predictionEnabled = *globalPredictionEnabled
		if !m.predictionEnabled {
			m.clearPrediction()
		}
	}
	// 更新记忆注入开关（下一个 turn 生效）
	if memoryInjectionEnabled != nil {
		m.memoryInjectionEnabled = *memoryInjectionEnabled
	}
	// 更新 diff/代码块高亮主题（markdown 代码块立即生效，diff 视图下次渲染生效）
	if theme := strings.TrimSpace(diffTheme); theme != m.diffTheme {
		m.diffTheme = theme
		if m.msgRenderer != nil {
			m.msgRenderer.SetChromaTheme(theme)
		}
	}

	m.appendSystem(i18n.T("settings.saved", m.state.Language), "success")
}

// handleCtxUsageTickMsg 处理 ctxUsageTickMsg（Update 分支提取）。fall-through。
func (m *AppModel) handleCtxUsageTickMsg(_ ctxUsageTickMsg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	if m.activeView == "panel" && m.activePanel == "context" {
		m.refreshContextPanel()
	} else if m.activeView == "panel" && m.activePanel == "memory" {
		m.refreshMemoryPanel()
	}
	// 状态栏 git 概览：节流刷新（工作区变化立即，否则 15s 一次）。
	if cmd := m.maybeRefreshGitSummary(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	cmds = append(cmds, m.ctxUsageTick())
	return m, m.finalizeUpdate(tea.Batch(cmds...))
}

// handleContextCompactMsg 处理 panels.ContextCompactMsg（Update 分支提取）。fall-through。
func (m *AppModel) handleContextCompactMsg(_ panels.ContextCompactMsg) (tea.Model, tea.Cmd) {
	if message, err := m.adapter.CompactContext(context.Background()); err != nil {
		m.appendSystem(fmt.Sprintf("%s: %v", m.localize("上下文压缩失败", "Context compact failed"), err), "error")
	} else if strings.TrimSpace(message) != "" {
		m.appendSystem(message, "success")
	} else {
		m.appendSystem(i18n.T("context.compacted", m.state.Language), "success")
	}
	m.refreshContextPanel()
	return m, m.finalizeUpdate(nil)
}

// handleContextClearMsg 处理 panels.ContextClearMsg（Update 分支提取）。fall-through。
func (m *AppModel) handleContextClearMsg(_ panels.ContextClearMsg) (tea.Model, tea.Cmd) {
	if err := m.adapter.ClearContext(context.Background()); err != nil {
		m.appendSystem(fmt.Sprintf("%s: %v", m.localize("清空上下文失败", "Context clear failed"), err), "error")
	} else {
		m.shell.ClearContent()
		m.history = m.history[:0]
		m.actionHits = nil
		m.appendSystem(i18n.T("context.cleared", m.state.Language), "success")
	}
	m.refreshContextPanel()
	return m, m.finalizeUpdate(nil)
}

// handleContextExportMsg 处理 panels.ContextExportMsg（Update 分支提取）。fall-through。
func (m *AppModel) handleContextExportMsg(_ panels.ContextExportMsg) (tea.Model, tea.Cmd) {
	exportPath := filepath.Join(m.currentWorkspaceRoot(), ".eos", fmt.Sprintf("context-%s.md", time.Now().Format("20060102-150405")))
	if err := m.adapter.ExportContext(context.Background(), exportPath); err != nil {
		m.appendSystem(fmt.Sprintf("%s: %v", m.localize("上下文导出失败", "Context export failed"), err), "error")
	} else {
		m.appendSystem(fmt.Sprintf("%s: %s", m.localize("上下文已导出", "Context exported"), exportPath), "success")
	}
	return m, m.finalizeUpdate(nil)
}

// handleMemoryPanelMsg 处理 memory 面板消息（Update 分支提取）。fall-through。
func (m *AppModel) handleMemoryRefreshMsg(_ panels.MemoryRefreshMsg) (tea.Model, tea.Cmd) {
	m.refreshMemoryPanel()
	return m, m.finalizeUpdate(nil)
}

func (m *AppModel) handleMemorySaveMsg(msg panels.MemorySaveMsg) (tea.Model, tea.Cmd) {
	m.handleMemorySave(msg)
	return m, m.finalizeUpdate(nil)
}

// handleCostPanelMsg 处理 cost 面板消息（Update 分支提取）。fall-through。
func (m *AppModel) handleCostClearMsg(_ panels.CostClearMsg) (tea.Model, tea.Cmd) {
	m.adapter.ClearTokenHistory()
	m.refreshCostPanel()
	m.appendSystem(i18n.T("cost.cleared", m.state.Language), "success")
	return m, m.finalizeUpdate(nil)
}

func (m *AppModel) handleCostExportMsg(_ panels.CostExportMsg) (tea.Model, tea.Cmd) {
	// TODO: 实现成本统计导出
	m.appendSystem(i18n.T("cost.export_nyi", m.state.Language), "info")
	return m, m.finalizeUpdate(nil)
}

func (m *AppModel) handleCostRefreshMsg(_ panels.CostRefreshMsg) (tea.Model, tea.Cmd) {
	m.refreshCostPanel()
	return m, m.finalizeUpdate(nil)
}

// handleSettingsPanelMsg 处理 settings 面板消息（Update 分支提取）。fall-through。
func (m *AppModel) handleSettingsSaveMsg(msg panels.SettingsSaveMsg) (tea.Model, tea.Cmd) {
	m.handleSettingsSave(msg.Settings, msg.GlobalPredictionEnabled, msg.MemoryInjectionEnabled, msg.DiffTheme)
	return m, m.finalizeUpdate(nil)
}

func (m *AppModel) handleSettingsResetMsg(_ panels.SettingsResetMsg) (tea.Model, tea.Cmd) {
	m.appendSystem(i18n.T("settings.reset", m.state.Language), "warning")
	return m, m.finalizeUpdate(nil)
}

// handleLanguageChangeMsg 处理 panels.LanguageChangeMsg（Update 分支提取）。fall-through。
func (m *AppModel) handleLanguageChangeMsg(msg panels.LanguageChangeMsg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	// 广播语言切换消息给所有面板
	for name, panel := range m.panels {
		updatedPanel, panelCmd := panel.Update(msg)
		m.panels[name] = updatedPanel
		if panelCmd != nil {
			cmds = append(cmds, panelCmd)
		}
	}
	// 更新 helpView 语言
	if m.helpView != nil {
		m.helpView.SetLanguage(msg.Language)
	}
	// 更新 shell 语言
	if m.shell != nil {
		m.shell.SetLanguage(msg.Language)
	}
	// 更新状态
	m.state.Language = msg.Language
	m.updateInlinePermissionUI()
	return m, m.finalizeUpdate(tea.Batch(cmds...))
}

// handleTasksTickMsg 处理 panels.TasksTickMsg（Update 分支提取）。fall-through。
func (m *AppModel) handleTasksTickMsg(msg panels.TasksTickMsg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	if m.activeView == "panel" && m.activePanel == "tasks" {
		if panel, ok := m.panels["tasks"]; ok {
			updatedPanel, cmd := panel.Update(msg)
			m.panels["tasks"] = updatedPanel
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	}
	return m, m.finalizeUpdate(tea.Batch(cmds...))
}

// handleTaskToastMsg 处理 panels.TaskToastMsg（Update 分支提取）。fall-through。
func (m *AppModel) handleTaskToastMsg(msg panels.TaskToastMsg) (tea.Model, tea.Cmd) {
	m.appendSystem(msg.Text, "info")
	return m, m.finalizeUpdate(nil)
}

// handleLSPRefreshMsg 处理 panels.LSPRefreshMsg（Update 分支提取）。原行为早退。
func (m *AppModel) handleLSPRefreshMsg(_ panels.LSPRefreshMsg) (tea.Model, tea.Cmd) {
	return m, func() tea.Msg { return LSPReloadDoneMsg{Err: m.adapter.Reload()} }
}

// handleRulesRefreshMsg 处理 panels.RulesRefreshMsg（Update 分支提取）。fall-through。
func (m *AppModel) handleRulesRefreshMsg(_ panels.RulesRefreshMsg) (tea.Model, tea.Cmd) {
	m.refreshRulesPanel()
	return m, m.finalizeUpdate(nil)
}

// handleRulesSaveMsg 处理 panels.RulesSaveMsg（Update 分支提取）。fall-through。
func (m *AppModel) handleRulesSaveMsg(msg panels.RulesSaveMsg) (tea.Model, tea.Cmd) {
	m.handleRulesSave(msg)
	return m, m.finalizeUpdate(nil)
}
