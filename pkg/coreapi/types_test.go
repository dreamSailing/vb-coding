package coreapi

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/eosaios/eos/pkg/protocol"
	"github.com/eosaios/eos/pkg/sandbox"
)

type compileTimeEngine struct{}

func (compileTimeEngine) Caller() Caller                 { return nil }
func (compileTimeEngine) State() StateService            { return nil }
func (compileTimeEngine) Workspaces() WorkspaceService   { return nil }
func (compileTimeEngine) Sessions() SessionService       { return nil }
func (compileTimeEngine) MCP() MCPService                { return nil }
func (compileTimeEngine) LSP() LSPService                { return nil }
func (compileTimeEngine) Config() ConfigService          { return nil }
func (compileTimeEngine) Permissions() PermissionService { return nil }
func (compileTimeEngine) Extensions() ExtensionService   { return nil }
func (compileTimeEngine) Context() ContextService        { return nil }
func (compileTimeEngine) Usage() UsageService            { return nil }
func (compileTimeEngine) Versions() VersionService       { return nil }
func (compileTimeEngine) Tasks() TaskService             { return nil }
func (compileTimeEngine) Goals() GoalService             { return nil }
func (compileTimeEngine) Modes() ModeService             { return nil }
func (compileTimeEngine) Models() ModelService           { return nil }
func (compileTimeEngine) RemoteWorkspaces() RemoteWorkspaceService {
	return nil
}
func (compileTimeEngine) Git() GitService                 { return nil }
func (compileTimeEngine) Insights() InsightService        { return nil }
func (compileTimeEngine) Memory() MemoryService           { return nil }
func (compileTimeEngine) Roles() RoleService              { return nil }
func (compileTimeEngine) Turns() TurnService              { return nil }
func (compileTimeEngine) Approvals() ApprovalService      { return nil }
func (compileTimeEngine) Inquiries() InquiryService       { return nil }
func (compileTimeEngine) Agents() AgentService            { return nil }
func (compileTimeEngine) Tools() ToolExecutor             { return nil }
func (compileTimeEngine) ToolCatalog() ToolCatalogService { return nil }
func (compileTimeEngine) ToolTelemetry() ToolTelemetryService {
	return nil
}
func (compileTimeEngine) Events() EventSubscriber         { return nil }
func (compileTimeEngine) Sandbox() SandboxService         { return nil }
func (compileTimeEngine) Diagnostics() DiagnosticsService { return nil }

type compileTimeSandbox struct{}

func (compileTimeSandbox) Policy(context.Context, SessionRef) (sandbox.Policy, error) {
	return sandbox.Policy{}, nil
}
func (compileTimeSandbox) SetPolicy(context.Context, SessionRef, sandbox.Policy) error { return nil }
func (compileTimeSandbox) DerivePolicy(context.Context, DeriveSandboxPolicyRequest) (sandbox.Policy, error) {
	return sandbox.Policy{}, nil
}
func (compileTimeSandbox) BackendStatus(context.Context) sandbox.BackendStatus {
	return sandbox.BackendStatus{}
}

type compileTimeEvents struct{}

func (compileTimeEvents) Subscribe(context.Context, EventFilter) (<-chan protocol.Envelope, error) {
	return nil, nil
}

type compileTimeToolTelemetry struct{}

func (compileTimeToolTelemetry) Traces(context.Context) ([]ToolTrace, error) { return nil, nil }
func (compileTimeToolTelemetry) Stats(context.Context) ([]ToolStat, error)   { return nil, nil }

type compileTimeToolCatalog struct{}

func (compileTimeToolCatalog) List(context.Context, ListToolCatalogRequest) ([]ToolDefinition, error) {
	return nil, nil
}

type compileTimeState struct{}

func (compileTimeState) Snapshot(context.Context, StateSnapshotRequest) (StateSnapshot, error) {
	return StateSnapshot{}, nil
}

type compileTimeWorkspaces struct{}

