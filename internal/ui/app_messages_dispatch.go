package ui

// app_messages_dispatch.go — 消息流相关 Update 分支的调度处理方法。
//
// 本文件只承载「Update case → 具体逻辑」的薄封装方法（handleXxxMsg），
// 底层渲染 / 历史维护 / AI 响应 / item / tool 等实现位于 app_messages.go。
// 这些方法把原本写在 Update switch case 里的逻辑按「早退」或「fall-through」
// 语义封装：fall-through 的方法调用 finalizeUpdate 走公共收尾，早退的直接返回。
//
// 代码原位于 app.go 的 Update 函数体内，仅做物理拆分与 case 体提取，不改行为。

import (
	"fmt"
	"strings"
	"time"

	"github.com/eosaios/eos/internal/i18n"
	"github.com/eosaios/eos/internal/ui/views/setup"

	tea "charm.land/bubbletea/v2"
)

// handleItemStartedMsg 处理 ItemStartedMsg（Update 分支提取）。
// 原行为：早退（return m, nil），不走尾部逻辑。
func (m *AppModel) handleItemStartedMsg(msg ItemStartedMsg) (tea.Model, tea.Cmd) {
	// A new AgentMessage text segment begins. Create a fresh history entry
	// and reset the live buffer so each round renders as its own paragraph.
	switch msg.ItemType {
	case "agent_message", "":
		m.startAgentMessageItem(msg.ItemID)
	case "reasoning":
		// A reasoning item begins: archive any in-progress text segment and
		// reset the thinking buffer so this block streams on its own.
		m.startReasoningItem(msg.ItemID)
	}
	return m, nil
}

// handleItemDeltaMsg 处理 ItemDeltaMsg（Update 分支提取）。
// 原行为：Early return，不走尾部逻辑（避免逐 token RPC 阻塞）。
func (m *AppModel) handleItemDeltaMsg(msg ItemDeltaMsg) (tea.Model, tea.Cmd) {
	if !m.state.Processing {
		return m, nil
	}
	m.handleItemDelta(msg)
	// Early return: per-token deltas must NOT fall through to the default path,
	// which calls updateContextUsageUI + updateBGTaskCountUI (2 synchronous
	// JSON-RPC round-trips). With hundreds of deltas per turn that blocks the
	// Update loop and freezes streaming until all RPCs complete.
	return m, nil
}

// handleItemCompletedMsg 处理 ItemCompletedMsg（Update 分支提取）。fall-through。
func (m *AppModel) handleItemCompletedMsg(msg ItemCompletedMsg) (tea.Model, tea.Cmd) {
	m.handleItemCompleted(msg)
	return m, m.finalizeUpdate(nil)
}

// handleInvokeDoneMsg 处理 InvokeDoneMsg（turn.completed，Update 分支提取）。
// 原行为：有早退（!Processing）与 fall-through 两条分支。
func (m *AppModel) handleInvokeDoneMsg(msg InvokeDoneMsg) (tea.Model, tea.Cmd) {
	if !m.state.Processing {
		return m, nil
	}
	// turn.completed: archive any remaining live text, then finalize.
	// Under the item model, most segments were already archived via
	// item_completed; this is a safety net for any trailing buffer.
	m.archiveAgentMessage()
	m.aiLive.Reset()
	m.activeItemID = ""
	m.shell.ClearLive()
	m.clearCurrentThinking()
	m.shell.SetStatusHints(false, false)
	m.state.Processing = false
	m.shell.SetProcessing(false)
	m.activeCancel = nil
	m.stopRequested = false
	_ = msg.Content
	// turn 结束后拉一次 git 概览：决定提交提醒 + 顺带刷新状态栏 git 项。
	return m, m.finalizeUpdate(m.scheduleGitCommitReminder())
}

// handleThinkingMsg 处理 ThinkingMsg（Update 分支提取）。
// 原行为：有早退（!Processing）与 fall-through（带 cmds）两条分支。
func (m *AppModel) handleThinkingMsg(msg ThinkingMsg) (tea.Model, tea.Cmd) {
	if !m.state.Processing {
		return m, nil
	}
	m.state.Thinking = true
	m.thinkingLive.WriteString(msg.Content)
	m.shell.SetThinking(true, "")
	m.refreshAILive()
	if !msg.Done {
		return m, m.finalizeUpdate(m.shell.StatusTick())
	}
	return m, m.finalizeUpdate(nil)
}

