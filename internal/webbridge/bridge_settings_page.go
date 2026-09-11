package webbridge

import (
	"log/slog"
	"strings"
)

type GUISettingsAppearanceState struct {
	Language string `json:"language"`
	Theme    string `json:"theme"`
	// DiffTheme 是代码/diff 高亮主题预设（github/catppuccin/everforest/rose-pine/vitesse）。
	DiffTheme string `json:"diffTheme"`
	// StayInTray 是「驻留系统托盘」开关（默认开）：关闭窗口隐藏到托盘
	// 并常驻，托盘退出才是真退出。
	StayInTray bool `json:"stayInTray"`
}

type GUISettingsRuntimeState struct {
	ExecutionMode  string `json:"executionMode"`
	SandboxMode    string `json:"sandboxMode"`
	ReasoningLevel string `json:"reasoningLevel"`
}

type GUISettingsContextState struct {
	AutoContext          bool `json:"autoContext"`
	DesktopNotifications bool `json:"desktopNotifications"`
	// GitCommitReminder 是「git 提交提醒」开关（默认开）。
	GitCommitReminder bool `json:"gitCommitReminder"`
	// GitCommitMarker 是「提交信息附带 eos 署名」开关（默认开）。
	GitCommitMarker bool `json:"gitCommitMarker"`
	// UseMemory 是「记忆注入」开关（默认开）：turn/start 随 use_memory 下发，
	// 注入裁决在内核。
	UseMemory bool `json:"useMemory"`
	// PromptTimeoutSecs 是询问（审批/问询）等待超时秒数（0 = 一直等待）。
	// 超时后内核自动响应：审批拒绝、问询选 (Recommended) 项。
	PromptTimeoutSecs int `json:"promptTimeoutSecs"`
	MaxInjectKB       int `json:"maxInjectKB"`
}

type GUISettingsWatchPlanState struct {
	WatchDebounceMs int    `json:"watchDebounceMs"`
	PollIntervalSec int    `json:"pollIntervalSec"`
	PlanPromptStyle string `json:"planPromptStyle"`
}

type GUISettingsMetaState struct {
	GlobalConfigPath      string         `json:"globalConfigPath"`
	WorkspaceSettingsPath string         `json:"workspaceSettingsPath"`
	ActiveWorkspace       string         `json:"activeWorkspace"`
	LogDir                string         `json:"logDir"`
	WorkspaceTrusted      bool           `json:"workspaceTrusted"`
	TrustedAt             string         `json:"trustedAt"`
	WorkspaceCount        int            `json:"workspaceCount"`
	WindowSnapshot        WindowSnapshot `json:"windowSnapshot"`
}

type GUISettingsState struct {
	Appearance     GUISettingsAppearanceState `json:"appearance"`
	Runtime        GUISettingsRuntimeState    `json:"runtime"`
	Context        GUISettingsContextState    `json:"context"`
	WatchPlan      GUISettingsWatchPlanState  `json:"watchPlan"`
	Meta           GUISettingsMetaState       `json:"meta"`
	MidRiskConfirm bool                       `json:"midRiskConfirm"`
	// UpdateProxyEnabled/UpdateProxyURL 是更新代理开关（全局配置）。
	UpdateProxyEnabled bool   `json:"updateProxyEnabled"`
	UpdateProxyURL     string `json:"updateProxyUrl"`
}

type SettingsSaveRequest struct {
	Language             string `json:"language"`
	LogDir               string `json:"logDir"`
	Theme                string `json:"theme"`
	DiffTheme            string `json:"diffTheme"`
	ExecutionMode        string `json:"executionMode"`
	SandboxMode          string `json:"sandboxMode"`
	ReasoningLevel       string `json:"reasoningLevel"`
	AutoContext          bool   `json:"autoContext"`
	DesktopNotifications bool   `json:"desktopNotifications"`
	GitCommitReminder    bool   `json:"gitCommitReminder"`
	GitCommitMarker      bool   `json:"gitCommitMarker"`
	UseMemory            bool   `json:"useMemory"`
	MaxInjectKB          int    `json:"maxInjectKB"`
	WatchDebounceMs      int    `json:"watchDebounceMs"`
	PollIntervalSec      int    `json:"pollIntervalSec"`
	PlanPromptStyle      string `json:"planPromptStyle"`
	PromptTimeoutSecs    int    `json:"promptTimeoutSecs"`
	UpdateProxyEnabled   bool   `json:"updateProxyEnabled"`
	UpdateProxyURL       string `json:"updateProxyUrl"`
	// StayInTray 是「驻留系统托盘」开关（默认开）。
	StayInTray bool `json:"stayInTray"`
}

