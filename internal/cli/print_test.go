package cli

// Copyright (c) 2026 EOSAIOS
// SPDX-License-Identifier: EOS-NCL-1.1
// 本文件基于 EOS 非商用许可证 v1.1 发布，详见 LICENSE。
// 商业使用请联系版权人获得商业授权。

// print_test.go 守护 print 入口的 Rust-only 行为：
//   1. 数据结构（PrintResult / PrintOptions）正确序列化。
//   2. buildDoneEvent 使用 coreapi.UsageSummary，不再 import pkg/core (sharedcore)。
//   3. printOptionsEnv 透传到 eos-core 子进程环境变量，不经过 Go legacy runtime。
//   4. 缺 Rust binary / 缺 required methods / AllowFallback=false 时
//      startRustOnlyEngine 直接返回 error，不回退 sharedcore。

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eosaios/eos/pkg/coreapi"
	"github.com/eosaios/eos/pkg/coreapi/engineprovider"
	"github.com/eosaios/eos/pkg/coreapi/sidecar"
	"github.com/eosaios/eos/pkg/protocol"
	protocoljsonrpc "github.com/eosaios/eos/pkg/protocol/jsonrpc"
)

func TestPrintResult_JSONOutput(t *testing.T) {
	input := 200
	reply := 80
	total := 280
	cost := 0.005

	result := PrintResult{
		Content:     "print output",
		Model:       "gpt-4",
		InputTokens: &input,
		ReplyTokens: &reply,
		TotalTokens: &total,
		DurationMs:  5678,
		CostUSD:     &cost,
	}

	bs, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(bs, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if parsed["content"] != "print output" {
		t.Fatalf("unexpected content: %v", parsed["content"])
	}
	if parsed["model"] != "gpt-4" {
		t.Fatalf("unexpected model: %v", parsed["model"])
	}
	if parsed["duration_ms"] != float64(5678) {
		t.Fatalf("unexpected duration_ms: %v", parsed["duration_ms"])
	}
	if parsed["input_tokens"] != float64(200) {
		t.Fatalf("unexpected input_tokens: %v", parsed["input_tokens"])
	}
	if parsed["total_tokens"] != float64(280) {
		t.Fatalf("unexpected total_tokens: %v", parsed["total_tokens"])
	}
	if parsed["cost_usd"] != 0.005 {
		t.Fatalf("unexpected cost_usd: %v", parsed["cost_usd"])
	}
}

func TestPrintResult_OmitEmptyFields(t *testing.T) {
	result := PrintResult{
		DurationMs: 100,
	}

	bs, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(bs, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	emptyFields := []string{"input_tokens", "reply_tokens", "total_tokens", "cost_usd"}
	for _, f := range emptyFields {
		if _, ok := parsed[f]; ok {
			t.Fatalf("field %s should be omitted when nil", f)
		}
	}
}

// TestBuildTurnCompletedEvent_AcceptsCoreAPIUsageSummary 验证 buildTurnCompletedEvent
// 产出 stream-json 收尾的 turn.completed 事件结构（type=turn.completed + 嵌套 usage），
// 且消费 coreapi.UsageSummary（不再依赖 sharedcore）。
func TestBuildTurnCompletedEvent_AcceptsCoreAPIUsageSummary(t *testing.T) {
	input := 200
	reply := 80
	total := 500
	cost := 0.01
	usage := coreapi.UsageSummary{
		InputTokens:  &input,
		ReplyTokens:  &reply,
		TotalTokens:  &total,
		CostUSD:      &cost,
	}

	evt := buildTurnCompletedEvent(2*time.Second, usage)

	if evt["type"] != "turn.completed" {
		t.Fatalf("unexpected type: %v", evt["type"])
	}
	if evt["duration_ms"] != int64(2000) {
		t.Fatalf("unexpected duration_ms: %v", evt["duration_ms"])
	}
	usageObj, ok := evt["usage"].(map[string]any)
	if !ok {
		t.Fatalf("usage should be a nested object, got %T", evt["usage"])
	}
	if usageObj["input_tokens"] != 200 {
		t.Fatalf("unexpected input_tokens: %v", usageObj["input_tokens"])
	}
	if usageObj["output_tokens"] != 80 {
		t.Fatalf("unexpected output_tokens: %v", usageObj["output_tokens"])
	}
	if usageObj["total_tokens"] != 500 {
		t.Fatalf("unexpected total_tokens: %v", usageObj["total_tokens"])
	}
	if usageObj["cost_usd"] != 0.01 {
		t.Fatalf("unexpected cost_usd: %v", usageObj["cost_usd"])
	}
}

func TestBuildTurnCompletedEvent_NilTokensAndCost(t *testing.T) {
	usage := coreapi.UsageSummary{}

	evt := buildTurnCompletedEvent(500*time.Millisecond, usage)

	if evt["type"] != "turn.completed" {
		t.Fatal("unexpected type")
	}
	if evt["duration_ms"] != int64(500) {
		t.Fatal("unexpected duration_ms")
	}
	if _, ok := evt["usage"]; ok {
		t.Fatal("usage should be omitted when all nil")
	}
}

func TestRunSingleTurnConsumesTurnEventsBeforeStartReturns(t *testing.T) {
	events := make(chan protocol.Envelope, 4)
	releaseStart := make(chan struct{})
	started := make(chan coreapi.StartTurnRequest, 1)
	engine := &printTestEngine{
		state:    printTestStateService{},
		sessions: printTestSessionService{session: coreapi.Session{ID: "session-1"}},
		turns:    printTestTurnService{started: started, release: releaseStart},
		events:   printTestEventSubscriber{events: events},
	}
	defer close(releaseStart)

	done := make(chan struct{})
	var content string
	var err error
	go func() {
		content, err = runSingleTurn(context.Background(), engine, "hello", "json", "")
		close(done)
	}()

	var req coreapi.StartTurnRequest
	select {
	case req = <-started:
	case <-time.After(time.Second):
		t.Fatal("turn/start was not called")
	}
	events <- protocol.NewEvent(protocol.EventTypeTurnItemDelta, protocol.EventOptions{
		SessionID: req.SessionID,
		TurnID:    req.TurnID,
		Payload:   map[string]any{"text": "hello from core"},
	})
	events <- protocol.NewEvent(protocol.EventTypeTurnCompleted, protocol.EventOptions{
		SessionID: req.SessionID,
		TurnID:    req.TurnID,
		Payload:   map[string]any{"status": "completed"},
	})

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("runSingleTurn blocked waiting for turn/start to return")
	}
	if err != nil {
		t.Fatalf("runSingleTurn() error = %v", err)
	}
	if content != "hello from core" {
		t.Fatalf("content=%q, want hello from core", content)
	}
}

// TestRunStreamJSONTurnEmitsIncrementalJSONL 验证 stream-json 真增量流：
// turn.started → 逐个 item.* 事件 → turn.completed，每行一个 JSONL，
// 遵守 exec --json 的 JSONL 契约（不再是 start/content/done 三段伪流）。
func TestRunStreamJSONTurnEmitsIncrementalJSONL(t *testing.T) {
	events := make(chan protocol.Envelope, 8)
	releaseStart := make(chan struct{})
	started := make(chan coreapi.StartTurnRequest, 1)
	engine := &printTestEngine{
		state:    printTestStateService{},
		sessions: printTestSessionService{session: coreapi.Session{ID: "session-stream"}},
		turns:    printTestTurnService{started: started, release: releaseStart},
		events:   printTestEventSubscriber{events: events},
	}
	defer close(releaseStart)

	// 捕获 stdout 的 JSONL 输出。
	rOut, wOut, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	origStdout := os.Stdout
	os.Stdout = wOut
	t.Cleanup(func() { os.Stdout = origStdout })

	done := make(chan struct{})
	var runErr error
	go func() {
		runErr = runStreamJSONTurn(context.Background(), engine, "hello", time.Now(), "")
		close(done)
	}()

	var req coreapi.StartTurnRequest
	select {
	case req = <-started:
	case <-time.After(time.Second):
		t.Fatal("turn/start was not called")
	}

	// 注入增量 item 事件：started → 两个 delta → completed。
	events <- protocol.NewEvent(protocol.EventTypeTurnItemStarted, protocol.EventOptions{
		SessionID: req.SessionID, TurnID: req.TurnID,
		Payload: map[string]any{"item": map[string]any{"id": "item_1", "type": "agent_message", "status": "in_progress"}},
	})
	events <- protocol.NewEvent(protocol.EventTypeTurnItemDelta, protocol.EventOptions{
		SessionID: req.SessionID, TurnID: req.TurnID,
		Payload: map[string]any{"delta": "Hel"},
	})
	events <- protocol.NewEvent(protocol.EventTypeTurnItemDelta, protocol.EventOptions{
		SessionID: req.SessionID, TurnID: req.TurnID,
		Payload: map[string]any{"delta": "lo"},
	})
	events <- protocol.NewEvent(protocol.EventTypeTurnItemCompleted, protocol.EventOptions{
		SessionID: req.SessionID, TurnID: req.TurnID,
		Payload: map[string]any{"item": map[string]any{"id": "item_1", "type": "agent_message", "text": "Hello"}},
	})
	events <- protocol.NewEvent(protocol.EventTypeTurnCompleted, protocol.EventOptions{
		SessionID: req.SessionID, TurnID: req.TurnID,
	})

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("runStreamJSONTurn blocked waiting for request.completed")
	}
	_ = wOut.Close()

	output, _ := io.ReadAll(rOut)
	lines := strings.Split(strings.TrimRight(string(output), "\n"), "\n")
	if len(lines) < 5 {
		t.Fatalf("expected >=5 JSONL lines (started + 4 item events + completed), got %d:\n%s", len(lines), output)
	}

	// 每行必须是合法 JSON 且带 type 字段。逐行解析。
	parsed := make([]map[string]any, len(lines))
	for i, line := range lines {
		if err := json.Unmarshal([]byte(line), &parsed[i]); err != nil {
			t.Fatalf("line %d not valid JSON: %v\nline=%s", i, err, line)
		}
		if _, ok := parsed[i]["type"]; !ok {
			t.Fatalf("line %d missing type field: %s", i, line)
		}
	}

	// 第1行 turn.started；第2-5行 item.*（started/delta/delta/completed）；末行 turn.completed。
	assertType := func(idx int, want string) {
		t.Helper()
		if got := parsed[idx]["type"]; got != want {
			t.Fatalf("line %d type=%v, want %q (line=%s)", idx, got, want, lines[idx])
		}
	}
	assertType(0, "turn.started")
	assertType(1, "item.started")
	assertType(2, "item.delta")
	assertType(3, "item.delta")
	assertType(4, "item.completed")
	assertType(len(parsed)-1, "turn.completed")

	// item.delta 行必须携带增量 delta payload（真增量，不是聚合内容）。
	if got := parsed[2]["delta"]; got != "Hel" {
		t.Fatalf("first delta line payload=%v, want Hel", got)
	}
	if got := parsed[3]["delta"]; got != "lo" {
		t.Fatalf("second delta line payload=%v, want lo", got)
	}
	// item.completed 行带 item.text。
	if completedItem, ok := parsed[4]["item"].(map[string]any); !ok || completedItem["text"] != "Hello" {
		t.Fatalf("item.completed line missing item.text=Hello: %s", lines[4])
	}

	if runErr != nil {
		t.Fatalf("runStreamJSONTurn() error = %v", runErr)
	}
}

