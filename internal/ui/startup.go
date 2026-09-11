package ui

// Copyright (c) 2026 EOSAIOS
// SPDX-License-Identifier: EOS-NCL-1.1
// 本文件基于 EOS 非商用许可证 v1.1 发布，详见 LICENSE。
// 商业使用请联系版权人获得商业授权。

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/eosaios/eos/internal/config"
	"github.com/eosaios/eos/internal/i18n"
	sidecarclient "github.com/eosaios/eos/pkg/coreapi/sidecar/client"

	tea "charm.land/bubbletea/v2"
)

// TUIOptions holds CLI-provided overrides for the interactive TUI
type TUIOptions struct {
	SessionID       string   // --continue ("latest") or --resume ("session-id")
	ModelOverride   string   // --model
	MaxTurns        int      // --max-turns
	AllowedTools    []string // --allowed-tools
	DisallowedTools []string // --disallowed-tools
	AccessMode      string   // --access-mode
	ApprovalMode    string   // --approval-mode
	SandboxMode     string   // --sandbox-mode legacy alias
	SkipPermissions bool     // --dangerously-skip-permissions
}

// T 是 i18n.T 的别名，用于简化调用
func T(key, lang string, args ...interface{}) string {
	return i18n.T(key, lang, args...)
}

// applyTUIStartupModel 启动期模型处理：
//   - 自愈当前会话的无效 model_name 覆盖（历史桌面端写入的目录 label 会让
//     会话每次对话 NotFound，这里归一化或清除）；
//   - --model 真正生效：EOS_MODEL_OVERRIDE 环境变量内核并不消费，这里显式
//     走归一化解析 + SelectModelForCurrentContext。输入支持条目名/模型 ID/
//     套餐模型 label（如 "MiniMax M3"）。
//
// 任一步失败只提示不阻断进入 TUI。
func applyTUIStartupModel(m *AppModel, override string) {
	if m == nil || m.adapter == nil {
		return
	}
	ctx := context.Background()
	if note := m.adapter.HealCurrentSessionModel(ctx); note != "" {
		m.appendSystem(note, "warning")
	}
	override = strings.TrimSpace(override)
	if override == "" {
		return
	}
	res, err := m.adapter.ResolveModelInput(ctx, override)
	if err != nil {
		m.appendSystem(fmt.Sprintf("--model: %v", err), "error")
		return
	}
	if res.NeedsPlanSwitch {
		if err := m.adapter.SwitchPlanModel(ctx, res.EntryName, res.PlanModelID); err != nil {
			m.appendSystem(fmt.Sprintf("--model: %v", err), "error")
			return
		}
	}
	if _, err := m.adapter.SelectModelForCurrentContext(ctx, res.EntryName); err != nil {
		m.appendSystem(fmt.Sprintf("--model: %v", err), "error")
		return
	}
	m.appendSystem(fmt.Sprintf("--model: %s", res.EntryName), "success")
}

// StartInteractiveTUIWithOptions 启动 TUI 入口。
// 生产路径：直接启动 eos-core --app-server --stdio sidecar 子进程，
// 用 sidecarclient.Client + NewAppModelFromCoreClient 构造 AppModel。
//
// StartCoreClient 是显式注入点，测试可通过 StartCoreClientForTest 替换为 stub。
var StartCoreClient = func(ctx context.Context, opts sidecarclient.Options) (*sidecarclient.Client, error) {
	return sidecarclient.Start(ctx, opts)
}

func StartInteractiveTUIWithOptions(opts TUIOptions) {
	root, _ := os.Getwd()
	if strings.TrimSpace(root) == "" {
		root = "."
	}
	rememberKnownWorkspace(root, true)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// stderr 落盘 writer 持有日志文件句柄，所有退出路径显式 Close。
	stderrWriter := newSidecarStderrWriter()

	client, err := StartCoreClient(ctx, tuiSidecarClientOptions(opts, stderrWriter))
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to start eos-core sidecar: %v\n", err)
		stderrWriter.Close()
		os.Exit(1) // 此时尚无 client/defer 需要履行
	}
	defer func() {
		_ = client.Close()
		stderrWriter.Close()
	}()

	if strings.TrimSpace(opts.SessionID) != "" {
		slog.Info("ui.startup.session", "session_id", opts.SessionID)
	}

	m := NewAppModelFromCoreClient(client, strings.TrimSpace(opts.SessionID))
	applyTUIStartupModel(m, opts.ModelOverride)
	// 启动模式快照同步：沙箱/审批裁决已由 env 在内核生效，这里只把解析后的
	// 模式对齐到 UI 快照，避免 /status、/permissions 回显空值（壳层与内核
	// 感知脱节）。失败只告警不阻断进入 TUI。
	if m != nil && m.adapter != nil {
		mode := strings.TrimSpace(opts.SandboxMode)
		if mode == "" {
			mode = strings.TrimSpace(opts.AccessMode)
		}
		if mode != "" || strings.TrimSpace(opts.ApprovalMode) != "" {
			if err := m.adapter.SyncModeSnapshots(ctx, mode, mode, opts.ApprovalMode); err != nil {
				slog.Warn("ui.startup.permissions.snapshot_sync_failed", "sandbox_mode", mode, "approval_mode", opts.ApprovalMode, "error", err)
			}
		}
	}
	slog.Info("ui.startup.app.run")
	if _, err := tea.NewProgram(m).Run(); err != nil {
		slog.Error("ui.startup.app.run.error", "error", err)
		fmt.Fprintf(os.Stderr, "\nError: Application failed to start: %v\n", err)
		fmt.Fprintf(os.Stderr, "Please check the logs for more details.\n")
		// os.Exit 不执行 defer——显式清理后再退出，避免 sidecar 僵尸进程与句柄泄漏。
		_ = client.Close()
		stderrWriter.Close()
		os.Exit(1)
	}
	slog.Info("ui.startup.app.stopped")
	fmt.Println(T("goodbye.emoji", "zh") + " " + T("goodbye.message", "zh"))
	fmt.Println(T("goodbye.ended", "zh"))
}

