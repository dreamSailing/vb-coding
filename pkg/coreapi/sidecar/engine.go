package sidecar

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/eosaios/eos/pkg/coreapi"
	coreapijsonrpc "github.com/eosaios/eos/pkg/coreapi/jsonrpc"
	"github.com/eosaios/eos/pkg/protocol"
	protocoljsonrpc "github.com/eosaios/eos/pkg/protocol/jsonrpc"
	"github.com/eosaios/eos/pkg/sandbox"
)

var ErrCallerUnavailable = errors.New("core sidecar caller is unavailable")

type Caller interface {
	Call(context.Context, string, any, any) error
}

type RemoteEngine struct {
	caller Caller
	client *ProcessClient

	initMu     sync.Mutex
	initResult coreapijsonrpc.InitializeResult
	initDone   bool

	eventMu          sync.RWMutex
	eventSubscribers map[uint64]*eventSubscriber
	nextEventID      atomic.Uint64
}

type eventSubscriber struct {
	ch     chan protocol.Envelope
	cancel context.CancelFunc
}

func NewRemoteEngine(caller Caller) *RemoteEngine {
	e := &RemoteEngine{
		caller:           caller,
		eventSubscribers: make(map[uint64]*eventSubscriber),
	}
	return e
}

func StartRemoteEngine(ctx context.Context, opts ProcessOptions) (*RemoteEngine, error) {
	if len(opts.Args) == 0 {
		opts.Args = []string{"--stdio"}
	}
	engine := &RemoteEngine{
		eventSubscribers: make(map[uint64]*eventSubscriber),
	}
	opts.NotificationHandler = engine.handleNotification
	client, err := StartProcess(ctx, opts)
	if err != nil {
		return nil, err
	}
	engine.caller = client
	engine.client = client
	if _, err := engine.Initialize(ctx); err != nil {
		_ = client.Close()
		return nil, err
	}
	go func() {
		<-client.Wait()
		engine.closeAllSubscribers()
	}()
	return engine, nil
}

func (e *RemoteEngine) Initialize(ctx context.Context) (coreapijsonrpc.InitializeResult, error) {
	e.initMu.Lock()
	if e.initDone {
		result := e.initResult
		e.initMu.Unlock()
		return result, nil
	}
	e.initMu.Unlock()

	var result coreapijsonrpc.InitializeResult
	if err := e.call(ctx, protocoljsonrpc.MethodInitialize, nil, &result); err != nil {
		return coreapijsonrpc.InitializeResult{}, err
	}
	e.initMu.Lock()
	e.initResult = result
	e.initDone = true
	e.initMu.Unlock()
	return result, nil
}

func (e *RemoteEngine) Shutdown(ctx context.Context) error {
	var result map[string]any
	return e.call(ctx, protocoljsonrpc.MethodShutdown, nil, &result)
}

func (e *RemoteEngine) Close() error {
	if e == nil || e.client == nil {
		return nil
	}
	return e.client.Close()
}

// ProcessClient 返回底层进程客户端。
// 供外部 facade（例如 sidecar/client.Client）做 lifecycle 与 Wait 监听。
// e 为 nil 或未启动子进程时返回 nil。
func (e *RemoteEngine) ProcessClient() *ProcessClient {
	if e == nil {
		return nil
	}
	return e.client
}

// Wait 阻塞至子进程退出。
// 仅当 RemoteEngine 是通过 StartRemoteEngine 启动的才有意义。
func (e *RemoteEngine) Wait() <-chan error {
	if e == nil || e.client == nil {
		ch := make(chan error, 1)
		ch <- ErrCallerUnavailable
		close(ch)
		return ch
	}
	return e.client.Wait()
}

func (e *RemoteEngine) State() coreapi.StateService {
	return remoteStateService{engine: e}
}

func (e *RemoteEngine) Workspaces() coreapi.WorkspaceService {
	return remoteWorkspaceService{engine: e}
}

func (e *RemoteEngine) Sessions() coreapi.SessionService {
	return remoteSessionService{engine: e}
}

func (e *RemoteEngine) MCP() coreapi.MCPService {
	return remoteMCPService{engine: e}
}

func (e *RemoteEngine) LSP() coreapi.LSPService {
	return remoteLSPService{engine: e}
}

func (e *RemoteEngine) Config() coreapi.ConfigService {
	return remoteConfigService{engine: e}
}

func (e *RemoteEngine) Permissions() coreapi.PermissionService {
	return remotePermissionService{engine: e}
}

func (e *RemoteEngine) Extensions() coreapi.ExtensionService {
	return remoteExtensionService{engine: e}
}

func (e *RemoteEngine) Context() coreapi.ContextService {
	return remoteContextService{engine: e}
}

func (e *RemoteEngine) Usage() coreapi.UsageService {
	return remoteUsageService{engine: e}
}

func (e *RemoteEngine) Versions() coreapi.VersionService {
	return remoteVersionService{engine: e}
}

func (e *RemoteEngine) Tasks() coreapi.TaskService {
	return remoteTaskService{engine: e}
}

func (e *RemoteEngine) Goals() coreapi.GoalService {
	return remoteGoalService{engine: e}
}

func (e *RemoteEngine) Modes() coreapi.ModeService {
	return remoteModeService{engine: e}
}

func (e *RemoteEngine) Models() coreapi.ModelService {
	return remoteModelService{engine: e}
}

func (e *RemoteEngine) RemoteWorkspaces() coreapi.RemoteWorkspaceService {
	return remoteRemoteWorkspaceService{engine: e}
}

func (e *RemoteEngine) Git() coreapi.GitService {
	return remoteGitService{engine: e}
}

func (e *RemoteEngine) Insights() coreapi.InsightService {
	return remoteInsightService{engine: e}
}

func (e *RemoteEngine) Memory() coreapi.MemoryService {
	return remoteMemoryService{engine: e}
}

func (e *RemoteEngine) Roles() coreapi.RoleService {
	return remoteRoleService{engine: e}
}

func (e *RemoteEngine) Turns() coreapi.TurnService {
	return remoteTurnService{engine: e}
}

func (e *RemoteEngine) Approvals() coreapi.ApprovalService {
	return remoteApprovalService{engine: e}
}

func (e *RemoteEngine) Inquiries() coreapi.InquiryService {
	return remoteInquiryService{engine: e}
}

func (e *RemoteEngine) Agents() coreapi.AgentService {
	return remoteAgentService{engine: e}
}

func (e *RemoteEngine) Tools() coreapi.ToolExecutor {
	return remoteToolExecutor{engine: e}
}

func (e *RemoteEngine) ToolCatalog() coreapi.ToolCatalogService {
	return remoteToolCatalogService{engine: e}
}

func (e *RemoteEngine) ToolTelemetry() coreapi.ToolTelemetryService {
	return remoteToolTelemetryService{engine: e}
}

func (e *RemoteEngine) Events() coreapi.EventSubscriber {
	return remoteEventSubscriber{engine: e}
}

func (e *RemoteEngine) Sandbox() coreapi.SandboxService {
	return remoteSandboxService{engine: e}
}

func (e *RemoteEngine) Diagnostics() coreapi.DiagnosticsService {
	return remoteDiagnosticsService{engine: e}
}

type remoteDiagnosticsService struct {
	engine *RemoteEngine
}

func (s remoteDiagnosticsService) Startup(ctx context.Context) (coreapi.StartupDiagnosticsResult, error) {
	var out coreapi.StartupDiagnosticsResult
	if err := s.engine.call(ctx, protocoljsonrpc.MethodDiagnosticsStartup, nil, &out); err != nil {
		return out, err
	}
	return out, nil
}

