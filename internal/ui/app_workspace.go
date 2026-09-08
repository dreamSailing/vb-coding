package ui

// app_workspace.go — 工作区（workspace）相关的刷新、切换、信任、添加。
//
// 本文件包含：
//   - WorkspaceReloadDoneMsg / MCPReloadDoneMsg / LSPReloadDoneMsg 消息类型
//   - refreshWorkspacePanel：刷新工作区面板列表
//   - handleWorkspaceUse / handleWorkspaceRemove / switchWorkspaceTrusted
//   - openWorkspaceAddConfirm / openWorkspaceTrustConfirm
//   - isWorkspaceTrusted / addTrustedWorkspace
//   - resolveWorkspaceInputPath：解析 ~ / 相对路径为标准化绝对路径
//   - Update 中三个 ReloadDone 分支的处理方法（workspace/mcp/lsp）
//   - Update 中 workspace 三个面板消息分支的处理方法
//
// 代码原位于 app.go，仅做物理拆分，不改行为。

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/eosaios/eos/internal/config"
	"github.com/eosaios/eos/internal/i18n"
	"github.com/eosaios/eos/internal/ui/panels"
	"github.com/eosaios/eos/internal/ui/views/confirm"

	tea "charm.land/bubbletea/v2"
)

// WorkspaceReloadDoneMsg 工作区重载完成消息
type WorkspaceReloadDoneMsg struct {
	Err error
}

// MCPReloadDoneMsg MCP 重载完成消息
type MCPReloadDoneMsg struct {
	Err error
}

// LSPReloadDoneMsg LSP 重载完成消息
type LSPReloadDoneMsg struct {
	Err error
}

func (m *AppModel) refreshWorkspacePanel() {
	panel, ok := m.panels["workspace"].(*panels.WorkspacePanel)
	if !ok {
		return
	}

	items, err := m.adapter.Workspaces(context.Background())
	if err != nil {
		m.appendSystem(fmt.Sprintf("Failed to load workspaces: %v", err), "error")
		panel.SetWorkspaces(nil, "")
		return
	}
	sort.Slice(items, func(i, j int) bool { return strings.ToLower(items[i].Path) < strings.ToLower(items[j].Path) })

	active := ""
	workspaces := make([]panels.Workspace, 0, len(items))
	for _, item := range items {
		p := strings.TrimSpace(item.Path)
		if p == "" {
			continue
		}
		if item.Active {
			active = p
		}
		workspaces = append(workspaces, panels.Workspace{
			Name: filepath.Base(p),
			Path: p,
		})
	}
	if strings.TrimSpace(active) == "" {
		active = m.currentWorkspaceRoot()
	}

	panel.SetWorkspaces(workspaces, active)
}

func (m *AppModel) handleWorkspaceRemove(path string) {
	if path == "" {
		return
	}
	if err := m.adapter.RemoveWorkspace(context.Background(), path); err != nil {
		m.appendSystem(err.Error(), "error")
		return
	}
	m.refreshWorkspacePanel()
	m.appendSystem(i18n.T("workspace.removed", m.state.Language)+path, "success")
}

func (m *AppModel) handleWorkspaceUse(rawPath string) tea.Cmd {
	if rawPath == "" {
		return nil
	}
	path, err := resolveWorkspaceInputPath(rawPath, m.state.Language)
	if err != nil {
		m.appendSystem(err.Error(), "error")
		return nil
	}
	fi, err := os.Stat(path)
	if err != nil || fi == nil || !fi.IsDir() {
		m.appendSystem(i18n.T("path_not_dir", m.state.Language)+path, "warning")
		return nil
	}
	if !m.isWorkspaceTrusted(path) {
		m.trustPendingPath = path
		m.trustPendingAction = "switch"
		m.openWorkspaceTrustConfirm(path)
		return nil
	}
	return m.switchWorkspaceTrusted(path)
}

func (m *AppModel) switchWorkspaceTrusted(path string) tea.Cmd {
	if err := m.adapter.UseWorkspace(context.Background(), path); err != nil {
		m.appendSystem(fmt.Sprintf(i18n.T("toast.workspace_switch_failed", m.state.Language), err), "error")
		return nil
	}
	_ = os.Chdir(path)
	_, _ = m.adapter.Settings(context.Background())
	m.refreshWorkspacePanel()
	m.appendSystem(i18n.T("workspace.switched", m.state.Language)+path, "success")
	return func() tea.Msg {
		return WorkspaceReloadDoneMsg{Err: m.adapter.Reload()}
	}
}

func (m *AppModel) openWorkspaceAddConfirm() {
	req := confirm.Request{
		Kind:      "workspace_add",
		Title:     i18n.T("workspace.add.title", m.state.Language),
		Question:  i18n.T("workspace.add.question", m.state.Language),
		Options:   []string{i18n.T("workspace.add.confirm", m.state.Language)},
		AllowText: true,
		TextHint:  i18n.T("workspace.add.hint", m.state.Language),
	}
	m.openConfirm(req)
}

