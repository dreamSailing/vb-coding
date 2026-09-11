package cli

// Copyright (c) 2026 EOSAIOS
// SPDX-License-Identifier: EOS-NCL-1.1
// 本文件基于 EOS 非商用许可证 v1.1 发布，详见 LICENSE。
// 商业使用请联系版权人获得商业授权。

// print.go 提供 headless 一次性查询入口。
//
// 引擎为 Rust-only：通过 pkg/coreapi/sidecar + engineprovider 启动
// eos-core --app-server --stdio 子进程。失败时直接返回 error，不存在
// Go 内核回退路径。
//
// 旧的 pkg/core.Runtime（Eino/Go 内核）路径已整体退役删除。

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/eosaios/eos/internal/ai"
	"github.com/eosaios/eos/internal/config"
	"github.com/eosaios/eos/pkg/coreapi"
	"github.com/eosaios/eos/pkg/coreapi/engineprovider"
	"github.com/eosaios/eos/pkg/coreapi/sidecar"
	sidecarclient "github.com/eosaios/eos/pkg/coreapi/sidecar/client"
	"github.com/eosaios/eos/pkg/protocol"
)

const rustCoreStoreDirEnv = "EOS_CORE_STORE_DIR"

// PrintOptions holds options for headless print mode
type PrintOptions struct {
	Query           string
	OutputFormat    string // "text", "json", "stream-json"
	AccessMode      string
	ApprovalMode    string
	SandboxMode     string
	SkipPermissions bool
	Workspace       string // optional workspace path; empty = use default
	ModelOverride   string // --model：条目名/模型 ID/套餐模型 label，空=用当前上下文模型
}

// PrintResult holds the result of a print mode execution
type PrintResult struct {
	Content     string   `json:"content"`
	Model       string   `json:"model"`
	InputTokens *int     `json:"input_tokens,omitempty"`
	ReplyTokens *int     `json:"reply_tokens,omitempty"`
	TotalTokens *int     `json:"total_tokens,omitempty"`
	DurationMs  int      `json:"duration_ms"`
	CostUSD     *float64 `json:"cost_usd,omitempty"`
}