func (compileTimeWorkspaces) List(context.Context, WorkspaceListRequest) ([]Workspace, error) {
	return nil, nil
}
func (compileTimeWorkspaces) Default(context.Context) (string, error) { return "", nil }
func (compileTimeWorkspaces) Last(context.Context) (string, error)    { return "", nil }
func (compileTimeWorkspaces) ResolveForeground(context.Context, ResolveForegroundWorkspaceRequest) (string, error) {
	return "", nil
}
func (compileTimeWorkspaces) Remember(context.Context, RememberWorkspaceRequest) error  { return nil }
func (compileTimeWorkspaces) Forget(context.Context, WorkspacePathRequest) error        { return nil }
func (compileTimeWorkspaces) Add(context.Context, WorkspacePathRequest) error           { return nil }
func (compileTimeWorkspaces) Remove(context.Context, WorkspacePathRequest) error        { return nil }
func (compileTimeWorkspaces) Use(context.Context, WorkspacePathRequest) error           { return nil }
func (compileTimeWorkspaces) SetForeground(context.Context, WorkspacePathRequest) error { return nil }
func (compileTimeWorkspaces) Trust(context.Context, WorkspacePathRequest) error         { return nil }
func (compileTimeWorkspaces) ListWorktrees(context.Context) ([]Worktree, error)         { return nil, nil }
func (compileTimeWorkspaces) CreateWorktree(context.Context, CreateWorktreeRequest) (Worktree, error) {
	return Worktree{}, nil
}
func (compileTimeWorkspaces) RemoveWorktree(context.Context, RemoveWorktreeRequest) error { return nil }

type compileTimeSessions struct{}

func (compileTimeSessions) Create(context.Context, CreateSessionRequest) (Session, error) {
	return Session{}, nil
}
func (compileTimeSessions) Resume(context.Context, ResumeSessionRequest) (Session, error) {
	return Session{}, nil
}
func (compileTimeSessions) List(context.Context, ListSessionsRequest) ([]Session, error) {
	return nil, nil
}
func (compileTimeSessions) Current(context.Context, CurrentSessionRequest) (Session, error) {
	return Session{}, nil
}
func (compileTimeSessions) SetCurrent(context.Context, SetCurrentSessionRequest) error { return nil }
func (compileTimeSessions) Delete(context.Context, DeleteSessionRequest) error         { return nil }
func (compileTimeSessions) Rename(context.Context, RenameSessionRequest) (Session, error) {
	return Session{}, nil
}
func (compileTimeSessions) SetMeta(context.Context, SetSessionMetaRequest) (Session, error) {
	return Session{}, nil
}
func (compileTimeSessions) LoadMessages(context.Context, LoadSessionMessagesRequest) ([]SessionMessage, error) {
	return nil, nil
}
func (compileTimeSessions) SaveMessages(context.Context, SaveSessionMessagesRequest) (Session, error) {
	return Session{}, nil
}

type compileTimeMCP struct{}

func (compileTimeMCP) List(context.Context) ([]MCPServer, error)              { return nil, nil }
func (compileTimeMCP) Upsert(context.Context, UpsertMCPRequest) error         { return nil }
func (compileTimeMCP) ImportJSON(context.Context, ImportMCPJSONRequest) error { return nil }
func (compileTimeMCP) Delete(context.Context, MCPNameRequest) error           { return nil }
func (compileTimeMCP) SetEnabled(context.Context, SetMCPEnabledRequest) error { return nil }

type compileTimeLSP struct{}

func (compileTimeLSP) List(context.Context) ([]LSPServer, error) { return nil, nil }
func (compileTimeLSP) Detect(context.Context, LSPLanguageRequest) (string, error) {
	return "", nil
}
func (compileTimeLSP) Start(context.Context, LSPLanguageRequest) (string, error) {
	return "", nil
}
func (compileTimeLSP) Install(context.Context, LSPLanguageRequest) (string, error) {
	return "", nil
}
func (compileTimeLSP) Diagnostics(context.Context) ([]string, error) { return nil, nil }
func (compileTimeLSP) DiagnosticsSummary(context.Context) (LSPDiagnosticsSummary, error) {
	return LSPDiagnosticsSummary{}, nil
}

type compileTimeConfig struct{}

func (compileTimeConfig) GetRules(context.Context) (string, error) { return "", nil }
func (compileTimeConfig) RulesSnapshot(context.Context) (RulesSnapshot, error) {
	return RulesSnapshot{}, nil
}
func (compileTimeConfig) SaveRules(context.Context, SaveRulesRequest) error { return nil }
func (compileTimeConfig) ResetRules(context.Context) error                  { return nil }
func (compileTimeConfig) GetSettings(context.Context) (Settings, error)     { return Settings{}, nil }
func (compileTimeConfig) SaveSettings(context.Context, Settings) error      { return nil }

type compileTimePermissions struct{}

