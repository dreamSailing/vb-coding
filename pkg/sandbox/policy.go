// Package sandbox 提供 EOS 壳层（eos-cli / eos-app）与 Rust 内核之间传递
// 沙箱策略所需的 DTO。
//
// 命令/写入/网络的实际裁决发生在 Rust 内核（eos-core-sandbox 的
// EnforcementSandboxRunner.check_command 等），壳层不做本地裁决，只通过
// sidecar RPC（sandbox/policy、sandbox/set_policy、sandbox/backend_status）
// 透传策略配置与后端状态。历史上壳层曾有一份本地裁决实现（GuardedRunner /
// CommandViolation / AllowsCommand 等），已作为死代码删除——裁决必须与工具
// 执行在同进程，避免 TOCTOU。
package sandbox

import (
	"strings"
)

// Mode 是沙箱策略的三档模式。仅作为 DTO 在壳层与内核间传递；
// 裁决语义由内核实现。
type Mode string

const (
	ModeReadOnly         Mode = "read-only"
	ModeWorkspaceWrite   Mode = "workspace-write"
	ModeDangerFullAccess Mode = "danger-full-access"
)

// Policy 是经 sidecar 透传给内核的沙箱策略。字段语义由内核消费，
// 壳层不基于这些字段做本地裁决。
//
// JSON 字段名必须与内核 eos-core-sandbox::SandboxPolicy 的 serde 契约一致
// （mode/workspace_root/writable_roots/allow_network）。历史上曾用
// network:"allow"/"deny" 字符串，与内核 allow_network bool 不匹配，导致
// sandbox/set_policy 静默丢失网络设置——已统一为 allow_network bool。
type Policy struct {
	Mode          Mode   `json:"mode"`
	WorkspaceRoot string `json:"workspace_root,omitempty"`
	WritableRoots []string `json:"writable_roots,omitempty"`
	AllowNetwork  bool   `json:"allow_network,omitempty"`
}

// BackendStatus 描述内核沙箱后端的当前能力与降级状态，由内核经 RPC 返回壳层。
type BackendStatus struct {
	GOOS                    string   `json:"goos"`
	Backend                 string   `json:"backend"`
	Enforced                bool     `json:"enforced"`
	Degraded                bool     `json:"degraded"`
	Reason                  string   `json:"reason,omitempty"`
	UnsupportedCapabilities []string `json:"unsupported_capabilities,omitempty"`
}

// NormalizeMode 把大小写/下划线变体归一化为标准 Mode 字符串。
// 用于壳层解析用户输入（CLI flag / 配置）后传给内核之前。
func NormalizeMode(mode string) Mode {
	key := strings.ToLower(strings.TrimSpace(mode))
	key = strings.ReplaceAll(key, "_", "-")
	switch key {
	case "read-only", "readonly":
		return ModeReadOnly
	case "danger-full-access", "dangerfullaccess", "full-access", "fullaccess", "full-access-mode":
		return ModeDangerFullAccess
	default:
		return ModeWorkspaceWrite
	}
}