// RunPrintMode executes a single query in headless mode and outputs the result.
//
// Production path: starts an eos-core sidecar via engineprovider.Select (ModeAuto
// → Rust-only). On Rust startup failure or missing required methods, returns
// an error; does NOT silently fall back to Go sharedcore.
func RunPrintMode(opts PrintOptions) error {
	if opts.OutputFormat == "" {
		opts.OutputFormat = "text"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	startedAt := time.Now()
	selected, err := startRustOnlyEngine(ctx, "print", printModeEnv(opts))
	if err != nil {
		return err
	}
	defer selected.Close()

	engine := selected.Engine
	if applyErr := applyPrintModeEnv(ctx, engine, opts); applyErr != nil {
		return applyErr
	}

	// stream-json 走真增量流式：订阅 turn 事件流，每个 item.* / 生命周期事件
	// 原样吐出一行 JSONL（对齐 codex exec --json 的 JSONL 契约），
	// 不走 runSingleTurn 的"累积完整内容后统一输出"伪流。
	if strings.EqualFold(strings.TrimSpace(opts.OutputFormat), "stream-json") {
		if err := runStreamJSONTurn(ctx, engine, opts.Query, startedAt, opts.ModelOverride); err != nil {
			writePrintError(opts.OutputFormat, err)
			return err
		}
		return nil
	}

	content, err := runSingleTurn(ctx, engine, opts.Query, opts.OutputFormat, opts.ModelOverride)
	if err != nil {
		writePrintError(opts.OutputFormat, err)
		return err
	}

	usage, err := engine.Usage().Summary(ctx)
	if err != nil {
		usage = coreapi.UsageSummary{}
	}
	modelName, _ := resolveActiveModelName(ctx, engine)

	result := PrintResult{
		Content:     content,
		Model:       modelName,
		InputTokens: usage.InputTokens,
		ReplyTokens: usage.ReplyTokens,
		TotalTokens: usage.TotalTokens,
		DurationMs:  int(time.Since(startedAt).Milliseconds()),
		CostUSD:     usage.CostUSD,
	}
	return emitPrintResult(opts.OutputFormat, result, startedAt, usage, modelName)
}

// printModeEnv 透传 headless mode 配置到 eos-core 子进程环境变量。
// 与 internal/ui/adapter.tuiOptionEnv 字段保持一致。
func printModeEnv(opts PrintOptions) map[string]string {
	env := map[string]string{}
	// 沙箱轴只经 EOS_SANDBOX_MODE 单通道下发（内核不读 EOS_ACCESS_MODE）；
	// AccessMode/SandboxMode 已由 resolveModeConfig 归一为内核 kebab-case 规范值。
	if v := strings.TrimSpace(opts.SandboxMode); v != "" {
		env["EOS_SANDBOX_MODE"] = v
	} else if v := strings.TrimSpace(opts.AccessMode); v != "" {
		env["EOS_SANDBOX_MODE"] = v
	}
	if v := strings.TrimSpace(opts.ApprovalMode); v != "" {
		env["EOS_APPROVAL_MODE"] = v
	}
	if ws := strings.TrimSpace(opts.Workspace); ws != "" {
		env["EOS_WORKSPACE_ROOT"] = ws
		env["EOS_SANDBOX_WORKSPACE_ROOT"] = ws
	} else if cwd, err := os.Getwd(); err == nil {
		if trimmedCWD := strings.TrimSpace(cwd); trimmedCWD != "" {
			env["EOS_WORKSPACE_ROOT"] = trimmedCWD
			env["EOS_SANDBOX_WORKSPACE_ROOT"] = trimmedCWD
		}
	}
	if opts.SkipPermissions {
		env["EOS_SKIP_PERMISSIONS"] = "1"
	}
	return env
}

// applyPrintModeEnv 把 startup options 推送到已 handshake 的 engine。
// 工作区切换 / execution mode 都走 coreapi.Workspaces / Modes 接口。
func applyPrintModeEnv(ctx context.Context, engine coreapi.Engine, opts PrintOptions) error {
	if engine == nil {
		return fmt.Errorf("core engine unavailable")
	}
	if ws := strings.TrimSpace(opts.Workspace); ws != "" {
		if err := engine.Workspaces().SetForeground(ctx, coreapi.WorkspacePathRequest{Path: ws}); err != nil {
			return fmt.Errorf("set foreground workspace: %w", err)
		}
	}
	return nil
}

func writePrintError(format string, err error) {
	if format == "json" {
		bs, _ := json.Marshal(map[string]string{"error": err.Error()})
		fmt.Fprintln(os.Stdout, string(bs))
		return
	}
	fmt.Fprintln(os.Stderr, "Error:", err.Error())
}

func emitPrintResult(format string, result PrintResult, started time.Time, usage coreapi.UsageSummary, modelName string) error {
	switch format {
	case "json":
		bs, err := json.Marshal(result)
		if err != nil {
			return err
		}
		fmt.Fprintln(os.Stdout, string(bs))
	default:
		// text 格式：runSingleTurn 已在收到 text_delta 时实时打印到 stdout，
		// 这里只补一行换行 + 元数据页脚到 stderr，避免重复输出正文。
		fmt.Fprintln(os.Stdout)
		parts := []string{
			fmt.Sprintf("Model: %s", modelName),
			fmt.Sprintf("Duration: %v", time.Since(started).Round(time.Millisecond)),
		}
		if usage.TotalTokens != nil {
			parts = append(parts, fmt.Sprintf("Tokens: %d", *usage.TotalTokens))
		}
		if usage.CostUSD != nil {
			parts = append(parts, fmt.Sprintf("Cost: $%.6f", *usage.CostUSD))
		}
		fmt.Fprintf(os.Stderr, "\n---\n%s\n", strings.Join(parts, " | "))
	}
	return nil
}

// buildTurnCompletedEvent 构造 stream-json 的收尾行（对齐 codex exec --json 的
// turn.completed 事件）。usage 以嵌套对象呈现，与 codex 的
// {"usage":{"input_tokens":...,"output_tokens":...}} 契约一致。
func buildTurnCompletedEvent(elapsed time.Duration, usage coreapi.UsageSummary) map[string]any {
	u := map[string]any{}
	if usage.InputTokens != nil {
		u["input_tokens"] = *usage.InputTokens
	}
	if usage.ReplyTokens != nil {
		u["output_tokens"] = *usage.ReplyTokens
	}
	if usage.TotalTokens != nil {
		u["total_tokens"] = *usage.TotalTokens
	}
	if usage.CostUSD != nil {
		u["cost_usd"] = *usage.CostUSD
	}
	event := map[string]any{
		"type":        "turn.completed",
		"duration_ms": elapsed.Milliseconds(),
	}
	if len(u) > 0 {
		event["usage"] = u
	}
	return event
}

// --- shared helpers used by print.go and exec.go ---

// startRustOnlyEngine 启动一次 eos-core sidecar。引擎已收敛为 Rust-only：
// 失败即返回 error，不再有 Go 内核回退路径。
func startRustOnlyEngine(ctx context.Context, callerLabel string, env map[string]string) (engineprovider.Selection, error) {
	_ = callerLabel
	processOpts := productionSidecarProcessOptions(env)
	selection, err := engineprovider.Select(ctx, engineprovider.Options{
		Mode:            engineprovider.ModeAuto,
		Sidecar:         processOpts,
		RequiredMethods: sidecarclient.RequiredMethods,
	})
	if err != nil {
		return engineprovider.Selection{}, fmt.Errorf("start eos-core sidecar (rust-only): %w", err)
	}
	return selection, nil
}

func productionSidecarProcessOptions(env map[string]string) sidecar.ProcessOptions {
	nextEnv := make(map[string]string, len(env)+1)
	maps.Copy(nextEnv, env)
	if value, ok := nextEnv[rustCoreStoreDirEnv]; !ok || strings.TrimSpace(value) == "" {
		if value, ok := os.LookupEnv(rustCoreStoreDirEnv); !ok || strings.TrimSpace(value) == "" {
			if dir := headlessRustCoreStoreDir(); dir != "" {
				nextEnv[rustCoreStoreDirEnv] = dir
			}
		}
	}
	return sidecar.ProcessOptions{
		Env:              nextEnv,
		VerifyChecksum:   true,
		RequireSignature: true,
		Stderr:           coreStderrWriter(),
	}
}

func coreStderrWriter() io.Writer {
	dir := filepath.Join(config.ConfiguredLogDir(), "core")
	_ = os.MkdirAll(dir, 0755)
	f, err := os.OpenFile(filepath.Join(dir, "eos-core.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return io.Discard
	}
	return f
}

func headlessRustCoreStoreDir() string {
	if dir, err := os.UserHomeDir(); err == nil && strings.TrimSpace(dir) != "" {
		return filepath.Join(dir, ".eos", "core")
	}
	return ""
}

// runSingleTurn 启动一个 turn，订阅事件流直到 request.done/failed，返回 final 文本。
//
// 对 text 输出格式，每个 text_delta 实时写入 stdout（逐 chunk 涌现，对齐 codex 体验）；
// json/stream-json 仍只在 turn 结束后输出完整结构，保持机器可读契约不变。
func runSingleTurn(ctx context.Context, engine coreapi.Engine, query, outputFormat, modelOverride string) (string, error) {
	if engine == nil {
		return "", fmt.Errorf("core engine unavailable")
	}
	if strings.TrimSpace(query) == "" {
		return "", fmt.Errorf("query is required")
	}
	session, err := ensureHeadlessSession(ctx, engine)
	if err != nil {
		return "", err
	}
	if err := applyModelOverride(ctx, engine, session, modelOverride); err != nil {
		return "", err
	}

	turnID := fmt.Sprintf("cli_turn_%d", time.Now().UnixNano())
	events, unsubscribe := subscribeTurnEvents(ctx, engine, session.ID, turnID)
	defer unsubscribe()

	startDone := startTurnAsync(ctx, engine, coreapi.StartTurnRequest{
		SessionID: session.ID,
		TurnID:    turnID,
		Input:     query,
	})

	// text 格式实时打印 delta；其它格式只累积，turn 结束后统一输出。
	streamLive := strings.EqualFold(strings.TrimSpace(outputFormat), "text")
	var content string
	eventsCh := events
	startDoneCh := startDone
	for {
		select {
		case <-ctx.Done():
			return content, ctx.Err()
		case result := <-startDoneCh:
			startDoneCh = nil
			if result.err != nil {
				return content, result.err
			}
			// turn/start is non-blocking; the turn runs on a background thread
			// and delivers results via events (item.delta / item.completed /
			// request.completed). No fallback timer needed — request.done or
			// request.failed is the termination signal.
		case ev, ok := <-eventsCh:
			if !ok {
				eventsCh = nil
				if startDoneCh == nil {
					return content, nil
				}
				continue
			}
			eventType, payload, rawEventType := normalizePrintEvent(ev)
			switch eventType {
			case protocol.EventTypeItemDelta:
				if dt := firstNonEmpty("", payload, "delta_type"); dt != "" && dt != "text" {
					continue // skip reasoning/tool_args deltas
				}
				text := firstNonEmpty("", payload, "delta", "text", "message")
				content += text
				if streamLive && text != "" {
					fmt.Fprint(os.Stdout, text)
				}
			case protocol.EventTypeItemCompleted:
				if item, ok := payload["item"].(map[string]any); ok {
					if k, _ := item["kind"].(string); k == "agent_message" {
						if text, _ := item["text"].(string); text != "" {
							content = text
						}
					}
				}
			case protocol.EventTypeTextFinal:
				if text := firstNonEmpty("", payload, "text", "message"); text != "" {
					content = text
				}
			case protocol.EventTypeRequestDone:
				return content, nil
			case protocol.EventTypeRequestFailed:
				return content, fmt.Errorf("%s", printFailureMessage(rawEventType, payload))
			}
		}
	}
}

// runStreamJSONTurn 以真增量 JSONL 流式输出一个 turn（对齐 codex exec --json）。
//
// 订阅 turn 事件流，每个 item.* / 生命周期事件原样吐出一行 JSON 到 stdout：
//   - turn.started / item.started / item.delta / item.completed 等事件 →
//     {"type":<事件类型>, ...payload}（payload 含 item/delta/text 等字段）
//   - request.completed (turn 结束) → turn.completed（带 usage，由 buildTurnCompletedEvent 构造）
//   - request.failed → turn.failed（带 error.message）
//
// 与 runSingleTurn 的区别：后者把所有 delta 累积成完整文本在 turn 结束后一次性返回
// （伪流）；本函数逐事件实时输出，可被 jq -c 等 JSONL 管道消费。
func runStreamJSONTurn(ctx context.Context, engine coreapi.Engine, query string, startedAt time.Time, modelOverride string) error {
	if engine == nil {
		return fmt.Errorf("core engine unavailable")
	}
	if strings.TrimSpace(query) == "" {
		return fmt.Errorf("query is required")
	}
	session, err := ensureHeadlessSession(ctx, engine)
	if err != nil {
		return err
	}
	if err := applyModelOverride(ctx, engine, session, modelOverride); err != nil {
		return err
	}

	turnID := fmt.Sprintf("cli_stream_%d", time.Now().UnixNano())
	events, unsubscribe := subscribeTurnEvents(ctx, engine, session.ID, turnID)
	defer unsubscribe()

	startDone := startTurnAsync(ctx, engine, coreapi.StartTurnRequest{
		SessionID: session.ID,
		TurnID:    turnID,
		Input:     query,
	})

	writeJSONL := func(event map[string]any) {
		if bs, err := json.Marshal(event); err == nil {
			fmt.Fprintln(os.Stdout, string(bs))
		}
	}
	// turn.started 作为流的第一行（对齐 codex 的 turn.started 生命周期标记）。
	writeJSONL(map[string]any{"type": "turn.started", "session_id": session.ID, "turn_id": turnID})

	eventsCh := events
	startDoneCh := startDone
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case result := <-startDoneCh:
			startDoneCh = nil
			if result.err != nil {
				writeJSONL(map[string]any{"type": "turn.failed", "error": map[string]string{"message": result.err.Error()}})
				return result.err
			}
			// turn/start 非阻塞；靠 request.completed/failed 终止。
		case ev, ok := <-eventsCh:
			if !ok {
				eventsCh = nil
				if startDoneCh == nil {
					return nil
				}
				continue
			}
			eventType, payload, _ := normalizePrintEvent(ev)
			switch eventType {
			case protocol.EventTypeItemStarted, protocol.EventTypeItemDelta, protocol.EventTypeItemCompleted:
				// item.* 事件原样透传：type + 完整 payload（含 item/delta/text 等字段）。
				writeJSONL(buildItemEvent(string(eventType), payload))
			case protocol.EventTypeRequestDone:
				usage, _ := engine.Usage().Summary(ctx)
				writeJSONL(buildTurnCompletedEvent(time.Since(startedAt), usage))
				return nil
			case protocol.EventTypeRequestFailed:
				msg := printFailureMessage(ev.EventType, payload)
				writeJSONL(map[string]any{"type": "turn.failed", "error": map[string]string{"message": msg}})
				return fmt.Errorf("%s", msg)
			}
		}
	}
}