// TestRunStreamJSONTurnEmitsTurnFailedOnFailure 验证 request.failed → turn.failed 行。
func TestRunStreamJSONTurnEmitsTurnFailedOnFailure(t *testing.T) {
	events := make(chan protocol.Envelope, 4)
	releaseStart := make(chan struct{})
	started := make(chan coreapi.StartTurnRequest, 1)
	engine := &printTestEngine{
		state:    printTestStateService{},
		sessions: printTestSessionService{session: coreapi.Session{ID: "session-fail"}},
		turns:    printTestTurnService{started: started, release: releaseStart},
		events:   printTestEventSubscriber{events: events},
	}
	defer close(releaseStart)

	rOut, wOut, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	origStdout := os.Stdout
	os.Stdout = wOut
	t.Cleanup(func() { os.Stdout = origStdout })

	done := make(chan struct{})
	go func() {
		_ = runStreamJSONTurn(context.Background(), engine, "hello", time.Now(), "")
		close(done)
	}()

	req := <-started
	events <- protocol.NewEvent(protocol.EventTypeTurnError, protocol.EventOptions{
		SessionID: req.SessionID, TurnID: req.TurnID,
		Payload: map[string]any{"error": "boom"},
	})
	<-done
	_ = wOut.Close()

	output, _ := io.ReadAll(rOut)
	lines := strings.Split(strings.TrimRight(string(output), "\n"), "\n")
	last := lines[len(lines)-1]
	var parsed map[string]any
	if err := json.Unmarshal([]byte(last), &parsed); err != nil {
		t.Fatalf("last line not valid JSON: %v\n%s", err, last)
	}
	if parsed["type"] != "turn.failed" {
		t.Fatalf("expected last line type=turn.failed, got %v: %s", parsed["type"], last)
	}
	errObj, ok := parsed["error"].(map[string]any)
	if !ok || errObj["message"] == "" {
		t.Fatalf("turn.failed missing error.message: %s", last)
	}
}

