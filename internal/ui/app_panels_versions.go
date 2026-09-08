package ui

// app_panels_versions.go — 版本（versions/history）面板与版本比较辅助函数。
//
// 本文件包含：
//   - refreshVersionsPanel：加载版本列表并按文件分组
//   - handleVersionsLoad / handleVersionsRollback / handleVersionsDelete /
//     handleVersionsDeleteFile / handleVersionsDeleteAll
//   - versionFileMatches / normalizeVersionPath / isAbsVersionPath /
//     versionSummarySize：版本路径匹配与摘要解析
//   - Update 中 5 个 Versions* 面板消息分支的处理方法
//
// 代码原位于 app.go，仅做物理拆分，不改行为。

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/eosaios/eos/internal/ui/panels"

	tea "charm.land/bubbletea/v2"
)

func (m *AppModel) refreshVersionsPanel() {
	panel, ok := m.panels["versions"].(*panels.VersionsPanel)
	if !ok {
		return
	}
	panel.SetLanguage(m.state.Language)
	panel.Reset()

	versions, err := m.adapter.Versions(context.Background())
	if err != nil {
		m.appendSystem(fmt.Sprintf("Failed to load versions: %v", err), "error")
		panel.SetFiles(nil)
		return
	}
	byFile := map[string]panels.FileItem{}
	for _, version := range versions {
		file := filepath.ToSlash(strings.TrimSpace(version.File))
		if file == "" {
			continue
		}
		item := byFile[file]
		item.Path = file
		item.Count++
		if version.CreatedAt.After(item.Last) {
			item.Last = version.CreatedAt
		}
		byFile[file] = item
	}
	items := make([]panels.FileItem, 0, len(byFile))
	for _, item := range byFile {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if !items[i].Last.Equal(items[j].Last) {
			return items[i].Last.After(items[j].Last)
		}
		return strings.ToLower(items[i].Path) < strings.ToLower(items[j].Path)
	})
	panel.SetFiles(items)
}

func (m *AppModel) handleVersionsLoad(pathRel string) {
	pathRel = strings.TrimSpace(pathRel)
	if pathRel == "" {
		return
	}
	panel, ok := m.panels["versions"].(*panels.VersionsPanel)
	if !ok {
		return
	}
	versions, err := m.adapter.Versions(context.Background())
	if err != nil {
		m.appendSystem(fmt.Sprintf("Failed to load versions: %v", err), "error")
		return
	}
	items := make([]panels.VersionItem, 0)
	for _, v := range versions {
		if !versionFileMatches(v.File, pathRel) {
			continue
		}
		items = append(items, panels.VersionItem{
			Timestamp: v.ID,
			Size:      versionSummarySize(v.Summary),
		})
	}
	panel.SetVersions(filepath.ToSlash(pathRel), items)
}

func (m *AppModel) handleVersionsRollback(pathRel string, versionID string) {
	pathRel = strings.TrimSpace(pathRel)
	versionID = strings.TrimSpace(versionID)
	if pathRel == "" || versionID == "" {
		return
	}
	if err := m.adapter.RollbackVersion(context.Background(), versionID); err != nil {
		m.appendSystem(fmt.Sprintf("%s: %v", m.localize("版本回滚失败", "Version rollback failed"), err), "error")
		return
	}
	m.appendSystem(fmt.Sprintf("%s: %s", m.localize("已回滚版本", "Rolled back version"), versionID), "warning")
	m.handleVersionsLoad(pathRel)
}

func (m *AppModel) handleVersionsDelete(pathRel string, versionID string) {
	pathRel = strings.TrimSpace(pathRel)
	versionID = strings.TrimSpace(versionID)
	if pathRel == "" || versionID == "" {
		return
	}
	if err := m.adapter.DeleteVersion(context.Background(), versionID); err != nil {
		m.appendSystem(fmt.Sprintf("%s: %v", m.localize("删除版本失败", "Version delete failed"), err), "error")
		return
	}
	m.appendSystem(fmt.Sprintf("%s: %s", m.localize("已删除版本", "Deleted version"), versionID), "warning")
	m.handleVersionsLoad(pathRel)
}

