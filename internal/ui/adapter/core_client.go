// CoreClientAdapter 是 TUI 与 Rust eos-core sidecar 之间的 RPC adapter。
//
// 设计目标：
//   - 取代 RuntimeAdapter 的所有 escape hatch（GetCore/GetContext/GetSettings/GetWorkspace）。
//   - 唯一通过 pkg/coreapi/sidecar/client → sidecar.RemoteEngine → JSON-RPC 访问 core。
//   - 不允许 import internal/bridge、internal/runtime、internal/tools、pkg/core。
//   - 所有 panels/slash actions 通过本 adapter 的显式 RPC 方法访问 core。
//
// 与 RuntimeAdapter 的关系：
//   - RuntimeAdapter 保留用于测试（wrap InProcessClient over Go runtime）。
//   - CoreClientAdapter 是生产路径，wrap eos-core --app-server --stdio 子进程。
//   - 两者的方法签名收敛在 coreapi.* DTO 上，UI 调用方可无缝切换。
package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/eosaios/eos/internal/ai"
	"github.com/eosaios/eos/internal/config"
	"github.com/eosaios/eos/internal/pkg/settings"
	"github.com/eosaios/eos/pkg/coreapi"
	sidecarclient "github.com/eosaios/eos/pkg/coreapi/sidecar/client"
	"github.com/eosaios/eos/pkg/protocol"
	protocoljsonrpc "github.com/eosaios/eos/pkg/protocol/jsonrpc"
)

// CoreClientAdapter wraps sidecar.Client 和其底层 coreapi.Engine。
// 维护事件订阅、活跃 turn 跟踪等 TUI 关心的状态。
type CoreClientAdapter struct {
	client *sidecarclient.Client
	engine coreapi.Engine

	eventsOnce     sync.Once
	eventsCh       chan RuntimeEvent
	notificationCh chan RuntimeEvent
	subscribersMu  sync.Mutex
	subscribers    map[int]chan RuntimeEvent
	nextSubscriber int

	pumpOnce       sync.Once
	pumpReady      chan struct{}
	pumpClosed     chan struct{}
	pumpCancel     context.CancelFunc
	dispatcherDone chan struct{}
	closeOnce      sync.Once
	closeErr       error

	activeTurnMu    sync.Mutex
	activeTurn      coreapi.TurnRef
	activeTurnAlive bool
}

// NewCoreClientAdapter 用已 handshake 完成的 sidecar Client 构造 adapter。
// 返回的 adapter 不会自动订阅 server-sent 事件；订阅在首次调用 Events() 时
// 同步触发，确保 `event/subscribe` 在调用方拿到 channel 之前已被 dispatch。
func NewCoreClientAdapter(client *sidecarclient.Client) *CoreClientAdapter {
	a := &CoreClientAdapter{
		pumpReady:      make(chan struct{}),
		pumpClosed:     make(chan struct{}),
		dispatcherDone: make(chan struct{}),
	}
	if client == nil {
		a.notificationCh = make(chan RuntimeEvent, 4096)
		a.subscribers = map[int]chan RuntimeEvent{}
		close(a.pumpClosed)
		a.startEventDispatcher()
		return a
	}
	a.client = client
	a.engine = client.Engine()
	a.notificationCh = make(chan RuntimeEvent, 128)
	a.subscribers = map[int]chan RuntimeEvent{}
	a.startEventDispatcher()
	return a
}

// NewCoreClientAdapterFromEngine 直接用 coreapi.Engine 构造 adapter。
// 供测试场景使用（无需启动 sidecar 子进程），也允许在 production 中注入
// 已初始化好的 engine 实例。订阅在首次调用 Events() 时同步触发。
func NewCoreClientAdapterFromEngine(engine coreapi.Engine) *CoreClientAdapter {
	a := &CoreClientAdapter{
		engine:         engine,
		pumpReady:      make(chan struct{}),
		pumpClosed:     make(chan struct{}),
		dispatcherDone: make(chan struct{}),
		notificationCh: make(chan RuntimeEvent, 128),
		subscribers:    map[int]chan RuntimeEvent{},
	}
	a.startEventDispatcher()
	return a
}

// Close 释放 adapter 持有的事件 channel 并关闭 sidecar Client。
func (a *CoreClientAdapter) Close() error {
	if a == nil {
		return nil
	}
	a.closeOnce.Do(func() {
		pumpStopped := a.pumpCancel == nil
		if a.pumpCancel != nil {
			a.pumpCancel()
			if a.pumpClosed != nil {
				select {
				case <-a.pumpClosed:
					pumpStopped = true
				case <-time.After(2 * time.Second):
				}
			}
		}
		if pumpStopped && a.notificationCh != nil {
			close(a.notificationCh)
			if a.dispatcherDone != nil {
				<-a.dispatcherDone
			}
		}
		if a.client != nil {
			a.closeErr = a.client.Close()
		}
	})
	return a.closeErr
}

// Engine 返回底层 coreapi.Engine，供高级调用方直接做 RPC。
func (a *CoreClientAdapter) Engine() coreapi.Engine {
	if a == nil {
		return nil
	}
	return a.engine
}

// Client 返回底层 sidecar Client。
func (a *CoreClientAdapter) Client() *sidecarclient.Client {
	if a == nil {
		return nil
	}
	return a.client
}

// Events 返回事件通道。事件类型与 RuntimeEvent 保持兼容（type, rid, content, data）。
// 首次调用时同步启动事件订阅，确保 `event/subscribe` JSON-RPC 调用在返回前
// 已被 dispatch，避免上层测试/UI 在调用 Events() 后立即断言 method 列表时的竞态。
func (a *CoreClientAdapter) Events() <-chan RuntimeEvent {
	if a == nil {
		return nil
	}
	a.eventsOnce.Do(func() {
		a.eventsCh = make(chan RuntimeEvent, 512)
		// 同步触发 engine.Events().Subscribe，保证 event/subscribe
		// 在 pumpReady 关闭前完成 dispatch。
		a.ensurePumpStarted()
		ch, unsubscribe := a.subscribeEvents(512)
		go func() {
			defer unsubscribe()
			defer close(a.eventsCh)
			for event := range ch {
				a.eventsCh <- event
			}
		}()
	})
	return a.eventsCh
}

// ensurePumpStarted 同步启动事件订阅 pump。多次调用幂等：
//   - engine 为 nil 时直接关闭 pumpReady（防止调用方永久阻塞）
//   - 否则启动后台 pump goroutine 并等待其首次 Subscribe 调用完成
func (a *CoreClientAdapter) ensurePumpStarted() {
	if a == nil {
		return
	}
	a.pumpOnce.Do(func() {
		if a.pumpReady == nil {
			a.pumpReady = make(chan struct{})
		}
		if a.pumpClosed == nil {
			a.pumpClosed = make(chan struct{})
		}
		if a.engine == nil {
			close(a.pumpReady)
			close(a.pumpClosed)
			return
		}
		ctx, cancel := context.WithCancel(context.Background())
		a.pumpCancel = cancel
		go func() {
			defer close(a.pumpClosed)
			a.runNotificationPump(ctx)
		}()
	})
	if a.pumpReady != nil {
		<-a.pumpReady
	}
}

