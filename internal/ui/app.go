package ui

// app.go — TUI 主模型 AppModel 的结构与生命周期。
//
// 本文件只保留应用模型的「骨架」：
//   - AppState / AppModel 结构定义
//   - NewAppModelFrom* 构造入口位于 app_constructors.go
//   - Init：初始化布局、启动事件监听
//   - Update：消息分发（switch 路由），具体 case 体抽成 handleXxx 方法，
//     分散在 app_messages.go / app_send.go / app_confirm.go / app_workspace.go /
//     app_panels*.go / app_slash.go / app_keys.go / app_runtime_events.go /
//     app_versions.go。
//   - View：根据 activeView 渲染对应视图
//   - finalizeUpdate：Update 末尾公共收尾（刷新上下文/任务计数 + 重新武装事件泵）。
//
// 历史代码（>4000 行）已按职责拆分到上述同包文件，本文件仅做调度，不含业务逻辑。

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/eosaios/eos/internal/config"
	"github.com/eosaios/eos/internal/ui/adapter"
	"github.com/eosaios/eos/internal/ui/components/messages"
	"github.com/eosaios/eos/internal/ui/panels"
	"github.com/eosaios/eos/internal/ui/styles"
	"github.com/eosaios/eos/internal/ui/views/confirm"
	"github.com/eosaios/eos/internal/ui/views/help"
	"github.com/eosaios/eos/internal/ui/views/setup"
	"github.com/eosaios/eos/internal/ui/views/shell"

	tea "charm.land/bubbletea/v2"
)

// AppState 应用程序状态
type AppState struct {
	Mode          string // "ai", "bash"
	Processing    bool
	Thinking      bool
	Language      string
	Theme         string
	ExecutionMode string
}

// AppModel 主应用模型
type AppModel struct {
	width, height int
	state         AppState

	adapter *adapter.CoreClientAdapter
	styles  *styles.Styles

	// 消息渲染器
	msgRenderer *messages.Renderer

	// 主视图
	shell *shell.Model

	// 面板系统
	panels      map[string]panels.Panel
	activePanel string

	// 其他视图
	helpView                 *help.HelpView
	setupView                any // 可以是 *setup.SetupView 或 *setup.ModelSetupView
	confirmView              *confirm.Model
	browserTakeoverConfirm   bool
	actionPopup              *confirm.ActionPopup // 点击消息文本弹出的操作选择框
	prevView                 string
	inlinePermissionReq      *confirm.Request
	inlinePermissionSelected int

	// 视图状态
	activeView       string // "shell", "panel", "help", "setup", "confirm"
	initialSetupFlow bool

	// 消息跟踪
	currentAIStartTime         time.Time
	currentAITokens            int
	aiLive                     strings.Builder
	thinkingLive               strings.Builder
	thinkingExpanded           bool
	reasoningStartTime         time.Time // 当前推理块开始时间，用于该块耗时统计
	activeItemID               string    // current in-progress turn item being streamed
	agentTextCommittedThisTurn bool      // item.completed 已落 history，忽略 legacy text.final 双写
	toolInflight               map[string]toolTrack
	history                    []historyEntry
	delegatedThisRound         bool
	lastAgentFinal             string

	actionHits []bubbleActionHit

	// 鼠标框选状态（selection.go）。
	selAnchor *selectionCoord
	selStart  selectionCoord
	selEnd    selectionCoord
	selActive bool

	pendingImagePaths    []string
	pendingPlanDownload  *planDownloadRequest
	pendingPluginConfirm string

	predictionText        string
	predictionSeq         int
	predictionDebounceSeq int
	predictionEnabled     bool

	// memoryInjectionEnabled 是 CLI 壳层的请求级记忆注入开关（config 持久化，
	// 默认开），随每次 Invoke 下发 turn/start.use_memory。
	memoryInjectionEnabled bool

	// diffTheme 是 diff/代码块 chroma 高亮主题（config 持久化，默认 monokai），
	// 供 /diff、审批 diff 与 markdown 代码块渲染使用。
	diffTheme string

	// gitSummaryRoot / gitSummaryCheckedAt 是状态栏 git 概览刷新的节流状态：
	// 工作区变化立即刷新，否则每 gitBranchRefreshInterval 至多查一次。
	gitSummaryRoot      string
	gitSummaryCheckedAt time.Time

	// gitHintedDirty / gitHintedAhead 是上次提交提醒时的计数（-1 = 从未提示），
	// 计数相对上次提示无变化则不重复提醒，避免刷屏。
	gitHintedDirty int
	gitHintedAhead int

	trustPendingPath   string
	trustPendingAction string
	activeCancel       context.CancelFunc
	stopRequested      bool

	// pendingResumeSession 由启动选项（--continue/--resume）注入。
	// Init() 在工作区信任检查通过后消费它：调 ResumeSession + restoreSessionHistory
	// 把指定会话的历史回填进 m.history。nil 表示不做启动期 resume。
	pendingResumeSession *string
}