func (compileTimePermissions) Snapshot(context.Context) (PermissionSnapshot, error) {
	return PermissionSnapshot{}, nil
}
func (compileTimePermissions) PendingReview(context.Context) (PendingReview, error) {
	return PendingReview{}, nil
}
func (compileTimePermissions) ClearPendingReview(context.Context) error { return nil }
func (compileTimePermissions) SetAccessMode(context.Context, SetModeRequest) error {
	return nil
}
func (compileTimePermissions) SetApprovalMode(context.Context, SetModeRequest) error {
	return nil
}
func (compileTimePermissions) EnterFullAccess(context.Context, EnterFullAccessRequest) error {
	return nil
}

type compileTimeExtensions struct{}

func (compileTimeExtensions) ListSkills(context.Context) ([]SkillInfo, error) { return nil, nil }
func (compileTimeExtensions) ReloadSkills(context.Context) error              { return nil }
func (compileTimeExtensions) SetSkillEnabled(context.Context, SetExtensionEnabledRequest) error {
	return nil
}
func (compileTimeExtensions) InvokeSkill(context.Context, InvokeSkillRequest) (InvokeSkillResult, error) {
	return InvokeSkillResult{}, nil
}
func (compileTimeExtensions) ListPlugins(context.Context) ([]PluginInfo, error) { return nil, nil }
func (compileTimeExtensions) SetPluginEnabled(context.Context, SetExtensionEnabledRequest) error {
	return nil
}
func (compileTimeExtensions) BrowserStatus(context.Context) (BrowserRuntimeStatus, error) {
	return BrowserRuntimeStatus{}, nil
}

func (compileTimeExtensions) BrowserLaunch(ctx context.Context, req BrowserLaunchRequest) error {
	return nil
}

func (compileTimeExtensions) BrowserClose(ctx context.Context, req BrowserCloseRequest) error {
	return nil
}

func (compileTimeExtensions) BrowserControlTakeover(ctx context.Context, req BrowserControlTakeoverRequest) error {
	return nil
}

func (compileTimeExtensions) BrowserControlResume(ctx context.Context) error {
	return nil
}

func (compileTimeExtensions) BrowserTabs(ctx context.Context) ([]BrowserTabInfo, error) {
	return nil, nil
}

func (compileTimeExtensions) BrowserProfiles(ctx context.Context) ([]BrowserProfileRecord, error) {
	return nil, nil
}

type compileTimeContext struct{}

func (compileTimeContext) Preview(context.Context) ([]string, error)   { return nil, nil }
func (compileTimeContext) Stats(context.Context) (ContextStats, error) { return ContextStats{}, nil }
func (compileTimeContext) WindowTokens(context.Context) (int, error)   { return 0, nil }
func (compileTimeContext) PinDocument(context.Context, PinDocumentRequest) error {
	return nil
}
func (compileTimeContext) Compact(context.Context) (string, error)            { return "", nil }
func (compileTimeContext) Clear(context.Context) error                        { return nil }
func (compileTimeContext) Export(context.Context, ExportContextRequest) error { return nil }

type compileTimeUsage struct{}

func (compileTimeUsage) Summary(context.Context) (UsageSummary, error) { return UsageSummary{}, nil }
func (compileTimeUsage) CostSummary(context.Context) (string, error)   { return "", nil }
func (compileTimeUsage) CostItems(context.Context) ([]CostItem, error) { return nil, nil }

type compileTimeVersions struct{}

func (compileTimeVersions) List(context.Context) ([]VersionItem, error) { return nil, nil }
func (compileTimeVersions) Rollback(context.Context, VersionIDRequest) error {
	return nil
}
func (compileTimeVersions) Delete(context.Context, VersionIDRequest) error { return nil }
func (compileTimeVersions) DeleteFile(context.Context, VersionFileRequest) (int, error) {
	return 0, nil
}
func (compileTimeVersions) Clear(context.Context) (int, error) { return 0, nil }

type compileTimeTasks struct{}

func (compileTimeTasks) List(context.Context) ([]TaskSnapshot, error) { return nil, nil }
func (compileTimeTasks) Todos(context.Context) ([]TodoItem, error)    { return nil, nil }
func (compileTimeTasks) Tail(context.Context, TaskIDRequest) ([]string, error) {
	return nil, nil
}
func (compileTimeTasks) Kill(context.Context, TaskIDRequest) error { return nil }
func (compileTimeTasks) Cleanup(context.Context) (int, error)      { return 0, nil }

type compileTimeModes struct{}

