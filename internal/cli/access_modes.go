package cli

import (
	"strings"

	"github.com/eosaios/eos/internal/config"
	"github.com/eosaios/eos/internal/modes"
	"github.com/spf13/pflag"
)

type resolvedModeConfig struct {
	AccessMode    string
	ApprovalMode  string
	SandboxMode   string
	SkipAllChecks bool
}

// mergeConfigPermissions 把配置文件 permissions.access_mode / approval_mode
// 合并进未显式传入的 flag（优先级：显式 flag > 配置文件 > 内置默认）。
// 这两个字段此前没有任何读取方（死配置）。sandboxFlagName 是各命令沙箱别名
// flag 的名字（root/serve/mcp 为 sandbox-mode，exec 为 sandbox）：显式沙箱
// flag 优先于配置文件 access_mode。
func mergeConfigPermissions(flags *pflag.FlagSet, sandboxFlagName, accessMode, approvalMode string) (string, string) {
	cfg, _ := config.Load()
	perms := cfg.Permissions
	if perms == nil {
		return accessMode, approvalMode
	}
	if !flags.Changed("access-mode") && !flags.Changed(sandboxFlagName) && strings.TrimSpace(accessMode) == "" {
		if v := strings.TrimSpace(perms.AccessMode); v != "" {
			accessMode = v
		}
	}
	if !flags.Changed("approval-mode") && strings.TrimSpace(approvalMode) == "" {
		if v := strings.TrimSpace(perms.ApprovalMode); v != "" {
			approvalMode = v
		}
	}
	return accessMode, approvalMode
}

// resolveModeConfig 把用户传入的访问/审批/沙箱模式解析成启动期 env 值。
//
// 沙箱轴与访问轴共用内核 SandboxMode 的 kebab-case 三值词表（read-only /
// workspace-write / danger-full-access）：--access-mode
// 是规范入口，--sandbox-mode / exec --sandbox 是历史别名（workspace→workspace-write、
// full_access→danger-full-access）。两个 flag 显式同传且冲突时以 --access-mode 为准。
//
// 启动期沙箱只经 EOS_SANDBOX_MODE 单通道下发——内核不读 EOS_ACCESS_MODE（历史
// 死参数已清理），审批轴独立经 EOS_APPROVAL_MODE 下发，双轴正交可自由组合；
// --dangerously-skip-permissions 才是双轴复合预设（approval=never + danger）。
//
// skipPermissions 分支不再在壳层合成 danger-full-access + never + full_access
// 三件套——双轴协同由内核 bin 侧读 EOS_SKIP_PERMISSIONS 后用
// permission_enter_full_access 单一真相源派生（AGENTS.md §3：壳层不做业务裁决）。
func resolveModeConfig(accessMode string, approvalMode string, sandboxMode string, skipPermissions bool) resolvedModeConfig {
	if skipPermissions {
		// 只透传 skip 标志，不合成 mode 值。execOptionEnv 会把 EOS_SKIP_PERMISSIONS=1
		// 传给内核，内核启动期原子地设双轴；显式 mode 与 skip 共存时内核会 fail-fast。
		return resolvedModeConfig{
			SkipAllChecks: true,
		}
	}

	resolved := modes.ResolveAccessMode(modes.ExecSession{
		AccessMode:  accessMode,
		SandboxMode: sandboxMode,
	})
	resolvedApproval := ""
	if approvalMode != "" {
		resolvedApproval = modes.NormalizeApprovalMode(approvalMode)
	}

	return resolvedModeConfig{
		AccessMode:   resolved,
		ApprovalMode: resolvedApproval,
		SandboxMode:  resolved,
	}
}
