package ui

// app_panels_mcp.go — MCP 面板的刷新与增删改，以及 MCP 配置编辑器提交。
//
// 本文件包含：
//   - refreshMCPPanel：从适配器加载 MCP 服务器与浏览器状态
//   - handleMCPToggle / handleMCPAdd / handleMCPAddBrowser /
//     handleMCPEdit / handleMCPDelete / handleMCPSave
//   - handleMCPConfigSubmit：MCP 配置编辑器提交（兼容旧版 JSON 与数组/对象格式）
//   - Update 中 MCP 相关面板消息分支的处理方法，以及 MCPConfigCancel /
//     MCPConfigSubmit 两个 setup 消息分支的处理方法
//
// 代码原位于 app.go，仅做物理拆分，不改行为。

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eosaios/eos/internal/config"
	"github.com/eosaios/eos/internal/i18n"
	"github.com/eosaios/eos/internal/ui/panels"
	"github.com/eosaios/eos/internal/ui/views/setup"
	"github.com/eosaios/eos/pkg/coreapi"

	tea "charm.land/bubbletea/v2"
)

// handleMCPToggle 切换 MCP 服务器的启用/禁用状态
func (m *AppModel) handleMCPToggle(msg panels.MCPToggleMsg) tea.Cmd {
	configServers, err := m.adapter.MCPServers(context.Background())
	if err != nil {
		m.appendSystem(fmt.Sprintf("Failed to toggle MCP server: %s", msg.Name), "error")
		return nil
	}
	for _, s := range configServers {
		if s.Name != msg.Name {
			continue
		}
		next := !s.Enabled
		if err := m.adapter.SetMCPEnabled(context.Background(), msg.Name, next); err != nil {
			m.appendSystem(fmt.Sprintf("Failed to toggle MCP server: %s", msg.Name), "error")
			return nil
		}
		m.refreshMCPPanel()
		status := i18n.T("mcp.status.disabled", m.state.Language)
		if next {
			status = i18n.T("mcp.status.enabled", m.state.Language)
		}
		m.appendSystem(fmt.Sprintf(i18n.T("mcp.msg.toggled", m.state.Language), status, msg.Name), "success")
		// 重新加载 MCP 配置
		return func() tea.Msg {
			return MCPReloadDoneMsg{Err: m.adapter.Reload()}
		}
	}
	m.appendSystem(fmt.Sprintf("Failed to toggle MCP server: %s", msg.Name), "error")
	return nil
}

// handleMCPAdd 处理添加 MCP 服务器
func (m *AppModel) handleMCPAdd() {
	initial := `[
  {
    "name": "my-mcp",
    "type": "stdio",
    "command": "",
    "args": [],
    "envs": {},
    "enabled": true
  }
]`
	m.activeView = "setup"
	editor := setup.NewMCPConfigEditorView(m.styles, m.state.Language, initial, false, "")
	editor.SetSize(m.width, m.height)
	m.setupView = editor
}

func (m *AppModel) handleMCPAddBrowser() {
	m.activeView = "setup"
	editor := setup.NewMCPConfigEditorView(m.styles, m.state.Language, recommendedBrowserPresetJSON(), false, "")
	editor.SetSize(m.width, m.height)
	m.setupView = editor
}

// handleMCPEdit 处理编辑 MCP 服务器
func (m *AppModel) handleMCPEdit(msg panels.MCPEditMsg) {
	var entry *config.MCPEntry
	entries, err := m.adapter.MCPServers(context.Background())
	if err != nil {
		m.appendSystem(fmt.Sprintf(i18n.T("toast.mcp_load_failed", m.state.Language), err), "error")
		return
	}
	for _, e := range entries {
		if e.Name == msg.Name {
			e2 := e
			entry = &e2
			break
		}
	}
	if entry == nil {
		m.appendSystem(i18n.T("mcp.not_found", m.state.Language)+msg.Name, "warning")
		return
	}
	b, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		m.appendSystem(fmt.Sprintf(i18n.T("toast.mcp_serialize_failed", m.state.Language), err), "error")
		return
	}
	m.activeView = "setup"
	editor := setup.NewMCPConfigEditorView(m.styles, m.state.Language, string(b), true, entry.Name)
	editor.SetSize(m.width, m.height)
	m.setupView = editor
}