// buildItemEvent 把 item.* 事件的 payload 装配成 JSONL 行：顶层 type + payload 全字段。
// payload 里的 original_event_type / event_id / session_id / turn_id 等归一化元数据
// 一并保留，便于下游管道关联事件与 item。
func buildItemEvent(eventType string, payload map[string]any) map[string]any {
	event := make(map[string]any, len(payload)+1)
	for k, v := range payload {
		event[k] = v
	}
	event["type"] = eventType
	return event
}

type asyncTurnStartResult struct {
	turn coreapi.Turn
	err  error
}

func startTurnAsync(ctx context.Context, engine coreapi.Engine, req coreapi.StartTurnRequest) <-chan asyncTurnStartResult {
	ch := make(chan asyncTurnStartResult, 1)
	go func() {
		if engine == nil {
			ch <- asyncTurnStartResult{err: fmt.Errorf("core engine unavailable")}
			return
		}
		// 请求级记忆注入开关：headless CLI 与 TUI 共用同一全局配置
		//（~/.eos.json memory_injection_enabled，默认开），壳层只透传，
		// 注入裁决在内核。
		cfg, _ := config.Load()
		useMemory := config.MemoryInjectionEnabled(&cfg)
		req.UseMemory = &useMemory
		turn, err := engine.Turns().Start(ctx, req)
		ch <- asyncTurnStartResult{turn: turn, err: err}
	}()
	return ch
}