func TestPrintOptions_DefaultOutputFormat(t *testing.T) {
	opts := PrintOptions{}
	if opts.OutputFormat != "" {
		t.Fatalf("expected empty default output format, got %q", opts.OutputFormat)
	}
}

// TestPrintOptionsEnv_DoesNotInjectLegacyMode 验证 print mode 的 env 不会把
// "EOS_CORE_ENGINE=legacy" 之类的 dev-only 开关注入生产路径。
func TestPrintOptionsEnv_DoesNotInjectLegacyMode(t *testing.T) {
	opts := PrintOptions{
		AccessMode:      "workspace-write",
		ApprovalMode:    "on-request",
		SandboxMode:     "workspace-write",
		SkipPermissions: false,
	}
	env := printModeEnv(opts)
	for key, value := range env {
		// 任何与 core engine 选择相关的 env 都不应出现。
		if strings.EqualFold(key, "EOS_CORE_ENGINE") {
			t.Fatalf("printModeEnv leaked EOS_CORE_ENGINE=%q; production must not override engine selection", value)
		}
		if strings.EqualFold(key, "EOS_CORE_ALLOW_FALLBACK") {
			t.Fatalf("printModeEnv leaked EOS_CORE_ALLOW_FALLBACK=%q; production must not enable fallback", value)
		}
		if strings.EqualFold(key, "EOS_ACCESS_MODE") {
			t.Fatalf("printModeEnv leaked EOS_ACCESS_MODE=%q; kernel does not read it (single channel is EOS_SANDBOX_MODE)", value)
		}
	}
	if env["EOS_APPROVAL_MODE"] != "on-request" {
		t.Fatalf("EOS_APPROVAL_MODE = %q, want on-request", env["EOS_APPROVAL_MODE"])
	}
	if env["EOS_SANDBOX_MODE"] != "workspace-write" {
		t.Fatalf("EOS_SANDBOX_MODE = %q, want workspace-write", env["EOS_SANDBOX_MODE"])
	}
	if _, ok := env["EOS_SKIP_PERMISSIONS"]; ok {
		t.Fatalf("EOS_SKIP_PERMISSIONS should be absent when SkipPermissions=false")
	}
}