func tuiSidecarClientOptions(opts TUIOptions, stderrWriter io.Writer) sidecarclient.Options {
	return sidecarclient.Options{
		Env:              tuiOptionEnv(opts),
		Stderr:           stderrWriter,
		VerifyChecksum:   true,
		RequireSignature: true,
	}
}

// tuiOptionEnv 把 TUIOptions 透传到 eos-core 子进程环境变量。
// 字段名与 eos-core-protocol 的 ENV_* 常量保持一致。
func tuiOptionEnv(opts TUIOptions) map[string]string {
	env := map[string]string{}
	// Production TUI never inherits a fake provider selection from the parent shell.
	env["EOS_MODEL_PROVIDER"] = ""
	// 日志级别跟随用户环境变量；默认 info——debug 会把 API key 片段/用户输入
	// 等敏感内容落盘 eos-core.log，仅排障时由用户显式开启（AGENTS.md：环境
	// 变量允许默认值，但需显式说明安全性）。
	if v := strings.TrimSpace(os.Getenv("EOS_LOG_LEVEL")); v != "" {
		env["EOS_LOG_LEVEL"] = v
	} else {
		env["EOS_LOG_LEVEL"] = "info"
	}
	if storeDir := defaultRustCoreStoreDir(); storeDir != "" {
		env["EOS_CORE_STORE_DIR"] = storeDir
	}
	if v := strings.TrimSpace(opts.ModelOverride); v != "" {
		env["EOS_MODEL_OVERRIDE"] = v
	}
	if opts.MaxTurns > 0 {
		env["EOS_MAX_TURNS"] = fmt.Sprintf("%d", opts.MaxTurns)
	}
	if len(opts.AllowedTools) > 0 {
		env["EOS_ALLOWED_TOOLS"] = strings.Join(opts.AllowedTools, ",")
	}
	if len(opts.DisallowedTools) > 0 {
		env["EOS_DISALLOWED_TOOLS"] = strings.Join(opts.DisallowedTools, ",")
	}
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
	// workspace-write 沙箱需要至少一个可写根；不透传工作区根时 sidecar 的沙箱策略
	// workspace_root 为空，会导致工作区内所有写操作被拒（审批通过后仍拒绝并触发 turn 恢复卡死）。
	if cwd, err := os.Getwd(); err == nil {
		if ws := strings.TrimSpace(cwd); ws != "" {
			env["EOS_WORKSPACE_ROOT"] = ws
			env["EOS_SANDBOX_WORKSPACE_ROOT"] = ws
		}
	}
	if opts.SkipPermissions {
		// 双轴（approval=Never + sandbox=DangerFullAccess）由内核 bin 侧读
		// EOS_SKIP_PERMISSIONS 后用 permission_enter_full_access 单一真相源派生。
		// 这里必须清掉可能由 flag 默认值（如 --sandbox-mode=workspace）带入的
		// EOS_APPROVAL_MODE/EOS_SANDBOX_MODE，否则内核会因 skip 与显式 mode 共存
		// 而 fail-fast（AGENTS.md §3：壳层不做业务裁决）。
		env["EOS_SKIP_PERMISSIONS"] = "1"
		delete(env, "EOS_APPROVAL_MODE")
		delete(env, "EOS_SANDBOX_MODE")
	}
	return env
}

func defaultRustCoreStoreDir() string {
	if dir, err := os.UserHomeDir(); err == nil && strings.TrimSpace(dir) != "" {
		return filepath.Join(dir, ".eos", "core")
	}
	return ""
}

// sidecarStderrWriter 把 eos-core sidecar 的 stderr（tracing 日志与 panic）
// 落盘到日志目录。f 为 nil 时降级为丢弃。持有进程生命周期长的日志文件
// 句柄，由启动路径的退出清理显式 Close。
type sidecarStderrWriter struct {
	f *os.File
}

func (w *sidecarStderrWriter) Write(p []byte) (int, error) {
	if w.f == nil {
		return len(p), nil
	}
	return w.f.Write(p)
}

// Close 幂等；打开失败的降级实例（f==nil）为 no-op。
func (w *sidecarStderrWriter) Close() {
	if w.f != nil {
		_ = w.f.Close()
		w.f = nil
	}
}

// newSidecarStderrWriter 打开落盘文件；目录/文件打开失败时返回降级实例
// （丢弃写入），不阻断 TUI 启动。
func newSidecarStderrWriter() *sidecarStderrWriter {
	dir := filepath.Join(config.ConfiguredLogDir(), "core")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		slog.Error("ui.sidecar.stderr.log_dir.error", "error", err)
		return &sidecarStderrWriter{}
	}
	f, err := os.OpenFile(filepath.Join(dir, "eos-core.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		slog.Error("ui.sidecar.stderr.open.error", "error", err)
		return &sidecarStderrWriter{}
	}
	return &sidecarStderrWriter{f: f}
}