// handleMCPDelete 处理删除 MCP 服务器
func (m *AppModel) handleMCPDelete(msg panels.MCPDeleteMsg) tea.Cmd {
	if err := m.adapter.DeleteMCPServer(context.Background(), msg.Name); err != nil {
		m.appendSystem(fmt.Sprintf("Failed to delete MCP server: %s", msg.Name), "error")
		return nil
	}
	m.refreshMCPPanel()
	m.appendSystem(fmt.Sprintf(i18n.T("mcp.msg.deleted", m.state.Language), msg.Name), "success")
	return func() tea.Msg {
		return MCPReloadDoneMsg{Err: m.adapter.Reload()}
	}
}

// handleMCPSave 处理保存 MCP 配置
func (m *AppModel) handleMCPSave() tea.Cmd {
	m.refreshMCPPanel()
	return func() tea.Msg {
		return MCPReloadDoneMsg{Err: m.adapter.Reload()}
	}
}

func (m *AppModel) refreshMCPPanel() {
	mcpPanel, ok := m.panels["mcp"].(*panels.MCPPanel)
	if !ok {
		return
	}
	cfgServers, err := m.adapter.MCPServers(context.Background())
	if err != nil {
		m.appendSystem(fmt.Sprintf("Failed to load MCP servers: %v", err), "error")
		mcpPanel.SetServers(nil)
		return
	}
	out := make([]panels.MCPServer, 0, len(cfgServers))
	for _, s := range cfgServers {
		out = append(out, panels.MCPServer{
			Name:    s.Name,
			Type:    string(s.Type),
			Enabled: s.Enabled,
		})
	}
	mcpPanel.SetServers(out)
	browser, err := m.adapter.BrowserStatus(context.Background())
	if err != nil {
		browser = coreapi.BrowserRuntimeStatus{}
	}
	mcpPanel.SetBrowserSummary(panels.BrowserSummary{
		Running:   browser.Running,
		Kind:      browser.BrowserKind,
		Version:   browser.BrowserVersion,
		Profile:   browser.Profile,
		LastError: browser.LastError,
	})
}

// handleMCPConfigSubmit 处理 MCP 配置提交
// 支持两种格式：旧版 JSON 标签格式和新版数组/对象格式
func (m *AppModel) handleMCPConfigSubmit(msg setup.MCPConfigSubmitMsg) tea.Cmd {
	raw := strings.TrimSpace(msg.Text)
	if raw == "" {
		m.appendSystem(i18n.T("mcp.input_empty", m.state.Language), "warning")
		return nil
	}

	// 尝试多种格式解析 MCP 配置
	parseEntries := func(text string) ([]config.MCPEntry, error) {
		// 尝试旧版格式
		if entries, err := config.ParseLegacyMCPServersJSON([]byte(text)); err == nil && len(entries) > 0 {
			return entries, nil
		}
		// 尝试数组格式
		var arr []config.MCPEntry
		if err := json.Unmarshal([]byte(text), &arr); err == nil && len(arr) > 0 {
			return arr, nil
		}
		// 尝试单对象格式
		var one config.MCPEntry
		if err := json.Unmarshal([]byte(text), &one); err != nil {
			return nil, err
		}
		return []config.MCPEntry{one}, nil
	}

	entries, err := parseEntries(raw)
	if err != nil {
		m.appendSystem(fmt.Sprintf(i18n.T("toast.json_parse_failed", m.state.Language), err), "error")
		return nil
	}

	if msg.Edit {
		// 编辑模式：只支持单个 MCPEntry
		if len(entries) != 1 {
			m.appendSystem(i18n.T("mcp.single_entry_only", m.state.Language), "warning")
			return nil
		}
		entry := entries[0]
		if strings.TrimSpace(entry.Name) == "" {
			m.appendSystem(i18n.T("mcp.name_required", m.state.Language), "warning")
			return nil
		}
		// 处理重命名：先添加新名称，再删除旧名称
		if msg.OriginalName != "" && entry.Name != msg.OriginalName {
			if err := m.adapter.AddMCPEntries(context.Background(), []config.MCPEntry{entry}); err != nil {
				m.appendSystem(fmt.Sprintf(i18n.T("toast.create_for_rename_failed", m.state.Language), err), "error")
				return nil
			}
			if err := m.adapter.DeleteMCPServer(context.Background(), msg.OriginalName); err != nil {
				_ = m.adapter.DeleteMCPServer(context.Background(), entry.Name)
				m.appendSystem(i18n.T("mcp.delete_old_failed", m.state.Language)+msg.OriginalName, "error")
				return nil
			}
		} else {
			// 直接更新
			if err := m.adapter.UpsertMCPEntry(context.Background(), entry); err != nil {
				m.appendSystem(i18n.T("mcp.update_failed", m.state.Language)+err.Error(), "error")
				return nil
			}
		}
	} else {
		// 添加模式
		if err := m.adapter.AddMCPEntries(context.Background(), entries); err != nil {
			m.appendSystem(fmt.Sprintf(i18n.T("toast.create_failed", m.state.Language), err), "error")
			return nil
		}
	}

	// 刷新 MCP 面板并重新加载配置
	m.activeView = "panel"
	m.activePanel = "mcp"
	m.shell.ClearInput()
	m.refreshMCPPanel()

	return func() tea.Msg {
		return MCPReloadDoneMsg{Err: m.adapter.Reload()}
	}
}

