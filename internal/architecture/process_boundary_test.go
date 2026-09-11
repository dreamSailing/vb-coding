// 本文件守护进程执行边界，冻结所有 exec.Command / utils.Command / shell 执行调用点。
//
// TestProcessExecutionCallSitesAreClassified 扫描整个仓库的 .go 文件（不含 _test.go），
// 找出所有 exec.Command、exec.CommandContext、utils.Command、utils.CommandContext、
// ExecuteWithWorkingDirCtx、ExecuteTypedWithWorkingDirCtx、StartAsyncWithWorkingDir 调用点，
// 并与 allowedProcessExecCalls 白名单比对。
//
// 规则：
//   - 新增的进程执行调用点必须通过 sandbox.GuardedRunner 路由，或在此白名单中显式分类。
//   - 不要扩大白名单，除非调用点属于内部系统执行（如 daemon 启动、LSP 检测、文件对话框）。
//   - 工具/代理执行路径应优先使用 sandbox.GuardedRunner，而非直接调用 shell executor。
//
// 维护指南：
//   - 如果测试因 "unclassified" 失败：新调用点需要通过 GuardedRunner 或加入白名单。
//   - 如果测试因 "count changed" 失败：调用点数量变化，需要更新白名单中的计数。
//   - 如果测试因 "missing/changed" 失败：白名单中的调用点已移除或重命名，需要清理白名单。
//   - 当 bridge 包完成 GuardedRunner 迁移后，对应的 bridge/* 条目可从白名单中移除。
package architecture

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// allowedProcessExecCalls 是已分类的进程执行调用点白名单。
// 格式：文件路径:函数名:被调用函数 = 出现次数。
// 新调用点必须通过 sandbox.GuardedRunner 或在此显式分类。
var allowedProcessExecCalls = map[string]int{
	"internal/document/convert.go:convertWithSoffice:utils.Command":              1,
	"internal/pkg/filedialog/filedialog_darwin.go:ChooseDirectory:exec.Command":  1,
	"internal/pkg/filedialog/filedialog_linux.go:ChooseDirectory:exec.Command":   1,
	"internal/pkg/filedialog/filedialog_windows.go:ChooseDirectory:exec.Command": 1,
	"internal/pkg/utils/command.go:Command:exec.Command":                         1,
	"internal/pkg/utils/command.go:CommandContext:exec.CommandContext":           1,
	"internal/ui/slash_runtime.go:copyToClipboard:exec.Command":                  3,
	// /feedback 打开系统浏览器（darwin/windows/linux 三分支）：用户可见的
	// 系统动作，与剪贴板同类，非工具/代理执行路径，不走 GuardedRunner。
	"internal/ui/app_slash_feedback.go:openInBrowser:exec.Command":    3,
	"pkg/coreapi/sidecar/process_client.go:StartProcess:exec.Command": 1,
	// webbridge（eos web / GUI 桥）的系统动作调用点：全部是用户可见的系统
	// 集成（开终端/开目录/开浏览器/打开第三方应用/PortableGit 安装引导/
	// 更新安装与重启/拉起 eos-core 子进程），与 filedialog、openInBrowser
	// 同类——非模型驱动的工具/代理执行路径，不经 GuardedRunner。
	// stdio 网关原型（stdio_client.go）后续接入沙箱执行时再迁移并移除条目。
	"internal/webbridge/adapter/stdio_client.go:startProcessLocked:exec.Command":          1,
	"internal/webbridge/bridge_feedback.go:OpenExternalURL:exec.Command":                  3,
	"internal/webbridge/bridge_terminal_other.go:startBridgeTerminalBackend:exec.Command": 1,
	"internal/webbridge/bridge_terminal_shell_install.go:extractPortableGit:exec.Command": 1,
	"internal/webbridge/external_apps.go:linuxTerminalCommand:exec.Command":               1,
	"internal/webbridge/external_apps.go:openTerminalApp:exec.Command":                    2,
	"internal/webbridge/external_apps.go:openThirdPartyApp:exec.Command":                  3,
	"internal/webbridge/open_directory.go:OpenDirectory:exec.Command":                     3,
	"internal/webbridge/reveal_path.go:RevealPath:exec.Command":                           2,
	"internal/webbridge/reveal_path.go:openDirectoryNoMkdir:exec.Command":                 3,
	"internal/webbridge/server_helpers.go:runBackgroundCommand:exec.Command":              1,
	"internal/webbridge/update_install_darwin.go:detachMacOSDmg:exec.Command":             1,
	"internal/webbridge/update_install_darwin.go:fallbackMacOSOpenDmg:exec.Command":       1,
	"internal/webbridge/update_install_darwin.go:mountMacOSDmg:exec.Command":              1,
	"internal/webbridge/update_install_darwin.go:replaceMacOSBundle:exec.Command":         1,
	"internal/webbridge/update_install_darwin.go:scheduleMacOSRelaunch:exec.Command":      1,
	"internal/webbridge/update_launch_other.go:launchUpdateInstaller:exec.Command":        1,
}

func TestProcessExecutionCallSitesAreClassified(t *testing.T) {
	root := moduleRoot(t)
	actual := map[string]int{}
	fset := token.NewFileSet()

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "dist", "build", "vendor", "scripts":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				callee := trackedProcessCallee(call)
				if callee == "" {
					return true
				}
				key := fmt.Sprintf("%s:%s:%s", rel, fn.Name.Name, callee)
				actual[key]++
				return true
			})
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan process execution call sites: %v", err)
	}

	var problems []string
	for key, count := range actual {
		want, ok := allowedProcessExecCalls[key]
		if !ok {
			problems = append(problems, "unclassified "+key)
			continue
		}
		if count != want {
			problems = append(problems, fmt.Sprintf("count changed %s got=%d want=%d", key, count, want))
		}
	}
	for key, want := range allowedProcessExecCalls {
		if actual[key] != want {
			problems = append(problems, fmt.Sprintf("missing/changed %s got=%d want=%d", key, actual[key], want))
		}
	}
	sort.Strings(problems)
	if len(problems) > 0 {
		t.Fatalf("process execution boundary changed; classify new call sites or route them through sandbox runner:\n%s", strings.Join(problems, "\n"))
	}
}

func trackedProcessCallee(call *ast.CallExpr) string {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	if id, ok := sel.X.(*ast.Ident); ok {
		switch id.Name {
		case "exec":
			if sel.Sel.Name == "Command" || sel.Sel.Name == "CommandContext" {
				return "exec." + sel.Sel.Name
			}
		case "utils":
			if sel.Sel.Name == "Command" || sel.Sel.Name == "CommandContext" {
				return "utils." + sel.Sel.Name
			}
		}
	}
	switch sel.Sel.Name {
	case "ExecuteWithWorkingDirCtx", "ExecuteTypedWithWorkingDirCtx", "StartAsyncWithWorkingDir":
		return sel.Sel.Name
	default:
		return ""
	}
}