func (m *AppModel) openWorkspaceTrustConfirm(path string) {
	req := confirm.Request{
		Kind:     "workspace_trust",
		Title:    i18n.T("workspace.trust.title", m.state.Language),
		Question: fmt.Sprintf(i18n.T("workspace.trust.question", m.state.Language), path),
		Options: []string{
			i18n.T("workspace.trust.confirm", m.state.Language),
			i18n.T("workspace.trust.exit", m.state.Language),
		},
	}
	m.openConfirm(req)
}

func (m *AppModel) isWorkspaceTrusted(path string) bool {
	if config.IsWorkspaceTrustedLocal(path) {
		return true
	}
	cfg, _ := config.Load()
	want := config.NormalizeWorkspacePath(path)
	for _, p := range cfg.TrustedWorkspaces {
		if config.PathsEqual(config.NormalizeWorkspacePath(p), want) {
			return true
		}
	}
	return false
}

func (m *AppModel) addTrustedWorkspace(path string) error {
	return config.TrustWorkspaceLocal(path)
}

func resolveWorkspaceInputPath(raw string, lang string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("%s", i18n.T("error.workspace_path_required", lang))
	}
	if raw == "~" || strings.HasPrefix(raw, "~"+string(os.PathSeparator)) || strings.HasPrefix(raw, "~/") {
		home, err := os.UserHomeDir()
		if err != nil || strings.TrimSpace(home) == "" {
			return "", fmt.Errorf("%s", i18n.T("error.tilde_unresolvable", lang))
		}
		rest := strings.TrimPrefix(raw, "~")
		rest = strings.TrimPrefix(rest, "/")
		rest = strings.TrimPrefix(rest, "\\")
		raw = filepath.Join(home, rest)
	}
	abs, err := filepath.Abs(raw)
	if err != nil {
		return "", fmt.Errorf(i18n.T("error.path_resolve_failed", lang), err)
	}
	return config.NormalizeWorkspacePath(abs), nil
}

// handleWorkspaceReloadDoneMsg 处理 WorkspaceReloadDoneMsg（Update 分支提取）。fall-through。
func (m *AppModel) handleWorkspaceReloadDoneMsg(msg WorkspaceReloadDoneMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		m.appendSystem(fmt.Sprintf(i18n.T("toast.workspace_reload_failed", m.state.Language), msg.Err), "error")
	} else {
		m.appendSystem(i18n.T("workspace.switched_reloaded", m.state.Language), "success")
	}
	m.refreshWorkspacePanel()
	m.refreshMemoryPanel()
	m.refreshRulesPanel()
	// 新工作区是新仓库：提交提醒按全新状态重新计数。
	m.gitHintedDirty = -1
	m.gitHintedAhead = -1
	return m, m.finalizeUpdate(nil)
}

// handleMCPReloadDoneMsg 处理 MCPReloadDoneMsg（Update 分支提取）。fall-through。
func (m *AppModel) handleMCPReloadDoneMsg(msg MCPReloadDoneMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		m.appendSystem(fmt.Sprintf(i18n.T("toast.mcp_reload_failed", m.state.Language), msg.Err), "error")
	} else {
		m.appendSystem(i18n.T("mcp.reloaded", m.state.Language), "success")
	}
	m.refreshMCPPanel()
	m.refreshLSPPanel()
	return m, m.finalizeUpdate(nil)
}

// handleLSPReloadDoneMsg 处理 LSPReloadDoneMsg（Update 分支提取）。fall-through。
func (m *AppModel) handleLSPReloadDoneMsg(msg LSPReloadDoneMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		m.appendSystem(fmt.Sprintf(i18n.T("toast.lsp_reload_failed", m.state.Language), msg.Err), "error")
	} else {
		m.appendSystem(i18n.T("lsp.reloaded", m.state.Language), "success")
	}
	m.refreshLSPPanel()
	return m, m.finalizeUpdate(nil)
}

// handleWorkspaceSelectMsg 处理 panels.WorkspaceSelectMsg（Update 分支提取）。fall-through。
func (m *AppModel) handleWorkspaceSelectMsg(msg panels.WorkspaceSelectMsg) (tea.Model, tea.Cmd) {
	return m, m.finalizeUpdate(m.handleWorkspaceUse(msg.Path))
}

// handleWorkspaceDeleteMsg 处理 panels.WorkspaceDeleteMsg（Update 分支提取）。fall-through。
func (m *AppModel) handleWorkspaceDeleteMsg(msg panels.WorkspaceDeleteMsg) (tea.Model, tea.Cmd) {
	m.handleWorkspaceRemove(msg.Path)
	return m, m.finalizeUpdate(nil)
}

// handleWorkspaceAddMsg 处理 panels.WorkspaceAddMsg（Update 分支提取）。原行为早退。
func (m *AppModel) handleWorkspaceAddMsg(_ panels.WorkspaceAddMsg) (tea.Model, tea.Cmd) {
	m.openWorkspaceAddConfirm()
	return m, nil
}