func (s *BridgeService) loadSettings(activeWorkspace string, workspaces []WorkspaceCard, mode, reasoningLevel string, window WindowSnapshot) GUISettingsState {
	fallback := s.settingsReadOnly()
	settingsWorkspace := s.settingsWorkspaceSource(activeWorkspace)
	snapshot, err := LoadGUISettings(
		s.configPathReadOnly(),
		settingsWorkspace,
		GUISettingsDefaults{
			Language: fallbackText(strings.TrimSpace(fallback.Language), "zh"),
			Theme:    fallbackText(strings.TrimSpace(fallback.Theme), "system"),
		},
	)
	if err != nil {
		snapshot = GUISettingsSnapshot{
			Language:              fallbackText(strings.TrimSpace(fallback.Language), "zh"),
			LogDir:                DefaultLogDir(),
			GlobalConfigPath:      s.configPathReadOnly(),
			WorkspaceSettingsPath: ResolveWorkspaceSettingsPath(settingsWorkspace),
			Workspace: GUIWorkspaceSettings{
				Theme:                fallbackText(strings.TrimSpace(fallback.Theme), "system"),
				DiffTheme:            defaultDiffTheme,
				SandboxMode:          "workspace",
				AutoContext:          true,
				DesktopNotifications: true,
				GitCommitReminder:    true,
				GitCommitMarker:      true,
				StayInTray:           true,
				UseMemory:            true,
				MaxInjectKB:          32,
				WatchDebounceMs:      500,
				PollIntervalSec:      5,
			},
		}
	}

	workspaceTrusted := snapshot.Workspace.Trusted
	for _, item := range workspaces {
		if sameWorkspacePath(item.Path, activeWorkspace) {
			workspaceTrusted = workspaceTrusted || item.Trusted
			break
		}
	}

	return GUISettingsState{
		Appearance: GUISettingsAppearanceState{
			Language:   snapshot.Language,
			Theme:      snapshot.Workspace.Theme,
			DiffTheme:  snapshot.Workspace.DiffTheme,
			StayInTray: snapshot.Workspace.StayInTray,
		},
		Runtime: GUISettingsRuntimeState{
			ExecutionMode:  normalizeExecutionMode(fallbackText(strings.TrimSpace(mode), "auto")),
			SandboxMode:    NormalizeSandboxMode(snapshot.Workspace.SandboxMode),
			ReasoningLevel: fallbackText(strings.TrimSpace(reasoningLevel), "off"),
		},
		Context: GUISettingsContextState{
			AutoContext:          snapshot.Workspace.AutoContext,
			DesktopNotifications: snapshot.Workspace.DesktopNotifications,
			GitCommitReminder:    snapshot.Workspace.GitCommitReminder,
			GitCommitMarker:      snapshot.Workspace.GitCommitMarker,
			UseMemory:            snapshot.Workspace.UseMemory,
			PromptTimeoutSecs:    snapshot.Workspace.PromptTimeoutSecs,
			MaxInjectKB:          snapshot.Workspace.MaxInjectKB,
		},
		WatchPlan: GUISettingsWatchPlanState{
			WatchDebounceMs: snapshot.Workspace.WatchDebounceMs,
			PollIntervalSec: snapshot.Workspace.PollIntervalSec,
			PlanPromptStyle: snapshot.Workspace.PlanPromptStyle,
		},
		Meta: GUISettingsMetaState{
			GlobalConfigPath:      snapshot.GlobalConfigPath,
			WorkspaceSettingsPath: snapshot.WorkspaceSettingsPath,
			ActiveWorkspace:       strings.TrimSpace(activeWorkspace),
			LogDir:                snapshot.LogDir,
			WorkspaceTrusted:      workspaceTrusted,
			TrustedAt:             strings.TrimSpace(snapshot.Workspace.TrustedAt),
			WorkspaceCount:        len(workspaces),
			WindowSnapshot:        window,
		},
		MidRiskConfirm:     false,
		UpdateProxyEnabled: snapshot.UpdateProxyEnabled,
		UpdateProxyURL:     snapshot.UpdateProxyURL,
	}
}

func (s *BridgeService) settingsWorkspaceSource(workspace string) string {
	workspace = strings.TrimSpace(workspace)
	defaultWorkspace := s.defaultWorkspacePathReadOnly()
	if workspace != "" && !sameWorkspacePath(workspace, defaultWorkspace) {
		return workspace
	}
	return ""
}

