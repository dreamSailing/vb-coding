package webbridge

import (
	"context"
	"encoding/json"

	"github.com/eosaios/eos/internal/webbridge/adapter"
	"github.com/eosaios/eos/pkg/coreapi"
	"github.com/eosaios/eos/pkg/sandbox"
)

type bridgeRuntimeGateway interface {
	CoreBrowserControlTakeoverRPC(context.Context, coreapi.BrowserControlTakeoverRequest) error
	CoreBrowserControlResumeRPC(context.Context) error
	CoreBrowserUploadProvideRPC(context.Context, string, []string) error
	CoreBrowserFocusRPC(context.Context) error
	CoreBrowserSetDefaultProfileRPC(context.Context, string) error
	CoreBrowserNavigateRPC(context.Context, string) error
	CoreBrowserTabNewRPC(context.Context, string) (coreapi.BrowserTabInfo, error)
	CoreBrowserTabSwitchRPC(context.Context, int) (coreapi.BrowserTabInfo, error)
	CoreBrowserTabCloseRPC(context.Context, *int) error
	CoreBrowserLiveStartRPC(context.Context, coreapi.BrowserLiveStartRequest) error
	CoreBrowserLiveStopRPC(context.Context) error
	CoreBrowserInputRPC(context.Context, coreapi.BrowserInputRequest) error
	CoreBrowserHistoryRPC(context.Context, string) error
	CoreBrowserProfilesRPC(context.Context) ([]coreapi.BrowserProfileRecord, error)
	CoreBrowserPickStartRPC(context.Context) error
	CoreBrowserPickStopRPC(context.Context) error
	CoreRunBashStreamRPC(context.Context, string) (<-chan adapter.Event, error)
	CoreStartTurnStreamWithRequestRPC(context.Context, coreapi.StartTurnRequest) (<-chan adapter.Event, coreapi.Turn, error)
	CoreResumeTurnStreamRPC(context.Context, string, string) (<-chan adapter.Event, coreapi.Turn, error)
	CoreMemorySaveRPC(context.Context, string) error
	CoreGitBranchesRPC(context.Context, string) (coreapi.GitBranchesResult, error)
	CoreGitSummaryRPC(context.Context, string) (coreapi.GitSummaryResult, error)
	CoreGitReposRPC(context.Context, string) (coreapi.GitReposResult, error)
	CoreGitStageRPC(context.Context, string, []string, bool, bool) error
	CoreGitCommitRPC(context.Context, string, string) (coreapi.GitCommitResult, error)
	CoreGitPushRPC(context.Context, string) (coreapi.GitPushResult, error)
	CoreGitMergeAbortRPC(context.Context, string) error
	CoreGitSuggestMessageRPC(context.Context, string) (coreapi.GitSuggestMessageResult, error)
	CoreInterruptTurnRPC(context.Context, string, string) error
	RunBash(context.Context, string) (<-chan adapter.Event, error)
	Invoke(context.Context, string) (<-chan adapter.Event, error)
	CoreConfigPath() string
	CoreDefaultWorkspaceRPC(context.Context) (string, error)
	CoreResolveForegroundWorkspaceRPC(context.Context, string) (string, error)
	CoreLastWorkspaceRPC(context.Context) (string, error)
	CoreAddWorkspaceRPC(context.Context, string) error
	CoreRemoveWorkspaceRPC(context.Context, string) error
	CoreUseWorkspaceRPC(context.Context, string) error
	CoreTrustWorkspaceRPC(context.Context, string) error
	CoreRememberWorkspaceRPC(context.Context, string, bool) error
	CoreCreateWorktreeRPC(context.Context, string) (adapter.Worktree, error)
	CoreRemoveWorktreeRPC(context.Context, string, bool) error
	CoreOpenRemoteWorkspaceRPC(context.Context, string) (adapter.RemoteWorkspace, error)
	OpenRemoteWorkspace(string) (adapter.RemoteWorkspace, error)
	CoreForgetRemoteWorkspaceRPC(context.Context, string) error
	ForgetRemoteWorkspace(string) error
	CoreClearRemoteWorkspaceCacheRPC(context.Context, string) error
	ClearRemoteWorkspaceCache(string) error
	CoreModeSnapshotRPC(context.Context) (coreapi.ModeSnapshot, error)
	CoreSetExecutionModeRPC(context.Context, string) error
	CoreGoalSetRPC(context.Context, coreapi.GoalSetRequest) (coreapi.ThreadGoal, error)
	CoreGoalGetRPC(context.Context, string) (coreapi.GoalGetResponse, error)
	CoreGoalPauseRPC(context.Context, string) (coreapi.ThreadGoal, error)
	CoreGoalResumeRPC(context.Context, string) (coreapi.ThreadGoal, error)
	CoreGoalClearRPC(context.Context, string) error
	CoreSetSandboxModeRPC(context.Context, string) error
	CoreSetSandboxPolicyRPC(context.Context, string, sandbox.Policy) error
	CoreDeriveSandboxPolicyRPC(context.Context, string, string) (sandbox.Policy, error)
	CoreEnterFullAccessRPC(context.Context, string) (sandbox.Policy, error)
	CoreApprovalPreviewRPC(context.Context, coreapi.ApprovalPreviewRequest) (coreapi.ApprovalPreviewResponse, error)
	CoreSetApprovalModeRPC(context.Context, string) error
	CoreSetReasoningLevelRPC(context.Context, string) error
	CoreListSessionsRPC(context.Context, string) ([]coreapi.Session, error)
	CoreListArchivedSessionsRPC(context.Context) ([]coreapi.Session, error)
	CoreCurrentSessionRPC(context.Context, string) (adapter.SessionMeta, error)
	CoreCreateSessionRPC(context.Context, string, string, string, []adapter.SessionMessage) (adapter.SessionMeta, error)
	CoreDeleteSessionRPC(context.Context, string, string) error
	CoreRenameSessionRPC(context.Context, string, string, string) (adapter.SessionMeta, error)
	CoreArchiveSessionRPC(context.Context, string, bool) error
	CoreSetSessionSandboxModeRPC(context.Context, string, string) error
	CoreLoadSessionMessagesRPC(context.Context, string, string) ([]adapter.SessionMessage, error)
	CoreSaveSessionMessagesRPC(context.Context, string, string, []adapter.SessionMessage) (adapter.SessionMeta, error)
	CoreRuntimeSnapshotRPC(context.Context) (adapter.RuntimeSnapshot, error)
	CoreStateSnapshotRPC(context.Context) (coreapi.StateSnapshot, error)
	ResolveSessionWorkspace(string) (string, error)
	CoreResumeSessionRPC(context.Context, string, string) (adapter.SessionMeta, error)
	CoreSetCurrentSessionRPC(context.Context, string, string) error
	CoreListModelsRPC(context.Context) ([]adapter.ModelConfig, error)
	CoreModelCatalogRPC(context.Context) (adapter.ModelCatalogState, error)
	CoreUpsertModelRPC(context.Context, string, string, string, string) error
	CoreSaveModelRPC(context.Context, adapter.ModelSaveRequest) error
	CoreVerifyModelRPC(context.Context, adapter.ModelSaveRequest) (coreapi.ModelVerifyResponse, error)
	CoreActivateModelRPC(context.Context, string) error
	CoreDeleteModelRPC(context.Context, string) error
	CoreListRemoteWorkspacesRPC(context.Context) ([]adapter.RemoteWorkspace, error)
	CoreCurrentRemoteRepoRPC(context.Context) (adapter.RemoteRepoState, bool, error)
	CorePredictNextUserMessageRPC(context.Context, string) (string, error)
	CoreRefineInputRPC(context.Context, string) (string, error)
	CoreListMCPRPC(context.Context) ([]adapter.MCPServer, error)
	CoreUpsertMCPRPC(context.Context, string, string, string, bool) error
	CoreImportMCPJSONRPC(context.Context, string) error
	CoreDeleteMCPRPC(context.Context, string) error
	CoreSetMCPEnabledRPC(context.Context, string, bool) error
	CoreListLSPRPC(context.Context) ([]adapter.LSPServer, error)
	CoreDetectLSPRPC(context.Context, string) (string, error)
	CoreStartLSPRPC(context.Context, string) (string, error)
	CoreInstallLSPRPC(context.Context, string) (string, error)
	CoreListSkillsRPC(context.Context) ([]adapter.SkillInfo, error)
	CoreReloadSkillsRPC(context.Context) error
	CoreSetSkillEnabledRPC(context.Context, string, bool) error
	CoreListPluginsRPC(context.Context) ([]adapter.PluginInfo, error)
	CoreSetPluginEnabledRPC(context.Context, string, bool) error
	CoreListWorktreesRPC(context.Context) ([]adapter.Worktree, error)
	CoreUsageSummaryRPC(context.Context) (adapter.UsageSummary, error)
	CoreCostItemsRPC(context.Context) ([]adapter.CostItem, error)
	CoreNetworkListRPC(context.Context, int) (coreapi.NetworkListResult, error)
	CoreToolExecuteRPC(context.Context, json.RawMessage) (json.RawMessage, error)
	CoreCallRPC(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error)
	CoreNetworkClearRPC(context.Context) (int, error)
	CoreListVersionsRPC(context.Context) ([]adapter.VersionItem, error)
	CoreRollbackVersionRPC(context.Context, string) error
	CoreDeleteVersionRPC(context.Context, string) error
	CoreClearVersionsRPC(context.Context) (int, error)
	CoreGetSettingsRPC(context.Context) (adapter.GUISettings, error)
	CoreGetFullSettingsRPC(context.Context) (coreapi.Settings, error)
	CoreSaveSettingsRPC(context.Context, coreapi.Settings) error
	CoreApprovalListRPC(context.Context, coreapi.PendingApprovalListRequest) (coreapi.PendingApprovalList, error)
	CoreRespondApprovalRPC(context.Context, string, coreapi.ApprovalDecision) error
	CoreRespondApprovalWithReasonRPC(context.Context, string, coreapi.ApprovalDecision, string) error
	CoreWorkspaceRollbackApplyRPC(context.Context, string, []coreapi.TurnRollback) error
	ResolveConfirmation(string, coreapi.ApprovalDecision)
	CorePermissionSnapshotRPC(context.Context) (adapter.PermissionSnapshot, error)
	ThreadCoreIfExists(string) adapter.Core
	CorePlanSnapshotRPC(context.Context) (adapter.PlanSnapshot, error)
	CoreMemorySnapshotRPC(context.Context) (adapter.MemorySnapshot, error)
	CorePendingReviewRPC(context.Context) (adapter.PendingReview, error)
	CoreLSPDiagnosticsRPC(context.Context) ([]string, error)
	CoreContextPreviewRPC(context.Context) ([]string, error)
	CoreContextStatsRPC(context.Context) (adapter.ContextStats, error)
	CoreCostSummaryRPC(context.Context) (string, error)
	CoreKillTaskRPC(context.Context, string) error
	CoreCleanupTasksRPC(context.Context) (int, error)
	CoreTaskListRPC(context.Context) ([]coreapi.TaskSnapshot, error)
	CoreSubscribeEventsRPC(context.Context, string, string, string, int) (<-chan adapter.Event, func(), error)
	StartupDiagnostics() adapter.StartupDiagnosticsResult
}

var _ bridgeRuntimeGateway = (*adapter.StdioGateway)(nil)

func (s *BridgeService) runtimeGatewayClient() bridgeRuntimeGateway {
	if s == nil {
		return nil
	}
	return s.runtimeGateway
}