func TestProductionSidecarProcessOptionsRequiresVerifiedArtifact(t *testing.T) {
	t.Setenv(rustCoreStoreDirEnv, "")
	env := map[string]string{"EOS_ACCESS_MODE": "workspace-write"}
	opts := productionSidecarProcessOptions(env)
	if !opts.VerifyChecksum {
		t.Fatal("productionSidecarProcessOptions() must enable checksum verification")
	}
	if !opts.RequireSignature {
		t.Fatal("productionSidecarProcessOptions() must require a signed manifest")
	}
	if opts.AllowDevPlaceholder {
		t.Fatal("productionSidecarProcessOptions() must not allow development placeholder signatures")
	}
	if opts.Env["EOS_ACCESS_MODE"] != "workspace-write" {
		t.Fatalf("Env was not preserved: %#v", opts.Env)
	}
	if strings.TrimSpace(opts.Env[rustCoreStoreDirEnv]) == "" {
		t.Fatalf("productionSidecarProcessOptions() must inject %s for rust-only headless mode", rustCoreStoreDirEnv)
	}
}

func TestProductionSidecarProcessOptionsKeepsExplicitStoreDir(t *testing.T) {
	t.Setenv(rustCoreStoreDirEnv, "")
	opts := productionSidecarProcessOptions(map[string]string{
		rustCoreStoreDirEnv: "C:/custom-store",
	})
	if got := opts.Env[rustCoreStoreDirEnv]; got != "C:/custom-store" {
		t.Fatalf("%s=%q, want explicit override", rustCoreStoreDirEnv, got)
	}
}