func ensureHeadlessSession(ctx context.Context, engine coreapi.Engine) (coreapi.Session, error) {
	if engine == nil {
		return coreapi.Session{}, fmt.Errorf("core engine unavailable")
	}
	workspaceRoot := headlessWorkspaceRoot(ctx, engine)
	session, err := engine.Sessions().Current(ctx, coreapi.CurrentSessionRequest{WorkspaceRoot: workspaceRoot})
	if err == nil && strings.TrimSpace(session.ID) != "" {
		// 会话自愈：历史版本（桌面端 GUI）曾把目录 label（如 "MiniMax M3"）
		// 写进 model_name，内核按条目名精确匹配会 NotFound，每次对话必炸。
		// 这里归一化修复或清除无效覆盖，避免复用旧会话时阻断。
		if note := ai.HealSessionModelOverride(ctx, engine.Models(), session); note != "" {
			slog.Warn("print.session.model.healed", "session_id", session.ID, "note", note)
		}
		return session, nil
	}
	session, err = engine.Sessions().Create(ctx, coreapi.CreateSessionRequest{
		WorkspaceRoot: workspaceRoot,
		Title:         "Headless session",
		Metadata:      map[string]any{"source": "cli"},
	})
	if err != nil {
		return coreapi.Session{}, fmt.Errorf("create headless session: %w", err)
	}
	if strings.TrimSpace(session.ID) == "" {
		return coreapi.Session{}, fmt.Errorf("create headless session returned empty id")
	}
	return session, nil
}