// Invoke 启动一次 turn，订阅事件流直到 RequestDone / RequestFailed，返回 final 文本。
// useMemory 是请求级记忆注入开关，透传给内核的 turn/start.use_memory
// （注入裁决在内核）；source 固定为 cli，仅在日志中标识来源壳层。
func (a *CoreClientAdapter) Invoke(ctx context.Context, query, executionMode string, imagePaths []string, useMemory bool) (string, error) {
	if a == nil || a.engine == nil {
		return "", errors.New("core client is not available")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := a.SetExecutionMode(ctx, executionMode); err != nil {
		return "", err
	}
	sessionID, err := a.ensureSessionID(ctx)
	if err != nil {
		return "", err
	}
	turnID := fmt.Sprintf("tui_turn_%d", time.Now().UnixNano())
	a.ensurePumpStarted()
	events, unsubscribe := a.subscribeEvents(4096)
	defer unsubscribe()
	a.setActiveTurn(coreapi.TurnRef{SessionID: sessionID, TurnID: turnID})
	defer a.clearActiveTurn(turnID)

	startDone := make(chan struct {
		turn coreapi.Turn
		err  error
	}, 1)
	go func() {
		req := coreapi.StartTurnRequest{
			SessionID:  sessionID,
			TurnID:     turnID,
			Input:      query,
			ImagePaths: append([]string(nil), imagePaths...),
			// 请求级记忆注入开关：壳层只透传，注入裁决在内核。
			UseMemory: &useMemory,
		}
		slog.Debug("core.turn.start", "use_memory", useMemory, "source", "cli")
		// Map the global execution_mode ("plan") to the per-turn
		// collaboration_mode, mirroring Codex's turn/start.collaborationMode.
		if strings.TrimSpace(strings.ToLower(executionMode)) == "plan" {
			req.CollaborationMode = &coreapi.CollaborationMode{Mode: coreapi.ModePlan}
		}
		turn, err := a.engine.Turns().Start(ctx, req)
		startDone <- struct {
			turn coreapi.Turn
			err  error
		}{turn: turn, err: err}
	}()

	var final string
	var content string
	startDoneCh := startDone
	// Safety net: if the core never emits a terminal event (request.completed
	// / request.failed) the subscription would block forever. The core always
	// publishes turn.completed (= request.completed) on turn end, so under
	// normal operation this never fires; it only guards against a wedged core.
	safetyTimer := time.After(15 * time.Minute)
	for {
		select {
		case <-ctx.Done():
			_, _ = a.interruptTurn(context.Background(), coreapi.TurnRef{SessionID: sessionID, TurnID: turnID})
			return "", ctx.Err()
		case result := <-startDoneCh:
			startDoneCh = nil
			if result.err != nil {
				return final, result.err
			}
		case <-safetyTimer:
			// Core appears wedged; interrupt and surface what we have rather
			// than blocking the UI's Invoke goroutine indefinitely.
			_, _ = a.interruptTurn(context.Background(), coreapi.TurnRef{SessionID: sessionID, TurnID: turnID})
			if final == "" {
				final = content
			}
			return final, nil
		case event, ok := <-events:
			if !ok {
				return final, nil
			}
			if rid := strings.TrimSpace(event.RID); rid != "" && rid != turnID {
				continue
			}
			switch event.Type {
			case string(protocol.EventTypeItemDelta), "delta":
				// Only accumulate text deltas (not reasoning/tool_args).
				if dt := eventString(event.Data, "delta_type"); dt == "" || dt == "text" {
					content += firstEventText(event.Data, event.Content, "delta", "text", "message")
				}
			case string(protocol.EventTypeItemCompleted):
				// An AgentMessage completion carries the full segment text.
				if text := itemCompletedText(event.Data); text != "" {
					final = text
				}
			case string(protocol.EventTypeTextFinal), "final":
				final = firstEventText(event.Data, event.Content, "text", "message")
			case string(protocol.EventTypeRequestDone):
				if final == "" {
					final = content
				}
				return final, nil
			case string(protocol.EventTypeRequestFailed), "error":
				msg := firstEventText(event.Data, event.Content, "error", "summary", "message", "text")
				if msg == "" {
					msg = "request failed"
				}
				return final, errors.New(msg)
			}
		}
	}
}

// ExecuteBash 走 tool/execute，name=bash。
func (a *CoreClientAdapter) ExecuteBash(ctx context.Context, command string) (string, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return "", errors.New("command required")
	}
	if a == nil || a.engine == nil {
		return "", errors.New("core client is not available")
	}
	sessionID, err := a.ensureSessionID(ctx)
	if err != nil {
		return "", err
	}
	args, _ := json.Marshal(map[string]any{"command": command})
	result, err := a.engine.Tools().Execute(ctx, coreapi.ToolRequest{
		SessionID: sessionID,
		RequestID: fmt.Sprintf("tui_bash_%d", time.Now().UnixNano()),
		Name:      "bash",
		Args:      args,
	})
	if err != nil {
		return "", err
	}
	output := toolResultOutputText(result)
	if output == "" {
		output = strings.TrimRight(result.Display, "\n")
	}
	if strings.TrimSpace(result.Error) != "" || strings.EqualFold(result.Status, "error") {
		if strings.TrimSpace(output) == "" {
			output = strings.TrimSpace(result.Error)
		}
		return output, errors.New(firstNonEmptyString(result.Error, output, "bash failed"))
	}
	return output, nil
}

// CancelForegroundRequest 中断当前活跃 turn。
func (a *CoreClientAdapter) CancelForegroundRequest() bool {
	if a == nil || a.engine == nil {
		return false
	}
	if ref, ok := a.currentActiveTurn(); ok && strings.TrimSpace(ref.TurnID) != "" {
		if err := a.engine.Turns().Interrupt(context.Background(), ref); err == nil {
			a.clearActiveTurn(ref.TurnID)
			return true
		}
	}
	return false
}

// StateSnapshot 读取 state/snapshot。
func (a *CoreClientAdapter) StateSnapshot(ctx context.Context) (coreapi.StateSnapshot, error) {
	if a == nil || a.engine == nil {
		return coreapi.StateSnapshot{}, errors.New("core client is not available")
	}
	return a.engine.State().Snapshot(ctx, coreapi.StateSnapshotRequest{})
}

// === Workspace ===

func (a *CoreClientAdapter) Workspaces(ctx context.Context) ([]coreapi.Workspace, error) {
	if a == nil || a.engine == nil {
		return nil, errors.New("core client is not available")
	}
	return a.engine.Workspaces().List(ctx, coreapi.WorkspaceListRequest{})
}

func (a *CoreClientAdapter) ActiveWorkspace(ctx context.Context) string {
	if a == nil || a.engine == nil {
		return ""
	}
	if snapshot, err := a.StateSnapshot(ctx); err == nil && strings.TrimSpace(snapshot.ForegroundWorkspace) != "" {
		return strings.TrimSpace(snapshot.ForegroundWorkspace)
	}
	return ""
}

func (a *CoreClientAdapter) AddWorkspace(ctx context.Context, path string) error {
	if a == nil || a.engine == nil {
		return errors.New("core client is not available")
	}
	return a.engine.Workspaces().Add(ctx, coreapi.WorkspacePathRequest{Path: strings.TrimSpace(path)})
}

func (a *CoreClientAdapter) RemoveWorkspace(ctx context.Context, path string) error {
	if a == nil || a.engine == nil {
		return errors.New("core client is not available")
	}
	return a.engine.Workspaces().Remove(ctx, coreapi.WorkspacePathRequest{Path: strings.TrimSpace(path)})
}

func (a *CoreClientAdapter) UseWorkspace(ctx context.Context, path string) error {
	if a == nil || a.engine == nil {
		return errors.New("core client is not available")
	}
	return a.engine.Workspaces().Use(ctx, coreapi.WorkspacePathRequest{Path: strings.TrimSpace(path)})
}

func (a *CoreClientAdapter) TrustWorkspace(ctx context.Context, path string) error {
	if a == nil || a.engine == nil {
		return errors.New("core client is not available")
	}
	return a.engine.Workspaces().Trust(ctx, coreapi.WorkspacePathRequest{Path: strings.TrimSpace(path)})
}

// StartContextEngine 切换前景工作区。eos-core 通过 workspace/set_foreground 实现。
func (a *CoreClientAdapter) StartContextEngine(ctx context.Context, workspacePath string) error {
	workspacePath = strings.TrimSpace(workspacePath)
	if workspacePath == "" {
		return errors.New("workspace path required")
	}
	if a == nil || a.engine == nil {
		return errors.New("core client is not available")
	}
	return a.engine.Workspaces().SetForeground(ctx, coreapi.WorkspacePathRequest{Path: workspacePath})
}

// === Settings / Config ===

func (a *CoreClientAdapter) Settings(ctx context.Context) (settings.Settings, error) {
	if a == nil || a.engine == nil {
		return settings.Settings{}, errors.New("core client is not available")
	}
	raw, err := a.engine.Config().GetSettings(ctx)
	if err != nil {
		return settings.Settings{}, err
	}
	return settingsFromCoreAPI(raw), nil
}

func (a *CoreClientAdapter) SaveSettings(ctx context.Context, s settings.Settings) error {
	if a == nil || a.engine == nil {
		return errors.New("core client is not available")
	}
	return a.engine.Config().SaveSettings(ctx, coreAPISettingsFromInternal(s))
}

func (a *CoreClientAdapter) RulesSnapshot(ctx context.Context) (coreapi.RulesSnapshot, error) {
	if a == nil || a.engine == nil {
		return coreapi.RulesSnapshot{}, errors.New("core client is not available")
	}
	return a.engine.Config().RulesSnapshot(ctx)
}

func (a *CoreClientAdapter) SaveRules(ctx context.Context, scope, content string) error {
	if a == nil || a.engine == nil {
		return errors.New("core client is not available")
	}
	scope = strings.ToLower(strings.TrimSpace(scope))
	if scope == "" {
		scope = "project"
	}
	return a.engine.Config().SaveRules(ctx, coreapi.SaveRulesRequest{Scope: scope, Content: content})
}

// === Permission / Mode ===

func (a *CoreClientAdapter) PermissionSnapshot(ctx context.Context) (coreapi.PermissionSnapshot, error) {
	if a == nil || a.engine == nil {
		return coreapi.PermissionSnapshot{}, errors.New("core client is not available")
	}
	return a.engine.Permissions().Snapshot(ctx)
}

// === Goal（目标模式）===

// SetGoal 设定会话目标：目标进入 active 后 agent 空闲自驱，持续朝目标工作。
// tokenBudget 为 nil 表示不设预算。
func (a *CoreClientAdapter) SetGoal(ctx context.Context, objective string, tokenBudget *int64) (coreapi.ThreadGoal, error) {
	if a == nil || a.engine == nil {
		return coreapi.ThreadGoal{}, errors.New("core client is not available")
	}
	sessionID, err := a.CurrentSessionID(ctx)
	if err != nil {
		return coreapi.ThreadGoal{}, err
	}
	return a.engine.Goals().Set(ctx, coreapi.GoalSetRequest{
		SessionID:   sessionID,
		Objective:   objective,
		TokenBudget: tokenBudget,
	})
}

// GetGoal 查询当前会话目标（Goal 为 nil 表示无目标）。
func (a *CoreClientAdapter) GetGoal(ctx context.Context) (coreapi.GoalGetResponse, error) {
	if a == nil || a.engine == nil {
		return coreapi.GoalGetResponse{}, errors.New("core client is not available")
	}
	sessionID, err := a.CurrentSessionID(ctx)
	if err != nil {
		return coreapi.GoalGetResponse{}, err
	}
	return a.engine.Goals().Get(ctx, coreapi.GoalRefRequest{SessionID: sessionID})
}