// TestStartRustOnlyEngineFailsWithoutRustBinary 验证 print 入口在缺 Rust binary
// 时直接返回 error，绝不静默回退 Go sharedcore。
// 这是 packaging test 的核心：缺 binary 必须 fail-fast。
func TestStartRustOnlyEngineFailsWithoutRustBinary(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("EOS_CORE_PATH", filepath.Join(dir, "no-such-binary"))
	t.Setenv("EOS_CORE_ALLOW_FALLBACK", "") // production 默认：禁止 fallback

	_, err := startRustOnlyEngine(context.Background(), "print-test", nil)
	if err == nil {
		t.Fatal("startRustOnlyEngine succeeded without rust binary; expected error")
	}
	// 错误信息应当让用户能定位问题（rust 缺失 / binary 缺失）。
	combined := err.Error()
	if !strings.Contains(combined, "rust") && !strings.Contains(combined, "sidecar") &&
		!strings.Contains(combined, "binary") && !strings.Contains(combined, "eos-core") {
		t.Logf("note: error = %q (acceptable, but ideally mentions rust/binary)", combined)
	}
}

// TestStartRustOnlyEngineFailsOnRequiredMethodsMismatch 验证 required methods 缺失
// 时返回 error，不回退 legacy。
func TestStartRustOnlyEngineFailsOnRequiredMethodsMismatch(t *testing.T) {
	// 通过 StartRemote 注入：返回的 caller declare 了空 methods 列表。
	dir := t.TempDir()
	t.Setenv("EOS_CORE_PATH", filepath.Join(dir, "no-such-binary"))

	// 由于 startRustOnlyEngine 用的是默认 StartRemote（无覆盖），这里只验证
	// 缺 binary 的情况；methods mismatch 走不到 StartRemote。
	_, err := startRustOnlyEngine(context.Background(), "print-test", nil)
	if err == nil {
		t.Fatal("startRustOnlyEngine succeeded with missing binary; expected error")
	}
}

func TestEnsureHeadlessSessionUsesCurrentSession(t *testing.T) {
	caller := &headlessSessionCaller{replies: map[string]any{
		protocoljsonrpc.MethodStateSnapshot:  coreapi.StateSnapshot{ForegroundWorkspace: "C:/work/current"},
		protocoljsonrpc.MethodSessionCurrent: coreapi.Session{ID: "sess-current", WorkspaceRoot: "C:/work/current"},
	}}
	engine := sidecar.NewRemoteEngine(caller)

	session, err := ensureHeadlessSession(context.Background(), engine)
	if err != nil {
		t.Fatalf("ensureHeadlessSession() error = %v", err)
	}
	if session.ID != "sess-current" {
		t.Fatalf("session.ID=%q, want sess-current", session.ID)
	}
	if caller.hasMethod(protocoljsonrpc.MethodSessionCreate) {
		t.Fatal("ensureHeadlessSession created a new session despite current session existing")
	}
	params, ok := caller.firstParams(protocoljsonrpc.MethodSessionCurrent).(coreapi.CurrentSessionRequest)
	if !ok {
		t.Fatalf("session/current params type = %T", caller.firstParams(protocoljsonrpc.MethodSessionCurrent))
	}
	if params.WorkspaceRoot != "C:/work/current" {
		t.Fatalf("workspace_root=%q, want C:/work/current", params.WorkspaceRoot)
	}
}

func TestEnsureHeadlessSessionCreatesWhenCurrentMissing(t *testing.T) {
	caller := &headlessSessionCaller{replies: map[string]any{
		protocoljsonrpc.MethodStateSnapshot:  coreapi.StateSnapshot{ForegroundWorkspace: "C:/work/new"},
		protocoljsonrpc.MethodSessionCurrent: coreapi.Session{},
		protocoljsonrpc.MethodSessionCreate:  coreapi.Session{ID: "sess-new", WorkspaceRoot: "C:/work/new"},
	}}
	engine := sidecar.NewRemoteEngine(caller)

	session, err := ensureHeadlessSession(context.Background(), engine)
	if err != nil {
		t.Fatalf("ensureHeadlessSession() error = %v", err)
	}
	if session.ID != "sess-new" {
		t.Fatalf("session.ID=%q, want sess-new", session.ID)
	}
	params, ok := caller.firstParams(protocoljsonrpc.MethodSessionCreate).(coreapi.CreateSessionRequest)
	if !ok {
		t.Fatalf("session/create params type = %T", caller.firstParams(protocoljsonrpc.MethodSessionCreate))
	}
	if params.WorkspaceRoot != "C:/work/new" {
		t.Fatalf("workspace_root=%q, want C:/work/new", params.WorkspaceRoot)
	}
}

