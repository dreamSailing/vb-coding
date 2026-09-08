package ui

// Copyright (c) 2026 EOSAIOS
// SPDX-License-Identifier: EOS-NCL-1.1
// 本文件基于 EOS 非商用许可证 v1.1 发布，详见 LICENSE。
// 商业使用请联系版权人获得商业授权。

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

func (m *AppModel) handleVerifySlash(args []string) tea.Cmd {
	if m == nil || m.adapter == nil {
		return nil
	}
	if m.state.Processing {
		m.appendSystem(m.localize("当前已有任务在运行，请等待完成后再发起验证。", "A task is already running. Wait for it to finish before starting verification."), "warning")
		return nil
	}

	request := strings.TrimSpace(buildVerifyPrompt(m, args))
	if request == "" {
		m.appendSystem(m.localize("无法生成验证请求。", "Failed to build verification request."), "error")
		return nil
	}

	m.shell.AddToHistory("/verify")
	m.clearPrediction()
	m.shell.ClearInput()
	m.state.Processing = true
	m.shell.SetProcessing(true)
	m.delegatedThisRound = false
	m.aiLive.Reset()
	m.clearCurrentThinking()
	m.shell.SetStatusHints(false, false)
	m.shell.ClearLive()
	m.currentAIStartTime = time.Now()
	m.currentAITokens = 0
	m.setActiveCancel(func() {
		m.adapter.CancelForegroundRequest()
	})

	display := m.localize("发起验证请求", "Start verification request")
	m.appendHistory(historyEntry{kind: "user", content: display, timestamp: time.Now()})
	m.appendSystem(request, "info")

	invoke := func() tea.Msg {
		ctx := context.Background()
		content, err := m.adapter.Invoke(ctx, request, m.state.ExecutionMode, nil, m.memoryInjectionEnabled)
		if err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "context canceled") {
				return nil
			}
			return ErrorMsg{Err: err}
		}
		return InvokeDoneMsg{Content: content}
	}
	return tea.Batch(invoke, m.shell.StatusTick())
}

func buildVerifyPrompt(m *AppModel, args []string) string {
	target := strings.TrimSpace(strings.Join(args, " "))
	root := ""
	if m != nil {
		root = strings.TrimSpace(m.currentWorkspaceRoot())
	}
	rootLabel := root
	if rootLabel == "" {
		rootLabel = m.localize("当前工作区未设置", "workspace not set")
	}

	detections := []verifierProjectDetection(nil)
	if root != "" {
		if got, err := detectVerifierProjectTypes(root); err == nil {
			detections = got
		}
	}

	lines := []string{
		m.localize("请以独立验证代理的方式验收当前改动。", "Please verify the current changes as an independent verification agent."),
		m.localize("不要被 80% 的成功欺骗。优先尝试证明实现在哪里会失败，并明确未覆盖的风险。", "Do not be fooled by 80% success. Try to find where the implementation fails and call out uncovered risks."),
		fmt.Sprintf("%s: %s", m.localize("工作区", "Workspace"), rootLabel),
	}

	if target != "" {
		lines = append(lines, fmt.Sprintf("%s: %s", m.localize("本次重点", "Focus"), target))
	}

	if len(detections) > 0 {
		typeNames := make([]string, 0, len(detections))
		for _, item := range detections {
			typeNames = append(typeNames, verifierProjectTypeLabel(item.Type, m.state.Language))
		}
		lines = append(lines, fmt.Sprintf("%s: %s", m.localize("检测到的项目类型", "Detected project types"), strings.Join(typeNames, ", ")))
		lines = append(lines, m.localize("优先按以下验证方向执行：", "Prioritize the following verification directions:"))
		for _, suggestion := range verifierToolSuggestions(detections) {
			lines = append(lines, fmt.Sprintf("- %s: %s", suggestion.Tool, localizeSuggestion(suggestion, m.state.Language)))
		}
	}

	lines = append(lines,
		m.localize("请输出 VERDICT: PASS、FAIL 或 PARTIAL。", "Return VERDICT: PASS, FAIL, or PARTIAL."),
		m.localize("请按固定标题列出：验收摘要、覆盖到的验证项、未覆盖的风险和空白、关键证据。", "Use fixed headings: verification summary, covered checks, uncovered risks, and key evidence."),
	)
	return strings.Join(lines, "\n")
}
