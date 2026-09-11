package webbridge

import (
	"log/slog"
	"strings"
	"time"
)

func newBridgeServiceWithDefaults(opts BridgeServiceOptions) *BridgeService {
	service := &BridgeService{
		runtimeGatewayMode:   bridgeRuntimeGatewayModeRust,
		logFile:              strings.TrimSpace(opts.LogFile),
		startupWorkspace:     strings.TrimSpace(opts.StartupWorkspace),
		startedAt:            time.Now(),
		stopCh:               make(chan struct{}),
		sessions:             map[string]*sessionState{},
		runningConversations: map[string]*runningConversationState{},
		prompts:              map[string]*promptState{},
		notifications:        nil,
		automationRuns:       nil,
		terminalSessions:     map[string]*terminalSessionHandle{},
		terminalLauncher:     startBridgeTerminalBackend,
	}
	service.configureRuntimeGateway(opts)
	service.stateProjectionSvc = NewStateProjectionService(service)
	service.workspaceSvc = NewWorkspaceService(service)
	service.settingsSvc = NewSettingsService(service)
	service.chatSvc = NewChatService(service)
	service.terminalSvc = NewTerminalService(service)
	service.attachmentSvc = NewAttachmentService(service)
	service.capabilitySvc = NewCapabilityService(service)
	service.commandSvc = NewCommandService(service)
	service.systemSvc = NewSystemService(service)
	service.notifyRuntimeGatewayFallback()
	service.ensureDefaultWorkspaceAvailable()
	service.bootstrapSessions()
	service.hydrateAutomationTemplates()
	service.syncSandboxModeForSession(service.activeWorkspaceValue(), service.currentSessionValue())
	return service
}

// Start 启动桥的后台同步：状态同步器、浏览器事件泵、自动化调度器、心跳、
// 任务监听。web 模式没有 Wails app 对象，事件发射器由 server 注入（SetEmitter）。
func (s *BridgeService) Start() {
	s.startStateSynchronizers()
	s.startBrowserEventPump()
	s.startAutomationScheduler()
	// 询问等待超时：把 workspace 设置文件里持久化的值推进内核（内核
	// Settings 不落盘，启动必须同步一次，超时看门狗才会生效）。
	s.syncPromptTimeoutAtStartup()
	go s.emitHeartbeat()
	// 后台任务监听：AI 起的 dev server / 长驻进程进出 task/list 时推送
	// shell-updated，任务页不用等下一次交互才有变化。
	go s.startTaskWatcher()
	s.emitStartupBootstrap()
}

func (s *BridgeService) Close() {
	s.stopAutomationScheduler()
	// 关闭时持久化当前工作区 + 会话，下次启动默认停留到这里。
	s.stateMu.RLock()
	lastWorkspace := s.activeWorkspace
	lastSession := s.currentSessionID
	s.stateMu.RUnlock()
	if lastWorkspace != "" || lastSession != "" {
		s.persistWorkspaceAndSession(lastWorkspace, lastSession)
	}
	s.stopOnce.Do(func() {
		close(s.stopCh)
	})
	s.stateMu.Lock()
	cancels := make([]func(), 0, len(s.runningConversations))
	for _, running := range s.runningConversations {
		if running != nil && running.Cancel != nil {
			cancels = append(cancels, running.Cancel)
		}
	}
	s.stateMu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
	s.closeAllTerminalSessions()
	s.conversationWG.Wait()
	if closeRuntimeGateway := s.runtimeGatewayClose; closeRuntimeGateway != nil {
		s.runtimeGatewayClose = nil
		if err := closeRuntimeGateway(); err != nil {
			slog.Warn("bridge.runtime_gateway.close.error", "mode", s.runtimeGatewayMode, "error", err)
		}
	}
}

// StayInTrayEnabled 读取「驻留系统托盘」开关当前值（持久化真相源是
// workspace 设置文件，与 loadSettings 同一来源）。读取失败按默认开。
func (s *BridgeService) StayInTrayEnabled() bool {
	snapshot, err := LoadGUISettings(
		s.configPathReadOnly(),
		s.settingsWorkspaceSource(s.activeWorkspaceValue()),
		GUISettingsDefaults{Language: "zh", Theme: "system"},
	)
	if err != nil {
		return true
	}
	return snapshot.Workspace.StayInTray
}

// SetStayInTrayChangedListener 注册「驻留系统托盘」开关变更回调
// （main.go 用它即时切换托盘图标显隐）。仅在保存设置时触发。
func (s *BridgeService) SetStayInTrayChangedListener(fn func(enabled bool)) {
	s.stayInTrayListener = fn
}

func (s *BridgeService) notifyStayInTrayChanged(enabled bool) {
	if s.stayInTrayListener != nil {
		s.stayInTrayListener(enabled)
	}
}

// syncPromptTimeoutAtStartup 把持久化的询问等待超时推进内核（失败只告警：
// 内核不可用时用户保存设置/重启后会再同步，不阻塞桥启动）。
func (s *BridgeService) syncPromptTimeoutAtStartup() {
	// 读 workspace 设置文件（持久化真相源），与 loadSettings 同一来源。
	snapshot, err := LoadGUISettings(
		s.configPathReadOnly(),
		s.settingsWorkspaceSource(s.activeWorkspaceValue()),
		GUISettingsDefaults{Language: "zh", Theme: "system"},
	)
	secs := 0
	if err == nil {
		secs = snapshot.Workspace.PromptTimeoutSecs
	} else {
		slog.Warn("bridge.prompt_timeout.load_failed", "error", err.Error())
	}
	if err := s.setPromptTimeoutRPC(secs); err != nil {
		slog.Warn("bridge.prompt_timeout.startup_sync_failed", "error", err.Error())
	}
}