// applyModelOverride 把 --model 的值解析并写入会话模型覆盖。
// EOS_MODEL_OVERRIDE 环境变量内核并不消费（历史遗留），--model 必须走
// model/session/set 才真正生效；输入支持条目名/模型 ID/套餐模型 label。
func applyModelOverride(ctx context.Context, engine coreapi.Engine, session coreapi.Session, override string) error {
	override = strings.TrimSpace(override)
	if override == "" {
		return nil
	}
	entries, err := engine.Models().List(ctx)
	if err != nil {
		return fmt.Errorf("--model: list models: %w", err)
	}
	var catalog *coreapi.ModelCatalogState
	if c, catErr := engine.Models().Catalog(ctx); catErr == nil {
		catalog = &c
	}
	res, err := ai.ResolveModelInput(override, entries, catalog)
	if err != nil {
		return fmt.Errorf("--model: %v", err)
	}
	if res.NeedsPlanSwitch {
		// 与 adapter.SaveModel 相同的明文 key 维护：内核保存会把 eos.json
		// api_key 覆写成 masked，旧内核加载时会把 masked 当真实 key（401）。
		plaintext := config.SnapshotPlaintextAPIKeys()
		if err := engine.Models().Save(ctx, coreapi.ModelSaveRequest{
			OriginalName: res.EntryName,
			Mode:         "preset",
			ProviderID:   res.ProviderID,
			PresetID:     res.PresetID,
			Name:         res.EntryName,
			Model:        res.PlanModelID,
		}); err != nil {
			return fmt.Errorf("--model: switch plan model: %w", err)
		}
		config.RestorePlaintextAPIKeys(plaintext)
	}
	if err := engine.Models().SetSession(ctx, coreapi.SetSessionModelRequest{
		SessionID: session.ID,
		ModelName: res.EntryName,
	}); err != nil {
		return fmt.Errorf("--model: set session model: %w", err)
	}
	return nil
}

