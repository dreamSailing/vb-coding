package webbridge

// Copyright (c) 2026 EOSAIOS
// SPDX-License-Identifier: EOS-NCL-1.1

import (
	"log/slog"
	"os"
	"strings"
	"time"
)

type StateProjectionService struct {
	bridge *BridgeService
}

func NewStateProjectionService(bridge *BridgeService) *StateProjectionService {
	return &StateProjectionService{bridge: bridge}
}

func (s *BridgeService) stateProjection() *StateProjectionService {
	if s == nil {
		return NewStateProjectionService(nil)
	}
	if s.stateProjectionSvc == nil {
		s.stateProjectionSvc = NewStateProjectionService(s)
	}
	return s.stateProjectionSvc
}

func (p *StateProjectionService) LoadBootstrap(source string, scope BootstrapLoadScope) BootstrapState {
	if p == nil || p.bridge == nil {
		return normalizeBootstrapState(BootstrapState{
			Runtime:       bootstrapRuntimeShell,
			BridgeMode:    "unavailable",
			UpdatedAt:     time.Now().Format(time.RFC3339),
			AppVersion:    BuildVersion,
			UpdateInstall: UpdateInstallState{Status: "idle"},
		})
	}
	s := p.bridge
	startedAt := time.Now()
	persistedLastWorkspace := s.persistedLastWorkspace()
	defaultWorkspace := s.ensureDefaultWorkspaceAvailable()
	runtimeSnapshot := s.runtimeSnapshotReadOnly()
	s.ensureWorkspaceAndSessionWithSnapshotAndLast(runtimeSnapshot, persistedLastWorkspace)
	runtimeSnapshot = s.runtimeSnapshotReadOnly()

	mode := strings.TrimSpace(s.executionModeReadOnly())
	workspaces, bridgeMode := s.loadWorkspacesFromSnapshot(runtimeSnapshot)
	models := s.loadModels()
	modelCatalog := s.loadModelCatalog()
	reasoningLevel := s.loadReasoningLevel()
	window := s.captureWindowSnapshot()
	commandPalette := s.commandService().DefaultCommandPalette()
	inputSuggestions := defaultInputSuggestions()
	automationTemplates := s.allAutomationTemplatesReadOnly()
	deferred := p.loadDeferredState(scope, runtimeSnapshot, bridgeMode, window)
	local := p.loadLocalBootstrapState(runtimeSnapshot)
	activeWorkspace, currentSessionID := p.resolveBootstrapSelection(local, runtimeSnapshot, defaultWorkspace, workspaces)

	workspaces = withActiveWorkspaceCards(workspaces, activeWorkspace)
	sessions := withActiveSessionCards(local.sessions, currentSessionID)
	remoteWorkspaces := s.loadRemoteWorkspaces(activeWorkspace)
	currentRemoteRepo := s.currentRemoteRepoState()
	rulesState := s.capabilityService().LoadRules(workspaces)
	rules := s.capabilityService().ActiveRulesContent(rulesState, activeWorkspace)
	// 单一数据源：审批/问询 UI 数据在 ThreadItem.Approval，不再 attach prompts 到 messages。
	messages := s.bootstrapMessages(currentSessionID, activeWorkspace, runtimeSnapshot)
	settings := s.loadSettings(activeWorkspace, workspaces, mode, reasoningLevel, window)
	modelContext := s.loadModelContext(activeWorkspace, currentSessionID)
	plan := s.planSnapshot()
	goal := s.goalSnapshotReadOnly()
	memory := s.memorySnapshot()
	gitBranch := s.gitBranchReadOnly(activeWorkspace)
	userHome, err := os.UserHomeDir()
	if err != nil {
		userHome = ""
	}
	slog.Info("bridge.load_bootstrap",
		"source", strings.TrimSpace(source),
		"include_deferred", scope == BootstrapLoadIncludeDeferred,
		"user_home", strings.TrimSpace(userHome),
		"config_path", s.coreConfigPathReadOnly(),
		"default_workspace", defaultWorkspace,
		"resolved_foreground_workspace", activeWorkspace,
		"workspace_count", len(workspaces),
		"model_count", len(models),
		"current_session_id", currentSessionID,
		"bootstrap_duration_ms", time.Since(startedAt).Milliseconds())

	return normalizeBootstrapState(BootstrapState{
		Runtime:            bootstrapRuntimeShell,
		BridgeMode:         bridgeMode,
		ExecutionMode:      mode,
		ReasoningLevel:     reasoningLevel,
		ConfigPath:         s.coreConfigPathReadOnly(),
		ActiveWorkspace:    activeWorkspace,
		WorkspaceCount:     len(workspaces),
		ModelCount:         len(models),
		HasConfiguredModel: hasConfiguredModel(models),
		TaskCount:          len(deferred.tasks),
		CurrentSessionID:   currentSessionID,
		Workspaces:         workspaces,
		RemoteWorkspaces:   remoteWorkspaces,
		CurrentRemoteRepo:  currentRemoteRepo,
		Worktrees:          deferred.worktrees,
		Models:             models,
		ModelContext:       modelContext,
		ModelCatalog:       modelCatalog,
		MCPServers:         deferred.mcpServers,
		LSPServers:         deferred.lspServers,
		Skills:             deferred.skills,
		Plugins:            deferred.plugins,
		UsageSummary:       deferred.usageSummary,
		CostItems:          deferred.costItems,
		Versions:           deferred.versions,
		Permission:         deferred.permission,
		Settings:           settings,
		RulesState:         rulesState,
		Rules:              rules,
		Bash:               local.bash,
		Terminal:           local.terminal,
		Tasks:              deferred.tasks,
		Sessions:           sessions,
		Messages:           messages,
		// 单一数据源：prompts 不再输出（前端从 ThreadItem.Approval 投影）。
		Prompts:             nil,
		Notifications:       local.notifications,
		AutomationTemplates: automationTemplates,
		AutomationRuns:      local.automationRuns,
		CommandPalette:      commandPalette,
		Diagnostics:         deferred.diagnostics,
		Clipboard:           deferred.clipboard,
		Window:              window,
		InputSuggestions:    inputSuggestions,
		MigrationBoundaries: migrationBoundaries(),
		ResourceChecks:      deferred.resourceChecks,
		AppVersion:          BuildVersion,
		UpdateInstall:       UpdateInstallState{Status: "idle"},
		Plan:                plan,
		Goal:                goal,
		Memory:              memory,
		GitBranch:           gitBranch,
		// 亚秒精度（RFC3339Nano）：前端 shouldApplyBootstrapSnapshot 守卫按本字段比较
		// 新旧快照，秒级精度会在并发 emit（审批/resume 过渡期有 200ms debouncer +
		// 各路径独立 goroutine）时造成同秒时间戳相等，晚到的旧快照覆盖新快照
		// （典型表现：审批后"等待确认"不变成"已允许"）。亚秒精度消除该竞态。
		UpdatedAt: time.Now().Format(time.RFC3339Nano),
	})
}

// gitBranchReadOnly 返回工作区所在 git 仓库的当前分支（状态栏 git-branch 项：
// git-branch 项：非 git 工作区 / git 不可用 / 查询失败一律返回空串，前端
// 省略显示，不向用户报错）。分支属于「AI 操作的工作区所在仓库」，workspace
// 为空时内核回填前台工作区。
func (s *BridgeService) gitBranchReadOnly(workspace string) string {
	if s == nil {
		return ""
	}
	gateway := s.runtimeGatewayClient()
	if gateway == nil {
		return ""
	}
	result, err := gateway.CoreGitBranchesRPC(coreCtx(), workspace)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(result.Current)
}