func (s *BridgeService) settingsWorkspaceTarget() string {
	if workspace := s.settingsWorkspaceSource(s.activeWorkspaceValue()); workspace != "" {
		return workspace
	}
	snapshot := s.runtimeSnapshotReadOnly()
	if workspace := s.settingsWorkspaceSource(snapshot.ForegroundWorkspace); workspace != "" {
		return workspace
	}
	for _, item := range snapshot.Workspaces {
		if item.Active {
			if workspace := s.settingsWorkspaceSource(item.Path); workspace != "" {
				return workspace
			}
		}
	}
	return ""
}

// useMemoryInjectionReadOnly 返回当前设置作用域的「记忆注入」开关，
// 供 turn/start 下发 use_memory 前读取。设置文件缺失/不可读时按默认开
// 处理——与 decodeWorkspaceSettings 的缺省一致（旧配置升级后行为不变），
// 且注入最终裁决在内核（全局 [memories].use_memories），这里只透传。
func (s *BridgeService) useMemoryInjectionReadOnly() bool {
	if s == nil {
		return true
	}
	fallback := s.settingsReadOnly()
	snapshot, err := LoadGUISettings(
		s.configPathReadOnly(),
		s.settingsWorkspaceTarget(),
		GUISettingsDefaults{
			Language: fallbackText(strings.TrimSpace(fallback.Language), "zh"),
			Theme:    fallbackText(strings.TrimSpace(fallback.Theme), "system"),
		},
	)
	if err != nil {
		return true
	}
	return snapshot.Workspace.UseMemory
}

func (s *BridgeService) syncSandboxModeForWorkspace(workspace string) string {
	return s.syncSandboxModeForSession(workspace, "")
}

// syncSandboxModeForSession 把沙箱模式恢复到内核，按会话维度生效：
//   - sessionID 非空且该会话 metadata 记录了 sandbox_mode → 用会话值（per-session 持久化）。
//   - 否则回落到 <workspace>/.eos/settings.json 的全局 sandbox_mode（默认 workspace）。
//
// 新会话没有 metadata 记录，自然回落到默认 workspace，符合「新会话默认工作区」语义。
// 走 applySandboxModeSemantics 单一入口：恢复 full_access 会话时同样推进复合态
// （approval=never + danger policy + 收口待审卡），否则切回完全访问会话后审批卡
// 会继续弹；恢复非 full_access 时审批轴复位 on-request。
// 调用方不得持有 stateMu（复合入口的待审收口需要拿锁）。
func (s *BridgeService) syncSandboxModeForSession(workspace, sessionID string) string {
	workspace = strings.TrimSpace(workspace)
	sessionID = strings.TrimSpace(sessionID)
	mode := ""
	if sessionID != "" {
		mode = NormalizeSandboxMode(s.sessionSandboxModeReadOnly(sessionID))
	}
	if mode == "" {
		fallback := s.settingsReadOnly()
		snapshot, err := LoadGUISettings(
			s.configPathReadOnly(),
			s.settingsWorkspaceSource(workspace),
			GUISettingsDefaults{
				Language: fallbackText(strings.TrimSpace(fallback.Language), "zh"),
				Theme:    fallbackText(strings.TrimSpace(fallback.Theme), "system"),
			},
		)
		if err == nil {
			mode = NormalizeSandboxMode(snapshot.Workspace.SandboxMode)
		}
	}
	if mode == "" {
		mode = "workspace"
	}
	if err := s.applySandboxModeSemantics(workspace, mode); err != nil {
		slog.Warn("bridge.settings.sync_sandbox_mode.failed", "workspace", workspace, "mode", mode, "error", err)
	}
	return mode
}

// sessionSandboxModeReadOnly 从内核读取单个会话 metadata 里的 sandbox_mode。
// 内核是单一数据源（session/set_meta 持久化），壳层只读。找不到会话或无记录返回空串。
func (s *BridgeService) sessionSandboxModeReadOnly(sessionID string) string {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return ""
	}
	workspace := s.workspaceForSessionFromSnapshotReadOnly(sessionID, s.runtimeSnapshotReadOnly())
	if workspace == "" {
		workspace = s.activeWorkspaceValue()
	}
	metas, err := s.listWorkspaceSessionsReadOnly(workspace)
	if err != nil {
		return ""
	}
	for _, meta := range metas {
		if meta.ID == sessionID {
			return strings.TrimSpace(meta.SandboxMode)
		}
	}
	return ""
}

func normalizeReasoningLevel(level string) string {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "low", "medium", "high":
		return strings.ToLower(strings.TrimSpace(level))
	default:
		return "off"
	}
}
