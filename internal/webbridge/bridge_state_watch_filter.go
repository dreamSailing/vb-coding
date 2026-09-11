package webbridge

import (
	"path/filepath"
	"strings"

	"github.com/fsnotify/fsnotify"
)

// 状态监听事件过滤：op 级别 + 路径级别。
// 只关心 .eos/skills、.eos/plugins 与 .agents/skills（SKILL.md 开放标准
// interop 落点，目录经 skills 快照动态加入监视）下的变更；其余第三方目录
// 的兼容监听已移除。

func isStateWatchOp(op fsnotify.Op) bool {
	return op&(fsnotify.Create|fsnotify.Write|fsnotify.Remove|fsnotify.Rename) != 0
}

func isStateWatchEventPath(path string) bool {
	path = strings.ToLower(filepath.ToSlash(filepath.Clean(strings.TrimSpace(path))))
	if path == "" || path == "." {
		return false
	}
	base := strings.ToLower(filepath.Base(path))
	if base == ".eos.json" {
		return true
	}
	if strings.Contains(path, "/.eos/") || strings.HasSuffix(path, "/.eos") {
		return true
	}
	return strings.Contains(path, "/.eos/skills") ||
		strings.Contains(path, "/.eos/plugins") ||
		strings.Contains(path, "/.agents/skills")
}
