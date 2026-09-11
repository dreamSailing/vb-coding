package adapter

// Copyright (c) 2026 EOSAIOS
// SPDX-License-Identifier: EOS-NCL-1.1
// 本文件基于 EOS 非商用许可证 v1.1 发布，详见 LICENSE。
// 商业使用请联系版权人获得商业授权。

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/eosaios/eos/pkg/coreapi"
	coreapijsonrpc "github.com/eosaios/eos/pkg/coreapi/jsonrpc"
	"github.com/eosaios/eos/pkg/protocol"
	protocoljsonrpc "github.com/eosaios/eos/pkg/protocol/jsonrpc"
	"github.com/eosaios/eos/pkg/sandbox"
)

// StdioGateway is a read-only JSON-RPC gateway backed by an external
// `eos app-server --transport stdio` process via StdioClient.
//
// It implements a subset of bridgeRuntimeGateway — only read-only / low-risk
// methods. Write methods return an error to prevent accidental mutation
// through the external transport during the prototype phase.
type StdioGateway struct {
	client *StdioClient
	events *runtimeJSONRPCEventSink
}

// rpcTimeout 是所有非 turn RPC 的默认超时。turn 相关 RPC（turn_start / approval_respond）
// 走各自的 watchdog，不走这个超时。60s 足够所有读/写操作（模型不在此路径）。
// S1 修复：原 rpcCtx() 无超时，模型卡住时永久阻塞 UI。
const rpcTimeout = 60 * time.Second

// rpcCtx 返回带超时的 context（cancel 在超时或请求完成后自动触发）。
// 用于替代裸 rpcCtx()——保证 RPC 不会永久阻塞。
func rpcCtx() context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
	// cancel 在 ctx 过期后自动调；这里起一个 goroutine 在 ctx done 后调 cancel，
	// 避免资源泄漏（Go linter 要求所有 WithTimeout 的 cancel 被调用）。
	go func() {
		<-ctx.Done()
		cancel()
	}()
	return ctx
}

// NewStdioGateway creates a stdio-backed gateway that implements
// bridgeRuntimeGateway (read-only subset). The StdioClient must already be
// started before calling any gateway methods.
func NewStdioGateway(client *StdioClient) *StdioGateway {
	gateway := &StdioGateway{client: client, events: newRuntimeJSONRPCEventSink()}
	if client != nil {
		client.SetNotificationHandler(gateway.events.Notify)
	}
	return gateway
}

// --- Read-only / low-risk methods (implemented) ---

func (g *StdioGateway) CoreInitializeRPC(ctx context.Context) (coreapijsonrpc.InitializeResult, error) {
	var out coreapijsonrpc.InitializeResult
	if err := g.client.Call(ctx, protocoljsonrpc.MethodInitialize, nil, &out); err != nil {
		return coreapijsonrpc.InitializeResult{}, err
	}
	return out, nil
}

// CoreBrowserControlTakeoverRPC 人主动接管浏览器控制权（内核裁决并发布事件）。
func (g *StdioGateway) CoreBrowserControlTakeoverRPC(ctx context.Context, req coreapi.BrowserControlTakeoverRequest) error {
	var out map[string]any
	return g.client.Call(ctx, protocoljsonrpc.MethodBrowserControlTakeover, req, &out)
}

// CoreBrowserControlResumeRPC 交还 AI 浏览器控制权。
func (g *StdioGateway) CoreBrowserControlResumeRPC(ctx context.Context) error {
	var out map[string]any
	return g.client.Call(ctx, protocoljsonrpc.MethodBrowserControlResume, coreapi.BrowserControlResumeRequest{}, &out)
}

// CoreBrowserUploadProvideRPC 壳层回填上传文件（browser.upload.needed 应答）。
func (g *StdioGateway) CoreBrowserUploadProvideRPC(ctx context.Context, requestID string, paths []string) error {
	var out map[string]any
	return g.client.Call(ctx, protocoljsonrpc.MethodBrowserUploadProvide, coreapi.BrowserUploadProvideRequest{
		RequestID: requestID,
		Paths:     paths,
	}, &out)
}

// CoreBrowserFocusRPC 置顶会话 tab。
func (g *StdioGateway) CoreBrowserFocusRPC(ctx context.Context) error {
	var out map[string]any
	return g.client.Call(ctx, protocoljsonrpc.MethodBrowserFocus, nil, &out)
}

// CoreBrowserSetDefaultProfileRPC 切换默认 profile。
func (g *StdioGateway) CoreBrowserSetDefaultProfileRPC(ctx context.Context, profile string) error {
	var out map[string]any
	return g.client.Call(ctx, protocoljsonrpc.MethodBrowserSetDefaultProfile, coreapi.BrowserSetDefaultProfileRequest{
		Profile: profile,
	}, &out)
}

// CoreBrowserNavigateRPC 人从面板地址栏导航（内核懒启动浏览器）。
func (g *StdioGateway) CoreBrowserNavigateRPC(ctx context.Context, url string) error {
	var out map[string]any
	return g.client.Call(ctx, protocoljsonrpc.MethodBrowserNavigate, coreapi.BrowserNavigateRequest{
		URL: url,
	}, &out)
}

// CoreBrowserTabNewRPC 面板「+」新开 tab（会话绑定切到新 tab）。
func (g *StdioGateway) CoreBrowserTabNewRPC(ctx context.Context, url string) (coreapi.BrowserTabInfo, error) {
	var out coreapi.BrowserTabInfo
	err := g.client.Call(ctx, protocoljsonrpc.MethodBrowserTabNew, coreapi.BrowserTabNewRequest{
		URL: url,
	}, &out)
	return out, err
}

// CoreBrowserTabSwitchRPC 面板标签条点击切换绑定 tab。
func (g *StdioGateway) CoreBrowserTabSwitchRPC(ctx context.Context, index int) (coreapi.BrowserTabInfo, error) {
	var out coreapi.BrowserTabInfo
	err := g.client.Call(ctx, protocoljsonrpc.MethodBrowserTabSwitch, coreapi.BrowserTabSwitchRequest{
		Index: index,
	}, &out)
	return out, err
}

// CoreBrowserTabCloseRPC 面板标签条关闭 tab（index nil = 关绑定 tab）。
func (g *StdioGateway) CoreBrowserTabCloseRPC(ctx context.Context, index *int) error {
	var out map[string]any
	return g.client.Call(ctx, protocoljsonrpc.MethodBrowserTabClose, coreapi.BrowserTabCloseRequest{
		Index: index,
	}, &out)
}

// CoreBrowserLiveStartRPC 开启内嵌实时视口流（CDP screencast 推帧）。
func (g *StdioGateway) CoreBrowserLiveStartRPC(ctx context.Context, request coreapi.BrowserLiveStartRequest) error {
	var out map[string]any
	return g.client.Call(ctx, protocoljsonrpc.MethodBrowserLiveStart, request, &out)
}

// CoreBrowserLiveStopRPC 停止内嵌实时视口流。
func (g *StdioGateway) CoreBrowserLiveStopRPC(ctx context.Context) error {
	var out map[string]any
	return g.client.Call(ctx, protocoljsonrpc.MethodBrowserLiveStop, nil, &out)
}

// CoreBrowserInputRPC 人在内嵌视口的输入注入（mouse/wheel/key/text）。
func (g *StdioGateway) CoreBrowserInputRPC(ctx context.Context, request coreapi.BrowserInputRequest) error {
	var out map[string]any
	return g.client.Call(ctx, protocoljsonrpc.MethodBrowserInput, request, &out)
}

// CoreBrowserHistoryRPC 地址栏历史导航（back/forward/reload）。
func (g *StdioGateway) CoreBrowserHistoryRPC(ctx context.Context, action string) error {
	var out map[string]any
	return g.client.Call(ctx, protocoljsonrpc.MethodBrowserHistory, coreapi.BrowserHistoryRequest{
		Action: action,
	}, &out)
}

// CoreBrowserPickStartRPC 开启元素选取模式。
func (g *StdioGateway) CoreBrowserPickStartRPC(ctx context.Context) error {
	var out map[string]any
	return g.client.Call(ctx, protocoljsonrpc.MethodBrowserPickStart, nil, &out)
}

// CoreBrowserPickStopRPC 退出选取模式。
func (g *StdioGateway) CoreBrowserPickStopRPC(ctx context.Context) error {
	var out map[string]any
	return g.client.Call(ctx, protocoljsonrpc.MethodBrowserPickStop, nil, &out)
}