// handleMCPToggleMsg 处理 panels.MCPToggleMsg（Update 分支提取）。fall-through。
func (m *AppModel) handleMCPToggleMsg(msg panels.MCPToggleMsg) (tea.Model, tea.Cmd) {
	return m, m.finalizeUpdate(m.handleMCPToggle(msg))
}

// handleMCPAddMsg 处理 panels.MCPAddMsg（Update 分支提取）。fall-through。
func (m *AppModel) handleMCPAddMsg(_ panels.MCPAddMsg) (tea.Model, tea.Cmd) {
	m.handleMCPAdd()
	return m, m.finalizeUpdate(nil)
}

// handleMCPAddBrowserMsg 处理 panels.MCPAddBrowserMsg（Update 分支提取）。fall-through。
func (m *AppModel) handleMCPAddBrowserMsg(_ panels.MCPAddBrowserMsg) (tea.Model, tea.Cmd) {
	m.handleMCPAddBrowser()
	return m, m.finalizeUpdate(nil)
}

// handleMCPEditMsg 处理 panels.MCPEditMsg（Update 分支提取）。fall-through。
func (m *AppModel) handleMCPEditMsg(msg panels.MCPEditMsg) (tea.Model, tea.Cmd) {
	m.handleMCPEdit(msg)
	return m, m.finalizeUpdate(nil)
}

// handleMCPDeleteMsg 处理 panels.MCPDeleteMsg（Update 分支提取）。fall-through。
func (m *AppModel) handleMCPDeleteMsg(msg panels.MCPDeleteMsg) (tea.Model, tea.Cmd) {
	return m, m.finalizeUpdate(m.handleMCPDelete(msg))
}

// handleMCPSaveMsg 处理 panels.MCPSaveMsg（Update 分支提取）。fall-through。
func (m *AppModel) handleMCPSaveMsg(_ panels.MCPSaveMsg) (tea.Model, tea.Cmd) {
	return m, m.finalizeUpdate(m.handleMCPSave())
}

// handleMCPConfigCancelMsg 处理 setup.MCPConfigCancelMsg（Update 分支提取）。fall-through。
func (m *AppModel) handleMCPConfigCancelMsg(_ setup.MCPConfigCancelMsg) (tea.Model, tea.Cmd) {
	m.activeView = "panel"
	m.activePanel = "mcp"
	m.shell.ClearInput()
	return m, m.finalizeUpdate(nil)
}

// handleMCPConfigSubmitMsg 处理 setup.MCPConfigSubmitMsg（Update 分支提取）。fall-through。
func (m *AppModel) handleMCPConfigSubmitMsg(msg setup.MCPConfigSubmitMsg) (tea.Model, tea.Cmd) {
	return m, m.finalizeUpdate(m.handleMCPConfigSubmit(msg))
}