// handleToolCallMsg 处理 ToolCallMsg（Update 分支提取）。fall-through。
func (m *AppModel) handleToolCallMsg(msg ToolCallMsg) (tea.Model, tea.Cmd) {
	cmd := m.handleToolCall(msg)
	return m, m.finalizeUpdate(cmd)
}

// handleToolResultMsg 处理 ToolResultMsg（Update 分支提取）。fall-through。
func (m *AppModel) handleToolResultMsg(msg ToolResultMsg) (tea.Model, tea.Cmd) {
	cmd := m.handleToolResult(msg)
	return m, m.finalizeUpdate(cmd)
}

// handleAgentTaskMsg 处理 AgentTaskMsg（Update 分支提取）。fall-through。
func (m *AppModel) handleAgentTaskMsg(msg AgentTaskMsg) (tea.Model, tea.Cmd) {
	m.delegatedThisRound = true
	m.shell.ClearLive()
	m.aiLive.Reset()
	m.clearCurrentThinking()
	m.appendHistory(historyEntry{
		kind:          "agent.task",
		agentName:     msg.AgentName,
		agentID:       msg.AgentID,
		agentEvent:    msg.Event,
		sourceAgent:   msg.SourceAgentName,
		sourceAgentID: msg.SourceAgentID,
		task:          msg.Task,
		timestamp:     time.Now(),
	})
	return m, m.finalizeUpdate(nil)
}

// handleAgentFinalMsg 处理 AgentFinalMsg（Update 分支提取）。fall-through。
func (m *AppModel) handleAgentFinalMsg(msg AgentFinalMsg) (tea.Model, tea.Cmd) {
	m.delegatedThisRound = true
	m.shell.ClearLive()
	m.aiLive.Reset()
	m.clearCurrentThinking()
	m.lastAgentFinal = msg.Content
	m.appendHistory(historyEntry{
		kind:          "agent.final",
		agentName:     msg.AgentName,
		agentID:       msg.AgentID,
		agentEvent:    msg.Event,
		sourceAgent:   msg.SourceAgentName,
		sourceAgentID: msg.SourceAgentID,
		content:       msg.Content,
		rawMarkdown:   msg.Content,
		executionMode: m.state.ExecutionMode,
		timestamp:     time.Now(),
	})
	m.state.Processing = false
	m.shell.SetProcessing(false)
	m.shell.SetStatusHints(false, false)
	m.activeCancel = nil
	m.stopRequested = false
	return m, m.finalizeUpdate(nil)
}

// handleErrorMsg 处理 ErrorMsg（Update 分支提取）。fall-through。
func (m *AppModel) handleErrorMsg(msg ErrorMsg) (tea.Model, tea.Cmd) {
	m.appendHistory(historyEntry{kind: "system", content: msg.Err.Error(), level: "error"})
	m.cancelProcessingUI()
	return m, m.finalizeUpdate(nil)
}

// handleClearCopiedMsg 处理 clearCopiedMsg（Update 分支提取）。fall-through。
func (m *AppModel) handleClearCopiedMsg(msg clearCopiedMsg) (tea.Model, tea.Cmd) {
	if msg.idx >= 0 && msg.idx < len(m.history) {
		m.history[msg.idx].copiedAt = time.Time{}
		m.rebuildHistoryContent()
	}
	return m, m.finalizeUpdate(nil)
}

// handleModeChangedMsg 处理 ModeChangedMsg（Update 分支提取）。fall-through。
func (m *AppModel) handleModeChangedMsg(msg ModeChangedMsg) (tea.Model, tea.Cmd) {
	mode := strings.TrimSpace(msg.Mode)
	if mode != "" {
		m.state.ExecutionMode = mode
		m.shell.SetExecutionMode(mode)
	}
	return m, m.finalizeUpdate(nil)
}