// CoreBrowserProfilesRPC 列出 profile 注册表。
func (g *StdioGateway) CoreBrowserProfilesRPC(ctx context.Context) ([]coreapi.BrowserProfileRecord, error) {
	var out []coreapi.BrowserProfileRecord
	if err := g.client.Call(ctx, protocoljsonrpc.MethodBrowserProfiles, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (g *StdioGateway) CoreStateSnapshotRPC(ctx context.Context) (coreapi.StateSnapshot, error) {
	var out coreapi.StateSnapshot
	// Always request the desktop's own source view from the shared core store.
	if err := g.client.Call(ctx, protocoljsonrpc.MethodStateSnapshot, coreapi.StateSnapshotRequest{Source: "gui"}, &out); err != nil {
		return coreapi.StateSnapshot{}, err
	}
	return out, nil
}

func (g *StdioGateway) CoreRuntimeSnapshotRPC(ctx context.Context) (RuntimeSnapshot, error) {
	snapshot, err := g.CoreStateSnapshotRPC(ctx)
	if err != nil {
		return RuntimeSnapshot{}, err
	}
	return runtimeSnapshotFromCoreAPI(snapshot), nil
}

func (g *StdioGateway) CoreListSessionsRPC(ctx context.Context, workspaceRoot string) ([]coreapi.Session, error) {
	var out []coreapi.Session
	if err := g.client.Call(ctx, protocoljsonrpc.MethodSessionList, coreapi.ListSessionsRequest{
		WorkspaceRoot: strings.TrimSpace(workspaceRoot),
		Source:        "gui",
	}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CoreListArchivedSessionsRPC returns the desktop's archived sessions.
func (g *StdioGateway) CoreListArchivedSessionsRPC(ctx context.Context) ([]coreapi.Session, error) {
	var out []coreapi.Session
	if err := g.client.Call(ctx, protocoljsonrpc.MethodSessionList, coreapi.ListSessionsRequest{
		Source:          "gui",
		IncludeArchived: true,
	}, &out); err != nil {
		return nil, err
	}
	archived := make([]coreapi.Session, 0, len(out))
	for _, s := range out {
		if s.Metadata != nil {
			if v, ok := s.Metadata["archived"]; ok {
				if b, _ := v.(bool); b {
					archived = append(archived, s)
				}
			}
		}
	}
	return archived, nil
}

func (g *StdioGateway) ListSessions() []SessionMeta {
	items, err := g.CoreListSessionsRPC(rpcCtx(), "")
	if err != nil {
		return nil
	}
	return sessionMetasFromCoreAPI(items)
}

func (g *StdioGateway) ListWorkspaceSessions(workspace string) ([]SessionMeta, error) {
	items, err := g.CoreListSessionsRPC(rpcCtx(), workspace)
	if err != nil {
		return nil, err
	}
	return sessionMetasFromCoreAPI(items), nil
}

func (g *StdioGateway) CoreResolveForegroundWorkspaceRPC(ctx context.Context, preferred string) (string, error) {
	var out string
	if err := g.client.Call(ctx, protocoljsonrpc.MethodWorkspaceResolve, coreapi.ResolveForegroundWorkspaceRequest{
		Preferred: strings.TrimSpace(preferred),
	}, &out); err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func (g *StdioGateway) ResolveForegroundWorkspace(preferred string) (string, error) {
	return g.CoreResolveForegroundWorkspaceRPC(rpcCtx(), preferred)
}

func (g *StdioGateway) CoreModeSnapshotRPC(ctx context.Context) (coreapi.ModeSnapshot, error) {
	var out coreapi.ModeSnapshot
	if err := g.client.Call(ctx, protocoljsonrpc.MethodRuntimeModesGet, nil, &out); err != nil {
		return coreapi.ModeSnapshot{}, err
	}
	out.ExecutionMode = strings.TrimSpace(out.ExecutionMode)
	out.SandboxMode = strings.TrimSpace(out.SandboxMode)
	out.ReasoningLevel = strings.TrimSpace(out.ReasoningLevel)
	return out, nil
}

func (g *StdioGateway) CoreListWorkspacesRPC(ctx context.Context) ([]Workspace, error) {
	var out []coreapi.Workspace
	if err := g.client.Call(ctx, protocoljsonrpc.MethodWorkspaceList, coreapi.WorkspaceListRequest{Source: "gui"}, &out); err != nil {
		return nil, err
	}
	return workspacesFromCoreAPI(out), nil
}

func (g *StdioGateway) CoreDefaultWorkspaceRPC(ctx context.Context) (string, error) {
	return g.coreWorkspacePathRPC(ctx, protocoljsonrpc.MethodWorkspaceDefault, nil)
}

func (g *StdioGateway) DefaultWorkspacePath() string {
	if path, err := g.CoreDefaultWorkspaceRPC(rpcCtx()); err == nil {
		return path
	}
	return ""
}

func (g *StdioGateway) CoreLastWorkspaceRPC(ctx context.Context) (string, error) {
	return g.coreWorkspacePathRPC(ctx, protocoljsonrpc.MethodWorkspaceLast, nil)
}

func (g *StdioGateway) LastWorkspace() string {
	if path, err := g.CoreLastWorkspaceRPC(rpcCtx()); err == nil {
		return path
	}
	return ""
}

func (g *StdioGateway) coreWorkspacePathRPC(ctx context.Context, method string, params any) (string, error) {
	var out string
	if err := g.client.Call(ctx, method, params, &out); err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// --- Methods not yet implemented for stdio gateway (write/high-risk) ---
// These return errors to prevent accidental use during the prototype phase.
// The BridgeService JSON-RPC-first helpers will fall back to the legacy
// adapter path when these fail, preserving current desktop behavior.

func (g *StdioGateway) CoreRunBashStreamRPC(context.Context, string) (<-chan Event, error) {
	return nil, errStdioNotImplemented
}

func (g *StdioGateway) RunBash(context.Context, string) (<-chan Event, error) {
	return nil, errStdioNotImplemented
}

func (g *StdioGateway) Invoke(context.Context, string) (<-chan Event, error) {
	return nil, errStdioNotImplemented
}

func (g *StdioGateway) CoreConfigPath() string {
	return ""
}

func (g *StdioGateway) CoreAddWorkspaceRPC(ctx context.Context, path string) error {
	return g.coreWorkspaceMutationRPC(ctx, protocoljsonrpc.MethodWorkspaceAdd, path)
}

func (g *StdioGateway) AddWorkspace(path string) error {
	return g.CoreAddWorkspaceRPC(rpcCtx(), path)
}

func (g *StdioGateway) CoreRemoveWorkspaceRPC(ctx context.Context, path string) error {
	return g.coreWorkspaceMutationRPC(ctx, protocoljsonrpc.MethodWorkspaceRemove, path)
}

func (g *StdioGateway) RemoveWorkspace(path string) error {
	return g.CoreRemoveWorkspaceRPC(rpcCtx(), path)
}

func (g *StdioGateway) CoreUseWorkspaceRPC(ctx context.Context, path string) error {
	return g.coreWorkspaceMutationRPC(ctx, protocoljsonrpc.MethodWorkspaceUse, path)
}

func (g *StdioGateway) UseWorkspace(path string) error {
	return g.CoreUseWorkspaceRPC(rpcCtx(), path)
}

func (g *StdioGateway) CoreTrustWorkspaceRPC(ctx context.Context, path string) error {
	return g.coreWorkspaceMutationRPC(ctx, protocoljsonrpc.MethodWorkspaceTrust, path)
}

func (g *StdioGateway) TrustWorkspace(path string) error {
	return g.CoreTrustWorkspaceRPC(rpcCtx(), path)
}

func (g *StdioGateway) CoreRememberWorkspaceRPC(ctx context.Context, path string, foreground bool) error {
	params := coreapi.RememberWorkspaceRequest{
		Path:       strings.TrimSpace(path),
		Foreground: foreground,
	}
	var out struct {
		OK bool `json:"ok"`
	}
	if err := g.client.Call(ctx, protocoljsonrpc.MethodWorkspaceRemember, params, &out); err != nil {
		return err
	}
	return nil
}

func (g *StdioGateway) RememberWorkspace(path string, foreground bool) error {
	return g.CoreRememberWorkspaceRPC(rpcCtx(), path, foreground)
}

func (g *StdioGateway) coreWorkspaceMutationRPC(ctx context.Context, method string, path string) error {
	params := coreapi.WorkspacePathRequest{
		Path: strings.TrimSpace(path),
	}
	var out struct {
		OK bool `json:"ok"`
	}
	if err := g.client.Call(ctx, method, params, &out); err != nil {
		return err
	}
	return nil
}

func (g *StdioGateway) coreSetModeRPC(ctx context.Context, method string, mode string) error {
	params := coreapi.SetModeRequest{
		Mode: strings.TrimSpace(mode),
	}
	var out struct {
		OK bool `json:"ok"`
	}
	if err := g.client.Call(ctx, method, params, &out); err != nil {
		return err
	}
	return nil
}

func (g *StdioGateway) CoreCreateWorktreeRPC(ctx context.Context, name string) (Worktree, error) {
	params := struct {
		Name string `json:"name"`
	}{
		Name: strings.TrimSpace(name),
	}
	var result struct {
		Name      string `json:"name"`
		Path      string `json:"path"`
		Branch    string `json:"branch"`
		Head      string `json:"head"`
		Active    bool   `json:"active"`
		Removable bool   `json:"removable"`
	}
	if err := g.client.Call(ctx, "workspace/worktree/create", params, &result); err != nil {
		return Worktree{}, err
	}
	return Worktree{
		Name:      result.Name,
		Path:      result.Path,
		Branch:    result.Branch,
		Head:      result.Head,
		Active:    result.Active,
		Removable: result.Removable,
	}, nil
}

func (g *StdioGateway) CreateWorktree(name string) (Worktree, error) {
	return g.CoreCreateWorktreeRPC(rpcCtx(), name)
}

func (g *StdioGateway) CoreRemoveWorktreeRPC(ctx context.Context, path string, force bool) error {
	params := struct {
		Path  string `json:"path"`
		Force bool   `json:"force"`
	}{
		Path:  strings.TrimSpace(path),
		Force: force,
	}
	var out struct {
		OK bool `json:"ok"`
	}
	return g.client.Call(ctx, "workspace/worktree/remove", params, &out)
}

func (g *StdioGateway) RemoveWorktree(path string, force bool) error {
	return g.CoreRemoveWorktreeRPC(rpcCtx(), path, force)
}

func (g *StdioGateway) CoreOpenRemoteWorkspaceRPC(ctx context.Context, idOrPath string) (RemoteWorkspace, error) {
	var out coreapi.RemoteWorkspace
	if err := g.client.Call(ctx, protocoljsonrpc.MethodRemoteWorkspaceOpen, coreapi.RemoteWorkspaceRef{
		IDOrPath: strings.TrimSpace(idOrPath),
	}, &out); err != nil {
		return RemoteWorkspace{}, err
	}
	return remoteWorkspaceFromCoreAPI(out), nil
}

func (g *StdioGateway) OpenRemoteWorkspace(idOrPath string) (RemoteWorkspace, error) {
	return g.CoreOpenRemoteWorkspaceRPC(rpcCtx(), idOrPath)
}

func (g *StdioGateway) CoreForgetRemoteWorkspaceRPC(ctx context.Context, idOrPath string) error {
	var out map[string]any
	return g.client.Call(ctx, protocoljsonrpc.MethodRemoteWorkspaceForget, coreapi.RemoteWorkspaceRef{
		IDOrPath: strings.TrimSpace(idOrPath),
	}, &out)
}

func (g *StdioGateway) ForgetRemoteWorkspace(idOrPath string) error {
	return g.CoreForgetRemoteWorkspaceRPC(rpcCtx(), idOrPath)
}

func (g *StdioGateway) CoreClearRemoteWorkspaceCacheRPC(ctx context.Context, idOrPath string) error {
	var out map[string]any
	return g.client.Call(ctx, protocoljsonrpc.MethodRemoteWorkspaceClearCache, coreapi.RemoteWorkspaceRef{
		IDOrPath: strings.TrimSpace(idOrPath),
	}, &out)
}

func (g *StdioGateway) ClearRemoteWorkspaceCache(idOrPath string) error {
	return g.CoreClearRemoteWorkspaceCacheRPC(rpcCtx(), idOrPath)
}

func (g *StdioGateway) CoreSetExecutionModeRPC(ctx context.Context, mode string) error {
	return g.coreSetModeRPC(ctx, protocoljsonrpc.MethodRuntimeExecutionModeSet, mode)
}

func (g *StdioGateway) SetExecutionMode(mode string) {
	_ = g.CoreSetExecutionModeRPC(rpcCtx(), mode)
}

func (g *StdioGateway) ExecutionMode() string {
	if snap, err := g.CoreModeSnapshotRPC(rpcCtx()); err == nil {
		return normalizeExecutionMode(snap.ExecutionMode)
	}
	return ""
}

func (g *StdioGateway) CoreSetSandboxModeRPC(ctx context.Context, mode string) error {
	return g.coreSetModeRPC(ctx, protocoljsonrpc.MethodRuntimeSandboxModeSet, mode)
}

func (g *StdioGateway) CoreSetApprovalModeRPC(ctx context.Context, mode string) error {
	return g.coreSetModeRPC(ctx, protocoljsonrpc.MethodPermissionApprovalModeSet, mode)
}

func (g *StdioGateway) SetSandboxMode(mode string) {
	_ = g.CoreSetSandboxModeRPC(rpcCtx(), mode)
}

func (g *StdioGateway) SandboxMode() string {
	if snap, err := g.CoreModeSnapshotRPC(rpcCtx()); err == nil {
		return normalizeSandboxMode(snap.SandboxMode)
	}
	return ""
}

func (g *StdioGateway) CoreSetReasoningLevelRPC(ctx context.Context, level string) error {
	return g.coreSetModeRPC(ctx, protocoljsonrpc.MethodRuntimeReasoningLevelSet, level)
}

func (g *StdioGateway) SetReasoningLevel(level string) error {
	return g.CoreSetReasoningLevelRPC(rpcCtx(), level)
}

func (g *StdioGateway) ReasoningLevel() string {
	if snap, err := g.CoreModeSnapshotRPC(rpcCtx()); err == nil {
		return strings.TrimSpace(snap.ReasoningLevel)
	}
	return ""
}

func (g *StdioGateway) CoreCreateSessionRPC(ctx context.Context, workspaceRoot string, title string, source string, messages []SessionMessage) (SessionMeta, error) {
	coreMsgs := coreAPISessionMessages(messages)
	params := coreapi.CreateSessionRequest{
		WorkspaceRoot: strings.TrimSpace(workspaceRoot),
		Title:         strings.TrimSpace(title),
		Messages:      coreMsgs,
	}
	if source = strings.TrimSpace(source); source != "" {
		params.Metadata = map[string]any{"source": source}
	}
	var out coreapi.Session
	if err := g.client.Call(ctx, protocoljsonrpc.MethodSessionCreate, params, &out); err != nil {
		return SessionMeta{}, err
	}
	return sessionMetaFromCoreAPI(out), nil
}

func (g *StdioGateway) CreateWorkspaceSession(workspaceRoot string, title string, messages []SessionMessage) (SessionMeta, error) {
	return g.CoreCreateSessionRPC(rpcCtx(), workspaceRoot, title, "gui", messages)
}

func (g *StdioGateway) CoreDeleteSessionRPC(ctx context.Context, workspaceRoot string, sessionID string) error {
	params := coreapi.DeleteSessionRequest{
		SessionID:     strings.TrimSpace(sessionID),
		WorkspaceRoot: strings.TrimSpace(workspaceRoot),
	}
	var out struct {
		OK bool `json:"ok"`
	}
	if err := g.client.Call(ctx, protocoljsonrpc.MethodSessionDelete, params, &out); err != nil {
		return err
	}
	return nil
}

func (g *StdioGateway) DeleteWorkspaceSession(workspaceRoot string, sessionID string) error {
	return g.CoreDeleteSessionRPC(rpcCtx(), workspaceRoot, sessionID)
}

func (g *StdioGateway) CoreRenameSessionRPC(ctx context.Context, workspaceRoot string, sessionID string, title string) (SessionMeta, error) {
	params := coreapi.RenameSessionRequest{
		SessionID:     strings.TrimSpace(sessionID),
		WorkspaceRoot: strings.TrimSpace(workspaceRoot),
		Title:         strings.TrimSpace(title),
	}
	var out coreapi.Session
	if err := g.client.Call(ctx, protocoljsonrpc.MethodSessionRename, params, &out); err != nil {
		return SessionMeta{}, err
	}
	return sessionMetaFromCoreAPI(out), nil
}

func (g *StdioGateway) UpdateWorkspaceSessionTitle(workspaceRoot string, sessionID string, title string) error {
	_, err := g.CoreRenameSessionRPC(rpcCtx(), workspaceRoot, sessionID, title)
	return err
}

// CoreArchiveSessionRPC sets the session's archived flag via session/set_meta.
func (g *StdioGateway) CoreArchiveSessionRPC(ctx context.Context, sessionID string, archived bool) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return errors.New("session id is required")
	}
	var value json.RawMessage
	if archived {
		value = json.RawMessage("true")
	}
	var out coreapi.Session
	if err := g.client.Call(ctx, protocoljsonrpc.MethodSessionSetMeta, coreapi.SetSessionMetaRequest{
		SessionID: sessionID,
		Key:       "archived",
		Value:     value,
	}, &out); err != nil {
		return err
	}
	return nil
}

// CoreSetSessionSandboxModeRPC 把会话级沙箱模式写入 session metadata（key=
// sandbox_mode），供下次启动按会话恢复。
//
// mode=="workspace"（默认）时删除键，让该会话回落到默认；其他值（如 full_access）
// 写入字符串值。归一化由调用方在 bridge 层完成（adapter 层只透传，单一真相源在内核）。
func (g *StdioGateway) CoreSetSessionSandboxModeRPC(ctx context.Context, sessionID, mode string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return errors.New("session id is required")
	}
	mode = strings.TrimSpace(mode)
	var value json.RawMessage
	if mode != "" && mode != "workspace" {
		value = json.RawMessage(`"` + mode + `"`)
	}
	var out coreapi.Session
	if err := g.client.Call(ctx, protocoljsonrpc.MethodSessionSetMeta, coreapi.SetSessionMetaRequest{
		SessionID: sessionID,
		Key:       "sandbox_mode",
		Value:     value,
	}, &out); err != nil {
		return err
	}
	return nil
}

func (g *StdioGateway) CoreLoadSessionMessagesRPC(ctx context.Context, workspaceRoot string, sessionID string) ([]SessionMessage, error) {
	params := coreapi.LoadSessionMessagesRequest{
		SessionID:     strings.TrimSpace(sessionID),
		WorkspaceRoot: strings.TrimSpace(workspaceRoot),
	}
	var out []coreapi.SessionMessage
	if err := g.client.Call(ctx, protocoljsonrpc.MethodSessionMessagesLoad, params, &out); err != nil {
		return nil, err
	}
	return sessionMessagesFromCoreAPI(out), nil
}

func (g *StdioGateway) LoadWorkspaceSessionMessages(workspaceRoot string, sessionID string) ([]SessionMessage, error) {
	return g.CoreLoadSessionMessagesRPC(rpcCtx(), workspaceRoot, sessionID)
}

func (g *StdioGateway) CoreSaveSessionMessagesRPC(ctx context.Context, workspaceRoot string, sessionID string, messages []SessionMessage) (SessionMeta, error) {
	coreMsgs := coreAPISessionMessages(messages)
	params := coreapi.SaveSessionMessagesRequest{
		SessionID:     strings.TrimSpace(sessionID),
		WorkspaceRoot: strings.TrimSpace(workspaceRoot),
		Messages:      coreMsgs,
	}
	var out coreapi.Session
	if err := g.client.Call(ctx, protocoljsonrpc.MethodSessionMessagesSave, params, &out); err != nil {
		return SessionMeta{}, err
	}
	return sessionMetaFromCoreAPI(out), nil
}

func (g *StdioGateway) SaveWorkspaceSessionMessages(workspaceRoot string, sessionID string, messages []SessionMessage) (string, error) {
	meta, err := g.CoreSaveSessionMessagesRPC(rpcCtx(), workspaceRoot, sessionID, messages)
	if err != nil {
		return "", err
	}
	return meta.ID, nil
}

func (g *StdioGateway) RuntimeSnapshot() RuntimeSnapshot {
	if snap, err := g.CoreRuntimeSnapshotRPC(rpcCtx()); err == nil {
		return snap
	}
	return RuntimeSnapshot{}
}

func (g *StdioGateway) GetWorkspaceCurrentSession(workspaceRoot string) (string, error) {
	var out coreapi.Session
	params := coreapi.CurrentSessionRequest{
		WorkspaceRoot: strings.TrimSpace(workspaceRoot),
	}
	if err := g.client.Call(rpcCtx(), protocoljsonrpc.MethodSessionCurrent, params, &out); err != nil {
		return "", err
	}
	return out.ID, nil
}

func (g *StdioGateway) CoreCurrentSessionRPC(ctx context.Context, workspaceRoot string) (SessionMeta, error) {
	var out coreapi.Session
	params := coreapi.CurrentSessionRequest{
		WorkspaceRoot: strings.TrimSpace(workspaceRoot),
	}
	if err := g.client.Call(ctx, protocoljsonrpc.MethodSessionCurrent, params, &out); err != nil {
		return SessionMeta{}, err
	}
	return sessionMetaFromCoreAPI(out), nil
}

func (g *StdioGateway) ResolveSessionWorkspace(sessionID string) (string, error) {
	// Use session list to find the workspace for this session
	sessions, err := g.CoreListSessionsRPC(rpcCtx(), "")
	if err != nil {
		return "", err
	}
	for _, s := range sessions {
		if s.ID == sessionID {
			return s.WorkspaceRoot, nil
		}
	}
	return "", errors.New("session not found")
}

func (g *StdioGateway) CoreResumeSessionRPC(ctx context.Context, workspaceRoot string, sessionID string) (SessionMeta, error) {
	params := coreapi.ResumeSessionRequest{
		SessionID:     strings.TrimSpace(sessionID),
		WorkspaceRoot: strings.TrimSpace(workspaceRoot),
	}
	var out coreapi.Session
	if err := g.client.Call(ctx, protocoljsonrpc.MethodSessionResume, params, &out); err != nil {
		return SessionMeta{}, err
	}
	return sessionMetaFromCoreAPI(out), nil
}

func (g *StdioGateway) CoreSetCurrentSessionRPC(ctx context.Context, workspaceRoot string, sessionID string) error {
	params := coreapi.SetCurrentSessionRequest{
		SessionID:     strings.TrimSpace(sessionID),
		WorkspaceRoot: strings.TrimSpace(workspaceRoot),
	}
	var out struct {
		OK bool `json:"ok"`
	}
	if err := g.client.Call(ctx, protocoljsonrpc.MethodSessionSetCurrent, params, &out); err != nil {
		return err
	}
	return nil
}

func (g *StdioGateway) SetWorkspaceCurrentSession(workspaceRoot string, sessionID string) error {
	return g.CoreSetCurrentSessionRPC(rpcCtx(), workspaceRoot, sessionID)
}

func (g *StdioGateway) ResumeWorkspaceSession(workspaceRoot string, sessionID string) error {
	_, err := g.CoreResumeSessionRPC(rpcCtx(), workspaceRoot, sessionID)
	return err
}

func (g *StdioGateway) CoreListModelsRPC(ctx context.Context) ([]ModelConfig, error) {
	var out []coreapi.ModelConfig
	if err := g.client.Call(ctx, protocoljsonrpc.MethodModelList, nil, &out); err != nil {
		return nil, err
	}
	return modelConfigsFromCoreAPI(out), nil
}

func (g *StdioGateway) ListModels() []ModelConfig {
	if models, err := g.CoreListModelsRPC(rpcCtx()); err == nil {
		return models
	}
	return nil
}

func (g *StdioGateway) CoreModelCatalogRPC(ctx context.Context) (ModelCatalogState, error) {
	var out coreapi.ModelCatalogState
	if err := g.client.Call(ctx, protocoljsonrpc.MethodModelCatalog, nil, &out); err != nil {
		return ModelCatalogState{}, err
	}
	return modelCatalogFromCoreAPI(out), nil
}

func (g *StdioGateway) ModelCatalog() ModelCatalogState {
	if catalog, err := g.CoreModelCatalogRPC(rpcCtx()); err == nil {
		return catalog
	}
	return ModelCatalogState{}
}

func modelContextFromCoreAPI(snapshot coreapi.ModelContextSnapshot) ModelContextSnapshot {
	return ModelContextSnapshot{
		WorkspaceRoot:      strings.TrimSpace(snapshot.WorkspaceRoot),
		SessionID:          strings.TrimSpace(snapshot.SessionID),
		GlobalDefaultName:  strings.TrimSpace(snapshot.GlobalDefaultName),
		WorkspaceModelName: strings.TrimSpace(snapshot.WorkspaceModelName),
		SessionModelName:   strings.TrimSpace(snapshot.SessionModelName),
		ResolvedModelName:  strings.TrimSpace(snapshot.ResolvedModelName),
		ResolvedScope:      strings.TrimSpace(snapshot.ResolvedScope),
	}
}

func (g *StdioGateway) CoreModelContextRPC(ctx context.Context, req ModelContextRequest) (ModelContextSnapshot, error) {
	var out coreapi.ModelContextSnapshot
	if err := g.client.Call(ctx, protocoljsonrpc.MethodModelContext, coreapi.ModelContextRequest{
		WorkspaceRoot: strings.TrimSpace(req.WorkspaceRoot),
		SessionID:     strings.TrimSpace(req.SessionID),
	}, &out); err != nil {
		return ModelContextSnapshot{}, err
	}
	return modelContextFromCoreAPI(out), nil
}

func (g *StdioGateway) CoreSetWorkspaceModelRPC(ctx context.Context, workspaceRoot, modelName string) error {
	return g.client.Call(ctx, protocoljsonrpc.MethodModelWorkspaceSet, coreapi.SetWorkspaceModelRequest{
		WorkspaceRoot: strings.TrimSpace(workspaceRoot),
		ModelName:     strings.TrimSpace(modelName),
	}, nil)
}

func (g *StdioGateway) CoreClearWorkspaceModelRPC(ctx context.Context, workspaceRoot string) error {
	return g.client.Call(ctx, protocoljsonrpc.MethodModelWorkspaceClear, coreapi.ClearWorkspaceModelRequest{
		WorkspaceRoot: strings.TrimSpace(workspaceRoot),
	}, nil)
}

func (g *StdioGateway) CoreSetSessionModelRPC(ctx context.Context, sessionID, modelName string) error {
	return g.client.Call(ctx, protocoljsonrpc.MethodModelSessionSet, coreapi.SetSessionModelRequest{
		SessionID: strings.TrimSpace(sessionID),
		ModelName: strings.TrimSpace(modelName),
	}, nil)
}

func (g *StdioGateway) CoreClearSessionModelRPC(ctx context.Context, sessionID string) error {
	return g.client.Call(ctx, protocoljsonrpc.MethodModelSessionClear, coreapi.ClearSessionModelRequest{
		SessionID: strings.TrimSpace(sessionID),
	}, nil)
}

func (g *StdioGateway) CoreUpsertModelRPC(ctx context.Context, name, base, key, model string) error {
	return g.client.Call(ctx, protocoljsonrpc.MethodModelUpsert, coreapi.UpsertModelRequest{
		Name:    strings.TrimSpace(name),
		APIBase: strings.TrimSpace(base),
		APIKey:  strings.TrimSpace(key),
		Model:   strings.TrimSpace(model),
	}, nil)
}

func (g *StdioGateway) UpsertModel(name, base, key, model string) error {
	return g.CoreUpsertModelRPC(rpcCtx(), name, base, key, model)
}

func (g *StdioGateway) CoreSaveModelRPC(ctx context.Context, req ModelSaveRequest) error {
	return g.client.Call(ctx, protocoljsonrpc.MethodModelSave, coreapi.ModelSaveRequest{
		OriginalName: strings.TrimSpace(req.OriginalName),
		Mode:         strings.TrimSpace(req.Mode),
		ProviderID:   strings.TrimSpace(req.ProviderID),
		PresetID:     strings.TrimSpace(req.PresetID),
		Name:         strings.TrimSpace(req.Name),
		APIKey:       strings.TrimSpace(req.APIKey),
		APIBase:      strings.TrimSpace(req.APIBase),
		Model:        strings.TrimSpace(req.Model),
	}, nil)
}

func (g *StdioGateway) CoreVerifyModelRPC(ctx context.Context, req ModelSaveRequest) (coreapi.ModelVerifyResponse, error) {
	var out coreapi.ModelVerifyResponse
	err := g.client.Call(ctx, protocoljsonrpc.MethodModelVerify, coreapi.ModelSaveRequest{
		OriginalName: strings.TrimSpace(req.OriginalName),
		Mode:         strings.TrimSpace(req.Mode),
		ProviderID:   strings.TrimSpace(req.ProviderID),
		PresetID:     strings.TrimSpace(req.PresetID),
		Name:         strings.TrimSpace(req.Name),
		APIKey:       strings.TrimSpace(req.APIKey),
		APIBase:      strings.TrimSpace(req.APIBase),
		Model:        strings.TrimSpace(req.Model),
	}, &out)
	return out, err
}

func (g *StdioGateway) SaveModel(req ModelSaveRequest) error {
	return g.CoreSaveModelRPC(rpcCtx(), req)
}

func (g *StdioGateway) CoreActivateModelRPC(ctx context.Context, name string) error {
	return g.client.Call(ctx, protocoljsonrpc.MethodModelActivate, coreapi.ModelNameRequest{Name: strings.TrimSpace(name)}, nil)
}

func (g *StdioGateway) ActivateModel(name string) error {
	return g.CoreActivateModelRPC(rpcCtx(), name)
}

func (g *StdioGateway) CoreDeleteModelRPC(ctx context.Context, name string) error {
	return g.client.Call(ctx, protocoljsonrpc.MethodModelDelete, coreapi.ModelNameRequest{Name: strings.TrimSpace(name)}, nil)
}

func (g *StdioGateway) DeleteModel(name string) error {
	return g.CoreDeleteModelRPC(rpcCtx(), name)
}

func (g *StdioGateway) CoreListRemoteWorkspacesRPC(ctx context.Context) ([]RemoteWorkspace, error) {
	var out []coreapi.RemoteWorkspace
	if err := g.client.Call(ctx, protocoljsonrpc.MethodRemoteWorkspaceList, nil, &out); err != nil {
		return nil, err
	}
	return remoteWorkspacesFromCoreAPI(out), nil
}

func (g *StdioGateway) ListRemoteWorkspaces() []RemoteWorkspace {
	if items, err := g.CoreListRemoteWorkspacesRPC(rpcCtx()); err == nil {
		return items
	}
	return nil
}

func (g *StdioGateway) CoreCurrentRemoteRepoRPC(ctx context.Context) (RemoteRepoState, bool, error) {
	var out struct {
		OK    bool                    `json:"ok"`
		State coreapi.RemoteRepoState `json:"state"`
	}
	if err := g.client.Call(ctx, protocoljsonrpc.MethodRemoteRepoCurrent, nil, &out); err != nil {
		return RemoteRepoState{}, false, err
	}
	if !out.OK {
		return RemoteRepoState{}, false, nil
	}
	return remoteRepoStateFromCoreAPI(out.State), true, nil
}

func (g *StdioGateway) CurrentRemoteRepo() (RemoteRepoState, bool) {
	state, ok, err := g.CoreCurrentRemoteRepoRPC(rpcCtx())
	if err != nil || !ok {
		return RemoteRepoState{}, false
	}
	return state, true
}

func (g *StdioGateway) CorePredictNextUserMessageRPC(ctx context.Context, draft string) (string, error) {
	var out struct {
		Message string `json:"message"`
	}
	if err := g.client.Call(ctx, protocoljsonrpc.MethodInsightPredictNextUser, coreapi.PredictNextUserMessageRequest{Draft: draft}, &out); err != nil {
		return "", err
	}
	return strings.TrimSpace(out.Message), nil
}

func (g *StdioGateway) PredictNextUserMessage(ctx context.Context, draft string) (string, error) {
	return g.CorePredictNextUserMessageRPC(ctx, draft)
}

func (g *StdioGateway) CoreRefineInputRPC(ctx context.Context, draft string) (string, error) {
	var out struct {
		Text string `json:"text"`
	}
	if err := g.client.Call(ctx, protocoljsonrpc.MethodInsightRefineInput, coreapi.RefineInputRequest{Draft: draft}, &out); err != nil {
		return "", err
	}
	return strings.TrimSpace(out.Text), nil
}

func (g *StdioGateway) CoreListMCPRPC(ctx context.Context) ([]MCPServer, error) {
	var out []coreapi.MCPServer
	if err := g.client.Call(ctx, protocoljsonrpc.MethodMCPList, nil, &out); err != nil {
		return nil, err
	}
	return mcpServersFromCoreAPI(out), nil
}

func (g *StdioGateway) ListMCP() []MCPServer {
	if items, err := g.CoreListMCPRPC(rpcCtx()); err == nil {
		return items
	}
	return nil
}

func (g *StdioGateway) CoreUpsertMCPRPC(ctx context.Context, name, kind, target string, enabled bool) error {
	return g.client.Call(ctx, protocoljsonrpc.MethodMCPUpsert, coreapi.UpsertMCPRequest{
		Name:    strings.TrimSpace(name),
		Type:    strings.TrimSpace(kind),
		Target:  strings.TrimSpace(target),
		Enabled: enabled,
	}, nil)
}

func (g *StdioGateway) UpsertMCP(name, kind, target string, enabled bool) error {
	return g.CoreUpsertMCPRPC(rpcCtx(), name, kind, target, enabled)
}

func (g *StdioGateway) CoreImportMCPJSONRPC(ctx context.Context, raw string) error {
	return g.client.Call(ctx, protocoljsonrpc.MethodMCPImportJSON, coreapi.ImportMCPJSONRequest{Raw: raw}, nil)
}

func (g *StdioGateway) ImportMCPJSON(raw string) error {
	return g.CoreImportMCPJSONRPC(rpcCtx(), raw)
}

func (g *StdioGateway) CoreDeleteMCPRPC(ctx context.Context, name string) error {
	return g.client.Call(ctx, protocoljsonrpc.MethodMCPDelete, coreapi.MCPNameRequest{Name: strings.TrimSpace(name)}, nil)
}

func (g *StdioGateway) DeleteMCP(name string) error {
	return g.CoreDeleteMCPRPC(rpcCtx(), name)
}

func (g *StdioGateway) CoreSetMCPEnabledRPC(ctx context.Context, name string, enabled bool) error {
	return g.client.Call(ctx, protocoljsonrpc.MethodMCPSetEnabled, coreapi.SetMCPEnabledRequest{
		Name:    strings.TrimSpace(name),
		Enabled: enabled,
	}, nil)
}

func (g *StdioGateway) SetMCPEnabled(name string, enabled bool) error {
	return g.CoreSetMCPEnabledRPC(rpcCtx(), name, enabled)
}

func (g *StdioGateway) CoreListLSPRPC(ctx context.Context) ([]LSPServer, error) {
	var out []coreapi.LSPServer
	if err := g.client.Call(ctx, protocoljsonrpc.MethodLSPList, nil, &out); err != nil {
		return nil, err
	}
	return lspServersFromCoreAPI(out), nil
}

func (g *StdioGateway) ListLSP() []LSPServer {
	if items, err := g.CoreListLSPRPC(rpcCtx()); err == nil {
		return items
	}
	return nil
}

func (g *StdioGateway) CoreDetectLSPRPC(ctx context.Context, language string) (string, error) {
	var out struct {
		Message string `json:"message"`
	}
	if err := g.client.Call(ctx, protocoljsonrpc.MethodLSPDetect, coreapi.LSPLanguageRequest{Language: strings.TrimSpace(language)}, &out); err != nil {
		return "", err
	}
	return strings.TrimSpace(out.Message), nil
}

func (g *StdioGateway) DetectLSP(language string) string {
	if msg, err := g.CoreDetectLSPRPC(rpcCtx(), language); err == nil {
		return msg
	}
	return ""
}

func (g *StdioGateway) CoreStartLSPRPC(ctx context.Context, language string) (string, error) {
	var out struct {
		Message string `json:"message"`
	}
	if err := g.client.Call(ctx, protocoljsonrpc.MethodLSPStart, coreapi.LSPLanguageRequest{Language: strings.TrimSpace(language)}, &out); err != nil {
		return "", err
	}
	return strings.TrimSpace(out.Message), nil
}

func (g *StdioGateway) StartLSP(language string) string {
	if msg, err := g.CoreStartLSPRPC(rpcCtx(), language); err == nil {
		return msg
	}
	return ""
}

func (g *StdioGateway) CoreInstallLSPRPC(ctx context.Context, language string) (string, error) {
	var out struct {
		Message string `json:"message"`
	}
	if err := g.client.Call(ctx, protocoljsonrpc.MethodLSPInstall, coreapi.LSPLanguageRequest{Language: strings.TrimSpace(language)}, &out); err != nil {
		return "", err
	}
	return strings.TrimSpace(out.Message), nil
}

func (g *StdioGateway) InstallLSP(language string) string {
	if msg, err := g.CoreInstallLSPRPC(rpcCtx(), language); err == nil {
		return msg
	}
	return ""
}

func (g *StdioGateway) CoreListSkillsRPC(ctx context.Context) ([]SkillInfo, error) {
	var out []coreapi.SkillInfo
	if err := g.client.Call(ctx, protocoljsonrpc.MethodExtensionsSkillsList, nil, &out); err != nil {
		return nil, err
	}
	return skillInfosFromCoreAPI(out), nil
}

func (g *StdioGateway) ListSkills() []SkillInfo {
	if items, err := g.CoreListSkillsRPC(rpcCtx()); err == nil {
		return items
	}
	return nil
}

func (g *StdioGateway) CoreReloadSkillsRPC(ctx context.Context) error {
	return g.client.Call(ctx, protocoljsonrpc.MethodExtensionsSkillsReload, nil, nil)
}

func (g *StdioGateway) ReloadSkills() error {
	return g.CoreReloadSkillsRPC(rpcCtx())
}

func (g *StdioGateway) CoreSetSkillEnabledRPC(ctx context.Context, name string, enabled bool) error {
	return g.client.Call(ctx, protocoljsonrpc.MethodExtensionsSkillSetEnabled, coreapi.SetExtensionEnabledRequest{
		Name:    strings.TrimSpace(name),
		Enabled: enabled,
	}, nil)
}

func (g *StdioGateway) SetSkillEnabled(name string, enabled bool) error {
	return g.CoreSetSkillEnabledRPC(rpcCtx(), name, enabled)
}

func (g *StdioGateway) CoreListPluginsRPC(ctx context.Context) ([]PluginInfo, error) {
	var out []coreapi.PluginInfo
	if err := g.client.Call(ctx, protocoljsonrpc.MethodExtensionsPluginsList, nil, &out); err != nil {
		return nil, err
	}
	return pluginInfosFromCoreAPI(out), nil
}

func (g *StdioGateway) ListPlugins() []PluginInfo {
	if items, err := g.CoreListPluginsRPC(rpcCtx()); err == nil {
		return items
	}
	return nil
}

func (g *StdioGateway) CoreSetPluginEnabledRPC(ctx context.Context, name string, enabled bool) error {
	return g.client.Call(ctx, protocoljsonrpc.MethodExtensionsPluginSetEnabled, coreapi.SetExtensionEnabledRequest{
		Name:    strings.TrimSpace(name),
		Enabled: enabled,
	}, nil)
}

func (g *StdioGateway) SetPluginEnabled(name string, enabled bool) error {
	return g.CoreSetPluginEnabledRPC(rpcCtx(), name, enabled)
}

func (g *StdioGateway) CoreListWorktreesRPC(ctx context.Context) ([]Worktree, error) {
	var out []coreapi.Worktree
	if err := g.client.Call(ctx, protocoljsonrpc.MethodWorkspaceWorktreeList, nil, &out); err != nil {
		return nil, err
	}
	return worktreesFromCoreAPI(out), nil
}

func (g *StdioGateway) ListWorktrees() []Worktree {
	if items, err := g.CoreListWorktreesRPC(rpcCtx()); err == nil {
		return items
	}
	return nil
}

func (g *StdioGateway) CoreUsageSummaryRPC(ctx context.Context) (UsageSummary, error) {
	var out coreapi.UsageSummary
	if err := g.client.Call(ctx, protocoljsonrpc.MethodUsageSummary, nil, &out); err != nil {
		return UsageSummary{}, err
	}
	return usageSummaryFromCoreAPI(out), nil
}

func (g *StdioGateway) UsageSummary() UsageSummary {
	if summary, err := g.CoreUsageSummaryRPC(rpcCtx()); err == nil {
		return summary
	}
	return UsageSummary{}
}

func (g *StdioGateway) CoreCostItemsRPC(ctx context.Context) ([]CostItem, error) {
	var out []coreapi.CostItem
	if err := g.client.Call(ctx, protocoljsonrpc.MethodUsageCostItems, nil, &out); err != nil {
		return nil, err
	}
	return costItemsFromCoreAPI(out), nil
}

func (g *StdioGateway) CostItems() []CostItem {
	if items, err := g.CoreCostItemsRPC(rpcCtx()); err == nil {
		return items
	}
	return nil
}

func (g *StdioGateway) CoreListVersionsRPC(ctx context.Context) ([]VersionItem, error) {
	var out []coreapi.VersionItem
	if err := g.client.Call(ctx, protocoljsonrpc.MethodVersionsList, nil, &out); err != nil {
		return nil, err
	}
	return versionItemsFromCoreAPI(out), nil
}

func (g *StdioGateway) ListVersions() []VersionItem {
	if items, err := g.CoreListVersionsRPC(rpcCtx()); err == nil {
		return items
	}
	return nil
}

func (g *StdioGateway) CoreRollbackVersionRPC(ctx context.Context, id string) error {
	return g.client.Call(ctx, protocoljsonrpc.MethodVersionsRollback, coreapi.VersionIDRequest{ID: strings.TrimSpace(id)}, nil)
}

func (g *StdioGateway) RollbackVersion(id string) error {
	return g.CoreRollbackVersionRPC(rpcCtx(), id)
}

func (g *StdioGateway) CoreDeleteVersionRPC(ctx context.Context, id string) error {
	return g.client.Call(ctx, protocoljsonrpc.MethodVersionsDelete, coreapi.VersionIDRequest{ID: strings.TrimSpace(id)}, nil)
}

func (g *StdioGateway) DeleteVersion(id string) error {
	return g.CoreDeleteVersionRPC(rpcCtx(), id)
}

func (g *StdioGateway) CoreClearVersionsRPC(ctx context.Context) (int, error) {
	var out struct {
		Count int `json:"count"`
	}
	if err := g.client.Call(ctx, protocoljsonrpc.MethodVersionsClear, nil, &out); err != nil {
		return 0, err
	}
	return out.Count, nil
}

func (g *StdioGateway) ClearVersions() int {
	if count, err := g.CoreClearVersionsRPC(rpcCtx()); err == nil {
		return count
	}
	return 0
}

func (g *StdioGateway) CoreGetSettingsRPC(ctx context.Context) (GUISettings, error) {
	var out coreapi.Settings
	if err := g.client.Call(ctx, protocoljsonrpc.MethodConfigSettingsGet, nil, &out); err != nil {
		return GUISettings{}, err
	}
	return guiSettingsFromCoreAPI(out), nil
}

// CoreGetFullSettingsRPC 返回内核完整 Settings（读改写场景用；
// CoreGetSettingsRPC 返回的是窄化后的 GUISettings 投影）。
func (g *StdioGateway) CoreGetFullSettingsRPC(ctx context.Context) (coreapi.Settings, error) {
	var out coreapi.Settings
	if err := g.client.Call(ctx, protocoljsonrpc.MethodConfigSettingsGet, nil, &out); err != nil {
		return coreapi.Settings{}, err
	}
	return out, nil
}

func (g *StdioGateway) CoreSaveSettingsRPC(ctx context.Context, settings coreapi.Settings) error {
	var out map[string]any
	return g.client.Call(ctx, protocoljsonrpc.MethodConfigSettingsSave, settings, &out)
}

func (g *StdioGateway) GetSettings() GUISettings {
	if settings, err := g.CoreGetSettingsRPC(rpcCtx()); err == nil {
		return settings
	}
	return GUISettings{}
}

func (g *StdioGateway) CoreApprovalListRPC(ctx context.Context, req coreapi.PendingApprovalListRequest) (coreapi.PendingApprovalList, error) {
	var out coreapi.PendingApprovalList
	if err := g.client.Call(ctx, protocoljsonrpc.MethodApprovalList, coreapi.PendingApprovalListRequest{
		SessionID: strings.TrimSpace(req.SessionID),
		TurnID:    strings.TrimSpace(req.TurnID),
		AgentID:   strings.TrimSpace(req.AgentID),
	}, &out); err != nil {
		return coreapi.PendingApprovalList{}, err
	}
	return out, nil
}

func (g *StdioGateway) CoreRespondApprovalRPC(ctx context.Context, approvalID string, decision coreapi.ApprovalDecision) error {
	params := coreapi.ApprovalResponse{
		ApprovalID: strings.TrimSpace(approvalID),
		Decision:   decision,
	}
	var out struct {
		OK bool `json:"ok"`
	}
	if err := g.client.Call(ctx, protocoljsonrpc.MethodApprovalRespond, params, &out); err != nil {
		return err
	}
	return nil
}

// CoreRespondApprovalWithReasonRPC sends an approval/respond with a reason
// string. Used by request_user_input resolution: decision=accept,
// reason=JSON(RequestUserInputResponse) so the core can feed the answers back
// to the model as a tool output.
func (g *StdioGateway) CoreRespondApprovalWithReasonRPC(ctx context.Context, approvalID string, decision coreapi.ApprovalDecision, reason string) error {
	params := coreapi.ApprovalResponse{
		ApprovalID: strings.TrimSpace(approvalID),
		Decision:   decision,
		Reason:     reason,
	}
	var out struct {
		OK bool `json:"ok"`
	}
	if err := g.client.Call(ctx, protocoljsonrpc.MethodApprovalRespond, params, &out); err != nil {
		return err
	}
	return nil
}

// CoreWorkspaceRollbackApplyRPC applies turn rollbacks through the core's
// workspace/rollback/apply RPC. The Go shell no longer applies rollbacks
// directly; it forwards the rollback descriptor to the Rust core.
func (g *StdioGateway) CoreWorkspaceRollbackApplyRPC(ctx context.Context, workspace string, rollbacks []coreapi.TurnRollback) error {
	params := coreapi.ApplyRollbackRequest{
		WorkspaceRoot: strings.TrimSpace(workspace),
		Rollbacks:     rollbacks,
	}
	var out struct {
		OK bool `json:"ok"`
	}
	if err := g.client.Call(ctx, protocoljsonrpc.MethodWorkspaceRollbackApply, params, &out); err != nil {
		return err
	}
	return nil
}

func (g *StdioGateway) ResolveConfirmation(approvalID string, decision coreapi.ApprovalDecision) {
	_ = g.CoreRespondApprovalRPC(rpcCtx(), approvalID, decision)
}

func (g *StdioGateway) CorePermissionSnapshotRPC(ctx context.Context) (PermissionSnapshot, error) {
	var out coreapi.PermissionSnapshot
	if err := g.client.Call(ctx, protocoljsonrpc.MethodPermissionSnapshot, nil, &out); err != nil {
		return PermissionSnapshot{}, err
	}
	return permissionSnapshotFromCoreAPI(out), nil
}

func (g *StdioGateway) PermissionSnapshot() PermissionSnapshot {
	if snapshot, err := g.CorePermissionSnapshotRPC(rpcCtx()); err == nil {
		return snapshot
	}
	return PermissionSnapshot{}
}

func (g *StdioGateway) ThreadCoreIfExists(string) Core {
	return nil
}

// CoreGitBranchesRPC 查询工作区所在 git 仓库的分支列表（含当前分支）。
// workspaceRoot 为空时内核回填前台工作区。
func (g *StdioGateway) CoreGitBranchesRPC(ctx context.Context, workspaceRoot string) (coreapi.GitBranchesResult, error) {
	var out coreapi.GitBranchesResult
	req := coreapi.GitBranchesRequest{WorkspaceRoot: strings.TrimSpace(workspaceRoot)}
	if err := g.client.Call(ctx, protocoljsonrpc.MethodGitBranches, req, &out); err != nil {
		return coreapi.GitBranchesResult{}, err
	}
	return out, nil
}

// CoreGitSummaryRPC 查询工作区所在 git 仓库的概览（分支/上游/ahead-behind/
// 未提交明细）。workspaceRoot 为空时内核回填前台工作区。
func (g *StdioGateway) CoreGitSummaryRPC(ctx context.Context, workspaceRoot string) (coreapi.GitSummaryResult, error) {
	var out coreapi.GitSummaryResult
	req := coreapi.GitSummaryRequest{WorkspaceRoot: strings.TrimSpace(workspaceRoot)}
	if err := g.client.Call(ctx, protocoljsonrpc.MethodGitSummary, req, &out); err != nil {
		return coreapi.GitSummaryResult{}, err
	}
	return out, nil
}

// CoreGitReposRPC 枚举工作区相关仓库（主仓库 + 一级子仓库）及各自概览。
func (g *StdioGateway) CoreGitReposRPC(ctx context.Context, workspaceRoot string) (coreapi.GitReposResult, error) {
	var out coreapi.GitReposResult
	req := coreapi.GitReposRequest{WorkspaceRoot: strings.TrimSpace(workspaceRoot)}
	if err := g.client.Call(ctx, protocoljsonrpc.MethodGitRepos, req, &out); err != nil {
		return coreapi.GitReposResult{}, err
	}
	return out, nil
}

// CoreGitStageRPC 暂存/取消暂存（all=true 处理全部，否则按 paths）。
func (g *StdioGateway) CoreGitStageRPC(ctx context.Context, workspaceRoot string, paths []string, all, unstage bool) error {
	req := coreapi.GitStageRequest{
		WorkspaceRoot: strings.TrimSpace(workspaceRoot),
		Paths:         paths,
		All:           all,
		Unstage:       unstage,
	}
	return g.client.Call(ctx, protocoljsonrpc.MethodGitStage, req, nil)
}

// CoreGitCommitRPC 以给定 message 提交暂存区（message 由 AI 生成 + 用户可编辑）。
func (g *StdioGateway) CoreGitCommitRPC(ctx context.Context, workspaceRoot, message string) (coreapi.GitCommitResult, error) {
	var out coreapi.GitCommitResult
	req := coreapi.GitCommitRequest{
		WorkspaceRoot: strings.TrimSpace(workspaceRoot),
		Message:       message,
	}
	if err := g.client.Call(ctx, protocoljsonrpc.MethodGitCommit, req, &out); err != nil {
		return coreapi.GitCommitResult{}, err
	}
	return out, nil
}

// CoreGitPushRPC 推送（fetch → 必要时 merge 上游 → push 的内核状态机）。
func (g *StdioGateway) CoreGitPushRPC(ctx context.Context, workspaceRoot string) (coreapi.GitPushResult, error) {
	var out coreapi.GitPushResult
	req := coreapi.GitPushRequest{WorkspaceRoot: strings.TrimSpace(workspaceRoot)}
	if err := g.client.Call(ctx, protocoljsonrpc.MethodGitPush, req, &out); err != nil {
		return coreapi.GitPushResult{}, err
	}
	return out, nil
}

// CoreGitMergeAbortRPC 放弃进行中的 merge，回到合并前状态。
func (g *StdioGateway) CoreGitMergeAbortRPC(ctx context.Context, workspaceRoot string) error {
	req := coreapi.GitAbortMergeRequest{WorkspaceRoot: strings.TrimSpace(workspaceRoot)}
	return g.client.Call(ctx, protocoljsonrpc.MethodGitMergeAbort, req, nil)
}

// CoreGitSuggestMessageRPC 让内核用全局默认模型为当前变更生成
// Conventional Commits 提交信息（一次性补全，不起会话）。
func (g *StdioGateway) CoreGitSuggestMessageRPC(ctx context.Context, workspaceRoot string) (coreapi.GitSuggestMessageResult, error) {
	var out coreapi.GitSuggestMessageResult
	req := coreapi.GitSuggestMessageRequest{WorkspaceRoot: strings.TrimSpace(workspaceRoot)}
	if err := g.client.Call(ctx, protocoljsonrpc.MethodGitSuggestMessage, req, &out); err != nil {
		return coreapi.GitSuggestMessageResult{}, err
	}
	return out, nil
}

func (g *StdioGateway) CorePlanSnapshotRPC(ctx context.Context) (PlanSnapshot, error) {
	var out coreapi.PlanSnapshot
	if err := g.client.Call(ctx, protocoljsonrpc.MethodInsightPlanSnapshot, nil, &out); err != nil {
		return PlanSnapshot{}, err
	}
	return planSnapshotFromCoreAPI(out), nil
}

func (g *StdioGateway) PlanSnapshot() PlanSnapshot {
	if snapshot, err := g.CorePlanSnapshotRPC(rpcCtx()); err == nil {
		return snapshot
	}
	return PlanSnapshot{}
}

func (g *StdioGateway) CoreMemorySnapshotRPC(ctx context.Context) (MemorySnapshot, error) {
	var out coreapi.MemorySnapshot
	if err := g.client.Call(ctx, protocoljsonrpc.MethodMemorySnapshot, nil, &out); err != nil {
		return MemorySnapshot{}, err
	}
	return memorySnapshotFromCoreAPI(out), nil
}

func (g *StdioGateway) MemorySnapshot() MemorySnapshot {
	if snapshot, err := g.CoreMemorySnapshotRPC(rpcCtx()); err == nil {
		return snapshot
	}
	return MemorySnapshot{}
}

// CoreMemorySaveRPC 经内核 memory/save 落一条 ad_hoc 记忆笔记（append-only，
// 空内容被内核拒绝）。
func (g *StdioGateway) CoreMemorySaveRPC(ctx context.Context, content string) error {
	return g.client.Call(ctx, protocoljsonrpc.MethodMemorySave, coreapi.SaveMemoryRequest{Content: content}, nil)
}

func (g *StdioGateway) CorePendingReviewRPC(ctx context.Context) (PendingReview, error) {
	var out coreapi.PendingReview
	if err := g.client.Call(ctx, protocoljsonrpc.MethodPermissionPendingReview, nil, &out); err != nil {
		return PendingReview{}, err
	}
	return PendingReview{Path: strings.TrimSpace(out.Path), Diff: strings.TrimSpace(out.Diff), HasDiff: out.HasDiff}, nil
}

func (g *StdioGateway) PendingReview() PendingReview {
	if review, err := g.CorePendingReviewRPC(rpcCtx()); err == nil {
		return review
	}
	return PendingReview{}
}

func (g *StdioGateway) CoreLSPDiagnosticsRPC(ctx context.Context) ([]string, error) {
	var out []string
	if err := g.client.Call(ctx, protocoljsonrpc.MethodLSPDiagnostics, nil, &out); err != nil {
		return nil, err
	}
	return append([]string(nil), out...), nil
}

func (g *StdioGateway) LSPDiagnostics() []string {
	if items, err := g.CoreLSPDiagnosticsRPC(rpcCtx()); err == nil {
		return items
	}
	return nil
}

func (g *StdioGateway) CoreContextPreviewRPC(ctx context.Context) ([]string, error) {
	var out []string
	if err := g.client.Call(ctx, protocoljsonrpc.MethodContextPreview, nil, &out); err != nil {
		return nil, err
	}
	return append([]string(nil), out...), nil
}

func (g *StdioGateway) ContextPreview() []string {
	if items, err := g.CoreContextPreviewRPC(rpcCtx()); err == nil {
		return items
	}
	return nil
}

func (g *StdioGateway) CoreContextStatsRPC(ctx context.Context) (ContextStats, error) {
	var out coreapi.ContextStats
	if err := g.client.Call(ctx, protocoljsonrpc.MethodContextStats, nil, &out); err != nil {
		return ContextStats{}, err
	}
	return contextStatsFromCoreAPI(out), nil
}

func (g *StdioGateway) ContextStats() ContextStats {
	if stats, err := g.CoreContextStatsRPC(rpcCtx()); err == nil {
		return stats
	}
	return ContextStats{}
}

func (g *StdioGateway) CoreCostSummaryRPC(ctx context.Context) (string, error) {
	// 内核 usage/cost_summary 返回 {"text": "..."}。此前读 "summary" 恒为空。
	var out struct {
		Text string `json:"text"`
	}
	if err := g.client.Call(ctx, protocoljsonrpc.MethodUsageCostSummary, nil, &out); err != nil {
		return "", err
	}
	return strings.TrimSpace(out.Text), nil
}

func (g *StdioGateway) CoreCallRPC(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	var out json.RawMessage
	var param any
	if len(params) > 0 {
		param = params
	}
	if err := g.client.Call(ctx, method, param, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (g *StdioGateway) CoreToolExecuteRPC(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
	var out json.RawMessage
	if err := g.client.Call(ctx, protocoljsonrpc.MethodToolExecute, params, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (g *StdioGateway) CoreNetworkListRPC(ctx context.Context, limit int) (coreapi.NetworkListResult, error) {
	var out coreapi.NetworkListResult
	params := map[string]any{}
	if limit > 0 {
		params["limit"] = limit
	}
	var param any
	if len(params) > 0 {
		param = params
	}
	if err := g.client.Call(ctx, protocoljsonrpc.MethodNetworkList, param, &out); err != nil {
		return coreapi.NetworkListResult{}, err
	}
	return out, nil
}

func (g *StdioGateway) CoreNetworkClearRPC(ctx context.Context) (int, error) {
	var out struct {
		Removed int `json:"removed"`
	}
	if err := g.client.Call(ctx, protocoljsonrpc.MethodNetworkClear, nil, &out); err != nil {
		return 0, err
	}
	return out.Removed, nil
}

func (g *StdioGateway) CostSummary() string {
	if summary, err := g.CoreCostSummaryRPC(rpcCtx()); err == nil {
		return summary
	}
	return ""
}

func (g *StdioGateway) CoreKillTaskRPC(ctx context.Context, taskID string) error {
	return g.client.Call(ctx, protocoljsonrpc.MethodTaskKill, coreapi.TaskIDRequest{TaskID: strings.TrimSpace(taskID)}, nil)
}

func (g *StdioGateway) CoreTaskListRPC(ctx context.Context) ([]coreapi.TaskSnapshot, error) {
	var out []coreapi.TaskSnapshot
	if err := g.client.Call(ctx, protocoljsonrpc.MethodTaskList, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (g *StdioGateway) KillTask(taskID string) error {
	return g.CoreKillTaskRPC(rpcCtx(), taskID)
}

func (g *StdioGateway) CoreCleanupTasksRPC(ctx context.Context) (int, error) {
	var out struct {
		Count int `json:"count"`
	}
	if err := g.client.Call(ctx, protocoljsonrpc.MethodTaskCleanup, nil, &out); err != nil {
		return 0, err
	}
	return out.Count, nil
}

func (g *StdioGateway) CleanupTasks() int {
	if count, err := g.CoreCleanupTasksRPC(rpcCtx()); err == nil {
		return count
	}
	return 0
}

func (g *StdioGateway) CoreSubscribeEventsRPC(ctx context.Context, sessionID, turnID, agentID string, buffer int) (<-chan Event, func(), error) {
	if g == nil || g.client == nil || g.events == nil {
		return nil, nil, errors.New("stdio gateway is not started")
	}
	if ctx == nil {
		ctx = rpcCtx()
	}
	filter := runtimeJSONRPCEventFilter{
		SessionID: strings.TrimSpace(sessionID),
		TurnID:    strings.TrimSpace(turnID),
		AgentID:   strings.TrimSpace(agentID),
	}
	filter.EventTypes = runtimeRPCSubscriptionEventTypes(filter)
	events, unsubscribeLocal := g.events.Subscribe(ctx, filter, buffer)

	var subscription struct {
		ID             string `json:"id"`
		SubscriptionID string `json:"subscription_id"`
	}
	request := coreapi.EventSubscribeRequest{
		EventTypes: filter.EventTypes,
		SessionID:  filter.SessionID,
		TurnID:     filter.TurnID,
		AgentID:    filter.AgentID,
	}
	if err := g.client.Call(ctx, protocoljsonrpc.MethodEventSubscribe, request, &subscription); err != nil {
		unsubscribeLocal()
		return nil, nil, err
	}
	subscriptionID := strings.TrimSpace(subscription.SubscriptionID)
	if subscriptionID == "" {
		subscriptionID = strings.TrimSpace(subscription.ID)
	}
	var once sync.Once
	unsubscribe := func() {
		once.Do(func() {
			unsubscribeLocal()
			if subscriptionID != "" {
				_ = g.client.Call(rpcCtx(), protocoljsonrpc.MethodEventUnsubscribe, coreapi.EventUnsubscribeRequest{ID: subscriptionID}, nil)
			}
		})
	}
	return events, unsubscribe, nil
}

// CoreStartTurnStreamWithRequestRPC 是唯一的 turn/start 入口：调用方构造
// 完整 StartTurnRequest（含 use_memory / collaboration_mode / 附件）。
func (g *StdioGateway) CoreStartTurnStreamWithRequestRPC(ctx context.Context, req coreapi.StartTurnRequest) (<-chan Event, coreapi.Turn, error) {
	return g.coreStartTurnStreamRPC(ctx, req)
}

func (g *StdioGateway) coreStartTurnStreamRPC(ctx context.Context, req coreapi.StartTurnRequest) (<-chan Event, coreapi.Turn, error) {
	if g == nil || g.client == nil {
		return nil, coreapi.Turn{}, errors.New("stdio gateway is not started")
	}
	if ctx == nil {
		ctx = rpcCtx()
	}
	req.SessionID = strings.TrimSpace(req.SessionID)
	req.TurnID = strings.TrimSpace(req.TurnID)
	if req.TurnID == "" {
		req.TurnID = stdioNewTurnID()
	}
	req.ImagePaths = stdioCompactStringSlice(req.ImagePaths)
	req.Attachments = stdioCompactCoreAPIAttachments(req.Attachments)
	req.Options = append(json.RawMessage(nil), req.Options...)
	return g.coreTurnStreamRPC(ctx, req.SessionID, req.TurnID, func(callCtx context.Context) (coreapi.Turn, error) {
		var turn coreapi.Turn
		err := g.client.Call(callCtx, protocoljsonrpc.MethodTurnStart, req, &turn)
		return turn, err
	})
}

// CoreResumeTurnStreamRPC 是 turn/resume 的流式入口：bridge 预生成 turnID 供
// 事件订阅过滤；内核不追加用户消息，按已提交历史续跑失败 turn（codex resume
// 语义）。
func (g *StdioGateway) CoreResumeTurnStreamRPC(ctx context.Context, sessionID, turnID string) (<-chan Event, coreapi.Turn, error) {
	if g == nil || g.client == nil {
		return nil, coreapi.Turn{}, errors.New("stdio gateway is not started")
	}
	if ctx == nil {
		ctx = rpcCtx()
	}
	sessionID = strings.TrimSpace(sessionID)
	turnID = strings.TrimSpace(turnID)
	if turnID == "" {
		turnID = stdioNewTurnID()
	}
	ref := coreapi.TurnRef{SessionID: sessionID, TurnID: turnID}
	return g.coreTurnStreamRPC(ctx, sessionID, turnID, func(callCtx context.Context) (coreapi.Turn, error) {
		var turn coreapi.Turn
		err := g.client.Call(callCtx, protocoljsonrpc.MethodTurnResume, ref, &turn)
		return turn, err
	})
}

// coreTurnStreamRPC 是 turn/start 与 turn/resume 共用的事件流包装：先按
// (session_id, turn_id) 订阅内核事件，再执行 start 发起 RPC，泵送事件直到
// 终态。start 返回错误或非 running 终态时合成兜底事件，保证壳层总能收到收尾
// 事件（原 coreStartTurnStreamRPC 语义，逐行保留）。
func (g *StdioGateway) coreTurnStreamRPC(ctx context.Context, sessionID, turnID string, start func(context.Context) (coreapi.Turn, error)) (<-chan Event, coreapi.Turn, error) {
	streamCtx, cancel := context.WithCancel(ctx)
	events, unsubscribe, err := g.CoreSubscribeEventsRPC(streamCtx, sessionID, turnID, "", 64)
	if err != nil {
		cancel()
		return nil, coreapi.Turn{}, err
	}

	type startResult struct {
		turn coreapi.Turn
		err  error
	}
	startDone := make(chan startResult, 1)
	go func() {
		turn, err := start(ctx)
		startDone <- startResult{turn: turn, err: err}
	}()

	now := time.Now()
	turn := coreapi.Turn{
		ID:        turnID,
		SessionID: sessionID,
		Status:    "running",
		StartedAt: now,
		UpdatedAt: now,
	}
	out := make(chan Event, 64)
	go func() {
		defer close(out)
		defer unsubscribe()
		defer cancel()
		startDoneCh := startDone
		var fallbackTimer <-chan time.Time
		var fallbackEvent Event
		for {
			select {
			case <-streamCtx.Done():
				return
			case result := <-startDoneCh:
				startDoneCh = nil
				status := strings.TrimSpace(result.turn.Status)
				if result.err != nil {
					event := newRequestFailedEvent(turnID, result.err.Error())
					event.SessionID = sessionID
					event.TurnID = turnID
					select {
					case out <- event:
					case <-streamCtx.Done():
					}
					return
				}
				if status != "" && !strings.EqualFold(status, "running") {
					fallbackEvent = Event{
						Type:      legacyTypeFromProtocol(protocol.EventTypeRequestDone),
						EventType: string(protocol.EventTypeRequestDone),
						RequestID: turnID,
						SessionID: sessionID,
						TurnID:    turnID,
						Message:   "request completed",
						Payload:   map[string]any{"status": status},
						Data:      map[string]any{"status": status},
					}
					if strings.EqualFold(status, "error") || strings.EqualFold(status, "interrupted") || strings.EqualFold(status, "cancelled") {
						fallbackEvent = newRequestFailedEvent(turnID, status)
						fallbackEvent.SessionID = sessionID
						fallbackEvent.TurnID = turnID
					}
					fallbackTimer = time.After(250 * time.Millisecond)
				}
			case <-fallbackTimer:
				select {
				case out <- fallbackEvent:
				case <-streamCtx.Done():
				}
				return
			case event, ok := <-events:
				if !ok {
					return
				}
				select {
				case out <- event:
				case <-streamCtx.Done():
					return
				}
				if stdioIsTerminalCoreTurnEvent(event) {
					return
				}
			}
		}
	}()
	return out, turn, nil
}

func (g *StdioGateway) CoreInterruptTurnRPC(ctx context.Context, sessionID, turnID string) error {
	return g.client.Call(ctx, protocoljsonrpc.MethodTurnInterrupt, coreapi.TurnRef{
		SessionID: strings.TrimSpace(sessionID),
		TurnID:    strings.TrimSpace(turnID),
	}, nil)
}

func (g *StdioGateway) CoreSandboxPolicyRPC(ctx context.Context, sessionID string) (sandbox.Policy, error) {
	var out sandbox.Policy
	if err := g.client.Call(ctx, protocoljsonrpc.MethodSandboxPolicy, coreapi.SandboxPolicyRequest{
		SessionID: strings.TrimSpace(sessionID),
	}, &out); err != nil {
		return sandbox.Policy{}, err
	}
	return out, nil
}

func (g *StdioGateway) CoreSetSandboxPolicyRPC(ctx context.Context, sessionID string, policy sandbox.Policy) error {
	return g.client.Call(ctx, protocoljsonrpc.MethodSandboxSetPolicy, coreapi.SetSandboxPolicyRequest{
		SessionID: strings.TrimSpace(sessionID),
		Policy:    policy,
	}, nil)
}

// CoreDeriveSandboxPolicyRPC 调用 sandbox/derive_policy：内核按 mode + workspace_root
// 派生完整 Policy（含 allow_network 等 mode-scoped 默认值）。壳层不再手组装 Policy。
func (g *StdioGateway) CoreDeriveSandboxPolicyRPC(ctx context.Context, mode, workspaceRoot string) (sandbox.Policy, error) {
	var out sandbox.Policy
	if err := g.client.Call(ctx, protocoljsonrpc.MethodSandboxDerivePolicy, coreapi.DeriveSandboxPolicyRequest{
		Mode:          strings.TrimSpace(mode),
		WorkspaceRoot: strings.TrimSpace(workspaceRoot),
	}, &out); err != nil {
		return sandbox.Policy{}, err
	}
	return out, nil
}

// CoreEnterFullAccessRPC 调用 permission/enter_full_access：内核原子地把双轴
// （approval=Never + sandbox=DangerFullAccess）一起推进。仅 --dangerously-skip-permissions 触发。
func (g *StdioGateway) CoreEnterFullAccessRPC(ctx context.Context, workspaceRoot string) (sandbox.Policy, error) {
	var out sandbox.Policy
	if err := g.client.Call(ctx, protocoljsonrpc.MethodPermissionEnterFullAccess, coreapi.EnterFullAccessRequest{
		WorkspaceRoot: strings.TrimSpace(workspaceRoot),
	}, &out); err != nil {
		return sandbox.Policy{}, err
	}
	return out, nil
}

// CoreApprovalPreviewRPC 调用 approval/preview：内核对给定工具调用做风险分类，
// 返回 level/decision/tags/candidates/reason。壳层据此渲染审批卡片，不再编造文案。
func (g *StdioGateway) CoreApprovalPreviewRPC(ctx context.Context, req coreapi.ApprovalPreviewRequest) (coreapi.ApprovalPreviewResponse, error) {
	var out coreapi.ApprovalPreviewResponse
	if err := g.client.Call(ctx, protocoljsonrpc.MethodApprovalPreview, req, &out); err != nil {
		return coreapi.ApprovalPreviewResponse{}, err
	}
	return out, nil
}

func (g *StdioGateway) CoreSandboxBackendStatusRPC(ctx context.Context) (sandbox.BackendStatus, error) {
	var out sandbox.BackendStatus
	if err := g.client.Call(ctx, protocoljsonrpc.MethodSandboxBackend, nil, &out); err != nil {
		return sandbox.BackendStatus{}, err
	}
	return out, nil
}

func (g *StdioGateway) StartupDiagnostics() StartupDiagnosticsResult {
	var out StartupDiagnosticsResult
	_ = g.client.Call(rpcCtx(), protocoljsonrpc.MethodDiagnosticsStartup, nil, &out)
	return out
}

// CoreAPIAttachments 把壳层附件转换为 coreapi DTO（供桥接层组装
// StartTurnRequest）。
func CoreAPIAttachments(items []Attachment) []coreapi.Attachment {
	out := make([]coreapi.Attachment, 0, len(items))
	for _, item := range items {
		path := strings.TrimSpace(item.Path)
		if path == "" {
			continue
		}
		out = append(out, coreapi.Attachment{
			Name: strings.TrimSpace(item.Name),
			Path: path,
			MIME: strings.TrimSpace(item.MIME),
			Kind: strings.TrimSpace(item.Kind),
		})
	}
	return out
}

func stdioCompactCoreAPIAttachments(items []coreapi.Attachment) []coreapi.Attachment {
	out := make([]coreapi.Attachment, 0, len(items))
	for _, item := range items {
		path := strings.TrimSpace(item.Path)
		if path == "" {
			continue
		}
		out = append(out, coreapi.Attachment{
			Name: strings.TrimSpace(item.Name),
			Path: path,
			MIME: strings.TrimSpace(item.MIME),
			Kind: strings.TrimSpace(item.Kind),
		})
	}
	return out
}

func stdioCompactStringSlice(items []string) []string {
	out := make([]string, 0, len(items))
	seen := map[string]struct{}{}
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func stdioNewTurnID() string {
	return "turn_" + strconv.FormatInt(time.Now().UTC().UnixNano(), 10)
}

func stdioIsTerminalCoreTurnEvent(event Event) bool {
	switch event.Kind() {
	case "request.completed", "request.failed":
		return true
	default:
		return false
	}
}

var errStdioNotImplemented = &stdioNotImplementedError{}

type stdioNotImplementedError struct{}

func (e *stdioNotImplementedError) Error() string {
	return "stdio gateway: method not implemented in external transport prototype"
}

// ── 目标模式（goal mode）RPC：goal/set|get|pause|resume|clear ──────────────

func (g *StdioGateway) CoreGoalSetRPC(ctx context.Context, req coreapi.GoalSetRequest) (coreapi.ThreadGoal, error) {
	var out coreapi.ThreadGoal
	if err := g.client.Call(ctx, protocoljsonrpc.MethodGoalSet, req, &out); err != nil {
		return coreapi.ThreadGoal{}, err
	}
	return out, nil
}

func (g *StdioGateway) CoreGoalGetRPC(ctx context.Context, sessionID string) (coreapi.GoalGetResponse, error) {
	var out coreapi.GoalGetResponse
	if err := g.client.Call(ctx, protocoljsonrpc.MethodGoalGet, coreapi.GoalRefRequest{SessionID: sessionID}, &out); err != nil {
		return coreapi.GoalGetResponse{}, err
	}
	return out, nil
}

func (g *StdioGateway) CoreGoalPauseRPC(ctx context.Context, sessionID string) (coreapi.ThreadGoal, error) {
	var out coreapi.ThreadGoal
	if err := g.client.Call(ctx, protocoljsonrpc.MethodGoalPause, coreapi.GoalRefRequest{SessionID: sessionID}, &out); err != nil {
		return coreapi.ThreadGoal{}, err
	}
	return out, nil
}

func (g *StdioGateway) CoreGoalResumeRPC(ctx context.Context, sessionID string) (coreapi.ThreadGoal, error) {
	var out coreapi.ThreadGoal
	if err := g.client.Call(ctx, protocoljsonrpc.MethodGoalResume, coreapi.GoalRefRequest{SessionID: sessionID}, &out); err != nil {
		return coreapi.ThreadGoal{}, err
	}
	return out, nil
}

func (g *StdioGateway) CoreGoalClearRPC(ctx context.Context, sessionID string) error {
	var out map[string]any
	return g.client.Call(ctx, protocoljsonrpc.MethodGoalClear, coreapi.GoalRefRequest{SessionID: sessionID}, &out)
}