func (m *AppModel) handleVersionsDeleteFile(pathRel string) {
	pathRel = strings.TrimSpace(pathRel)
	if pathRel == "" {
		return
	}
	count, err := m.adapter.DeleteFileVersions(context.Background(), pathRel)
	if err != nil {
		m.appendSystem(fmt.Sprintf("%s: %v", m.localize("删除文件版本失败", "Failed to delete file versions"), err), "error")
		return
	}
	m.appendSystem(fmt.Sprintf("%s: %s (%d)", m.localize("已删除文件版本", "Deleted file versions"), pathRel, count), "warning")
	m.refreshVersionsPanel()
}

func (m *AppModel) handleVersionsDeleteAll() {
	count, err := m.adapter.ClearVersions(context.Background())
	if err != nil {
		m.appendSystem(fmt.Sprintf("%s: %v", m.localize("清空版本历史失败", "Failed to clear version history"), err), "error")
		return
	}
	m.appendSystem(fmt.Sprintf("%s: %d", m.localize("已清空版本历史", "Cleared version history"), count), "warning")
	m.refreshVersionsPanel()
}

// versionFileMatches 检查版本文件路径是否匹配目标路径
// 支持绝对路径和相对路径的模糊匹配
func versionFileMatches(file, target string) bool {
	file = normalizeVersionPath(file)
	target = normalizeVersionPath(target)
	if file == "" || target == "" {
		return false
	}
	if strings.EqualFold(file, target) {
		return true
	}
	// 绝对路径与相对路径的后缀匹配
	if isAbsVersionPath(file) && !isAbsVersionPath(target) {
		return strings.HasSuffix(strings.ToLower(file), "/"+strings.ToLower(target))
	}
	if isAbsVersionPath(target) && !isAbsVersionPath(file) {
		return strings.HasSuffix(strings.ToLower(target), "/"+strings.ToLower(file))
	}
	return false
}

// normalizeVersionPath 标准化版本文件路径
func normalizeVersionPath(path string) string {
	path = filepath.ToSlash(strings.TrimSpace(path))
	if path == "" {
		return ""
	}
	path = filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
	if path == "." {
		return ""
	}
	path = strings.TrimPrefix(path, "./")
	return path
}

// isAbsVersionPath 检查路径是否为绝对路径
func isAbsVersionPath(path string) bool {
	return filepath.IsAbs(filepath.FromSlash(path))
}

// versionSummarySize 从版本摘要中提取文件大小
func versionSummarySize(summary string) int {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return 0
	}
	match := regexp.MustCompile(`(?:^|[,\s])size=(\d+)`).FindStringSubmatch(summary)
	if len(match) != 2 {
		return 0
	}
	var size int
	_, _ = fmt.Sscanf(match[1], "%d", &size)
	return size
}

// handleVersionsLoadMsg 处理 panels.VersionsLoadMsg（Update 分支提取）。fall-through。
func (m *AppModel) handleVersionsLoadMsg(msg panels.VersionsLoadMsg) (tea.Model, tea.Cmd) {
	m.handleVersionsLoad(msg.FilePath)
	return m, m.finalizeUpdate(nil)
}

// handleVersionsRollbackMsg 处理 panels.VersionsRollbackMsg（Update 分支提取）。fall-through。
func (m *AppModel) handleVersionsRollbackMsg(msg panels.VersionsRollbackMsg) (tea.Model, tea.Cmd) {
	m.handleVersionsRollback(msg.FilePath, msg.Timestamp)
	return m, m.finalizeUpdate(nil)
}

// handleVersionsDeleteMsg 处理 panels.VersionsDeleteMsg（Update 分支提取）。fall-through。
func (m *AppModel) handleVersionsDeleteMsg(msg panels.VersionsDeleteMsg) (tea.Model, tea.Cmd) {
	m.handleVersionsDelete(msg.FilePath, msg.Timestamp)
	return m, m.finalizeUpdate(nil)
}

// handleVersionsDeleteFileMsg 处理 panels.VersionsDeleteFileMsg（Update 分支提取）。fall-through。
func (m *AppModel) handleVersionsDeleteFileMsg(msg panels.VersionsDeleteFileMsg) (tea.Model, tea.Cmd) {
	m.handleVersionsDeleteFile(msg.FilePath)
	return m, m.finalizeUpdate(nil)
}

// handleVersionsDeleteAllMsg 处理 panels.VersionsDeleteAllMsg（Update 分支提取）。fall-through。
func (m *AppModel) handleVersionsDeleteAllMsg(_ panels.VersionsDeleteAllMsg) (tea.Model, tea.Cmd) {
	m.handleVersionsDeleteAll()
	return m, m.finalizeUpdate(nil)
}
