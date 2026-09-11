package jsonrpc

import "testing"

func TestAllCoreMethodsFreezesMigrationSurface(t *testing.T) {
	methods := AllCoreMethods()
	// 149 = 139（历史冻结基线）
	//   + 3 新增内核方法（approval/preview、sandbox/derive_policy、permission/enter_full_access）
	//   + 5 补齐既有遗漏（session/set_meta、session/search、model/bundled_mcp、
	//     workspace/changes、workspace/rollback/{build,apply}）
	//   - 1 移除死方法（insight/memory_snapshot，内核系统 B 清理后已无此路由）
	// 与 generated.CoreMethods()（schema.json 单一真相源）保持一致。
	//   + 14 浏览器方法（+upload_provide/preview_start/preview_stop/focus/set_default_profile/navigate）
	//   + 4 内嵌实时视口方法（live_start/live_stop/input/history）
	//   - 2 移除死方法（preview_start/preview_stop——live 实时流取代）
	//   + 1 git/summary（工作区未提交/未推送提示，status -sb 单命令聚合）
	//   + 3 浏览器面板标签条方法（tab_new/tab_switch/tab_close，桌面端
	//     「顶部标签条 + 工具栏 + 视口尺寸行」布局的可操作 tab 支持）
	//   + 1 model/verify（新增/编辑模型前的连通测试，桌面端向导前置校验）
	//   + 1 turn/resume（续跑失败 turn：不追加用户消息，内核按已提交历史续写；
	//     对齐 codex resume 语义，桌面端错误面板「重试」按钮的新链路）
	//   + 6 git 操作方法（repos/stage/commit/push/merge_abort/suggest_message，
	//     桌面端 git 提交推送操作台：确定性 git 命令 + 一次性 LLM 提交信息）
	//   + 1 lsp/install（语言服务一键安装：语言 → 生态安装命令映射，装后重探测）
	//   + 1 insight/refine_input（输入框 AI 优化表达：一次性 LLM 改写用户草稿，
	//     桌面端发送按钮旁的润色入口）
	if len(methods) != 183 {
		t.Fatalf("AllCoreMethods() len=%d, want 183", len(methods))
	}

	seen := make(map[string]bool, len(methods))
	for _, method := range methods {
		if method == "" {
			t.Fatal("AllCoreMethods() contains empty method")
		}
		if seen[method] {
			t.Fatalf("AllCoreMethods() contains duplicate method %q", method)
		}
		seen[method] = true
	}

	required := []string{
		MethodWorkspaceWorktreeCreate,
		MethodSessionDelete,
		MethodMCPList,
		MethodLSPDiagnosticsSummary,
		MethodConfigSettingsSave,
		MethodPermissionApprovalModeSet,
		MethodExtensionsSkillInvoke,
		MethodContextCompact,
		MethodUsageCostItems,
		MethodVersionsRollback,
		MethodTaskTail,
		MethodRuntimeReasoningLevelSet,
		MethodModelActivate,
		MethodModelContext,
		MethodModelWorkspaceSet,
		MethodModelSessionSet,
		MethodRemoteWorkspaceOpen,
		MethodGitShow,
		MethodMemorySnapshot,
		MethodRoleResolve,
		MethodAgentToolExecute,
		MethodToolStats,
		MethodSandboxSetPolicy,
	}
	for _, method := range required {
		if !seen[method] {
			t.Fatalf("AllCoreMethods() missing %s", method)
		}
	}
}