// PauseGoal 暂停目标（停止自驱；进行中的 turn 不打断）。
func (a *CoreClientAdapter) PauseGoal(ctx context.Context) (coreapi.ThreadGoal, error) {
	if a == nil || a.engine == nil {
		return coreapi.ThreadGoal{}, errors.New("core client is not available")
	}
	sessionID, err := a.CurrentSessionID(ctx)
	if err != nil {
		return coreapi.ThreadGoal{}, err
	}
	return a.engine.Goals().Pause(ctx, coreapi.GoalRefRequest{SessionID: sessionID})
}

// ResumeGoal 恢复目标并立即触发自驱。
func (a *CoreClientAdapter) ResumeGoal(ctx context.Context) (coreapi.ThreadGoal, error) {
	if a == nil || a.engine == nil {
		return coreapi.ThreadGoal{}, errors.New("core client is not available")
	}
	sessionID, err := a.CurrentSessionID(ctx)
	if err != nil {
		return coreapi.ThreadGoal{}, err
	}
	return a.engine.Goals().Resume(ctx, coreapi.GoalRefRequest{SessionID: sessionID})
}

// ClearGoal 清除会话目标（幂等）。
func (a *CoreClientAdapter) ClearGoal(ctx context.Context) error {
	if a == nil || a.engine == nil {
		return errors.New("core client is not available")
	}
	sessionID, err := a.CurrentSessionID(ctx)
	if err != nil {
		return err
	}
	return a.engine.Goals().Clear(ctx, coreapi.GoalRefRequest{SessionID: sessionID})
}

func (a *CoreClientAdapter) ModeSnapshot(ctx context.Context) (coreapi.ModeSnapshot, error) {
	if a == nil || a.engine == nil {
		return coreapi.ModeSnapshot{}, errors.New("core client is not available")
	}
	return a.engine.Modes().Snapshot(ctx)
}

func (a *CoreClientAdapter) SetExecutionMode(ctx context.Context, mode string) error {
	mode = strings.TrimSpace(mode)
	if mode == "" {
		return nil
	}
	if a == nil || a.engine == nil {
		return errors.New("core client is not available")
	}
	return a.engine.Modes().SetExecutionMode(ctx, coreapi.SetModeRequest{Mode: mode})
}

func (a *CoreClientAdapter) SetAccessMode(ctx context.Context, mode string) error {
	mode = strings.TrimSpace(mode)
	if mode == "" {
		return nil
	}
	if a == nil || a.engine == nil {
		return errors.New("core client is not available")
	}
	return a.engine.Permissions().SetAccessMode(ctx, coreapi.SetModeRequest{Mode: mode})
}

// ApplyAccessMode 是 TUI 切换访问/沙箱模式的语义入口，与桌面端
// applySandboxModeSemantics 维护同一套双轴不变式：
//   - danger-full-access：走内核 permission/enter_full_access 原子推进双轴
//     （approval=never + danger policy）并自动放行待审项；
//   - 其余（read-only / workspace-write）：同步快照字符串 + sandbox/derive_policy
//   - sandbox/set_policy 让真实裁决路径生效，并把审批轴复位内核默认 on-request
//     ——离开完全访问时若不复位，中高风险工具会被 Never→Deny 静默拒绝。
//
// 历史问题：permission/access_mode/set 只写 UI 快照不影响裁决；单独调用会让
// /permissions 显示已切换而沙箱策略未变（UI 与内核脱节）。
func (a *CoreClientAdapter) ApplyAccessMode(ctx context.Context, mode string) error {
	mode = strings.TrimSpace(mode)
	if mode == "" {
		return nil
	}
	if a == nil || a.engine == nil {
		return errors.New("core client is not available")
	}
	workspace := ""
	if cwd, err := os.Getwd(); err == nil {
		workspace = strings.TrimSpace(cwd)
	}
	if mode == "danger-full-access" {
		if err := a.engine.Permissions().EnterFullAccess(ctx, coreapi.EnterFullAccessRequest{WorkspaceRoot: workspace}); err != nil {
			return err
		}
		return nil
	}
	if err := a.engine.Modes().SetSandboxMode(ctx, coreapi.SetModeRequest{Mode: mode}); err != nil {
		return err
	}
	if err := a.engine.Permissions().SetAccessMode(ctx, coreapi.SetModeRequest{Mode: mode}); err != nil {
		return err
	}
	policy, err := a.engine.Sandbox().DerivePolicy(ctx, coreapi.DeriveSandboxPolicyRequest{Mode: mode, WorkspaceRoot: workspace})
	if err != nil {
		return fmt.Errorf("derive sandbox policy: %w", err)
	}
	if err := a.engine.Sandbox().SetPolicy(ctx, coreapi.SessionRef{}, policy); err != nil {
		return fmt.Errorf("set sandbox policy: %w", err)
	}
	return a.engine.Permissions().SetApprovalMode(ctx, coreapi.SetModeRequest{Mode: "on-request"})
}

func (a *CoreClientAdapter) SetApprovalMode(ctx context.Context, mode string) error {
	mode = strings.TrimSpace(mode)
	if mode == "" {
		return nil
	}
	if a == nil || a.engine == nil {
		return errors.New("core client is not available")
	}
	return a.engine.Permissions().SetApprovalMode(ctx, coreapi.SetModeRequest{Mode: mode})
}

// SyncModeSnapshots 把启动期已由 env（EOS_SANDBOX_MODE/EOS_APPROVAL_MODE）在
// 内核生效的模式同步进 UI 快照（mode_snapshot / permission_snapshot）。内核
// ConfigState 不会自动反映启动 env——不同步会让 /status、/permissions 回显
// 空值，造成壳层与内核感知脱节。
//
// 与 ApplyAccessMode 不同：这里不动真实裁决状态（沙箱策略与审批已由 env
// 生效），只做显示层对齐；approval 用同值重放是幂等的。
func (a *CoreClientAdapter) SyncModeSnapshots(ctx context.Context, sandboxMode, accessMode, approvalMode string) error {
	if a == nil || a.engine == nil {
		return errors.New("core client is not available")
	}
	if v := strings.TrimSpace(sandboxMode); v != "" {
		if err := a.engine.Modes().SetSandboxMode(ctx, coreapi.SetModeRequest{Mode: v}); err != nil {
			return err
		}
	}
	if v := strings.TrimSpace(accessMode); v != "" {
		if err := a.engine.Permissions().SetAccessMode(ctx, coreapi.SetModeRequest{Mode: v}); err != nil {
			return err
		}
	}
	if v := strings.TrimSpace(approvalMode); v != "" {
		if err := a.engine.Permissions().SetApprovalMode(ctx, coreapi.SetModeRequest{Mode: v}); err != nil {
			return err
		}
	}
	return nil
}

func (a *CoreClientAdapter) PendingReview(ctx context.Context) (coreapi.PendingReview, error) {
	if a == nil || a.engine == nil {
		return coreapi.PendingReview{}, errors.New("core client is not available")
	}
	return a.engine.Permissions().PendingReview(ctx)
}

// === Sessions ===

func (a *CoreClientAdapter) ListSessions(ctx context.Context) ([]coreapi.Session, error) {
	if a == nil || a.engine == nil {
		return nil, errors.New("core client is not available")
	}
	return a.engine.Sessions().List(ctx, coreapi.ListSessionsRequest{})
}

func (a *CoreClientAdapter) CurrentSessionID(ctx context.Context) (string, error) {
	if a == nil || a.engine == nil {
		return "", errors.New("core client is not available")
	}
	session, err := a.engine.Sessions().Current(ctx, coreapi.CurrentSessionRequest{})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(session.ID), nil
}