// handleMouseMsg 处理 tea.MouseMsg（Update 分支提取）。
// 各分支均早退或经 finalizeUpdate 走尾部逻辑。
func (m *AppModel) handleMouseMsg(msg tea.MouseMsg, cmds []tea.Cmd) (tea.Model, tea.Cmd) {
	if m.activeView == "shell" {
		// 先尝试框选/点击（selection.go）；消耗事件则不再交给 shell/viewport。
		if m.handleContentSelection(msg) {
			return m, m.finalizeUpdate(tea.Batch(cmds...))
		}
		if click, ok := msg.(tea.MouseClickMsg); ok && click.Button == tea.MouseLeft {
			if cmd := m.tryHandleBubbleActionAt(click.Mouse().X, click.Mouse().Y); cmd != nil {
				cmds = append(cmds, cmd)
				return m, tea.Batch(cmds...)
			}
		}
		updatedShell, shellCmd := m.shell.Update(msg)
		*m.shell = updatedShell
		if shellCmd != nil {
			cmds = append(cmds, shellCmd)
		}
	} else if m.activeView == "panel" && m.activePanel != "" {
		if panel, ok := m.panels[m.activePanel]; ok {
			updatedPanel, cmd := panel.Update(msg)
			m.panels[m.activePanel] = updatedPanel
			return m, m.handlePanelMsg(cmd)
		}
	} else if m.activeView == "help" && m.helpView != nil {
		updated, cmd := m.helpView.Update(msg)
		m.helpView = updated
		return m, cmd
	}
	return m, m.finalizeUpdate(tea.Batch(cmds...))
}

// handleSetupCompleteMsg 处理 setup.SetupCompleteMsg（Update 分支提取）。fall-through。
func (m *AppModel) handleSetupCompleteMsg(msg setup.SetupCompleteMsg) (tea.Model, tea.Cmd) {
	// 设置完成
	m.activeView = "shell"
	m.initialSetupFlow = false
	m.shell.FocusInput()
	m.appendSystem(fmt.Sprintf("Setup complete! Provider: %s, Model: %s", msg.Config.Provider, msg.Config.Model), "info")
	return m, m.finalizeUpdate(nil)
}

// handleSetupCancelMsg 处理 setup.SetupCancelMsg（Update 分支提取）。fall-through。
func (m *AppModel) handleSetupCancelMsg(_ setup.SetupCancelMsg) (tea.Model, tea.Cmd) {
	// 设置取消
	m.activeView = "shell"
	m.initialSetupFlow = false
	m.shell.FocusInput()
	m.appendSystem(i18n.T("setup.cancelled", m.state.Language), "warning")
	return m, m.finalizeUpdate(nil)
}

// handleWindowSizeMsg 处理 tea.WindowSizeMsg（Update 分支提取）。
// 负责把新尺寸同步到 shell / help / confirm / actionPopup / 渲染器 /
// setup 视图 / 所有面板，并重建历史与实时显示。原行为：fall-through。
func (m *AppModel) handleWindowSizeMsg(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	m.width, m.height = msg.Width, msg.Height
	m.shell.SetSize(msg.Width, msg.Height)
	m.updateInlinePermissionUI()
	m.helpView.SetSize(msg.Width, msg.Height)
	if m.confirmView != nil {
		m.confirmView.SetSize(msg.Width, msg.Height)
	}
	if m.actionPopup != nil {
		m.actionPopup.SetSize(msg.Width, msg.Height)
	}
	// 更新消息渲染器宽度
	if m.msgRenderer != nil {
		m.msgRenderer.SetWidth(msg.Width - 4)
	}
	m.refreshAILive()
	m.rebuildHistoryContent()
	// 更新 setup 视图大小
	switch sv := m.setupView.(type) {
	case *setup.SetupView:
		sv.SetSize(msg.Width, msg.Height)
	case *setup.ModelSetupView:
		sv.SetSize(msg.Width, msg.Height)
	}
	for _, p := range m.panels {
		p.SetSize(msg.Width, msg.Height)
	}
	return m, m.finalizeUpdate(nil)
}

// handleShowHintsMsg 处理 ShowHintsMsg（Update 分支提取）。fall-through。
func (m *AppModel) handleShowHintsMsg(msg ShowHintsMsg) (tea.Model, tea.Cmd) {
	// 显示提示
	switch msg.Type {
	case "slash":
		m.shell.ShowSlashHints("")
	case "path":
		m.shell.ShowPathHints("")
	}
	return m, m.finalizeUpdate(nil)
}