func (e *RemoteEngine) Caller() coreapi.Caller {
	if e == nil {
		return nil
	}
	return e.caller
}

func (e *RemoteEngine) call(ctx context.Context, method string, params any, out any) error {
	if e == nil || e.caller == nil {
		return fmt.Errorf("%s: %w", method, ErrCallerUnavailable)
	}
	if err := e.caller.Call(ctx, method, params, out); err != nil {
		return normalizeRemoteError(method, err)
	}
	return nil
}

type rustEventEnvelope struct {
	EventType string         `json:"event_type"`
	RequestID string         `json:"request_id"`
	Source    string         `json:"source"`
	Payload   map[string]any `json:"payload"`
	SessionID string         `json:"session_id"`
	TurnID    string         `json:"turn_id"`
	AgentID   string         `json:"agent_id"`
}

func (e *RemoteEngine) handleNotification(_ context.Context, n protocoljsonrpc.Notification) error {
	if n.Method != protocoljsonrpc.NotificationEvent {
		return nil
	}
	if len(n.Params) == 0 {
		return nil
	}
	var rust rustEventEnvelope
	if err := json.Unmarshal(n.Params, &rust); err != nil {
		return nil
	}
	env := protocol.NewEvent(protocol.EventType(rust.EventType), protocol.EventOptions{
		SessionID: rust.SessionID,
		TurnID:    rust.TurnID,
		AgentID:   rust.AgentID,
		RequestID: rust.RequestID,
		Source:    protocol.Source(rust.Source),
		Payload:   rust.Payload,
	})
	e.eventMu.RLock()
	defer e.eventMu.RUnlock()
	for _, sub := range e.eventSubscribers {
		select {
		case sub.ch <- env:
		default:
		}
	}
	return nil
}

func (e *RemoteEngine) closeAllSubscribers() {
	e.eventMu.Lock()
	defer e.eventMu.Unlock()
	for id, sub := range e.eventSubscribers {
		sub.cancel()
		close(sub.ch)
		delete(e.eventSubscribers, id)
	}
}

func normalizeRemoteError(method string, err error) error {
	if err == nil {
		return nil
	}
	lower := strings.ToLower(err.Error())
	if strings.Contains(lower, strings.ToLower(coreapi.ErrUnsupported.Error())) ||
		strings.Contains(lower, "unsupported operation") ||
		strings.Contains(lower, "method not found") ||
		strings.Contains(lower, "-32601") {
		return unsupported(method)
	}
	return err
}

func unsupported(method string) error {
	return fmt.Errorf("%s: %w", method, coreapi.ErrUnsupported)
}

type remoteStateService struct {
	engine *RemoteEngine
}

func (s remoteStateService) Snapshot(ctx context.Context, params coreapi.StateSnapshotRequest) (coreapi.StateSnapshot, error) {
	var out coreapi.StateSnapshot
	if err := s.engine.call(ctx, protocoljsonrpc.MethodStateSnapshot, params, &out); err != nil {
		return coreapi.StateSnapshot{}, err
	}
	return out, nil
}

type remoteSessionService struct {
	engine *RemoteEngine
}

func (s remoteSessionService) Create(ctx context.Context, req coreapi.CreateSessionRequest) (coreapi.Session, error) {
	var out coreapi.Session
	if err := s.engine.call(ctx, protocoljsonrpc.MethodSessionCreate, req, &out); err != nil {
		return coreapi.Session{}, err
	}
	return out, nil
}

func (s remoteSessionService) Resume(ctx context.Context, req coreapi.ResumeSessionRequest) (coreapi.Session, error) {
	var out coreapi.Session
	if err := s.engine.call(ctx, protocoljsonrpc.MethodSessionResume, req, &out); err != nil {
		return coreapi.Session{}, err
	}
	return out, nil
}