func (compileTimeModes) Snapshot(context.Context) (ModeSnapshot, error) { return ModeSnapshot{}, nil }
func (compileTimeModes) SetExecutionMode(context.Context, SetModeRequest) error {
	return nil
}
func (compileTimeModes) SetSandboxMode(context.Context, SetModeRequest) error { return nil }
func (compileTimeModes) SetReasoningLevel(context.Context, SetModeRequest) error {
	return nil
}

type compileTimeModels struct{}

func (compileTimeModels) List(context.Context) ([]ModelConfig, error) { return nil, nil }
func (compileTimeModels) Catalog(context.Context) (ModelCatalogState, error) {
	return ModelCatalogState{}, nil
}
func (compileTimeModels) Upsert(context.Context, UpsertModelRequest) error { return nil }
func (compileTimeModels) Save(context.Context, ModelSaveRequest) error     { return nil }
func (compileTimeModels) Delete(context.Context, ModelNameRequest) error   { return nil }
func (compileTimeModels) Activate(context.Context, ModelNameRequest) error { return nil }
func (compileTimeModels) SyncEnv(context.Context) error                    { return nil }
func (compileTimeModels) Context(context.Context, ModelContextRequest) (ModelContextSnapshot, error) {
	return ModelContextSnapshot{}, nil
}
func (compileTimeModels) SetWorkspace(context.Context, SetWorkspaceModelRequest) error {
	return nil
}
func (compileTimeModels) ClearWorkspace(context.Context, ClearWorkspaceModelRequest) error {
	return nil
}
func (compileTimeModels) SetSession(context.Context, SetSessionModelRequest) error {
	return nil
}
func (compileTimeModels) ClearSession(context.Context, ClearSessionModelRequest) error {
	return nil
}

type compileTimeRemoteWorkspaces struct{}

func (compileTimeRemoteWorkspaces) List(context.Context) ([]RemoteWorkspace, error) {
	return nil, nil
}
func (compileTimeRemoteWorkspaces) Open(context.Context, RemoteWorkspaceRef) (RemoteWorkspace, error) {
	return RemoteWorkspace{}, nil
}
func (compileTimeRemoteWorkspaces) Forget(context.Context, RemoteWorkspaceRef) error {
	return nil
}
func (compileTimeRemoteWorkspaces) ClearCache(context.Context, RemoteWorkspaceRef) error {
	return nil
}
func (compileTimeRemoteWorkspaces) CurrentRepo(context.Context) (RemoteRepoState, bool, error) {
	return RemoteRepoState{}, false, nil
}

type compileTimeGit struct{}

func (compileTimeGit) Status(context.Context, GitStatusRequest) ([]GitChange, error) {
	return nil, nil
}
func (compileTimeGit) Summary(context.Context, GitSummaryRequest) (GitSummaryResult, error) {
	return GitSummaryResult{}, nil
}
func (compileTimeGit) Diff(context.Context, GitDiffRequest) (GitTextResult, error) {
	return GitTextResult{}, nil
}
func (compileTimeGit) Branches(context.Context, GitBranchesRequest) (GitBranchesResult, error) {
	return GitBranchesResult{}, nil
}
func (compileTimeGit) Log(context.Context, GitLogRequest) (GitLogResult, error) {
	return GitLogResult{}, nil
}
func (compileTimeGit) Show(context.Context, GitShowRequest) (GitShowResult, error) {
	return GitShowResult{}, nil
}

type compileTimeInsights struct{}

func (compileTimeInsights) PredictNextUserMessage(context.Context, PredictNextUserMessageRequest) (string, error) {
	return "", nil
}
func (compileTimeInsights) RefineInput(context.Context, RefineInputRequest) (string, error) {
	return "", nil
}
func (compileTimeInsights) PlanSnapshot(context.Context) (PlanSnapshot, error) {
	return PlanSnapshot{}, nil
}
func (compileTimeInsights) MemorySnapshot(context.Context) (MemorySnapshot, error) {
	return MemorySnapshot{}, nil
}

type compileTimeMemory struct{}

func (compileTimeMemory) Snapshot(context.Context) (MemorySnapshot, error) {
	return MemorySnapshot{}, nil
}
func (compileTimeMemory) Save(context.Context, SaveMemoryRequest) error { return nil }
func (compileTimeMemory) RebuildIndex(context.Context) error            { return nil }
func (compileTimeMemory) RecordAdd(context.Context, AddMemoryRecordRequest) (MemoryRecord, error) {
	return MemoryRecord{}, nil
}
func (compileTimeMemory) RecordList(context.Context, ListMemoryRecordsRequest) ([]MemoryRecord, error) {
	return nil, nil
}
func (compileTimeMemory) RecordSearch(context.Context, SearchMemoryRecordsRequest) ([]MemoryRecord, error) {
	return nil, nil
}
func (compileTimeMemory) RecordDelete(context.Context, DeleteMemoryRecordRequest) error { return nil }