// Init 初始化应用模型
func (m *AppModel) Init() tea.Cmd {
	// 初始化时设置默认大小，防止一直显示 Loading...
	m.width = 80
	m.height = 24

	if p, _ := os.Getwd(); p != "" {
		abs := config.NormalizeWorkspacePath(p)
		rememberKnownWorkspace(abs, true)
		if m.isWorkspaceTrusted(abs) {
			_ = m.adapter.StartContextEngine(context.Background(), abs)
			_, _ = m.adapter.Settings(context.Background())
			// 工作区已信任：立即消费 --continue/--resume 指定的会话。
			// 不信任的分支在 handleConfirmResultWorkspaceTrust 信任后再消费。
			m.resumeStartupSession()
		} else {
			m.trustPendingPath = abs
			m.trustPendingAction = "init"
			m.openWorkspaceTrustConfirm(abs)
		}
	}

	// 确保 shell 和所有面板都有初始大小
	m.shell.SetSize(m.width, m.height)
	m.helpView.SetSize(m.width, m.height)
	switch sv := m.setupView.(type) {
	case *setup.SetupView:
		sv.SetSize(m.width, m.height)
	case *setup.ModelSetupView:
		sv.SetSize(m.width, m.height)
	}
	for _, p := range m.panels {
		p.SetSize(m.width, m.height)
	}

	// 启动事件监听（阻塞式：parked goroutine，事件到达即返回并重武装）
	// 之前用非阻塞 select + default 返回 nil，nil 消息不会重武装泵，
	// 导致 Init 后泵永久死亡、流式 item.delta 事件无人消费，UI 只能等
	// 阻塞 Invoke() 返回最终文本时一次性显示。改为阻塞 listenEvents()。
	return tea.Batch(
		m.shell.Init(),
		m.ctxUsageTick(),
		m.listenEvents(),
	)
}

// finalizeUpdate 执行原 Update 函数末尾的公共收尾：
// 刷新上下文使用率与后台任务计数显示，并重新武装事件监听泵。
// 各 case 提取出的 handleXxx 方法在原本会「fall through」到末尾的地方调用本函数。
func (m *AppModel) finalizeUpdate(cmds ...tea.Cmd) tea.Cmd {
	m.updateContextUsageUI()
	m.updateBGTaskCountUI()
	// 继续监听事件
	cmds = append(cmds, m.listenEvents())
	return tea.Batch(cmds...)
}