func (s remoteSessionService) List(ctx context.Context, req coreapi.ListSessionsRequest) ([]coreapi.Session, error) {
	var out []coreapi.Session
	if err := s.engine.call(ctx, protocoljsonrpc.MethodSessionList, req, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (s remoteSessionService) Current(ctx context.Context, req coreapi.CurrentSessionRequest) (coreapi.Session, error) {
	var out coreapi.Session
	if err := s.engine.call(ctx, protocoljsonrpc.MethodSessionCurrent, req, &out); err != nil {
		return coreapi.Session{}, err
	}
	return out, nil
}

func (s remoteSessionService) SetCurrent(ctx context.Context, req coreapi.SetCurrentSessionRequest) error {
	var out map[string]any
	return s.engine.call(ctx, protocoljsonrpc.MethodSessionSetCurrent, req, &out)
}

func (s remoteSessionService) Delete(ctx context.Context, req coreapi.DeleteSessionRequest) error {
	var out map[string]any
	return s.engine.call(ctx, protocoljsonrpc.MethodSessionDelete, req, &out)
}

func (s remoteSessionService) Rename(ctx context.Context, req coreapi.RenameSessionRequest) (coreapi.Session, error) {
	var out coreapi.Session
	if err := s.engine.call(ctx, protocoljsonrpc.MethodSessionRename, req, &out); err != nil {
		return coreapi.Session{}, err
	}
	return out, nil
}

func (s remoteSessionService) SetMeta(ctx context.Context, req coreapi.SetSessionMetaRequest) (coreapi.Session, error) {
	var out coreapi.Session
	if err := s.engine.call(ctx, protocoljsonrpc.MethodSessionSetMeta, req, &out); err != nil {
		return coreapi.Session{}, err
	}
	return out, nil
}

func (s remoteSessionService) LoadMessages(ctx context.Context, req coreapi.LoadSessionMessagesRequest) ([]coreapi.SessionMessage, error) {
	var out []coreapi.SessionMessage
	if err := s.engine.call(ctx, protocoljsonrpc.MethodSessionMessagesLoad, req, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (s remoteSessionService) SaveMessages(ctx context.Context, req coreapi.SaveSessionMessagesRequest) (coreapi.Session, error) {
	var out coreapi.Session
	if err := s.engine.call(ctx, protocoljsonrpc.MethodSessionMessagesSave, req, &out); err != nil {
		return coreapi.Session{}, err
	}
	return out, nil
}

type remoteTurnService struct {
	engine *RemoteEngine
}

func (s remoteTurnService) Start(ctx context.Context, req coreapi.StartTurnRequest) (coreapi.Turn, error) {
	var out coreapi.Turn
	if err := s.engine.call(ctx, protocoljsonrpc.MethodTurnStart, req, &out); err != nil {
		return coreapi.Turn{}, err
	}
	return out, nil
}

func (s remoteTurnService) Interrupt(ctx context.Context, ref coreapi.TurnRef) error {
	var out map[string]any
	return s.engine.call(ctx, protocoljsonrpc.MethodTurnInterrupt, ref, &out)
}

func (s remoteTurnService) Resume(ctx context.Context, ref coreapi.TurnRef) (coreapi.Turn, error) {
	var out coreapi.Turn
	if err := s.engine.call(ctx, protocoljsonrpc.MethodTurnResume, ref, &out); err != nil {
		return coreapi.Turn{}, err
	}
	return out, nil
}

type remoteApprovalService struct {
	engine *RemoteEngine
}

func (s remoteApprovalService) Respond(ctx context.Context, req coreapi.ApprovalResponse) error {
	var out map[string]any
	return s.engine.call(ctx, protocoljsonrpc.MethodApprovalRespond, req, &out)
}

type remoteAgentService struct {
	engine *RemoteEngine
}

type remoteAgentControlRequest struct {
	AgentID       string         `json:"agent_id,omitempty"`
	Action        string         `json:"action"`
	RoleID        string         `json:"role_id,omitempty"`
	ParentAgentID string         `json:"parent_agent_id,omitempty"`
	Task          string         `json:"task,omitempty"`
	Reason        string         `json:"reason,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}

type remoteAgentControlResponse struct {
	AgentID        string          `json:"agent_id,omitempty"`
	Status         string          `json:"status,omitempty"`
	PreviousStatus string          `json:"previous_status,omitempty"`
	Agents         []coreapi.Agent `json:"agents,omitempty"`
}

func (s remoteAgentService) Spawn(ctx context.Context, req coreapi.SpawnAgentRequest) (coreapi.Agent, error) {
	resp, err := s.control(ctx, remoteAgentControlRequest{
		Action:        "create",
		RoleID:        strings.TrimSpace(req.RoleID),
		ParentAgentID: strings.TrimSpace(req.ParentAgentID),
		Task:          strings.TrimSpace(req.Task),
	})
	if err != nil {
		return coreapi.Agent{}, err
	}
	return firstAgent(resp), nil
}

func (s remoteAgentService) SendInput(ctx context.Context, input coreapi.AgentInput) error {
	var out map[string]any
	return s.engine.call(ctx, protocoljsonrpc.MethodAgentInput, input, &out)
}

func (s remoteAgentService) Wait(ctx context.Context, ref coreapi.AgentRef) (coreapi.Agent, error) {
	resp, err := s.control(ctx, remoteAgentControlRequest{
		Action:  "status",
		AgentID: strings.TrimSpace(ref.AgentID),
	})
	if err != nil {
		return coreapi.Agent{}, err
	}
	return firstAgent(resp), nil
}

func (s remoteAgentService) Run(ctx context.Context, req coreapi.RunAgentRequest) (coreapi.AgentRunResult, error) {
	var out coreapi.AgentRunResult
	if err := s.engine.call(ctx, protocoljsonrpc.MethodAgentRun, req, &out); err != nil {
		return coreapi.AgentRunResult{}, err
	}
	return out, nil
}

func (s remoteAgentService) RunTool(ctx context.Context, req coreapi.AgentToolRequest) (coreapi.AgentToolResult, error) {
	result, err := s.engine.Tools().Execute(ctx, coreapi.ToolRequest{
		SessionID: strings.TrimSpace(req.SessionID),
		TurnID:    strings.TrimSpace(req.TurnID),
		AgentID:   strings.TrimSpace(req.AgentID),
		Name:      strings.TrimSpace(req.Name),
		Args:      req.Args,
	})
	if err != nil {
		return coreapi.AgentToolResult{}, err
	}
	return coreapi.AgentToolResult{
		Name:    result.Name,
		Display: result.Display,
		Output:  result.Output,
		Error:   result.Error,
	}, nil
}

func (s remoteAgentService) List(ctx context.Context, _ coreapi.ListAgentsRequest) ([]coreapi.Agent, error) {
	resp, err := s.control(ctx, remoteAgentControlRequest{Action: "list"})
	if err != nil {
		return nil, err
	}
	return resp.Agents, nil
}

func (s remoteAgentService) Close(ctx context.Context, ref coreapi.AgentRef) error {
	_, err := s.control(ctx, remoteAgentControlRequest{
		Action:  "cancel",
		AgentID: strings.TrimSpace(ref.AgentID),
	})
	return err
}

func (s remoteAgentService) control(ctx context.Context, req remoteAgentControlRequest) (remoteAgentControlResponse, error) {
	var out remoteAgentControlResponse
	if err := s.engine.call(ctx, protocoljsonrpc.MethodAgentControl, req, &out); err != nil {
		return remoteAgentControlResponse{}, err
	}
	return out, nil
}

func firstAgent(resp remoteAgentControlResponse) coreapi.Agent {
	if len(resp.Agents) > 0 {
		return resp.Agents[0]
	}
	return coreapi.Agent{
		ID:     resp.AgentID,
		Status: resp.Status,
	}
}

type remoteToolExecutor struct {
	engine *RemoteEngine
}

func (s remoteToolExecutor) Execute(ctx context.Context, req coreapi.ToolRequest) (coreapi.ToolResult, error) {
	var out coreapi.ToolResult
	if err := s.engine.call(ctx, protocoljsonrpc.MethodToolExecute, req, &out); err != nil {
		return coreapi.ToolResult{}, err
	}
	return out, nil
}

type remoteToolCatalogService struct {
	engine *RemoteEngine
}

func (s remoteToolCatalogService) List(ctx context.Context, req coreapi.ListToolCatalogRequest) ([]coreapi.ToolDefinition, error) {
	var out []coreapi.ToolDefinition
	if err := s.engine.call(ctx, protocoljsonrpc.MethodToolCatalog, req, &out); err != nil {
		return nil, err
	}
	return out, nil
}

type remoteSandboxService struct {
	engine *RemoteEngine
}

func (s remoteSandboxService) Policy(ctx context.Context, ref coreapi.SessionRef) (sandbox.Policy, error) {
	var out sandbox.Policy
	if err := s.engine.call(ctx, protocoljsonrpc.MethodSandboxPolicy, coreapi.SandboxPolicyRequest(ref), &out); err != nil {
		return sandbox.Policy{}, err
	}
	return out, nil
}

func (s remoteSandboxService) SetPolicy(ctx context.Context, ref coreapi.SessionRef, policy sandbox.Policy) error {
	var out map[string]any
	return s.engine.call(ctx, protocoljsonrpc.MethodSandboxSetPolicy, coreapi.SetSandboxPolicyRequest{
		SessionID: ref.SessionID,
		Policy:    policy,
	}, &out)
}

func (s remoteSandboxService) DerivePolicy(ctx context.Context, req coreapi.DeriveSandboxPolicyRequest) (sandbox.Policy, error) {
	var out sandbox.Policy
	if err := s.engine.call(ctx, protocoljsonrpc.MethodSandboxDerivePolicy, req, &out); err != nil {
		return sandbox.Policy{}, err
	}
	return out, nil
}

func (s remoteSandboxService) BackendStatus(ctx context.Context) sandbox.BackendStatus {
	var out sandbox.BackendStatus
	if err := s.engine.call(ctx, protocoljsonrpc.MethodSandboxBackend, nil, &out); err == nil {
		return out
	}
	return sandbox.BackendStatus{
		GOOS:     runtime.GOOS,
		Backend:  "rust-sidecar",
		Enforced: false,
		Degraded: true,
		Reason:   "rust sidecar sandbox backend is unavailable",
		UnsupportedCapabilities: []string{
			"sidecar-sandbox-policy",
			"sidecar-sandbox-runner",
		},
	}
}

type remoteWorkspaceService struct {
	engine *RemoteEngine
}

func (s remoteWorkspaceService) List(ctx context.Context, params coreapi.WorkspaceListRequest) ([]coreapi.Workspace, error) {
	var out []coreapi.Workspace
	if err := s.engine.call(ctx, protocoljsonrpc.MethodWorkspaceList, params, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (s remoteWorkspaceService) Default(ctx context.Context) (string, error) {
	var out string
	if err := s.engine.call(ctx, protocoljsonrpc.MethodWorkspaceDefault, nil, &out); err != nil {
		return "", err
	}
	return out, nil
}

func (s remoteWorkspaceService) Last(ctx context.Context) (string, error) {
	var out string
	if err := s.engine.call(ctx, protocoljsonrpc.MethodWorkspaceLast, nil, &out); err != nil {
		return "", err
	}
	return out, nil
}

func (s remoteWorkspaceService) ResolveForeground(ctx context.Context, req coreapi.ResolveForegroundWorkspaceRequest) (string, error) {
	var out string
	if err := s.engine.call(ctx, protocoljsonrpc.MethodWorkspaceResolve, req, &out); err != nil {
		return "", err
	}
	return out, nil
}

func (s remoteWorkspaceService) Remember(ctx context.Context, req coreapi.RememberWorkspaceRequest) error {
	var out map[string]any
	return s.engine.call(ctx, protocoljsonrpc.MethodWorkspaceRemember, req, &out)
}

func (s remoteWorkspaceService) Forget(ctx context.Context, req coreapi.WorkspacePathRequest) error {
	var out map[string]any
	return s.engine.call(ctx, protocoljsonrpc.MethodWorkspaceForget, req, &out)
}

func (s remoteWorkspaceService) Add(ctx context.Context, req coreapi.WorkspacePathRequest) error {
	var out map[string]any
	return s.engine.call(ctx, protocoljsonrpc.MethodWorkspaceAdd, req, &out)
}

func (s remoteWorkspaceService) Remove(ctx context.Context, req coreapi.WorkspacePathRequest) error {
	var out map[string]any
	return s.engine.call(ctx, protocoljsonrpc.MethodWorkspaceRemove, req, &out)
}

func (s remoteWorkspaceService) Use(ctx context.Context, req coreapi.WorkspacePathRequest) error {
	var out map[string]any
	return s.engine.call(ctx, protocoljsonrpc.MethodWorkspaceUse, req, &out)
}

func (s remoteWorkspaceService) SetForeground(ctx context.Context, req coreapi.WorkspacePathRequest) error {
	var out map[string]any
	return s.engine.call(ctx, protocoljsonrpc.MethodWorkspaceSetForeground, req, &out)
}

func (s remoteWorkspaceService) Trust(ctx context.Context, req coreapi.WorkspacePathRequest) error {
	var out map[string]any
	return s.engine.call(ctx, protocoljsonrpc.MethodWorkspaceTrust, req, &out)
}

func (s remoteWorkspaceService) ListWorktrees(ctx context.Context) ([]coreapi.Worktree, error) {
	var out []coreapi.Worktree
	if err := s.engine.call(ctx, protocoljsonrpc.MethodWorkspaceWorktreeList, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (s remoteWorkspaceService) CreateWorktree(ctx context.Context, req coreapi.CreateWorktreeRequest) (coreapi.Worktree, error) {
	var out coreapi.Worktree
	if err := s.engine.call(ctx, protocoljsonrpc.MethodWorkspaceWorktreeCreate, req, &out); err != nil {
		return coreapi.Worktree{}, err
	}
	return out, nil
}

func (s remoteWorkspaceService) RemoveWorktree(ctx context.Context, req coreapi.RemoveWorktreeRequest) error {
	var out map[string]any
	return s.engine.call(ctx, protocoljsonrpc.MethodWorkspaceWorktreeRemove, req, &out)
}

type remoteMCPService struct {
	engine *RemoteEngine
}

func (s remoteMCPService) List(ctx context.Context) ([]coreapi.MCPServer, error) {
	var out []coreapi.MCPServer
	if err := s.engine.call(ctx, protocoljsonrpc.MethodMCPList, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (s remoteMCPService) Upsert(ctx context.Context, req coreapi.UpsertMCPRequest) error {
	var out map[string]any
	return s.engine.call(ctx, protocoljsonrpc.MethodMCPUpsert, req, &out)
}

func (s remoteMCPService) ImportJSON(ctx context.Context, req coreapi.ImportMCPJSONRequest) error {
	var out map[string]any
	return s.engine.call(ctx, protocoljsonrpc.MethodMCPImportJSON, req, &out)
}

func (s remoteMCPService) Delete(ctx context.Context, req coreapi.MCPNameRequest) error {
	var out map[string]any
	return s.engine.call(ctx, protocoljsonrpc.MethodMCPDelete, req, &out)
}

func (s remoteMCPService) SetEnabled(ctx context.Context, req coreapi.SetMCPEnabledRequest) error {
	var out map[string]any
	return s.engine.call(ctx, protocoljsonrpc.MethodMCPSetEnabled, req, &out)
}

type remoteLSPService struct {
	engine *RemoteEngine
}

func (s remoteLSPService) List(ctx context.Context) ([]coreapi.LSPServer, error) {
	var out []coreapi.LSPServer
	if err := s.engine.call(ctx, protocoljsonrpc.MethodLSPList, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (s remoteLSPService) Detect(ctx context.Context, req coreapi.LSPLanguageRequest) (string, error) {
	var out string
	if err := s.engine.call(ctx, protocoljsonrpc.MethodLSPDetect, req, &out); err != nil {
		return "", err
	}
	return out, nil
}

func (s remoteLSPService) Start(ctx context.Context, req coreapi.LSPLanguageRequest) (string, error) {
	var out string
	if err := s.engine.call(ctx, protocoljsonrpc.MethodLSPStart, req, &out); err != nil {
		return "", err
	}
	return out, nil
}

func (s remoteLSPService) Install(ctx context.Context, req coreapi.LSPLanguageRequest) (string, error) {
	var out string
	if err := s.engine.call(ctx, protocoljsonrpc.MethodLSPInstall, req, &out); err != nil {
		return "", err
	}
	return out, nil
}

func (s remoteLSPService) Diagnostics(ctx context.Context) ([]string, error) {
	var out []string
	if err := s.engine.call(ctx, protocoljsonrpc.MethodLSPDiagnostics, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (s remoteLSPService) DiagnosticsSummary(ctx context.Context) (coreapi.LSPDiagnosticsSummary, error) {
	var out coreapi.LSPDiagnosticsSummary
	if err := s.engine.call(ctx, protocoljsonrpc.MethodLSPDiagnosticsSummary, nil, &out); err != nil {
		return coreapi.LSPDiagnosticsSummary{}, err
	}
	return out, nil
}

type remoteExtensionService struct {
	engine *RemoteEngine
}

func (s remoteExtensionService) ListSkills(ctx context.Context) ([]coreapi.SkillInfo, error) {
	var out []coreapi.SkillInfo
	if err := s.engine.call(ctx, protocoljsonrpc.MethodExtensionsSkillsList, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (s remoteExtensionService) ReloadSkills(ctx context.Context) error {
	var out map[string]any
	return s.engine.call(ctx, protocoljsonrpc.MethodExtensionsSkillsReload, nil, &out)
}

func (s remoteExtensionService) SetSkillEnabled(ctx context.Context, req coreapi.SetExtensionEnabledRequest) error {
	var out map[string]any
	return s.engine.call(ctx, protocoljsonrpc.MethodExtensionsSkillSetEnabled, req, &out)
}

func (s remoteExtensionService) InvokeSkill(ctx context.Context, req coreapi.InvokeSkillRequest) (coreapi.InvokeSkillResult, error) {
	var out coreapi.InvokeSkillResult
	if err := s.engine.call(ctx, protocoljsonrpc.MethodExtensionsSkillInvoke, req, &out); err != nil {
		return coreapi.InvokeSkillResult{}, err
	}
	return out, nil
}

func (s remoteExtensionService) ListPlugins(ctx context.Context) ([]coreapi.PluginInfo, error) {
	var out []coreapi.PluginInfo
	if err := s.engine.call(ctx, protocoljsonrpc.MethodExtensionsPluginsList, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (s remoteExtensionService) SetPluginEnabled(ctx context.Context, req coreapi.SetExtensionEnabledRequest) error {
	var out map[string]any
	return s.engine.call(ctx, protocoljsonrpc.MethodExtensionsPluginSetEnabled, req, &out)
}

func (s remoteExtensionService) BrowserStatus(ctx context.Context) (coreapi.BrowserRuntimeStatus, error) {
	var out coreapi.BrowserRuntimeStatus
	if err := s.engine.call(ctx, protocoljsonrpc.MethodBrowserStatus, nil, &out); err != nil {
		return coreapi.BrowserRuntimeStatus{}, err
	}
	return out, nil
}

func (s remoteExtensionService) BrowserLaunch(ctx context.Context, req coreapi.BrowserLaunchRequest) error {
	var out map[string]any
	return s.engine.call(ctx, protocoljsonrpc.MethodBrowserLaunch, req, &out)
}

func (s remoteExtensionService) BrowserClose(ctx context.Context, req coreapi.BrowserCloseRequest) error {
	var out map[string]any
	return s.engine.call(ctx, protocoljsonrpc.MethodBrowserClose, req, &out)
}

func (s remoteExtensionService) BrowserControlTakeover(ctx context.Context, req coreapi.BrowserControlTakeoverRequest) error {
	var out map[string]any
	return s.engine.call(ctx, protocoljsonrpc.MethodBrowserControlTakeover, req, &out)
}

func (s remoteExtensionService) BrowserControlResume(ctx context.Context) error {
	var out map[string]any
	return s.engine.call(ctx, protocoljsonrpc.MethodBrowserControlResume, coreapi.BrowserControlResumeRequest{}, &out)
}

func (s remoteExtensionService) BrowserTabs(ctx context.Context) ([]coreapi.BrowserTabInfo, error) {
	var out []coreapi.BrowserTabInfo
	if err := s.engine.call(ctx, protocoljsonrpc.MethodBrowserTabs, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (s remoteExtensionService) BrowserProfiles(ctx context.Context) ([]coreapi.BrowserProfileRecord, error) {
	var out []coreapi.BrowserProfileRecord
	if err := s.engine.call(ctx, protocoljsonrpc.MethodBrowserProfiles, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// --- remote engineering services (git / task / version / usage) -----------
//
// These proxy the migrated RPC surface from the Rust sidecar.
// `RemoteEngine` exposes the live implementations as the default. When the
// sidecar reports an unsupported operation, `normalizeRemoteError` maps it
// to `coreapi.ErrUnsupported` via the `unsupported(method)` helper.

type remoteGitService struct {
	engine *RemoteEngine
}

func (s remoteGitService) Status(ctx context.Context, req coreapi.GitStatusRequest) ([]coreapi.GitChange, error) {
	var out []coreapi.GitChange
	if err := s.engine.call(ctx, protocoljsonrpc.MethodGitStatus, req, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (s remoteGitService) Summary(ctx context.Context, req coreapi.GitSummaryRequest) (coreapi.GitSummaryResult, error) {
	var out coreapi.GitSummaryResult
	if err := s.engine.call(ctx, protocoljsonrpc.MethodGitSummary, req, &out); err != nil {
		return coreapi.GitSummaryResult{}, err
	}
	return out, nil
}

func (s remoteGitService) Diff(ctx context.Context, req coreapi.GitDiffRequest) (coreapi.GitTextResult, error) {
	var out coreapi.GitTextResult
	if err := s.engine.call(ctx, protocoljsonrpc.MethodGitDiff, req, &out); err != nil {
		return coreapi.GitTextResult{}, err
	}
	return out, nil
}

func (s remoteGitService) Branches(ctx context.Context, req coreapi.GitBranchesRequest) (coreapi.GitBranchesResult, error) {
	var out coreapi.GitBranchesResult
	if err := s.engine.call(ctx, protocoljsonrpc.MethodGitBranches, req, &out); err != nil {
		return coreapi.GitBranchesResult{}, err
	}
	return out, nil
}

func (s remoteGitService) Log(ctx context.Context, req coreapi.GitLogRequest) (coreapi.GitLogResult, error) {
	var out coreapi.GitLogResult
	if err := s.engine.call(ctx, protocoljsonrpc.MethodGitLog, req, &out); err != nil {
		return coreapi.GitLogResult{}, err
	}
	return out, nil
}

func (s remoteGitService) Show(ctx context.Context, req coreapi.GitShowRequest) (coreapi.GitShowResult, error) {
	var out coreapi.GitShowResult
	if err := s.engine.call(ctx, protocoljsonrpc.MethodGitShow, req, &out); err != nil {
		return coreapi.GitShowResult{}, err
	}
	return out, nil
}

type remoteTaskService struct {
	engine *RemoteEngine
}

type remoteGoalService struct {
	engine *RemoteEngine
}

func (s remoteGoalService) Set(ctx context.Context, req coreapi.GoalSetRequest) (coreapi.ThreadGoal, error) {
	var out coreapi.ThreadGoal
	if err := s.engine.call(ctx, protocoljsonrpc.MethodGoalSet, req, &out); err != nil {
		return coreapi.ThreadGoal{}, err
	}
	return out, nil
}

func (s remoteGoalService) Get(ctx context.Context, req coreapi.GoalRefRequest) (coreapi.GoalGetResponse, error) {
	var out coreapi.GoalGetResponse
	if err := s.engine.call(ctx, protocoljsonrpc.MethodGoalGet, req, &out); err != nil {
		return coreapi.GoalGetResponse{}, err
	}
	return out, nil
}

func (s remoteGoalService) Pause(ctx context.Context, req coreapi.GoalRefRequest) (coreapi.ThreadGoal, error) {
	var out coreapi.ThreadGoal
	if err := s.engine.call(ctx, protocoljsonrpc.MethodGoalPause, req, &out); err != nil {
		return coreapi.ThreadGoal{}, err
	}
	return out, nil
}

func (s remoteGoalService) Resume(ctx context.Context, req coreapi.GoalRefRequest) (coreapi.ThreadGoal, error) {
	var out coreapi.ThreadGoal
	if err := s.engine.call(ctx, protocoljsonrpc.MethodGoalResume, req, &out); err != nil {
		return coreapi.ThreadGoal{}, err
	}
	return out, nil
}

func (s remoteGoalService) Clear(ctx context.Context, req coreapi.GoalRefRequest) error {
	var out map[string]any
	return s.engine.call(ctx, protocoljsonrpc.MethodGoalClear, req, &out)
}

func (s remoteTaskService) List(ctx context.Context) ([]coreapi.TaskSnapshot, error) {
	var out []coreapi.TaskSnapshot
	if err := s.engine.call(ctx, protocoljsonrpc.MethodTaskList, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (s remoteTaskService) Todos(ctx context.Context) ([]coreapi.TodoItem, error) {
	var out []coreapi.TodoItem
	if err := s.engine.call(ctx, protocoljsonrpc.MethodTaskTodos, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (s remoteTaskService) Tail(ctx context.Context, req coreapi.TaskIDRequest) ([]string, error) {
	var out []string
	if err := s.engine.call(ctx, protocoljsonrpc.MethodTaskTail, req, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (s remoteTaskService) Kill(ctx context.Context, req coreapi.TaskIDRequest) error {
	var out map[string]any
	return s.engine.call(ctx, protocoljsonrpc.MethodTaskKill, req, &out)
}

type remoteTaskCleanupResult struct {
	Removed int `json:"removed"`
}

func (s remoteTaskService) Cleanup(ctx context.Context) (int, error) {
	var out remoteTaskCleanupResult
	if err := s.engine.call(ctx, protocoljsonrpc.MethodTaskCleanup, nil, &out); err != nil {
		return 0, err
	}
	return out.Removed, nil
}

type remoteVersionService struct {
	engine *RemoteEngine
}

func (s remoteVersionService) List(ctx context.Context) ([]coreapi.VersionItem, error) {
	var out []coreapi.VersionItem
	if err := s.engine.call(ctx, protocoljsonrpc.MethodVersionsList, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (s remoteVersionService) Rollback(ctx context.Context, req coreapi.VersionIDRequest) error {
	var out map[string]any
	return s.engine.call(ctx, protocoljsonrpc.MethodVersionsRollback, req, &out)
}

func (s remoteVersionService) Delete(ctx context.Context, req coreapi.VersionIDRequest) error {
	var out map[string]any
	return s.engine.call(ctx, protocoljsonrpc.MethodVersionsDelete, req, &out)
}

type remoteVersionCountResult struct {
	Removed int `json:"removed"`
}

func (s remoteVersionService) DeleteFile(ctx context.Context, req coreapi.VersionFileRequest) (int, error) {
	var out remoteVersionCountResult
	if err := s.engine.call(ctx, protocoljsonrpc.MethodVersionsDeleteFile, req, &out); err != nil {
		return 0, err
	}
	return out.Removed, nil
}

func (s remoteVersionService) Clear(ctx context.Context) (int, error) {
	var out remoteVersionCountResult
	if err := s.engine.call(ctx, protocoljsonrpc.MethodVersionsClear, nil, &out); err != nil {
		return 0, err
	}
	return out.Removed, nil
}

type remoteUsageService struct {
	engine *RemoteEngine
}

func (s remoteUsageService) Summary(ctx context.Context) (coreapi.UsageSummary, error) {
	var out coreapi.UsageSummary
	if err := s.engine.call(ctx, protocoljsonrpc.MethodUsageSummary, nil, &out); err != nil {
		return coreapi.UsageSummary{}, err
	}
	return out, nil
}

type remoteUsageCostSummary struct {
	Text string `json:"text"`
}

func (s remoteUsageService) CostSummary(ctx context.Context) (string, error) {
	var out remoteUsageCostSummary
	if err := s.engine.call(ctx, protocoljsonrpc.MethodUsageCostSummary, nil, &out); err != nil {
		return "", err
	}
	return out.Text, nil
}

func (s remoteUsageService) CostItems(ctx context.Context) ([]coreapi.CostItem, error) {
	var out []coreapi.CostItem
	if err := s.engine.call(ctx, protocoljsonrpc.MethodUsageCostItems, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

var (
	_ coreapi.GitService     = remoteGitService{}
	_ coreapi.TaskService    = remoteTaskService{}
	_ coreapi.GoalService    = remoteGoalService{}
	_ coreapi.VersionService = remoteVersionService{}
	_ coreapi.UsageService   = remoteUsageService{}
)

type remoteConfigService struct {
	engine *RemoteEngine
}

func (s remoteConfigService) GetRules(ctx context.Context) (string, error) {
	var out string
	if err := s.engine.call(ctx, protocoljsonrpc.MethodConfigRulesGet, nil, &out); err != nil {
		return "", err
	}
	return out, nil
}

func (s remoteConfigService) RulesSnapshot(ctx context.Context) (coreapi.RulesSnapshot, error) {
	var out coreapi.RulesSnapshot
	if err := s.engine.call(ctx, protocoljsonrpc.MethodConfigRulesSnapshot, nil, &out); err != nil {
		return coreapi.RulesSnapshot{}, err
	}
	return out, nil
}

func (s remoteConfigService) SaveRules(ctx context.Context, req coreapi.SaveRulesRequest) error {
	var out map[string]any
	return s.engine.call(ctx, protocoljsonrpc.MethodConfigRulesSave, req, &out)
}

func (s remoteConfigService) ResetRules(ctx context.Context) error {
	var out map[string]any
	return s.engine.call(ctx, protocoljsonrpc.MethodConfigRulesReset, nil, &out)
}

func (s remoteConfigService) GetSettings(ctx context.Context) (coreapi.Settings, error) {
	var out coreapi.Settings
	if err := s.engine.call(ctx, protocoljsonrpc.MethodConfigSettingsGet, nil, &out); err != nil {
		return coreapi.Settings{}, err
	}
	return out, nil
}

func (s remoteConfigService) SaveSettings(ctx context.Context, settings coreapi.Settings) error {
	var out map[string]any
	return s.engine.call(ctx, protocoljsonrpc.MethodConfigSettingsSave, settings, &out)
}

type remotePermissionService struct {
	engine *RemoteEngine
}

func (s remotePermissionService) Snapshot(ctx context.Context) (coreapi.PermissionSnapshot, error) {
	var out coreapi.PermissionSnapshot
	if err := s.engine.call(ctx, protocoljsonrpc.MethodPermissionSnapshot, nil, &out); err != nil {
		return coreapi.PermissionSnapshot{}, err
	}
	return out, nil
}

func (s remotePermissionService) PendingReview(ctx context.Context) (coreapi.PendingReview, error) {
	var out coreapi.PendingReview
	if err := s.engine.call(ctx, protocoljsonrpc.MethodPermissionPendingReview, nil, &out); err != nil {
		return coreapi.PendingReview{}, err
	}
	return out, nil
}

func (s remotePermissionService) ClearPendingReview(ctx context.Context) error {
	var out map[string]any
	return s.engine.call(ctx, protocoljsonrpc.MethodPermissionClearReview, nil, &out)
}

func (s remotePermissionService) SetAccessMode(ctx context.Context, req coreapi.SetModeRequest) error {
	var out map[string]any
	return s.engine.call(ctx, protocoljsonrpc.MethodPermissionAccessModeSet, req, &out)
}

func (s remotePermissionService) SetApprovalMode(ctx context.Context, req coreapi.SetModeRequest) error {
	var out map[string]any
	return s.engine.call(ctx, protocoljsonrpc.MethodPermissionApprovalModeSet, req, &out)
}

func (s remotePermissionService) EnterFullAccess(ctx context.Context, req coreapi.EnterFullAccessRequest) error {
	var out map[string]any
	return s.engine.call(ctx, protocoljsonrpc.MethodPermissionEnterFullAccess, req, &out)
}

type remoteContextService struct {
	engine *RemoteEngine
}

func (s remoteContextService) Preview(ctx context.Context) ([]string, error) {
	var out []string
	if err := s.engine.call(ctx, protocoljsonrpc.MethodContextPreview, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (s remoteContextService) Stats(ctx context.Context) (coreapi.ContextStats, error) {
	var out coreapi.ContextStats
	if err := s.engine.call(ctx, protocoljsonrpc.MethodContextStats, nil, &out); err != nil {
		return coreapi.ContextStats{}, err
	}
	return out, nil
}

func (s remoteContextService) WindowTokens(ctx context.Context) (int, error) {
	var out struct {
		Tokens int `json:"tokens"`
	}
	if err := s.engine.call(ctx, protocoljsonrpc.MethodContextWindow, nil, &out); err != nil {
		return 0, err
	}
	return out.Tokens, nil
}

func (s remoteContextService) PinDocument(ctx context.Context, req coreapi.PinDocumentRequest) error {
	return s.engine.call(ctx, protocoljsonrpc.MethodContextPin, req, nil)
}

func (s remoteContextService) Compact(ctx context.Context) (string, error) {
	var out struct {
		Summary string `json:"summary"`
	}
	if err := s.engine.call(ctx, protocoljsonrpc.MethodContextCompact, nil, &out); err != nil {
		return "", err
	}
	return out.Summary, nil
}

func (s remoteContextService) Clear(ctx context.Context) error {
	return s.engine.call(ctx, protocoljsonrpc.MethodContextClear, nil, nil)
}

func (s remoteContextService) Export(ctx context.Context, req coreapi.ExportContextRequest) error {
	return s.engine.call(ctx, protocoljsonrpc.MethodContextExport, req, nil)
}

type remoteInsightService struct {
	engine *RemoteEngine
}

func (s remoteInsightService) PredictNextUserMessage(ctx context.Context, req coreapi.PredictNextUserMessageRequest) (string, error) {
	var out struct {
		Text string `json:"text"`
	}
	if err := s.engine.call(ctx, protocoljsonrpc.MethodInsightPredictNextUser, req, &out); err != nil {
		return "", err
	}
	return out.Text, nil
}

func (s remoteInsightService) RefineInput(ctx context.Context, req coreapi.RefineInputRequest) (string, error) {
	var out struct {
		Text string `json:"text"`
	}
	if err := s.engine.call(ctx, protocoljsonrpc.MethodInsightRefineInput, req, &out); err != nil {
		return "", err
	}
	return out.Text, nil
}

func (s remoteInsightService) PlanSnapshot(ctx context.Context) (coreapi.PlanSnapshot, error) {
	var out coreapi.PlanSnapshot
	if err := s.engine.call(ctx, protocoljsonrpc.MethodInsightPlanSnapshot, nil, &out); err != nil {
		return coreapi.PlanSnapshot{}, err
	}
	return out, nil
}

type remoteMemoryService struct {
	engine *RemoteEngine
}

func (s remoteMemoryService) Snapshot(ctx context.Context) (coreapi.MemorySnapshot, error) {
	var out coreapi.MemorySnapshot
	if err := s.engine.call(ctx, protocoljsonrpc.MethodMemorySnapshot, nil, &out); err != nil {
		return coreapi.MemorySnapshot{}, err
	}
	return out, nil
}

func (s remoteMemoryService) Save(ctx context.Context, req coreapi.SaveMemoryRequest) error {
	return s.engine.call(ctx, protocoljsonrpc.MethodMemorySave, req, nil)
}

func (s remoteMemoryService) RebuildIndex(ctx context.Context) error {
	return s.engine.call(ctx, protocoljsonrpc.MethodMemoryRebuildIndex, nil, nil)
}

func (s remoteMemoryService) RecordAdd(ctx context.Context, req coreapi.AddMemoryRecordRequest) (coreapi.MemoryRecord, error) {
	var out coreapi.MemoryRecord
	if err := s.engine.call(ctx, protocoljsonrpc.MethodMemoryRecordAdd, req, &out); err != nil {
		return coreapi.MemoryRecord{}, err
	}
	return out, nil
}

func (s remoteMemoryService) RecordList(ctx context.Context, req coreapi.ListMemoryRecordsRequest) ([]coreapi.MemoryRecord, error) {
	var out []coreapi.MemoryRecord
	if err := s.engine.call(ctx, protocoljsonrpc.MethodMemoryRecordList, req, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (s remoteMemoryService) RecordSearch(ctx context.Context, req coreapi.SearchMemoryRecordsRequest) ([]coreapi.MemoryRecord, error) {
	var out []coreapi.MemoryRecord
	if err := s.engine.call(ctx, protocoljsonrpc.MethodMemoryRecordSearch, req, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (s remoteMemoryService) RecordDelete(ctx context.Context, req coreapi.DeleteMemoryRecordRequest) error {
	return s.engine.call(ctx, protocoljsonrpc.MethodMemoryRecordDelete, req, nil)
}

type remoteRoleService struct {
	engine *RemoteEngine
}

func (s remoteRoleService) List(ctx context.Context) ([]coreapi.RoleConfig, error) {
	var out []coreapi.RoleConfig
	if err := s.engine.call(ctx, protocoljsonrpc.MethodRoleList, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (s remoteRoleService) Resolve(ctx context.Context, ref coreapi.RoleRef) (coreapi.RoleConfig, error) {
	var out coreapi.RoleConfig
	if err := s.engine.call(ctx, protocoljsonrpc.MethodRoleResolve, ref, &out); err != nil {
		return coreapi.RoleConfig{}, err
	}
	return out, nil
}

type remoteModeService struct {
	engine *RemoteEngine
}

func (s remoteModeService) Snapshot(ctx context.Context) (coreapi.ModeSnapshot, error) {
	var out coreapi.ModeSnapshot
	if err := s.engine.call(ctx, protocoljsonrpc.MethodRuntimeModesGet, nil, &out); err != nil {
		return coreapi.ModeSnapshot{}, err
	}
	return out, nil
}

func (s remoteModeService) SetExecutionMode(ctx context.Context, req coreapi.SetModeRequest) error {
	var out map[string]any
	return s.engine.call(ctx, protocoljsonrpc.MethodRuntimeExecutionModeSet, req, &out)
}

func (s remoteModeService) SetSandboxMode(ctx context.Context, req coreapi.SetModeRequest) error {
	var out map[string]any
	return s.engine.call(ctx, protocoljsonrpc.MethodRuntimeSandboxModeSet, req, &out)
}

func (s remoteModeService) SetReasoningLevel(ctx context.Context, req coreapi.SetModeRequest) error {
	var out map[string]any
	return s.engine.call(ctx, protocoljsonrpc.MethodRuntimeReasoningLevelSet, req, &out)
}

type remoteModelService struct {
	engine *RemoteEngine
}

func (s remoteModelService) List(ctx context.Context) ([]coreapi.ModelConfig, error) {
	var out []coreapi.ModelConfig
	if err := s.engine.call(ctx, protocoljsonrpc.MethodModelList, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (s remoteModelService) Catalog(ctx context.Context) (coreapi.ModelCatalogState, error) {
	var out coreapi.ModelCatalogState
	if err := s.engine.call(ctx, protocoljsonrpc.MethodModelCatalog, nil, &out); err != nil {
		return coreapi.ModelCatalogState{}, err
	}
	return out, nil
}

func (s remoteModelService) Upsert(ctx context.Context, req coreapi.UpsertModelRequest) error {
	var out map[string]any
	return s.engine.call(ctx, protocoljsonrpc.MethodModelUpsert, req, &out)
}

func (s remoteModelService) Save(ctx context.Context, req coreapi.ModelSaveRequest) error {
	var out map[string]any
	return s.engine.call(ctx, protocoljsonrpc.MethodModelSave, req, &out)
}

func (s remoteModelService) Delete(ctx context.Context, req coreapi.ModelNameRequest) error {
	var out map[string]any
	return s.engine.call(ctx, protocoljsonrpc.MethodModelDelete, req, &out)
}

func (s remoteModelService) Activate(ctx context.Context, req coreapi.ModelNameRequest) error {
	var out map[string]any
	return s.engine.call(ctx, protocoljsonrpc.MethodModelActivate, req, &out)
}

func (s remoteModelService) SyncEnv(ctx context.Context) error {
	var out map[string]any
	return s.engine.call(ctx, protocoljsonrpc.MethodModelSyncEnv, nil, &out)
}

func (s remoteModelService) Context(ctx context.Context, req coreapi.ModelContextRequest) (coreapi.ModelContextSnapshot, error) {
	var out coreapi.ModelContextSnapshot
	if err := s.engine.call(ctx, protocoljsonrpc.MethodModelContext, req, &out); err != nil {
		return coreapi.ModelContextSnapshot{}, err
	}
	return out, nil
}

func (s remoteModelService) SetWorkspace(ctx context.Context, req coreapi.SetWorkspaceModelRequest) error {
	var out map[string]any
	return s.engine.call(ctx, protocoljsonrpc.MethodModelWorkspaceSet, req, &out)
}

func (s remoteModelService) ClearWorkspace(ctx context.Context, req coreapi.ClearWorkspaceModelRequest) error {
	var out map[string]any
	return s.engine.call(ctx, protocoljsonrpc.MethodModelWorkspaceClear, req, &out)
}

func (s remoteModelService) SetSession(ctx context.Context, req coreapi.SetSessionModelRequest) error {
	var out map[string]any
	return s.engine.call(ctx, protocoljsonrpc.MethodModelSessionSet, req, &out)
}

func (s remoteModelService) ClearSession(ctx context.Context, req coreapi.ClearSessionModelRequest) error {
	var out map[string]any
	return s.engine.call(ctx, protocoljsonrpc.MethodModelSessionClear, req, &out)
}

type remoteRemoteWorkspaceService struct {
	engine *RemoteEngine
}

func (s remoteRemoteWorkspaceService) List(ctx context.Context) ([]coreapi.RemoteWorkspace, error) {
	var out []coreapi.RemoteWorkspace
	if err := s.engine.call(ctx, protocoljsonrpc.MethodRemoteWorkspaceList, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (s remoteRemoteWorkspaceService) Open(ctx context.Context, ref coreapi.RemoteWorkspaceRef) (coreapi.RemoteWorkspace, error) {
	var out coreapi.RemoteWorkspace
	if err := s.engine.call(ctx, protocoljsonrpc.MethodRemoteWorkspaceOpen, ref, &out); err != nil {
		return coreapi.RemoteWorkspace{}, err
	}
	return out, nil
}

func (s remoteRemoteWorkspaceService) Forget(ctx context.Context, ref coreapi.RemoteWorkspaceRef) error {
	var out map[string]any
	return s.engine.call(ctx, protocoljsonrpc.MethodRemoteWorkspaceForget, ref, &out)
}

func (s remoteRemoteWorkspaceService) ClearCache(ctx context.Context, ref coreapi.RemoteWorkspaceRef) error {
	var out map[string]any
	return s.engine.call(ctx, protocoljsonrpc.MethodRemoteWorkspaceClearCache, ref, &out)
}

func (s remoteRemoteWorkspaceService) CurrentRepo(ctx context.Context) (coreapi.RemoteRepoState, bool, error) {
	var out coreapi.RemoteRepoState
	if err := s.engine.call(ctx, protocoljsonrpc.MethodRemoteRepoCurrent, nil, &out); err != nil {
		return coreapi.RemoteRepoState{}, false, err
	}
	return out, out.Mode != "" || out.RepoURL != "", nil
}

type remoteInquiryService struct {
	engine *RemoteEngine
}

func (s remoteInquiryService) Respond(ctx context.Context, req coreapi.InquiryResponse) error {
	var out map[string]any
	return s.engine.call(ctx, protocoljsonrpc.MethodInquiryRespond, req, &out)
}

type remoteToolTelemetryService struct {
	engine *RemoteEngine
}

func (s remoteToolTelemetryService) Traces(ctx context.Context) ([]coreapi.ToolTrace, error) {
	var out []coreapi.ToolTrace
	if err := s.engine.call(ctx, protocoljsonrpc.MethodToolTraces, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (s remoteToolTelemetryService) Stats(ctx context.Context) ([]coreapi.ToolStat, error) {
	var out []coreapi.ToolStat
	if err := s.engine.call(ctx, protocoljsonrpc.MethodToolStats, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

type remoteEventSubscriber struct {
	engine *RemoteEngine
}

func (b remoteEventSubscriber) Subscribe(ctx context.Context, filter coreapi.EventFilter) (<-chan protocol.Envelope, error) {
	var out struct {
		ID             string `json:"id"`
		SubscriptionID string `json:"subscription_id"`
	}
	err := b.engine.call(ctx, protocoljsonrpc.MethodEventSubscribe, coreapi.EventSubscribeRequest{
		SessionID: filter.SessionID,
		TurnID:    filter.TurnID,
		AgentID:   filter.AgentID,
	}, &out)
	if err != nil {
		return nil, err
	}
	id := out.ID
	if id == "" {
		id = out.SubscriptionID
	}
	subCtx, subCancel := context.WithCancel(ctx)
	ch := make(chan protocol.Envelope, 64)
	subID := b.engine.nextEventID.Add(1)
	sub := &eventSubscriber{ch: ch, cancel: subCancel}
	b.engine.eventMu.Lock()
	b.engine.eventSubscribers[subID] = sub
	b.engine.eventMu.Unlock()
	go func() {
		<-subCtx.Done()
		b.engine.eventMu.Lock()
		if _, ok := b.engine.eventSubscribers[subID]; ok {
			close(ch)
			delete(b.engine.eventSubscribers, subID)
		}
		b.engine.eventMu.Unlock()
		if id != "" {
			var discard map[string]any
			_ = b.engine.call(context.Background(), protocoljsonrpc.MethodEventUnsubscribe, coreapi.EventUnsubscribeRequest{ID: id}, &discard)
		}
	}()
	return ch, nil
}

var _ coreapi.Engine = (*RemoteEngine)(nil)
var _ coreapi.WorkspaceService = remoteWorkspaceService{}
var _ coreapi.StateService = remoteStateService{}
var _ coreapi.SessionService = remoteSessionService{}
var _ coreapi.MCPService = remoteMCPService{}
var _ coreapi.LSPService = remoteLSPService{}
var _ coreapi.ExtensionService = remoteExtensionService{}
var _ coreapi.PermissionService = remotePermissionService{}
var _ coreapi.ModeService = remoteModeService{}
var _ coreapi.ModelService = remoteModelService{}
var _ coreapi.TurnService = remoteTurnService{}
var _ coreapi.ApprovalService = remoteApprovalService{}
var _ coreapi.InquiryService = remoteInquiryService{}
var _ coreapi.ToolExecutor = remoteToolExecutor{}
var _ coreapi.ToolCatalogService = remoteToolCatalogService{}
var _ coreapi.ToolTelemetryService = remoteToolTelemetryService{}
var _ coreapi.RemoteWorkspaceService = remoteRemoteWorkspaceService{}
var _ coreapi.EventSubscriber = remoteEventSubscriber{}
var _ coreapi.SandboxService = remoteSandboxService{}
var _ coreapi.ConfigService = remoteConfigService{}
