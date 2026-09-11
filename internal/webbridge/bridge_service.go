package webbridge

// Copyright (c) 2026 EOSAIOS
// SPDX-License-Identifier: EOS-NCL-1.1
// 本文件基于 EOS 非商用许可证 v1.1 发布，详见 LICENSE。
// 商业使用请联系版权人获得商业授权。

import (
	"context"
	"sync"
	"time"

	"github.com/eosaios/eos/internal/webbridge/adapter"
	"github.com/robfig/cron/v3"
)

const (
	heartbeatEventName             = "eos:bridge:heartbeat"
	shellUpdatedEventName          = "eos:bridge:shell-updated"
	conversationDeltaEventName     = "eos:bridge:conversation-delta"
	usageUpdatedEventName          = "eos:bridge:usage-updated"
	composerFilesDroppedEventName  = "eos:composer:files-dropped"
	guiRuntimeMetadataKey          = "eos_gui"
	maxNotificationCount           = 10
	maxImportedAttachmentBytes     = 20 * 1024 * 1024
	maxGeneralAttachmentBytes      = 50 * 1024 * 1024
	maxImportedAttachmentBase64Len = 28 * 1024 * 1024
)

type BridgeService struct {
	// stayInTrayListener 是「驻留系统托盘」开关变更回调（web 模式下无托盘，恒为空）。
	stayInTrayListener           func(enabled bool)
	runtimeGateway               bridgeRuntimeGateway
	runtimeGatewayMode           string
	runtimeGatewayClose          func() error
	runtimeGatewayCoreBinDir     string
	runtimeGatewayResolvedBinary adapter.StdioResolvedBinary
	runtimeGatewayStartError     string
	stateProjectionSvc           *StateProjectionService
	workspaceSvc                 *WorkspaceService
	settingsSvc                  *SettingsService
	chatSvc                      *ChatService
	terminalSvc                  *TerminalService
	attachmentSvc                *AttachmentService
	workspaceFilesSvc            *WorkspaceFilesService
	capabilitySvc                *CapabilityService
	commandSvc                   *CommandService
	systemSvc                    *SystemService
	automationSvc                *AutomationService
	logFile                      string
	startupWorkspace             string
	startedAt                    time.Time
	stopOnce                     sync.Once
	stopCh                       chan struct{}
	conversationWG               sync.WaitGroup

	stateMu                   sync.RWMutex
	activeWorkspace           string
	currentSessionID          string
	bootstrapHydrated         bool
	sessions                  map[string]*sessionState
	runningConversations      map[string]*runningConversationState
	prompts                   map[string]*promptState
	notifications             []NotificationItem
	automationRuns            []AutomationRunCard
	customAutomationTemplates []AutomationTemplateCard
	automationScheduler       *cron.Cron
	automationSchedulerMu     sync.Mutex
	bash                      BashState
	terminalSessions          map[string]*terminalSessionHandle
	terminalActiveSessionID   string
	terminalSequence          int
	terminalShell             terminalShellState
	// 应用内更新下载状态（update_download.go），独立小锁不与 stateMu 纠缠
	updateMu            sync.Mutex
	updateDownload      UpdateDownloadState
	updateCancel        context.CancelFunc
	emitEvent           func(string, any)
	invokeSession       func(adapter.Core, context.Context, string, []string) (<-chan adapter.Event, error)
	saveSessionMessages func(adapter.Core, string, string, []adapter.SessionMessage) (string, error)
	terminalLauncher    terminalLauncher
	// modelCatalogFallback, when non-empty, captures why the live Rust
	// model catalog was unavailable or empty. It is read by resourceChecks()
	// to surface a diagnostic and by the bootstrap flow to add a
	// notification; the GUI must not synthesize its own model catalog.
	modelCatalogFallback string
	// degradedNotified records which read-only domains have already pushed a
	// user-visible degradation notice in the current outage, so a frequently
	// called LoadBootstrap does not spam the notification feed. Cleared by
	// notifyDegraded when the domain recovers.
	degradedNotified map[string]bool
}

func (s *BridgeService) LoadBootstrap() BootstrapState {
	return s.loadBootstrap("rpc")
}

func (s *BridgeService) loadBootstrap(source string) BootstrapState {
	return s.loadBootstrapWithOptions(source, BootstrapLoadIncludeDeferred)
}

func (s *BridgeService) loadBootstrapWithOptions(source string, scope BootstrapLoadScope) BootstrapState {
	return s.stateProjection().LoadBootstrap(source, scope)
}

func (s *BridgeService) SendChat(sessionID, workspace, input string, attachments []string) (BootstrapState, error) {
	return s.chatService().SendChat(sessionID, workspace, input, attachments)
}

func (s *BridgeService) SendChatWithReasoning(sessionID, workspace, input string, attachments []string, reasoningLevel string) (BootstrapState, error) {
	return s.chatService().SendChatWithReasoning(sessionID, workspace, input, attachments, reasoningLevel)
}

func (s *BridgeService) ResumeFailedTurn(sessionID string) (BootstrapState, error) {
	return s.chatService().ResumeFailedTurn(sessionID)
}

func (s *BridgeService) CancelSession(sessionID string) (BootstrapState, error) {
	return s.chatService().CancelSession(sessionID)
}

func (s *BridgeService) RollbackChatTurn(sessionID, userMessageID string) (BootstrapState, error) {
	return s.chatService().RollbackChatTurn(sessionID, userMessageID)
}

func (s *BridgeService) CreateSession(workspacePath string) (BootstrapState, error) {
	return s.chatService().CreateSession(workspacePath)
}

func (s *BridgeService) EnsureWorkspaceSession(workspacePath string) (BootstrapState, error) {
	return s.chatService().EnsureWorkspaceSession(workspacePath)
}