type compileTimeRoles struct{}

func (compileTimeRoles) List(context.Context) ([]RoleConfig, error) { return nil, nil }
func (compileTimeRoles) Resolve(context.Context, RoleRef) (RoleConfig, error) {
	return RoleConfig{}, nil
}

func TestInterfacesAreSatisfiable(t *testing.T) {
	var _ Engine = compileTimeEngine{}
	var _ StateService = compileTimeState{}
	var _ WorkspaceService = compileTimeWorkspaces{}
	var _ SessionService = compileTimeSessions{}
	var _ MCPService = compileTimeMCP{}
	var _ LSPService = compileTimeLSP{}
	var _ ConfigService = compileTimeConfig{}
	var _ PermissionService = compileTimePermissions{}
	var _ ExtensionService = compileTimeExtensions{}
	var _ ContextService = compileTimeContext{}
	var _ UsageService = compileTimeUsage{}
	var _ VersionService = compileTimeVersions{}
	var _ TaskService = compileTimeTasks{}
	var _ ModeService = compileTimeModes{}
	var _ ModelService = compileTimeModels{}
	var _ RemoteWorkspaceService = compileTimeRemoteWorkspaces{}
	var _ GitService = compileTimeGit{}
	var _ InsightService = compileTimeInsights{}
	var _ MemoryService = compileTimeMemory{}
	var _ RoleService = compileTimeRoles{}
	var _ ToolTelemetryService = compileTimeToolTelemetry{}
	var _ ToolCatalogService = compileTimeToolCatalog{}
	var _ SandboxService = compileTimeSandbox{}
	var _ EventSubscriber = compileTimeEvents{}
}

func TestErrUnsupportedSupportsErrorsIs(t *testing.T) {
	if !errors.Is(ErrUnsupported, ErrUnsupported) {
		t.Fatal("ErrUnsupported should support errors.Is")
	}
}

func TestStartTurnRequestUseMemorySerialization(t *testing.T) {
	enabled := false
	set := StartTurnRequest{SessionID: "s1", Input: "hi", UseMemory: &enabled}
	body, err := json.Marshal(set)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(body), `"use_memory":false`) {
		t.Fatalf("expected use_memory field in %s", body)
	}

	unset := StartTurnRequest{SessionID: "s1", Input: "hi"}
	body, err = json.Marshal(unset)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(body), "use_memory") {
		t.Fatalf("expected use_memory omitted when nil, got %s", body)
	}
}

func TestThreadGoalWireShapeMatchesKernelCamelCase(t *testing.T) {
	budget := int64(50000)
	goal := ThreadGoal{
		ThreadID:        "s1",
		GoalID:          "goal_1",
		Objective:       "让测试全绿",
		Status:          "active",
		TokenBudget:     &budget,
		TokensUsed:      1200,
		TimeUsedSeconds: 90,
		CreatedAt:       1787145692,
		UpdatedAt:       1787145782,
	}
	body, err := json.Marshal(goal)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, key := range []string{
		`"threadId"`, `"goalId"`, `"objective"`, `"status"`,
		`"tokenBudget"`, `"tokensUsed"`, `"timeUsedSeconds"`,
		`"createdAt"`, `"updatedAt"`,
	} {
		if !strings.Contains(string(body), key) {
			t.Fatalf("expected %s in %s", key, body)
		}
	}

	// goal/get 响应：无目标时 goal 字段整体省略（内核 skip_serializing_if）。
	empty, err := json.Marshal(GoalGetResponse{})
	if err != nil {
		t.Fatalf("marshal empty get response: %v", err)
	}
	if strings.Contains(string(empty), "goal") {
		t.Fatalf("expected goal omitted when nil, got %s", empty)
	}

	// goal/set 请求：tokenBudget 缺省省略，sessionId/objective 必带。
	req, err := json.Marshal(GoalSetRequest{SessionID: "s1", Objective: "ship it"})
	if err != nil {
		t.Fatalf("marshal set request: %v", err)
	}
	if !strings.Contains(string(req), `"sessionId"`) || strings.Contains(string(req), "tokenBudget") {
		t.Fatalf("unexpected set request shape: %s", req)
	}
}