func (a *CoreClientAdapter) ensureSessionID(ctx context.Context) (string, error) {
	if a == nil || a.engine == nil {
		return "", errors.New("core client is not available")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if sessionID, err := a.CurrentSessionID(ctx); err == nil && strings.TrimSpace(sessionID) != "" {
		return strings.TrimSpace(sessionID), nil
	}
	workspaceRoot := strings.TrimSpace(a.ActiveWorkspace(ctx))
	session, err := a.engine.Sessions().Create(ctx, coreapi.CreateSessionRequest{
		WorkspaceRoot: workspaceRoot,
		Title:         "CLI session",
		Metadata:      map[string]any{"source": "cli"},
	})
	if err != nil {
		return "", err
	}
	sessionID := strings.TrimSpace(session.ID)
	if sessionID == "" {
		return "", errors.New("session_id is required: failed to resolve or create a session")
	}
	return sessionID, nil
}

func (a *CoreClientAdapter) SaveSessionMessages(ctx context.Context, id string, messages []coreapi.SessionMessage) (string, error) {
	if a == nil || a.engine == nil {
		return "", errors.New("core client is not available")
	}
	session, err := a.engine.Sessions().SaveMessages(ctx, coreapi.SaveSessionMessagesRequest{
		SessionID: strings.TrimSpace(id),
		Messages:  messages,
	})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(session.ID), nil
}

func (a *CoreClientAdapter) LoadSessionMessages(ctx context.Context, id string) ([]coreapi.SessionMessage, error) {
	if a == nil || a.engine == nil {
		return nil, errors.New("core client is not available")
	}
	return a.engine.Sessions().LoadMessages(ctx, coreapi.LoadSessionMessagesRequest{SessionID: strings.TrimSpace(id)})
}

func (a *CoreClientAdapter) RenameSession(ctx context.Context, id, title string) error {
	if a == nil || a.engine == nil {
		return errors.New("core client is not available")
	}
	_, err := a.engine.Sessions().Rename(ctx, coreapi.RenameSessionRequest{
		SessionID: strings.TrimSpace(id),
		Title:     strings.TrimSpace(title),
	})
	return err
}

func (a *CoreClientAdapter) ResumeSession(ctx context.Context, id string) error {
	if a == nil || a.engine == nil {
		return errors.New("core client is not available")
	}
	_, err := a.engine.Sessions().Resume(ctx, coreapi.ResumeSessionRequest{SessionID: strings.TrimSpace(id)})
	return err
}

func (a *CoreClientAdapter) SessionsDir(ctx context.Context) string {
	root := a.ActiveWorkspace(ctx)
	if root == "" {
		root, _ = filepath.Abs(".")
	}
	return filepath.Join(root, ".eos", "sessions")
}

// === Models ===

func (a *CoreClientAdapter) Models(ctx context.Context) ([]coreapi.ModelConfig, error) {
	if a == nil || a.engine == nil {
		return nil, errors.New("core client is not available")
	}
	return a.engine.Models().List(ctx)
}

func (a *CoreClientAdapter) ModelEntries(ctx context.Context) ([]config.ModelEntry, string, error) {
	items, err := a.Models(ctx)
	if err != nil {
		return nil, "", err
	}
	entries := make([]config.ModelEntry, 0, len(items))
	active := ""
	for _, item := range items {
		entries = append(entries, config.ModelEntry{
			Name:                    strings.TrimSpace(item.Name),
			APIBase:                 strings.TrimSpace(item.APIBase),
			APIKey:                  strings.TrimSpace(item.APIKeyMasked),
			Model:                   strings.TrimSpace(item.Model),
			Source:                  strings.TrimSpace(item.Source),
			Provider:                strings.TrimSpace(item.ProviderID),
			PresetID:                strings.TrimSpace(item.PresetID),
			SupportsReasoningEffort: item.SupportsReasoningEffort,
			SupportsVision:          item.SupportsVision,
			SupportsTools:           item.SupportsTools,
		})
		if item.Active {
			active = strings.TrimSpace(item.Name)
		}
	}
	return entries, active, nil
}

func (a *CoreClientAdapter) ActivateModel(ctx context.Context, name string) error {
	if a == nil || a.engine == nil {
		return errors.New("core client is not available")
	}
	return a.engine.Models().Activate(ctx, coreapi.ModelNameRequest{Name: strings.TrimSpace(name)})
}

func (a *CoreClientAdapter) ModelCatalog(ctx context.Context) (coreapi.ModelCatalogState, error) {
	if a == nil || a.engine == nil {
		return coreapi.ModelCatalogState{}, errors.New("core client is not available")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return a.engine.Models().Catalog(ctx)
}

func (a *CoreClientAdapter) ModelContext(ctx context.Context) (coreapi.ModelContextSnapshot, error) {
	if a == nil || a.engine == nil {
		return coreapi.ModelContextSnapshot{}, errors.New("core client is not available")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	sessionID, err := a.CurrentSessionID(ctx)
	if err != nil {
		sessionID = ""
	}
	return a.engine.Models().Context(ctx, coreapi.ModelContextRequest{
		WorkspaceRoot: strings.TrimSpace(a.ActiveWorkspace(ctx)),
		SessionID:     strings.TrimSpace(sessionID),
	})
}

func (a *CoreClientAdapter) SelectModelForCurrentContext(ctx context.Context, name string) (string, error) {
	if a == nil || a.engine == nil {
		return "", errors.New("core client is not available")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("model name is required")
	}
	sessionID, err := a.CurrentSessionID(ctx)
	if err == nil && strings.TrimSpace(sessionID) != "" {
		if err := a.engine.Models().SetSession(ctx, coreapi.SetSessionModelRequest{
			SessionID: strings.TrimSpace(sessionID),
			ModelName: name,
		}); err != nil {
			return "", err
		}
		return "session", nil
	}
	if workspaceRoot := strings.TrimSpace(a.ActiveWorkspace(ctx)); workspaceRoot != "" {
		if err := a.engine.Models().SetWorkspace(ctx, coreapi.SetWorkspaceModelRequest{
			WorkspaceRoot: workspaceRoot,
			ModelName:     name,
		}); err != nil {
			return "", err
		}
		return "workspace", nil
	}
	if err := a.engine.Models().Activate(ctx, coreapi.ModelNameRequest{Name: name}); err != nil {
		return "", err
	}
	return "global", nil
}

// SelectWorkspaceModel 将模型写入当前工作区默认（供新增模型后联动后续会话的默认模型）。
func (a *CoreClientAdapter) SelectWorkspaceModel(ctx context.Context, name string) error {
	if a == nil || a.engine == nil {
		return errors.New("core client is not available")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("model name is required")
	}
	workspaceRoot := strings.TrimSpace(a.ActiveWorkspace(ctx))
	if workspaceRoot == "" {
		return nil
	}
	return a.engine.Models().SetWorkspace(ctx, coreapi.SetWorkspaceModelRequest{
		WorkspaceRoot: workspaceRoot,
		ModelName:     name,
	})
}

func (a *CoreClientAdapter) DeleteModel(ctx context.Context, name string) error {
	if a == nil || a.engine == nil {
		return errors.New("core client is not available")
	}
	return a.engine.Models().Delete(ctx, coreapi.ModelNameRequest{Name: strings.TrimSpace(name)})
}

func (a *CoreClientAdapter) UpsertModelEntry(ctx context.Context, entry config.ModelEntry) error {
	if a == nil || a.engine == nil {
		return errors.New("core client is not available")
	}
	// 内核 upsert 持久化会把 eos.json 的 api_key 覆写成 masked；当前内核的
	// eos.json 加载路径会把 masked 当真实 key 用（重启后 401），保存前快照、
	// 成功后回补明文。见 internal/config/apikey_patch.go。
	plaintext := config.SnapshotPlaintextAPIKeys()
	if k := strings.TrimSpace(entry.APIKey); k != "" && !config.LooksMaskedAPIKey(k) {
		plaintext[entry.Name] = entry.APIKey
	}
	if err := a.engine.Models().Upsert(ctx, coreapi.UpsertModelRequest{
		Name:    strings.TrimSpace(entry.Name),
		APIBase: strings.TrimSpace(entry.APIBase),
		APIKey:  strings.TrimSpace(entry.APIKey),
		Model:   strings.TrimSpace(entry.Model),
	}); err != nil {
		return err
	}
	config.RestorePlaintextAPIKeys(plaintext)
	return nil
}

// SaveModel 走内核 model/save（preset/custom_model/custom_provider 三种模式），
// 保留 provider/preset 关联：内核按 (plan, format) 解析端点与请求构造器，
// 套餐类 preset（如 MiniMax Token Plan）的端点与鉴权才能选对。
//
// 保存前后维护明文 key：内核持久化会把 eos.json 的 api_key 写成 masked 占位
// （真实 key 进钥匙串），而当前内核的加载路径会把 masked 灌进 secrets 并屏蔽
// 钥匙串——重启后 401。保存前快照明文、成功后回补，保证旧内核下可用。
func (a *CoreClientAdapter) SaveModel(ctx context.Context, req coreapi.ModelSaveRequest) error {
	if a == nil || a.engine == nil {
		return errors.New("core client is not available")
	}
	plaintext := config.SnapshotPlaintextAPIKeys()
	if k := strings.TrimSpace(req.APIKey); k != "" && !config.LooksMaskedAPIKey(k) {
		plaintext[strings.TrimSpace(req.Name)] = k
		// 改名场景：旧名下的明文 key 转移到新名。
		if old := strings.TrimSpace(req.OriginalName); old != "" && old != strings.TrimSpace(req.Name) {
			delete(plaintext, old)
		}
	}
	if err := a.engine.Models().Save(ctx, req); err != nil {
		return err
	}
	config.RestorePlaintextAPIKeys(plaintext)
	return nil
}

// ModelCatalogSnapshot 返回内核内置目录（providers + presets），供 /model
// 归一化解析（旧条目按端点兜底关联）与套餐内模型切换使用；失败返回 nil，
// 调用方按「无目录」降级。
func (a *CoreClientAdapter) ModelCatalogSnapshot(ctx context.Context) *coreapi.ModelCatalogState {
	if a == nil || a.engine == nil {
		return nil
	}
	catalog, err := a.engine.Models().Catalog(ctx)
	if err != nil {
		return nil
	}
	return &catalog
}

// ResolveModelInput 把用户输入（条目名/模型 ID/套餐模型 label/preset 名，
// 空格连字符不敏感）解析到已配置条目。见 ai.ResolveModelInput。
func (a *CoreClientAdapter) ResolveModelInput(ctx context.Context, input string) (*ai.ModelResolution, error) {
	if a == nil || a.engine == nil {
		return nil, errors.New("core client is not available")
	}
	entries, err := a.Models(ctx)
	if err != nil {
		return nil, err
	}
	return ai.ResolveModelInput(input, entries, a.ModelCatalogSnapshot(ctx))
}

// SwitchPlanModel 在套餐类条目内切换具体模型（如 MiniMax Token Plan 的
// MiniMax-M3 / MiniMax-M2.7）：mode=preset 的 model/save 更新，original_name
// 指向现有条目（内核走更新路径而非新增，不会触发同名冲突），api_key 留空
// 由内核保留已存的 key。
func (a *CoreClientAdapter) SwitchPlanModel(ctx context.Context, entryName, planModelID string) error {
	if a == nil || a.engine == nil {
		return errors.New("core client is not available")
	}
	entryName = strings.TrimSpace(entryName)
	planModelID = strings.TrimSpace(planModelID)
	if entryName == "" || planModelID == "" {
		return errors.New("entry name and plan model id are required")
	}
	entries, err := a.Models(ctx)
	if err != nil {
		return err
	}
	var entry *coreapi.ModelConfig
	for i := range entries {
		if entries[i].Name == entryName {
			entry = &entries[i]
			break
		}
	}
	if entry == nil {
		return fmt.Errorf("model entry %q not found", entryName)
	}
	if strings.TrimSpace(entry.PresetID) == "" {
		return fmt.Errorf("model entry %q is not a built-in preset; edit it instead", entryName)
	}
	return a.SaveModel(ctx, coreapi.ModelSaveRequest{
		OriginalName: entry.Name,
		Mode:         "preset",
		ProviderID:   strings.TrimSpace(entry.ProviderID),
		PresetID:     strings.TrimSpace(entry.PresetID),
		Name:         entry.Name,
		APIBase:      strings.TrimSpace(entry.APIBase),
		Model:        planModelID,
	})
}

// HealCurrentSessionModel 自愈当前会话的无效 model_name 覆盖（历史版本
// 写入的目录 label）。返回用户可见的提示文本，空串表示无需处理。
func (a *CoreClientAdapter) HealCurrentSessionModel(ctx context.Context) string {
	if a == nil || a.engine == nil {
		return ""
	}
	session, err := a.engine.Sessions().Current(ctx, coreapi.CurrentSessionRequest{})
	if err != nil || strings.TrimSpace(session.ID) == "" {
		return ""
	}
	return ai.HealSessionModelOverride(ctx, a.engine.Models(), session)
}

func (a *CoreClientAdapter) SyncEnvModel(ctx context.Context) error {
	if a == nil || a.engine == nil {
		return errors.New("core client is not available")
	}
	return a.engine.Models().SyncEnv(ctx)
}

func (a *CoreClientAdapter) GetModelInfo() (modelName, modelBase string) {
	if a == nil || a.engine == nil {
		return "", ""
	}
	snapshot, err := a.ModelContext(context.Background())
	if err == nil && strings.TrimSpace(snapshot.ResolvedModelName) != "" {
		items, listErr := a.engine.Models().List(context.Background())
		if listErr == nil {
			for _, desc := range items {
				if desc.Name == snapshot.ResolvedModelName {
					return strings.TrimSpace(desc.Model), strings.TrimSpace(desc.APIBase)
				}
			}
		}
	}
	return "", ""
}

// === MCP ===

func (a *CoreClientAdapter) MCPServers(ctx context.Context) ([]config.MCPEntry, error) {
	if a == nil || a.engine == nil {
		return nil, errors.New("core client is not available")
	}
	items, err := a.engine.MCP().List(ctx)
	if err != nil {
		return nil, err
	}
	return mcpEntriesFromCoreAPI(items), nil
}

func (a *CoreClientAdapter) SetMCPEnabled(ctx context.Context, name string, enabled bool) error {
	if a == nil || a.engine == nil {
		return errors.New("core client is not available")
	}
	return a.engine.MCP().SetEnabled(ctx, coreapi.SetMCPEnabledRequest{Name: strings.TrimSpace(name), Enabled: enabled})
}

func (a *CoreClientAdapter) DeleteMCPServer(ctx context.Context, name string) error {
	if a == nil || a.engine == nil {
		return errors.New("core client is not available")
	}
	return a.engine.MCP().Delete(ctx, coreapi.MCPNameRequest{Name: strings.TrimSpace(name)})
}

func (a *CoreClientAdapter) ImportMCPJSON(ctx context.Context, raw string) error {
	if a == nil || a.engine == nil {
		return errors.New("core client is not available")
	}
	return a.engine.MCP().ImportJSON(ctx, coreapi.ImportMCPJSONRequest{Raw: raw})
}

func (a *CoreClientAdapter) UpsertMCPEntry(ctx context.Context, entry config.MCPEntry) error {
	if a == nil || a.engine == nil {
		return errors.New("core client is not available")
	}
	return a.engine.MCP().Upsert(ctx, coreAPIUpsertMCPRequest(entry))
}

func (a *CoreClientAdapter) AddMCPEntries(ctx context.Context, entries []config.MCPEntry) error {
	if len(entries) == 0 {
		return errors.New("empty config")
	}
	raw, err := json.Marshal(entries)
	if err != nil {
		return err
	}
	return a.ImportMCPJSON(ctx, string(raw))
}

// === LSP ===

func (a *CoreClientAdapter) LSPServers(ctx context.Context) ([]coreapi.LSPServer, error) {
	if a == nil || a.engine == nil {
		return nil, errors.New("core client is not available")
	}
	return a.engine.LSP().List(ctx)
}

func (a *CoreClientAdapter) LSPDiagnostics(ctx context.Context) ([]string, error) {
	if a == nil || a.engine == nil {
		return nil, errors.New("core client is not available")
	}
	return a.engine.LSP().Diagnostics(ctx)
}

func (a *CoreClientAdapter) LSPDiagnosticsSummary(ctx context.Context) (coreapi.LSPDiagnosticsSummary, error) {
	if a == nil || a.engine == nil {
		return coreapi.LSPDiagnosticsSummary{}, errors.New("core client is not available")
	}
	return a.engine.LSP().DiagnosticsSummary(ctx)
}

func (a *CoreClientAdapter) LSPDiagnosticsMarkdown(ctx context.Context) string {
	lines, err := a.LSPDiagnostics(ctx)
	if err != nil || len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n")
}

// === Tools / Tasks ===

// ExecuteCoreTool 直接执行一次内核工具（不经 turn 循环），供 TUI 斜杠
// 命令等宿主功能复用内核工具能力（如 /screenshot）。
func (a *CoreClientAdapter) ExecuteCoreTool(ctx context.Context, req coreapi.ToolRequest) (coreapi.ToolResult, error) {
	if a == nil || a.engine == nil {
		return coreapi.ToolResult{}, errors.New("core client is not available")
	}
	return a.engine.Tools().Execute(ctx, req)
}

// CallCore 通用 JSON-RPC 调用（供 /plugin 等斜杠命令直接调协议方法）。
func (a *CoreClientAdapter) CallCore(ctx context.Context, method string, params interface{}, result interface{}) error {
	if a == nil || a.engine == nil {
		return errors.New("core client is not available")
	}
	if params == nil {
		params = map[string]interface{}{}
	}
	rawParams, _ := json.Marshal(params)
	caller := a.engine.Caller()
	if caller == nil {
		return errors.New("core caller is not available")
	}
	return caller.Call(ctx, method, json.RawMessage(rawParams), result)
}

func (a *CoreClientAdapter) ToolTraces(ctx context.Context) ([]coreapi.ToolTrace, error) {
	if a == nil || a.engine == nil {
		return nil, errors.New("core client is not available")
	}
	return a.engine.ToolTelemetry().Traces(ctx)
}

func (a *CoreClientAdapter) ToolStats(ctx context.Context) ([]coreapi.ToolStat, error) {
	if a == nil || a.engine == nil {
		return nil, errors.New("core client is not available")
	}
	return a.engine.ToolTelemetry().Stats(ctx)
}

func (a *CoreClientAdapter) Tasks(ctx context.Context) ([]coreapi.TaskSnapshot, error) {
	if a == nil || a.engine == nil {
		return nil, errors.New("core client is not available")
	}
	return a.engine.Tasks().List(ctx)
}

func (a *CoreClientAdapter) KillTask(ctx context.Context, taskID string) error {
	if a == nil || a.engine == nil {
		return errors.New("core client is not available")
	}
	return a.engine.Tasks().Kill(ctx, coreapi.TaskIDRequest{TaskID: strings.TrimSpace(taskID)})
}

func (a *CoreClientAdapter) TailTask(ctx context.Context, taskID string) ([]string, error) {
	if a == nil || a.engine == nil {
		return nil, errors.New("core client is not available")
	}
	return a.engine.Tasks().Tail(ctx, coreapi.TaskIDRequest{TaskID: strings.TrimSpace(taskID)})
}

func (a *CoreClientAdapter) CleanupTasks(ctx context.Context) (int, error) {
	if a == nil || a.engine == nil {
		return 0, errors.New("core client is not available")
	}
	return a.engine.Tasks().Cleanup(ctx)
}

func (a *CoreClientAdapter) Todos(ctx context.Context) ([]coreapi.TodoItem, error) {
	if a == nil || a.engine == nil {
		return nil, errors.New("core client is not available")
	}
	return a.engine.Tasks().Todos(ctx)
}

func (a *CoreClientAdapter) Agents(ctx context.Context) ([]coreapi.Agent, error) {
	if a == nil || a.engine == nil {
		return nil, errors.New("core client is not available")
	}
	return a.engine.Agents().List(ctx, coreapi.ListAgentsRequest{})
}

// === Context ===

func (a *CoreClientAdapter) ContextPreview(ctx context.Context) ([]string, error) {
	if a == nil || a.engine == nil {
		return nil, errors.New("core client is not available")
	}
	return a.engine.Context().Preview(ctx)
}

func (a *CoreClientAdapter) ContextStats(ctx context.Context) (coreapi.ContextStats, error) {
	if a == nil || a.engine == nil {
		return coreapi.ContextStats{}, errors.New("core client is not available")
	}
	return a.engine.Context().Stats(ctx)
}

func (a *CoreClientAdapter) ContextWindowTokens(ctx context.Context) (int, error) {
	if a == nil || a.engine == nil {
		return 0, errors.New("core client is not available")
	}
	return a.engine.Context().WindowTokens(ctx)
}

func (a *CoreClientAdapter) CurrentContextUsage(ctx context.Context) (int, float64, error) {
	stats, err := a.ContextStats(ctx)
	if err != nil {
		return 0, 0, err
	}
	window, err := a.ContextWindowTokens(ctx)
	if err != nil {
		return 0, 0, err
	}
	ratio := 0.0
	if window > 0 {
		ratio = float64(stats.Estimated) / float64(window)
	}
	return stats.Estimated, ratio, nil
}

func (a *CoreClientAdapter) PinContextDocument(ctx context.Context, id, content string, tokenBudget int) error {
	if a == nil || a.engine == nil {
		return errors.New("core client is not available")
	}
	return a.engine.Context().PinDocument(ctx, coreapi.PinDocumentRequest{
		ID:          strings.TrimSpace(id),
		Content:     content,
		TokenBudget: tokenBudget,
	})
}

func (a *CoreClientAdapter) CompactContext(ctx context.Context) (string, error) {
	if a == nil || a.engine == nil {
		return "", errors.New("core client is not available")
	}
	return a.engine.Context().Compact(ctx)
}

func (a *CoreClientAdapter) ClearContext(ctx context.Context) error {
	if a == nil || a.engine == nil {
		return errors.New("core client is not available")
	}
	return a.engine.Context().Clear(ctx)
}

func (a *CoreClientAdapter) ExportContext(ctx context.Context, path string) error {
	if a == nil || a.engine == nil {
		return errors.New("core client is not available")
	}
	return a.engine.Context().Export(ctx, coreapi.ExportContextRequest{Path: strings.TrimSpace(path)})
}

// === Usage / Versions ===

func (a *CoreClientAdapter) UsageSummary(ctx context.Context) (coreapi.UsageSummary, error) {
	if a == nil || a.engine == nil {
		return coreapi.UsageSummary{}, errors.New("core client is not available")
	}
	return a.engine.Usage().Summary(ctx)
}

func (a *CoreClientAdapter) CostItems(ctx context.Context) ([]coreapi.CostItem, error) {
	if a == nil || a.engine == nil {
		return nil, errors.New("core client is not available")
	}
	return a.engine.Usage().CostItems(ctx)
}

func (a *CoreClientAdapter) Versions(ctx context.Context) ([]coreapi.VersionItem, error) {
	if a == nil || a.engine == nil {
		return nil, errors.New("core client is not available")
	}
	return a.engine.Versions().List(ctx)
}

func (a *CoreClientAdapter) RollbackVersion(ctx context.Context, id string) error {
	if a == nil || a.engine == nil {
		return errors.New("core client is not available")
	}
	return a.engine.Versions().Rollback(ctx, coreapi.VersionIDRequest{ID: strings.TrimSpace(id)})
}

func (a *CoreClientAdapter) DeleteVersion(ctx context.Context, id string) error {
	if a == nil || a.engine == nil {
		return errors.New("core client is not available")
	}
	return a.engine.Versions().Delete(ctx, coreapi.VersionIDRequest{ID: strings.TrimSpace(id)})
}

func (a *CoreClientAdapter) DeleteFileVersions(ctx context.Context, file string) (int, error) {
	if a == nil || a.engine == nil {
		return 0, errors.New("core client is not available")
	}
	return a.engine.Versions().DeleteFile(ctx, coreapi.VersionFileRequest{File: strings.TrimSpace(file)})
}

func (a *CoreClientAdapter) ClearVersions(ctx context.Context) (int, error) {
	if a == nil || a.engine == nil {
		return 0, errors.New("core client is not available")
	}
	return a.engine.Versions().Clear(ctx)
}

// === Memory ===

func (a *CoreClientAdapter) MemorySnapshot(ctx context.Context) (coreapi.MemorySnapshot, error) {
	if a == nil || a.engine == nil {
		return coreapi.MemorySnapshot{}, errors.New("core client is not available")
	}
	return a.engine.Memory().Snapshot(ctx)
}

// SaveMemory 写一条 ad_hoc 记忆笔记（内核 memory/save；scope 为 "project" 落
// 当前项目分区，默认落全局分区 ~/.eos/memories/extensions/ad_hoc/notes/，
// 空内容会被内核拒绝）。
func (a *CoreClientAdapter) SaveMemory(ctx context.Context, scope, content string) error {
	if a == nil || a.engine == nil {
		return errors.New("core client is not available")
	}
	return a.engine.Memory().Save(ctx, coreapi.SaveMemoryRequest{Scope: scope, Content: content})
}

// === Extensions / Insights ===

func (a *CoreClientAdapter) Skills(ctx context.Context) ([]coreapi.SkillInfo, error) {
	if a == nil || a.engine == nil {
		return nil, errors.New("core client is not available")
	}
	return a.engine.Extensions().ListSkills(ctx)
}

func (a *CoreClientAdapter) ReloadSkills(ctx context.Context) error {
	if a == nil || a.engine == nil {
		return errors.New("core client is not available")
	}
	return a.engine.Extensions().ReloadSkills(ctx)
}

func (a *CoreClientAdapter) InvokeSkill(ctx context.Context, name, arguments string) (bool, error) {
	if a == nil || a.engine == nil {
		return false, errors.New("core client is not available")
	}
	out, err := a.engine.Extensions().InvokeSkill(ctx, coreapi.InvokeSkillRequest{
		Name:      strings.TrimSpace(name),
		Arguments: strings.TrimSpace(arguments),
	})
	if err != nil {
		return false, err
	}
	return out.Invoked, nil
}

func (a *CoreClientAdapter) Plugins(ctx context.Context) ([]coreapi.PluginInfo, error) {
	if a == nil || a.engine == nil {
		return nil, errors.New("core client is not available")
	}
	return a.engine.Extensions().ListPlugins(ctx)
}

func (a *CoreClientAdapter) BrowserStatus(ctx context.Context) (coreapi.BrowserRuntimeStatus, error) {
	if a == nil || a.engine == nil {
		return coreapi.BrowserRuntimeStatus{}, errors.New("core client is not available")
	}
	return a.engine.Extensions().BrowserStatus(ctx)
}

func (a *CoreClientAdapter) BrowserLaunch(ctx context.Context, req coreapi.BrowserLaunchRequest) error {
	if a == nil || a.engine == nil {
		return errors.New("core client is not available")
	}
	return a.engine.Extensions().BrowserLaunch(ctx, req)
}

func (a *CoreClientAdapter) BrowserClose(ctx context.Context, req coreapi.BrowserCloseRequest) error {
	if a == nil || a.engine == nil {
		return errors.New("core client is not available")
	}
	return a.engine.Extensions().BrowserClose(ctx, req)
}

func (a *CoreClientAdapter) BrowserControlTakeover(ctx context.Context, req coreapi.BrowserControlTakeoverRequest) error {
	if a == nil || a.engine == nil {
		return errors.New("core client is not available")
	}
	return a.engine.Extensions().BrowserControlTakeover(ctx, req)
}

func (a *CoreClientAdapter) BrowserControlResume(ctx context.Context) error {
	if a == nil || a.engine == nil {
		return errors.New("core client is not available")
	}
	return a.engine.Extensions().BrowserControlResume(ctx)
}

func (a *CoreClientAdapter) BrowserTabs(ctx context.Context) ([]coreapi.BrowserTabInfo, error) {
	if a == nil || a.engine == nil {
		return nil, errors.New("core client is not available")
	}
	return a.engine.Extensions().BrowserTabs(ctx)
}

func (a *CoreClientAdapter) BrowserProfiles(ctx context.Context) ([]coreapi.BrowserProfileRecord, error) {
	if a == nil || a.engine == nil {
		return nil, errors.New("core client is not available")
	}
	return a.engine.Extensions().BrowserProfiles(ctx)
}

func (a *CoreClientAdapter) CurrentRemoteRepo(ctx context.Context) (coreapi.RemoteRepoState, bool, error) {
	if a == nil || a.engine == nil {
		return coreapi.RemoteRepoState{}, false, errors.New("core client is not available")
	}
	state, ok, err := a.engine.RemoteWorkspaces().CurrentRepo(ctx)
	return state, ok, err
}

func (a *CoreClientAdapter) PredictNextUserMessage(ctx context.Context, draft string) (string, error) {
	if a == nil || a.engine == nil {
		return "", errors.New("core client is not available")
	}
	return a.engine.Insights().PredictNextUserMessage(ctx, coreapi.PredictNextUserMessageRequest{Draft: draft})
}

// === Git ===

func (a *CoreClientAdapter) GitStatus(ctx context.Context) ([]coreapi.GitChange, error) {
	if a == nil || a.engine == nil {
		return nil, errors.New("core client is not available")
	}
	return a.engine.Git().Status(ctx, coreapi.GitStatusRequest{})
}

func (a *CoreClientAdapter) GitDiff(ctx context.Context, path string) (string, error) {
	if a == nil || a.engine == nil {
		return "", errors.New("core client is not available")
	}
	out, err := a.engine.Git().Diff(ctx, coreapi.GitDiffRequest{Path: strings.TrimSpace(path)})
	if err != nil {
		return "", err
	}
	return out.Text, nil
}

func (a *CoreClientAdapter) GitBranches(ctx context.Context, workspaceRoot string) (coreapi.GitBranchesResult, error) {
	if a == nil || a.engine == nil {
		return coreapi.GitBranchesResult{}, errors.New("core client is not available")
	}
	return a.engine.Git().Branches(ctx, coreapi.GitBranchesRequest{WorkspaceRoot: strings.TrimSpace(workspaceRoot)})
}

// GitSummary 一次取回分支/上游/ahead-behind/未提交明细，是状态栏 git 项
// 与提交提醒的唯一数据源（比 status+branches 两次调用省一遍 RPC）。
func (a *CoreClientAdapter) GitSummary(ctx context.Context, workspaceRoot string) (coreapi.GitSummaryResult, error) {
	if a == nil || a.engine == nil {
		return coreapi.GitSummaryResult{}, errors.New("core client is not available")
	}
	return a.engine.Git().Summary(ctx, coreapi.GitSummaryRequest{WorkspaceRoot: strings.TrimSpace(workspaceRoot)})
}

func (a *CoreClientAdapter) GitLog(ctx context.Context, req coreapi.GitLogRequest) (coreapi.GitLogResult, error) {
	if a == nil || a.engine == nil {
		return coreapi.GitLogResult{}, errors.New("core client is not available")
	}
	return a.engine.Git().Log(ctx, req)
}

func (a *CoreClientAdapter) GitShow(ctx context.Context, req coreapi.GitShowRequest) (coreapi.GitShowResult, error) {
	if a == nil || a.engine == nil {
		return coreapi.GitShowResult{}, errors.New("core client is not available")
	}
	return a.engine.Git().Show(ctx, req)
}

// === Prompts ===

func (a *CoreClientAdapter) RespondPrompt(ctx context.Context, id, kind string, response PromptResponse) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	if a == nil || a.engine == nil {
		return errors.New("core client is not available")
	}
	if strings.EqualFold(strings.TrimSpace(kind), "inquiry") {
		return a.engine.Inquiries().Respond(ctx, coreapi.InquiryResponse{
			InquiryID: id,
			Option:    strings.TrimSpace(response.Option),
			Text:      strings.TrimSpace(response.Text),
		})
	}
	decision := approvalDecisionFromPrompt(strings.TrimSpace(response.Decision))
	if decision == "" {
		decision = approvalDecisionFromPrompt(strings.TrimSpace(response.Option))
	}
	if decision == "" {
		decision = coreapi.ApprovalDecline
	}
	reason := strings.TrimSpace(response.Text)
	return a.engine.Approvals().Respond(ctx, coreapi.ApprovalResponse{
		ApprovalID: id,
		Decision:   decision,
		Reason:     reason,
	})
}

// approvalDecisionFromPrompt maps a confirm-UI decision/option string to the
// canonical typed wire decision. The confirm model emits canonical values
// (accept / acceptForSession / decline / cancel); legacy option keys are mapped
// explicitly so the typed wire layer always receives a fixed decision.
func approvalDecisionFromPrompt(value string) coreapi.ApprovalDecision {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "accept", "allow", "allow_once", "approve":
		return coreapi.ApprovalAccept
	case "acceptforsession", "allow_session", "session":
		return coreapi.ApprovalAcceptForSession
	case "decline", "deny":
		return coreapi.ApprovalDecline
	case "cancel":
		return coreapi.ApprovalCancel
	default:
		return ""
	}
}

// === Event pump ===

// runNotificationPump 同步订阅 engine 事件流，把 envelope 投递到 notificationCh。
// pumpReady 在 Subscribe 调用完成（成功或失败）后关闭，让 ensurePumpStarted 同步返回。
func (a *CoreClientAdapter) runNotificationPump(ctx context.Context) {
	if a == nil || a.engine == nil {
		if a.pumpReady != nil {
			select {
			case <-a.pumpReady:
			default:
				close(a.pumpReady)
			}
		}
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	envCh, err := a.engine.Events().Subscribe(ctx, coreapi.EventFilter{})
	if a.pumpReady != nil {
		select {
		case <-a.pumpReady:
		default:
			close(a.pumpReady)
		}
	}
	if err != nil {
		return
	}
	for env := range envCh {
		event := runtimeEventFromEnvelope(env)
		// 阻塞发送以确保事件不丢失。如果 dispatcher 来不及消费，
		// 该 goroutine 会暂时阻塞直到 dispatcher 腾出空间。
		func() {
			defer func() { _ = recover() }()
			a.notificationCh <- event
		}()
	}
}

func (a *CoreClientAdapter) startEventDispatcher() {
	if a == nil {
		return
	}
	go func() {
		defer close(a.dispatcherDone)
		for event := range a.notificationCh {
			a.publishEvent(event)
		}
		a.closeSubscribers()
	}()
}

func (a *CoreClientAdapter) subscribeEvents(buffer int) (<-chan RuntimeEvent, func()) {
	if buffer <= 0 {
		buffer = 256
	}
	ch := make(chan RuntimeEvent, buffer)
	a.subscribersMu.Lock()
	a.nextSubscriber++
	id := a.nextSubscriber
	a.subscribers[id] = ch
	a.subscribersMu.Unlock()
	var once sync.Once
	unsubscribe := func() {
		once.Do(func() {
			a.subscribersMu.Lock()
			if cur, ok := a.subscribers[id]; ok {
				delete(a.subscribers, id)
				close(cur)
			}
			a.subscribersMu.Unlock()
		})
	}
	return ch, unsubscribe
}

func (a *CoreClientAdapter) publishEvent(event RuntimeEvent) {
	a.subscribersMu.Lock()
	// 快照当前订阅者列表，在锁外发送，避免阻塞时死锁
	channels := make([]chan RuntimeEvent, 0, len(a.subscribers))
	for _, ch := range a.subscribers {
		channels = append(channels, ch)
	}
	a.subscribersMu.Unlock()
	for _, ch := range channels {
		// 阻塞发送以确保事件不丢失。如果订阅者来不及消费，
		// 发布 goroutine 会暂时阻塞直到订阅者腾出空间。
		// 如果期间订阅者被取消（channel 已关闭），recover 吞掉 panic。
		func() {
			defer func() { _ = recover() }()
			ch <- event
		}()
	}
}

func (a *CoreClientAdapter) closeSubscribers() {
	a.subscribersMu.Lock()
	defer a.subscribersMu.Unlock()
	for id, ch := range a.subscribers {
		delete(a.subscribers, id)
		close(ch)
	}
}

func (a *CoreClientAdapter) setActiveTurn(ref coreapi.TurnRef) {
	a.activeTurnMu.Lock()
	a.activeTurn = ref
	a.activeTurnAlive = strings.TrimSpace(ref.TurnID) != ""
	a.activeTurnMu.Unlock()
}

func (a *CoreClientAdapter) currentActiveTurn() (coreapi.TurnRef, bool) {
	a.activeTurnMu.Lock()
	defer a.activeTurnMu.Unlock()
	return a.activeTurn, a.activeTurnAlive
}

func (a *CoreClientAdapter) clearActiveTurn(turnID string) {
	a.activeTurnMu.Lock()
	if strings.TrimSpace(turnID) == "" || strings.TrimSpace(a.activeTurn.TurnID) == strings.TrimSpace(turnID) {
		a.activeTurn = coreapi.TurnRef{}
		a.activeTurnAlive = false
	}
	a.activeTurnMu.Unlock()
}

func (a *CoreClientAdapter) interruptTurn(ctx context.Context, ref coreapi.TurnRef) (bool, error) {
	if a == nil || a.engine == nil || strings.TrimSpace(ref.TurnID) == "" {
		return false, nil
	}
	err := a.engine.Turns().Interrupt(ctx, ref)
	return err == nil, err
}

// === Local helpers (no legacy imports) ===

func toolResultOutputText(result coreapi.ToolResult) string {
	if len(result.Output) == 0 {
		return strings.TrimRight(result.Display, "\n")
	}
	var payload map[string]any
	if err := json.Unmarshal(result.Output, &payload); err != nil {
		return strings.TrimRight(result.Display, "\n")
	}
	for _, key := range []string{"stdout", "output", "text", "stderr"} {
		if value, ok := payload[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimRight(value, "\n")
		}
	}
	return strings.TrimRight(result.Display, "\n")
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func firstEventText(data map[string]any, fallback string, keys ...string) string {
	for _, key := range keys {
		value, _ := data[key].(string)
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return strings.TrimSpace(fallback)
}

// itemCompletedText extracts the text from an item.completed event's payload,
// which nests the TurnItem under payload.item. AgentMessage and Plan items
// carry displayable text; tool_call items do not.
func itemCompletedText(data map[string]any) string {
	item, ok := data["item"].(map[string]any)
	if !ok {
		return ""
	}
	kind, _ := item["kind"].(string)
	if kind != "agent_message" && kind != "plan" {
		return ""
	}
	text, _ := item["text"].(string)
	return strings.TrimSpace(text)
}

func eventString(data map[string]any, key string) string {
	v, _ := data[key].(string)
	return v
}

func runtimeEventFromEnvelope(envelope protocol.Envelope) RuntimeEvent {
	data := protocol.ClonePayload(envelope.Payload)
	if data == nil {
		data = map[string]any{}
	}
	rawEventType := envelope.EventType
	eventType := protocol.NormalizeEventType(rawEventType)
	if rawEventType != eventType {
		data["original_event_type"] = string(rawEventType)
	}
	if strings.TrimSpace(envelope.EventID) != "" {
		data["event_id"] = strings.TrimSpace(envelope.EventID)
	}
	if strings.TrimSpace(envelope.SessionID) != "" {
		data["session_id"] = strings.TrimSpace(envelope.SessionID)
	}
	if strings.TrimSpace(envelope.TurnID) != "" {
		data["turn_id"] = strings.TrimSpace(envelope.TurnID)
	}
	if strings.TrimSpace(envelope.AgentID) != "" {
		data["agent_id"] = strings.TrimSpace(envelope.AgentID)
	}
	if strings.TrimSpace(envelope.RequestID) != "" {
		data["request_id"] = strings.TrimSpace(envelope.RequestID)
	}
	if eventType == protocol.EventTypeRequestFailed && firstEventText(data, "", "error", "message", "text") == "" {
		switch rawEventType {
		case protocol.EventTypeTurnCancelled:
			data["error"] = "request cancelled"
		case protocol.EventTypeTurnInterrupted:
			data["error"] = "request interrupted"
		}
	}
	content := firstEventText(data, "", "text", "message", "summary", "error")
	return RuntimeEvent{
		Type:    string(eventType),
		RID:     firstNonEmptyString(envelope.TurnID, envelope.RequestID, envelope.CorrelationID, envelope.EventID),
		Content: content,
		Data:    data,
	}
}

// Reload 触发 core 配置热重载（MethodConfigReload）。
// protocol 层已定义此方法，eos-core 收到后会重新加载 .eos 配置。
func (a *CoreClientAdapter) Reload() error {
	if a == nil || a.client == nil || a.client.Process() == nil {
		return errors.New("core client is not available")
	}
	var out map[string]any
	return a.client.Process().Call(context.Background(), protocoljsonrpc.MethodConfigReload, nil, &out)
}

// ClearTokenHistory 当前 protocol 层尚未定义 usage/clear_history 方法。
// 保留为 no-op；TUI 旧路径曾直连 a.core.ClearTokenHistory()，现在以 sidecar 为准。
func (a *CoreClientAdapter) ClearTokenHistory() {
	// no-op until protocol 添加对应方法；adapter 编译期保留签名以避免 TUI 编译失败。
	_ = a
}

// ExportSessionMarkdown 把会话消息转成 markdown 写入本地文件。
// 不依赖 bridge.SessionTranscriptMessage，直接消费 coreapi.SessionMessage。
func (a *CoreClientAdapter) ExportSessionMarkdown(ctx context.Context, id, outputPath string) error {
	id = strings.TrimSpace(id)
	outputPath = strings.TrimSpace(outputPath)
	if id == "" {
		return errors.New("session id required")
	}
	if outputPath == "" {
		return errors.New("output path required")
	}
	messages, err := a.LoadSessionMessages(ctx, id)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(outputPath, []byte(renderSessionMarkdown(id, messages)), 0o644)
}

func renderSessionMarkdown(id string, messages []coreapi.SessionMessage) string {
	var b strings.Builder
	b.WriteString("# Session: ")
	b.WriteString(strings.TrimSpace(id))
	b.WriteString("\n\n")
	for _, msg := range messages {
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		role := strings.TrimSpace(msg.Role)
		if role == "" {
			role = "unknown"
		}
		b.WriteString("**")
		b.WriteString(role)
		b.WriteString("**: ")
		b.WriteString(content)
		b.WriteString("\n\n")
	}
	return b.String()
}

// === MCP type bridges (no legacy imports) ===

func mcpEntriesFromCoreAPI(items []coreapi.MCPServer) []config.MCPEntry {
	out := make([]config.MCPEntry, 0, len(items))
	for _, item := range items {
		entry := config.MCPEntry{
			Name:                 strings.TrimSpace(item.Name),
			Type:                 config.MCPClientType(strings.TrimSpace(item.Type)),
			Command:              strings.TrimSpace(item.Command),
			Args:                 append([]string(nil), item.Args...),
			Envs:                 cloneStringMap(item.Envs),
			BaseURL:              strings.TrimSpace(item.BaseURL),
			Enabled:              item.Enabled,
			Auth:                 configMCPAuthFromCoreAPI(item.Auth),
			ApprovalMode:         strings.TrimSpace(item.ApprovalMode),
			ToolApprovalOverride: cloneStringMap(item.ToolApprovalOverride),
		}
		if entry.Command == "" && strings.TrimSpace(item.Target) != "" {
			if entry.Type == config.MCPTypeSSE || entry.Type == config.MCPTypeStreamableHTTP {
				entry.BaseURL = strings.TrimSpace(item.Target)
			} else {
				entry.Command = strings.TrimSpace(item.Target)
			}
		}
		out = append(out, entry)
	}
	return out
}

func coreAPIUpsertMCPRequest(entry config.MCPEntry) coreapi.UpsertMCPRequest {
	target := strings.TrimSpace(entry.Command)
	if strings.TrimSpace(entry.BaseURL) != "" {
		target = strings.TrimSpace(entry.BaseURL)
	}
	return coreapi.UpsertMCPRequest{
		Name:                 strings.TrimSpace(entry.Name),
		Type:                 string(entry.Type),
		Target:               target,
		Command:              strings.TrimSpace(entry.Command),
		Args:                 append([]string(nil), entry.Args...),
		Envs:                 cloneStringMap(entry.Envs),
		BaseURL:              strings.TrimSpace(entry.BaseURL),
		Enabled:              entry.Enabled,
		Auth:                 coreAPIMCPAuthFromConfig(entry.Auth),
		ApprovalMode:         strings.TrimSpace(entry.ApprovalMode),
		ToolApprovalOverride: cloneStringMap(entry.ToolApprovalOverride),
	}
}

func configMCPAuthFromCoreAPI(auth *coreapi.MCPAuth) *config.MCPAuth {
	if auth == nil {
		return nil
	}
	return &config.MCPAuth{
		Type:       strings.TrimSpace(auth.Type),
		Token:      auth.Token,
		Headers:    cloneStringMap(auth.Headers),
		HeadersEnv: cloneStringMap(auth.HeadersEnv),
	}
}

func coreAPIMCPAuthFromConfig(auth *config.MCPAuth) *coreapi.MCPAuth {
	if auth == nil {
		return nil
	}
	return &coreapi.MCPAuth{
		Type:       strings.TrimSpace(auth.Type),
		Token:      auth.Token,
		Headers:    cloneStringMap(auth.Headers),
		HeadersEnv: cloneStringMap(auth.HeadersEnv),
	}
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

// === settings bridging (coreapi.Settings ↔ internal/pkg/settings.Settings) ===
//
// coreapi.Settings 用 *bool + 字符串 PlanBubbleColor（json 名 plan_bubble）；
// internal/pkg/settings.Settings 用 bool AutoContext/Trusted，但 DesktopNotifications 是 *bool。
// 适配器在边界处做转换，对 TUI 调用方只暴露 internal settings 类型。

func coreAPISettingsFromInternal(s settings.Settings) coreapi.Settings {
	return coreapi.Settings{
		PlanPromptStyle:      strings.TrimSpace(s.PlanPromptStyle),
		PlanBubbleColor:      strings.TrimSpace(s.PlanBubbleColor),
		AutoContext:          boolPtr(s.AutoContext),
		DesktopNotifications: s.DesktopNotifications,
		GitCommitReminder:    s.GitCommitReminder,
		MaxInjectKB:          s.MaxInjectKB,
		WatchMode:            strings.TrimSpace(s.WatchMode),
		WatchDebounceMs:      s.WatchDebounceMs,
		PollIntervalSec:      s.PollIntervalSec,
		Language:             strings.TrimSpace(s.Language),
		Theme:                strings.TrimSpace(s.Theme),
		Trusted:              boolPtr(s.Trusted),
		MaxTurnTokens:        s.MaxTurnTokens,
		MaxSessionTokens:     s.MaxSessionTokens,
	}
}

func settingsFromCoreAPI(s coreapi.Settings) settings.Settings {
	out := settings.Settings{
		PlanPromptStyle:      strings.TrimSpace(s.PlanPromptStyle),
		PlanBubbleColor:      strings.TrimSpace(s.PlanBubbleColor),
		AutoContext:          boolFromPtr(s.AutoContext, true),
		DesktopNotifications: s.DesktopNotifications,
		GitCommitReminder:    s.GitCommitReminder,
		MaxInjectKB:          s.MaxInjectKB,
		WatchMode:            strings.TrimSpace(s.WatchMode),
		WatchDebounceMs:      s.WatchDebounceMs,
		PollIntervalSec:      s.PollIntervalSec,
		Language:             strings.TrimSpace(s.Language),
		Theme:                strings.TrimSpace(s.Theme),
		Trusted:              boolFromPtr(s.Trusted, false),
		MaxTurnTokens:        s.MaxTurnTokens,
		MaxSessionTokens:     s.MaxSessionTokens,
	}
	if strings.TrimSpace(out.PlanPromptStyle) == "" {
		out.PlanPromptStyle = "concise"
	}
	if out.MaxInjectKB <= 0 {
		out.MaxInjectKB = 32
	}
	if out.WatchDebounceMs <= 0 {
		out.WatchDebounceMs = 500
	}
	if out.PollIntervalSec <= 0 {
		out.PollIntervalSec = 5
	}
	if strings.TrimSpace(out.Language) == "" {
		out.Language = "zh"
	}
	if strings.TrimSpace(out.Theme) == "" {
		out.Theme = "dark"
	}
	return out
}

func boolPtr(v bool) *bool {
	out := v
	return &out
}

func boolFromPtr(p *bool, fallback bool) bool {
	if p == nil {
		return fallback
	}
	return *p
}

// silence unused import warnings if any of the helpers above stop being referenced.
var _ = protocoljsonrpc.NotificationEvent