func (s *BridgeService) SelectSession(workspacePath, sessionID string) (BootstrapState, error) {
	return s.chatService().SelectSession(workspacePath, sessionID)
}

func (s *BridgeService) RenameSession(sessionID, title string) (BootstrapState, error) {
	return s.chatService().RenameSession(sessionID, title)
}

func (s *BridgeService) DeleteSession(workspacePath, sessionID string) (BootstrapState, error) {
	return s.chatService().DeleteSession(workspacePath, sessionID)
}

func (s *BridgeService) ArchiveSession(sessionID string, archived bool) (BootstrapState, error) {
	return s.chatService().ArchiveSession(sessionID, archived)
}

func (s *BridgeService) LoadArchivedSessions() []SessionCard {
	return s.loadArchivedSessionCardsReadOnly()
}

func (s *BridgeService) SelectWorkspace(path string) (BootstrapState, error) {
	return s.workspaceService().SelectWorkspace(path)
}

func (s *BridgeService) ListRemoteWorkspaces() []WorkspaceCard {
	return s.workspaceService().ListRemoteWorkspaces()
}

func (s *BridgeService) OpenRemoteWorkspace(idOrPath string) (BootstrapState, error) {
	return s.workspaceService().OpenRemoteWorkspace(idOrPath)
}

func (s *BridgeService) ForgetRemoteWorkspace(idOrPath string) (BootstrapState, error) {
	return s.workspaceService().ForgetRemoteWorkspace(idOrPath)
}

func (s *BridgeService) ClearRemoteWorkspaceCache(idOrPath string) (BootstrapState, error) {
	return s.workspaceService().ClearRemoteWorkspaceCache(idOrPath)
}

func (s *BridgeService) StartRemoteRepoFlow(req RemoteRepoFlowRequest) (BootstrapState, error) {
	return s.workspaceService().StartRemoteRepoFlow(req)
}

func (s *BridgeService) PredictNextUserMessage(draft string) (string, error) {
	return s.chatService().PredictNextUserMessage(draft)
}

func (s *BridgeService) RefineInput(draft string) (string, error) {
	return s.chatService().RefineInput(draft)
}

func (s *BridgeService) ResolvePrompt(promptID, decision, note string) (BootstrapState, error) {
	return s.commandService().ResolvePrompt(promptID, decision, note)
}

func (s *BridgeService) KillTask(taskID string) (BootstrapState, error) {
	return s.commandService().KillTask(taskID)
}

func (s *BridgeService) DismissNotification(notificationID string) BootstrapState {
	return s.commandService().DismissNotification(notificationID)
}

func (s *BridgeService) RunCommandPalette(command string) (BootstrapState, error) {
	return s.commandService().RunCommandPalette(command)
}

func (s *BridgeService) RunAutomationTemplate(templateID string) (BootstrapState, error) {
	return s.commandService().RunAutomationTemplate(templateID)
}

func (s *BridgeService) OpenAttachmentDialog() (FileDialogResult, error) {
	return s.attachmentService().OpenAttachmentDialog()
}

func (s *BridgeService) ImportAttachment(name string, mime string, base64Data string) (AttachmentRef, error) {
	return s.attachmentService().ImportAttachment(name, mime, base64Data)
}

func (s *BridgeService) PreviewAttachment(path string) (AttachmentPreview, error) {
	return s.attachmentService().PreviewAttachment(path)
}

func (s *BridgeService) PreviewWorkspaceFile(path string, line int) (WorkspaceFilePreview, error) {
	return s.attachmentService().PreviewWorkspaceFile(path, line)
}

func (s *BridgeService) ListWorkspaceDirectory(relPath string) (DirectoryListing, error) {
	return s.workspaceFilesService().ListWorkspaceDirectory(relPath)
}

func (s *BridgeService) OpenWorkspaceDialog() (FileDialogResult, error) {
	return s.systemService().OpenWorkspaceDialog()
}

func (s *BridgeService) ExportDiagnosticsBundle() (ExportResult, error) {
	return s.systemService().ExportDiagnosticsBundle()
}

func (s *BridgeService) OpenLogDirectory() error {
	return s.systemService().OpenLogDirectory()
}

func (s *BridgeService) RevealInFileManager(path string) error {
	return s.systemService().RevealInFileManager(path)
}

func (s *BridgeService) ListExternalApps() ([]ExternalAppInfo, error) {
	return s.systemService().ListExternalApps()
}

func (s *BridgeService) OpenInExternalApp(appID string, path string) error {
	return s.systemService().OpenInExternalApp(appID, path)
}

func (s *BridgeService) ReadClipboardText() string {
	return s.systemService().ReadClipboardText()
}

func (s *BridgeService) WriteClipboardText(text string) BootstrapState {
	return s.systemService().WriteClipboardText(text)
}

func (s *BridgeService) AcknowledgeCrashReport() (BootstrapState, error) {
	return s.systemService().AcknowledgeCrashReport()
}

func (s *BridgeService) ProbeInvoke(input string) BridgeProbe {
	return s.systemService().ProbeInvoke(input)
}

// GetStatus returns runtime status information
func (s *BridgeService) GetStatus() RuntimeStatus {
	return s.systemService().GetStatus()
}

// ToggleFastMode toggles fast mode and returns updated bootstrap state
func (s *BridgeService) ToggleFastMode() (BootstrapState, error) {
	return s.settingsService().ToggleFastMode()
}

// SetTheme sets the UI theme
func (s *BridgeService) SetTheme(name string) BootstrapState {
	return s.settingsService().SetTheme(name)
}

// GetStats returns session statistics
func (s *BridgeService) GetStats() SessionStats {
	return s.systemService().GetStats()
}