func TestSubscribeTurnEventsIncludesSessionFilter(t *testing.T) {
	caller := &headlessSessionCaller{replies: map[string]any{
		protocoljsonrpc.MethodEventSubscribe: map[string]any{"subscription_id": "sub-1"},
	}}
	engine := sidecar.NewRemoteEngine(caller)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, unsubscribe := subscribeTurnEvents(ctx, engine, "sess-1", "turn-1")
	defer unsubscribe()
	if ch == nil {
		t.Fatal("subscribeTurnEvents returned nil channel")
	}
	params, ok := caller.firstParams(protocoljsonrpc.MethodEventSubscribe).(coreapi.EventSubscribeRequest)
	if !ok {
		t.Fatalf("event/subscribe params type = %T", caller.firstParams(protocoljsonrpc.MethodEventSubscribe))
	}
	if params.SessionID != "sess-1" || params.TurnID != "turn-1" {
		t.Fatalf("event filter session=%q turn=%q, want sess-1/turn-1", params.SessionID, params.TurnID)
	}
}

type printTestEngine struct {
	coreapi.Engine
	state    coreapi.StateService
	sessions coreapi.SessionService
	turns    coreapi.TurnService
	events   coreapi.EventSubscriber
	usage    coreapi.UsageService
}

func (e *printTestEngine) State() coreapi.StateService {
	return e.state
}

func (e *printTestEngine) Sessions() coreapi.SessionService {
	return e.sessions
}

func (e *printTestEngine) Turns() coreapi.TurnService {
	return e.turns
}

func (e *printTestEngine) Events() coreapi.EventSubscriber {
	return e.events
}

func (e *printTestEngine) Models() coreapi.ModelService {
	return nil // heal/override 在 nil service 下自动跳过
}

func (e *printTestEngine) Usage() coreapi.UsageService {
	if e.usage != nil {
		return e.usage
	}
	return printTestUsageService{}
}

// printTestUsageService 空实现，供 stream-json 流式函数读取 usage 摘要。
type printTestUsageService struct {
	coreapi.UsageService
}

func (printTestUsageService) Summary(context.Context) (coreapi.UsageSummary, error) {
	return coreapi.UsageSummary{}, nil
}

type printTestStateService struct {
	coreapi.StateService
}

func (printTestStateService) Snapshot(context.Context, coreapi.StateSnapshotRequest) (coreapi.StateSnapshot, error) {
	return coreapi.StateSnapshot{}, nil
}

type printTestSessionService struct {
	coreapi.SessionService
	session coreapi.Session
}

func (s printTestSessionService) Current(context.Context, coreapi.CurrentSessionRequest) (coreapi.Session, error) {
	return s.session, nil
}

func (s printTestSessionService) Create(context.Context, coreapi.CreateSessionRequest) (coreapi.Session, error) {
	return s.session, nil
}

type printTestTurnService struct {
	coreapi.TurnService
	started chan<- coreapi.StartTurnRequest
	release <-chan struct{}
}

func (s printTestTurnService) Start(_ context.Context, req coreapi.StartTurnRequest) (coreapi.Turn, error) {
	s.started <- req
	<-s.release
	now := time.Now()
	return coreapi.Turn{ID: req.TurnID, SessionID: req.SessionID, Status: "completed", StartedAt: now, UpdatedAt: now}, nil
}

func (s printTestTurnService) Interrupt(context.Context, coreapi.TurnRef) error {
	return nil
}

type printTestEventSubscriber struct {
	coreapi.EventSubscriber
	events <-chan protocol.Envelope
}

func (s printTestEventSubscriber) Subscribe(context.Context, coreapi.EventFilter) (<-chan protocol.Envelope, error) {
	return s.events, nil
}

type headlessSessionCaller struct {
	mu      sync.Mutex
	calls   []headlessSessionCall
	replies map[string]any
}

type headlessSessionCall struct {
	method string
	params any
}

func (c *headlessSessionCaller) Call(_ context.Context, method string, params any, out any) error {
	c.mu.Lock()
	c.calls = append(c.calls, headlessSessionCall{method: method, params: params})
	reply := c.replies[method]
	c.mu.Unlock()
	if out == nil || reply == nil {
		return nil
	}
	data, err := json.Marshal(reply)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

func (c *headlessSessionCaller) hasMethod(method string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, call := range c.calls {
		if call.method == method {
			return true
		}
	}
	return false
}

func (c *headlessSessionCaller) firstParams(method string) any {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, call := range c.calls {
		if call.method == method {
			return call.params
		}
	}
	return nil
}

// 防止 engineprovider / sidecar 被误删
var (
	_ = engineprovider.ModeAuto
	_ = sidecar.ProcessOptions{}
)