func headlessWorkspaceRoot(ctx context.Context, engine coreapi.Engine) string {
	if engine != nil {
		if snapshot, err := engine.State().Snapshot(ctx, coreapi.StateSnapshotRequest{}); err == nil {
			if root := strings.TrimSpace(snapshot.ForegroundWorkspace); root != "" {
				return root
			}
		}
	}
	if cwd, err := os.Getwd(); err == nil {
		return cwd
	}
	return ""
}

// subscribeTurnEvents 复用 engine 的事件总线，filter 当前 session + turn ID。
// 返回的 channel 在 sidecar 进程退出或 ctx cancel 时自动关闭。
func subscribeTurnEvents(ctx context.Context, engine coreapi.Engine, sessionID, turnID string) (<-chan protocol.Envelope, func()) {
	noop := func() {}
	if engine == nil {
		ch := make(chan protocol.Envelope)
		close(ch)
		return ch, noop
	}
	ch, err := engine.Events().Subscribe(ctx, coreapi.EventFilter{SessionID: sessionID, TurnID: turnID})
	if err != nil {
		// Subscribe 失败：返回已关闭的 channel，调用方在 turn.Start 失败时能直接感知。
		closed := make(chan protocol.Envelope)
		close(closed)
		return closed, noop
	}
	return ch, noop
}

func resolveActiveModelName(ctx context.Context, engine coreapi.Engine) (string, error) {
	if engine == nil {
		return "", nil
	}
	items, err := engine.Models().List(ctx)
	if err != nil {
		return "", err
	}
	for _, item := range items {
		if item.Active {
			return strings.TrimSpace(item.Model), nil
		}
	}
	return "", nil
}

func firstNonEmpty(fallback string, data map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := data[k].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return strings.TrimSpace(fallback)
}

func normalizePrintEvent(ev protocol.Envelope) (protocol.EventType, map[string]any, protocol.EventType) {
	payload := protocol.ClonePayload(ev.Payload)
	if payload == nil {
		payload = map[string]any{}
	}
	rawEventType := ev.EventType
	eventType := protocol.NormalizeEventType(rawEventType)
	if rawEventType != eventType {
		payload["original_event_type"] = string(rawEventType)
	}
	if eventType == protocol.EventTypeRequestFailed && firstNonEmpty("", payload, "error", "summary", "message", "text") == "" {
		switch rawEventType {
		case protocol.EventTypeTurnCancelled:
			payload["error"] = "request cancelled"
		case protocol.EventTypeTurnInterrupted:
			payload["error"] = "request interrupted"
		}
	}
	return eventType, payload, rawEventType
}

func printFailureMessage(rawEventType protocol.EventType, payload map[string]any) string {
	msg := firstNonEmpty("", payload, "error", "summary", "message", "text")
	if msg != "" {
		return msg
	}
	switch rawEventType {
	case protocol.EventTypeTurnCancelled:
		return "request cancelled"
	case protocol.EventTypeTurnInterrupted:
		return "request interrupted"
	default:
		return "request failed"
	}
}
