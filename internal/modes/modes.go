package modes

// Copyright (c) 2026 EOSAIOS
// SPDX-License-Identifier: EOS-NCL-1.1
// 本文件基于 EOS 非商用许可证 v1.1 发布，详见 LICENSE。
// 商业使用请联系版权人获得商业授权。

// modes.go holds the execution/access/approval/sandbox mode normalization
// helpers previously provided by internal/toolapi.policy.go. They moved here
// when the Go gateway layer (internal/toolapi) was removed; the Go TUI and the
// --print headless path are the only remaining consumers.

import "strings"

// ExecSession mirrors the subset of toolapi.ExecSession that the mode helpers
// consult. Kept local so ui has no dependency on the deleted toolapi package.
type ExecSession struct {
	WorkspaceRoot         string
	AllowedTools          map[string]bool
	TraceID               string
	ExecutionMode         string
	AccessMode            string
	ApprovalMode          string
	SandboxMode           string
	RequireApprovalDigest bool
}

type ExecutionModeDescriptor struct {
	Name             string
	Aliases          []string
	Description      string
	ApprovalBehavior string
}

type AccessModeDescriptor struct {
	Name        string
	Aliases     []string
	Description string
}

type ApprovalModeDescriptor struct {
	Name        string
	Aliases     []string
	Description string
}

var executionModeDescriptors = []ExecutionModeDescriptor{
	{
		Name:             "plan",
		Aliases:          []string{"计划优先", "先出计划", "plan_first", "plan-first"},
		Description:      "Planning mode: read-only tools stay visible, mutating tools are hidden.",
		ApprovalBehavior: "read_only_only",
	},
	{
		Name:             "auto",
		Aliases:          []string{"自动", "auto_mode", "auto-mode"},
		Description:      "Balanced mode: low/medium risk tools run directly, high risk tools still prompt.",
		ApprovalBehavior: "prompt_high_only",
	},
}

var accessModeDescriptors = []AccessModeDescriptor{
	{
		Name:        "read-only",
		Aliases:     []string{"readonly", "read_only", "只读"},
		Description: "Read-only: blocks mutating tools and side effects.",
	},
	{
		Name:        "workspace-write",
		Aliases:     []string{"workspace_write", "workspace", "sandbox", "workspace_sandbox", "workspace-sandbox", "工作区写入", "工作区沙箱"},
		Description: "Workspace-write: allows writes inside the workspace boundary.",
	},
	{
		Name:        "danger-full-access",
		Aliases:     []string{"danger_full_access", "danger", "full", "full_access", "full-access", "fullaccess", "allow_all", "allow-all", "完全访问", "完全访问权限"},
		Description: "Danger-full-access: removes the workspace boundary for privileged tools.",
	},
}

var approvalModeDescriptors = []ApprovalModeDescriptor{
	{
		Name:        "untrusted",
		Aliases:     []string{"cautious", "strict", "unless_trusted", "unless-trusted", "不信任"},
		Description: "Always ask before non-read-only or elevated actions.",
	},
	{
		Name:    "on-request",
		Aliases: []string{"on_request", "onrequest", "request", "on-failure", "on_failure", "onfailure", "失败后审批", "请求时审批"},
		// 内核 ApprovalMode 只有三个值（untrusted/on-request/never，对标 Codex
		// approval_policy）；on-failure 是内核解析侧的历史别名，折叠到 on-request，
		// 不再作为独立档位宣传。
		Description: "Allow the agent to request approval when it decides escalation is needed.",
	},
	{
		Name:        "never",
		Aliases:     []string{"no_approval", "no-approval", "skip_approval", "skip-approval", "从不审批"},
		Description: "Never ask for approval (pending approvals are denied, not auto-approved).",
	},
}

// sandboxModeDescriptors 不再定义独立的沙箱轴词表：沙箱轴与访问轴共用内核
// SandboxMode 的 kebab-case 三值（read-only / workspace-write / danger-full-access，
// 对标 Codex sandbox_mode）。历史上的 GUI 双值（workspace / full_access）只作为
// NormalizeSandboxMode 的兼容别名保留。

func NormalizeExecutionMode(mode string) string {
	key := normalizeModeKey(mode)
	if key == "" {
		return "auto"
	}
	for _, item := range executionModeDescriptors {
		if key == normalizeModeKey(item.Name) {
			return item.Name
		}
		for _, alias := range item.Aliases {
			if key == normalizeModeKey(alias) {
				return item.Name
			}
		}
	}
	return "auto"
}

func SupportedExecutionModes() []ExecutionModeDescriptor {
	out := make([]ExecutionModeDescriptor, 0, len(executionModeDescriptors))
	for _, item := range executionModeDescriptors {
		copied := item
		copied.Aliases = append([]string(nil), item.Aliases...)
		out = append(out, copied)
	}
	return out
}

func NormalizeAccessMode(mode string) string {
	key := normalizeModeKey(mode)
	if key == "" {
		return "workspace-write"
	}
	for _, item := range accessModeDescriptors {
		if key == normalizeModeKey(item.Name) {
			return item.Name
		}
		for _, alias := range item.Aliases {
			if key == normalizeModeKey(alias) {
				return item.Name
			}
		}
	}
	return "workspace-write"
}

func SupportedAccessModes() []AccessModeDescriptor {
	out := make([]AccessModeDescriptor, 0, len(accessModeDescriptors))
	for _, item := range accessModeDescriptors {
		copied := item
		copied.Aliases = append([]string(nil), item.Aliases...)
		out = append(out, copied)
	}
	return out
}

func NormalizeApprovalMode(mode string) string {
	key := normalizeModeKey(mode)
	if key == "" {
		return "on-request"
	}
	for _, item := range approvalModeDescriptors {
		if key == normalizeModeKey(item.Name) {
			return item.Name
		}
		for _, alias := range item.Aliases {
			if key == normalizeModeKey(alias) {
				return item.Name
			}
		}
	}
	return "on-request"
}

func SupportedApprovalModes() []ApprovalModeDescriptor {
	out := make([]ApprovalModeDescriptor, 0, len(approvalModeDescriptors))
	for _, item := range approvalModeDescriptors {
		copied := item
		copied.Aliases = append([]string(nil), item.Aliases...)
		out = append(out, copied)
	}
	return out
}

// NormalizeSandboxMode 归一化沙箱轴输入到内核 SandboxMode 的 kebab-case 三值。
// 历史别名（workspace / full_access / 工作区沙箱 等）全部折叠到对应规范值，
// 与 NormalizeAccessMode 同一张词表——修复旧实现把 danger-full-access 等
// 未知值静默降级为 workspace 的问题。
func NormalizeSandboxMode(mode string) string {
	if strings.TrimSpace(mode) == "" {
		return "workspace-write"
	}
	return NormalizeAccessMode(mode)
}

func ResolveAccessMode(sess ExecSession) string {
	if strings.TrimSpace(sess.AccessMode) != "" {
		return NormalizeAccessMode(sess.AccessMode)
	}
	if strings.TrimSpace(sess.SandboxMode) != "" {
		return NormalizeSandboxMode(sess.SandboxMode)
	}
	return "workspace-write"
}

func normalizeModeKey(mode string) string {
	mode = strings.ToLower(strings.TrimSpace(mode))
	mode = strings.ReplaceAll(mode, "_", "")
	mode = strings.ReplaceAll(mode, "-", "")
	mode = strings.ReplaceAll(mode, " ", "")
	return mode
}
