package ui

// Copyright (c) 2026 EOSAIOS
// SPDX-License-Identifier: EOS-NCL-1.1

import (
	"context"
	"fmt"
	"strings"

	"github.com/eosaios/eos/internal/i18n"
	"github.com/eosaios/eos/internal/ui/views/confirm"
	tea "charm.land/bubbletea/v2"
)

// browser_takeover 本地确认框 kind（与 bg_kill/workspace_trust 同模式：
// 确认结果不回内核 approval 通路，直接调 browser/control_resume RPC）。
const browserTakeoverKind = "browser_takeover"

// handleBrowserTakeoverStarted 人工接管开始：系统消息 + 本地确认框
//（样式与交互对齐既有 confirm 浮层--up/down/enter/esc）。
func (m *AppModel) handleBrowserTakeoverStarted(msg BrowserTakeoverStartedMsg) (tea.Model, tea.Cmd) {
	reason := strings.TrimSpace(msg.Reason)
	if reason == "" {
		reason = "manual"
	}
	note := strings.TrimSpace(msg.Note)
	question := fmt.Sprintf(i18n.T("browser.takeover.question", m.state.Language), reason)
	if note != "" {
		question = question + "\\n" + note
	}
	m.appendSystem(i18n.T("browser.takeover.started", m.state.Language), "warning")
	req := confirm.Request{
		ID:       "browser-takeover",
		Kind:     browserTakeoverKind,
		Title:    i18n.T("browser.takeover.title", m.state.Language),
		Question: question,
		Options:  []string{i18n.T("browser.takeover.resume", m.state.Language), i18n.T("browser.takeover.wait", m.state.Language)},
	}
	if m.confirmView == nil {
		m.prevView = m.activeView
	}
	m.browserTakeoverConfirm = true
	m.confirmView = confirm.New(m.styles, m.state.Language, m.diffHighlightTheme(), req)
	m.confirmView.SetSize(m.width, m.height)
	m.activeView = "confirm"
	m.shell.BlurInput()
	return m, nil
}

// handleBrowserTakeoverEnded 接管结束：关确认框回 shell。
func (m *AppModel) handleBrowserTakeoverEnded(msg BrowserTakeoverEndedMsg) (tea.Model, tea.Cmd) {
	m.appendSystem(fmt.Sprintf(i18n.T("browser.takeover.ended", m.state.Language), msg.Result), "info")
	if m.browserTakeoverConfirm {
		m.browserTakeoverConfirm = false
		m.confirmView = nil
		if m.prevView != "" {
			m.activeView = m.prevView
			m.prevView = ""
		} else {
			m.activeView = "shell"
		}
		m.shell.FocusInput()
	}
	return m, nil
}

// handleBrowserActionMsg 步骤日志行（browser.action）。
func (m *AppModel) handleBrowserActionMsg(msg BrowserActionMsg) (tea.Model, tea.Cmd) {
	mark := "ok"
	if msg.Result != "" && msg.Result != "ok" {
		mark = msg.Result
	}
	target := strings.TrimSpace(msg.Target)
	if target != "" {
		target = " " + target
	}
	m.appendSystem(fmt.Sprintf("[browser] %s%s (%s)", msg.Action, target, mark), "info")
	return m, nil
}

// handleBrowserDownloadDoneMsg 下载完成提示（含落盘路径）。
func (m *AppModel) handleBrowserDownloadDoneMsg(msg BrowserDownloadDoneMsg) (tea.Model, tea.Cmd) {
	m.appendSystem(fmt.Sprintf("[browser] %s: %s", i18n.T("browser.download.completed", m.state.Language), msg.Path), "success")
	return m, nil
}

// handleBrowserPickSelectedMsg 选取器结构化引用插入输入行。
func (m *AppModel) handleBrowserPickSelectedMsg(msg BrowserPickSelectedMsg) (tea.Model, tea.Cmd) {
	chip := msg.FormatPickChip()
	current := m.shell.GetInputValue()
	next := chip
	if strings.TrimSpace(current) != "" {
		next = current + "\\n" + chip
	}
	m.shell.SetInputValue(next)
	m.appendSystem(fmt.Sprintf("[browser] %s: %s", i18n.T("browser.pick.inserted", m.state.Language), chip), "info")
	return m, nil
}

// handleConfirmResultBrowserTakeover 确认框结果：交还 AI。
func (m *AppModel) handleConfirmResultBrowserTakeover(msg confirm.ResultMsg) (tea.Model, tea.Cmd) {
	m.browserTakeoverConfirm = false
	m.confirmView = nil
	if m.prevView != "" {
		m.activeView = m.prevView
		m.prevView = ""
	} else {
		m.activeView = "shell"
	}
	m.shell.FocusInput()
	if msg.OptionIndex == 0 {
		if err := m.adapter.BrowserControlResume(context.Background()); err != nil {
			m.appendSystem(fmt.Sprintf(i18n.T("browser.takeover.resume_failed", m.state.Language), err), "error")
		}
	}
	return m, nil
}