// Update 更新应用状态。仅做消息分发，具体逻辑见各 handleXxx 方法。
func (m *AppModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case ctxUsageTickMsg:
		return m.handleCtxUsageTickMsg(msg)
	case GitSummaryMsg:
		return m.handleGitSummaryMsg(msg)
	case GitCommitHintMsg:
		return m.handleGitCommitHintMsg(msg)
	case tea.WindowSizeMsg:
		return m.handleWindowSizeMsg(msg)
	case tea.MouseMsg:
		return m.handleMouseMsg(msg, nil)
	case tea.KeyMsg:
		return m.handleKeyMsg(msg)
	case adapter.RuntimeEvent:
		return m.handleRuntimeEvent(msg)
	case predictionDebounceMsg:
		return m.handlePredictionDebounceMsg(msg)
	case PredictionUpdateMsg:
		return m.handlePredictionUpdateMsg(msg)

	case AIResponseMsg:
		return m.handleAIResponseMsg(msg)
	case ItemStartedMsg:
		return m.handleItemStartedMsg(msg)
	case ItemDeltaMsg:
		return m.handleItemDeltaMsg(msg)
	case ItemCompletedMsg:
		return m.handleItemCompletedMsg(msg)
	case InvokeDoneMsg:
		return m.handleInvokeDoneMsg(msg)
	case ThinkingMsg:
		return m.handleThinkingMsg(msg)
	case ToolCallMsg:
		return m.handleToolCallMsg(msg)
	case ToolResultMsg:
		return m.handleToolResultMsg(msg)
	case ModeChangedMsg:
		return m.handleModeChangedMsg(msg)
	case AgentTaskMsg:
		return m.handleAgentTaskMsg(msg)
	case AgentFinalMsg:
		return m.handleAgentFinalMsg(msg)
	case ErrorMsg:
		return m.handleErrorMsg(msg)
	case clearCopiedMsg:
		return m.handleClearCopiedMsg(msg)

	case BrowserTakeoverStartedMsg:
		return m.handleBrowserTakeoverStarted(msg)
	case BrowserTakeoverEndedMsg:
		return m.handleBrowserTakeoverEnded(msg)
	case BrowserActionMsg:
		return m.handleBrowserActionMsg(msg)
	case BrowserDownloadDoneMsg:
		return m.handleBrowserDownloadDoneMsg(msg)
	case BrowserPickSelectedMsg:
		return m.handleBrowserPickSelectedMsg(msg)

	case PromptRequestMsg:
		return m.handlePromptRequestMsg(msg)
	case confirm.ResultMsg:
		return m.handleConfirmResultMsg(msg)
	case confirm.ActionResultMsg:
		return m.handleActionResultMsg(msg)

	case setup.SetupCompleteMsg:
		return m.handleSetupCompleteMsg(msg)
	case setup.SetupCancelMsg:
		return m.handleSetupCancelMsg(msg)
	case setup.ModelFormCompleteMsg:
		return m.handleModelFormCompleteMsg(msg)
	case setup.MCPConfigCancelMsg:
		return m.handleMCPConfigCancelMsg(msg)
	case setup.MCPConfigSubmitMsg:
		return m.handleMCPConfigSubmitMsg(msg)

	case ShowHintsMsg:
		return m.handleShowHintsMsg(msg)

	// 模型面板
	case panels.ModelSelectMsg:
		return m.handleModelSelectMsg(msg)
	case panels.ModelPlanSelectMsg:
		return m.handleModelPlanSelectMsg(msg)
	case panels.ModelAddMsg:
		return m.handleModelAddMsg(msg)
	case panels.ModelEditMsg:
		return m.handleModelEditMsg(msg)
	case panels.ModelDeleteMsg:
		return m.handleModelDeleteMsg(msg)
	case panels.ModelSyncMsg:
		return m.handleModelSyncMsg(msg)
	case panels.ModelRefreshMsg:
		return m.handleModelRefreshMsg(msg)
	case panels.LanguageChangeMsg:
		return m.handleLanguageChangeMsg(msg)

	// 工作区面板
	case panels.WorkspaceSelectMsg:
		return m.handleWorkspaceSelectMsg(msg)
	case panels.WorkspaceDeleteMsg:
		return m.handleWorkspaceDeleteMsg(msg)
	case panels.WorkspaceAddMsg:
		return m.handleWorkspaceAddMsg(msg)

	case WorkspaceReloadDoneMsg:
		return m.handleWorkspaceReloadDoneMsg(msg)
	case MCPReloadDoneMsg:
		return m.handleMCPReloadDoneMsg(msg)
	case LSPReloadDoneMsg:
		return m.handleLSPReloadDoneMsg(msg)

	// MCP 面板
	case panels.MCPToggleMsg:
		return m.handleMCPToggleMsg(msg)
	case panels.MCPAddMsg:
		return m.handleMCPAddMsg(msg)
	case panels.MCPAddBrowserMsg:
		return m.handleMCPAddBrowserMsg(msg)
	case panels.MCPEditMsg:
		return m.handleMCPEditMsg(msg)
	case panels.MCPDeleteMsg:
		return m.handleMCPDeleteMsg(msg)
	case panels.MCPSaveMsg:
		return m.handleMCPSaveMsg(msg)

	// Context 面板
	case panels.ContextCompactMsg:
		return m.handleContextCompactMsg(msg)
	case panels.ContextClearMsg:
		return m.handleContextClearMsg(msg)
	case panels.ContextExportMsg:
		return m.handleContextExportMsg(msg)

	// Memory 面板
	case panels.MemoryRefreshMsg:
		return m.handleMemoryRefreshMsg(msg)
	case panels.MemorySaveMsg:
		return m.handleMemorySaveMsg(msg)

	// Cost 面板
	case panels.CostClearMsg:
		return m.handleCostClearMsg(msg)
	case panels.CostExportMsg:
		return m.handleCostExportMsg(msg)
	case panels.CostRefreshMsg:
		return m.handleCostRefreshMsg(msg)

	// Settings 面板
	case panels.SettingsSaveMsg:
		return m.handleSettingsSaveMsg(msg)
	case panels.SettingsResetMsg:
		return m.handleSettingsResetMsg(msg)

	// Versions 面板
	case panels.VersionsLoadMsg:
		return m.handleVersionsLoadMsg(msg)
	case panels.VersionsRollbackMsg:
		return m.handleVersionsRollbackMsg(msg)
	case panels.VersionsDeleteMsg:
		return m.handleVersionsDeleteMsg(msg)
	case panels.VersionsDeleteFileMsg:
		return m.handleVersionsDeleteFileMsg(msg)
	case panels.VersionsDeleteAllMsg:
		return m.handleVersionsDeleteAllMsg(msg)

	// Tasks 面板
	case panels.TasksTickMsg:
		return m.handleTasksTickMsg(msg)
	case panels.TaskToastMsg:
		return m.handleTaskToastMsg(msg)
	case panels.TaskKillRequestMsg:
		return m.handleTaskKillRequestMsg(msg)

	// Rules / LSP 面板
	case panels.LSPRefreshMsg:
		return m.handleLSPRefreshMsg(msg)
	case panels.RulesRefreshMsg:
		return m.handleRulesRefreshMsg(msg)
	case panels.RulesSaveMsg:
		return m.handleRulesSaveMsg(msg)
	}

	// default：未识别消息交给 shell 处理，并走公共收尾。
	if m.activeView == "shell" {
		updatedShell, shellCmd := m.shell.Update(msg)
		*m.shell = updatedShell
		m.syncPredictionState()
		return m, m.finalizeUpdate(shellCmd)
	}
	return m, m.finalizeUpdate(nil)
}

// View 渲染应用视图。v2 下 AltScreen / 鼠标模式也在这里声明。
func (m *AppModel) View() tea.View {
	if m.width == 0 || m.height == 0 {
		return tea.NewView("Loading...")
	}

	var content string
	switch m.activeView {
	case "shell":
		view := m.shell.View()
		if m.actionPopup != nil {
			content = overlayCenter(m.width, m.height, view, m.actionPopup.View())
		} else {
			content = view
		}
	case "confirm":
		if m.confirmView != nil {
			content = m.confirmView.View()
		} else {
			content = m.shell.View()
		}
	case "help":
		content = m.helpView.View()
	case "setup":
		switch sv := m.setupView.(type) {
		case *setup.SetupView:
			content = sv.View()
		case *setup.ModelSetupView:
			content = sv.View()
		case *setup.MCPConfigEditorView:
			content = sv.View()
		default:
			content = "Loading..."
		}
	case "panel":
		if panel, ok := m.panels[m.activePanel]; ok {
			content = panel.View()
		} else {
			content = m.styles.App.Render("Panel not found: " + m.activePanel)
		}
	default:
		content = m.styles.App.Render("Welcome to EOS!")
	}

	v := tea.NewView(content)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}
