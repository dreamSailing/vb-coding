package ui

// app_send.go — 用户消息发送、预测与输入提示相关逻辑。
//
// 本文件包含：
//   - sendMessage / sendBashCommand：发送 AI 消息与 Bash 命令
//   - shouldSendMessage：判断是否发送，处理斜杠命令分发
//   - 预测（next-message prediction）：schedulePrediction / requestPrediction /
//     syncPredictionState / clearPrediction / canPredict
//   - predictionDebounceMsg / ShowHintsMsg 消息类型
//   - updateHintsBasedOnInput：根据输入内容更新 slash / path hints
//   - pasteClipboardImage / toggleThinkingExpand
//   - handleKeyMsg：Update 中 KeyMsg case 的全部键盘路由逻辑。
//
// 这些代码原位于 app.go，仅做物理拆分，不改行为。

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/eosaios/eos/internal/ai"
	"github.com/eosaios/eos/internal/i18n"
	"github.com/eosaios/eos/internal/pkg/clip"
	"github.com/eosaios/eos/internal/ui/features/slash"
	"github.com/eosaios/eos/internal/ui/panels"
	"github.com/eosaios/eos/internal/ui/views/setup"
	"github.com/eosaios/eos/internal/ui/views/shell"

	tea "charm.land/bubbletea/v2"
)

// predictionDebounceMsg 预测防抖消息，用于延迟触发下一条消息预测
type predictionDebounceMsg struct {
	Seq   int    // 序列号，用于丢弃过期的防抖消息
	Draft string // 当时的输入草稿内容
}

// ShowHintsMsg 显示提示消息
type ShowHintsMsg struct {
	Type string // "help" 或 "slash"
}

// clearPrediction 清除预测文本，递增序列号使旧预测失效
func (m *AppModel) clearPrediction() {
	if m == nil {
		return
	}
	m.predictionSeq++
	m.predictionDebounceSeq++
	m.predictionText = ""
	if m.shell != nil {
		m.shell.ClearPrediction()
	}
}

// syncPredictionState 同步 shell 的预测状态到本地缓存
func (m *AppModel) syncPredictionState() {
	if m == nil || m.shell == nil {
		return
	}
	if !m.shell.HasPrediction() {
		m.predictionText = ""
	}
}

// canPredict 判断当前是否可以触发下一条消息预测
// 需要满足：模型适配器就绪、shell 就绪、预测功能开启、
// 处于 shell 视图、AI 模式、且不在处理中
func (m *AppModel) canPredict() bool {
	return m != nil &&
		m.adapter != nil &&
		m.shell != nil &&
		m.predictionEnabled &&
		m.activeView == "shell" &&
		m.state.Mode == "ai" &&
		!m.state.Processing
}

// schedulePrediction 调度一次预测请求，300ms 防抖后触发
func (m *AppModel) schedulePrediction(draft string) tea.Cmd {
	if !m.canPredict() {
		m.clearPrediction()
		return nil
	}
	m.predictionDebounceSeq++
	seq := m.predictionDebounceSeq
	return tea.Tick(300*time.Millisecond, func(time.Time) tea.Msg {
		return predictionDebounceMsg{Seq: seq, Draft: draft}
	})
}

// requestPrediction 向适配器请求下一条用户消息的预测建议
func (m *AppModel) requestPrediction(draft string) tea.Cmd {
	if m == nil || m.adapter == nil || m.shell == nil {
		return nil
	}
	if !m.canPredict() {
		m.clearPrediction()
		return nil
	}
	m.predictionSeq++
	seq := m.predictionSeq
	return func() tea.Msg {
		text, err := m.adapter.PredictNextUserMessage(context.Background(), draft)
		if err != nil {
			return PredictionUpdateMsg{Seq: seq, Draft: draft}
		}
		return PredictionUpdateMsg{Seq: seq, Draft: draft, Text: text}
	}
}

// shouldSendMessage 检查是否应该发送消息，如果是 /exit 命令返回 true 和退出命令
func (m *AppModel) shouldSendMessage() (bool, tea.Cmd) {
	value := m.shell.GetInputValue()
	if value == "" {
		if m.state.Mode == "ai" && !m.state.Processing && len(m.pendingImagePaths) > 0 {
			return true, nil
		}
		return false, nil
	}

	// 如果只是单独的 / 或 @（用于触发提示），不发送
	if value == "/" || value == "@" {
		return false, nil
	}

	// 检查是否是斜杠命令（必须是有效命令）
	if cmd, args, isCmd := slash.ParseCommand(value); isCmd {
		if normalized := slash.NormalizeCommand(cmd); normalized != "" {
			exitCmd := m.handleSlashCommand(normalized, args)
			m.shell.ClearInput()
			if exitCmd != nil {
				return false, exitCmd
			}
			return false, nil
		}
		if len(cmd) > 1 {
			skillName := strings.TrimPrefix(cmd, "/")
			skillName = strings.TrimSpace(skillName)
			if skillName != "" && m.tryInvokeSkillSlash(skillName, args) {
				m.shell.ClearInput()
				return false, nil
			}
		}
	}

	return m.state.Mode == "ai" && !m.state.Processing, nil
}

// sendMessage 发送用户消息到 AI 引擎
// 1. 展开特殊宏（如 #problems_and_diagnostics）
// 2. 更新 UI 状态为处理中
// 3. 处理图片附件
// 4. 异步调用适配器执行 AI 请求
func (m *AppModel) sendMessage() tea.Cmd {
	value := m.shell.GetInputValue()
	return m.sendMessageText(value, true)
}

// sendMessageText 把一段文本作为用户消息直派给 AI 引擎（不经输入框）。
// withPendingImages=true 时消费输入框上挂起的图片附件（用户主动发送路径）；
// 程序化派发（如 /commit 提交推送指令）传 false，不碰用户未发送的附件。
func (m *AppModel) sendMessageText(text string, withPendingImages bool) tea.Cmd {
	if m == nil || m.adapter == nil {
		return nil
	}
	if strings.TrimSpace(text) == "" && (!withPendingImages || len(m.pendingImagePaths) == 0) {
		return nil
	}
	expanded := text
	// 展开 LSP 诊断宏
	if strings.Contains(strings.ToLower(expanded), "#problems_and_diagnostics") {
		md := ""
		if m.adapter != nil {
			md = m.adapter.LSPDiagnosticsMarkdown(context.Background())
		}
		if strings.TrimSpace(md) != "" {
			re := regexp.MustCompile(`(?i)#problems_and_diagnostics`)
			expanded = re.ReplaceAllStringFunc(expanded, func(string) string { return md })
		}
	}
	m.shell.AddToHistory(text)
	m.clearPrediction()
	m.shell.ClearInput()
	m.state.Processing = true
	m.shell.SetProcessing(true)
	m.delegatedThisRound = false
	m.agentTextCommittedThisTurn = false
	m.activeItemID = ""
	m.aiLive.Reset()
	m.clearCurrentThinking()
	m.shell.SetStatusHints(false, false)
	m.shell.ClearLive()

	// 记录 AI 开始时间和 token 计数
	m.currentAIStartTime = time.Now()
	m.currentAITokens = 0
	m.setActiveCancel(func() {
		m.adapter.CancelForegroundRequest()
	})

	// 处理图片附件
	var imagePaths []string
	if withPendingImages {
		imagePaths = m.pendingImagePaths
		m.pendingImagePaths = nil
		if len(imagePaths) > 0 {
			var names []string
			for _, p := range imagePaths {
				b := strings.TrimSpace(filepath.Base(p))
				if b != "" {
					names = append(names, b)
				}
				if len(names) >= 4 {
					break
				}
			}
			if len(names) > 0 {
				m.appendSystem(i18n.T("image.attached", m.state.Language)+strings.Join(names, ", "), "info")
			} else {
				m.appendSystem(i18n.T("image.attached_short", m.state.Language), "info")
			}
		}
	}

	// 显示用户消息
	display := text
	if strings.TrimSpace(display) == "" && len(imagePaths) > 0 {
		display = i18n.T("chat.image_only", m.state.Language)
	}
	m.appendHistory(historyEntry{kind: "user", content: display, timestamp: time.Now()})

	// 异步调用 AI 引擎
	invoke := func() tea.Msg {
		ctx := context.Background()
		content, err := m.adapter.Invoke(ctx, expanded, m.state.ExecutionMode, imagePaths, m.memoryInjectionEnabled)
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

// sendBashCommand 执行 Bash 命令
// 带有 30 秒超时，执行结果通过 ToolResultMsg 返回
func (m *AppModel) sendBashCommand() tea.Cmd {
	value := strings.TrimSpace(m.shell.GetInputValue())
	m.shell.AddToHistory(value)
	m.clearPrediction()
	m.shell.ClearInput()
	m.state.Processing = true
	m.shell.SetProcessing(true)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	m.setActiveCancel(cancel)

	id := fmt.Sprintf("bash:%d", time.Now().UnixNano())
	m.handleToolCall(ToolCallMsg{ID: id, Name: "bash", Params: map[string]any{"command": value}})

	exec := func() tea.Msg {
		defer cancel()
		out, err := m.adapter.ExecuteBash(ctx, value)
		if err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "context canceled") {
				return ToolResultMsg{ID: id, Status: "canceled"}
			}
			msg := strings.ReplaceAll(err.Error(), "\r\n", "\n")
			msg = strings.ReplaceAll(msg, "\r", "")
			return ToolResultMsg{ID: id, Status: "error", Output: msg}
		}
		out = strings.ReplaceAll(out, "\r\n", "\n")
		out = strings.ReplaceAll(out, "\r", "")
		return ToolResultMsg{ID: id, Status: "success", Output: strings.TrimRight(out, "\n")}
	}
	// Keep the status-bar spinner animating for the whole bash run.
	return tea.Batch(exec, m.shell.StatusTick())
}

// pasteClipboardImage 粘贴剪贴板中的图片到附件列表
// 仅在 AI 模式的 shell 视图中有效
func (m *AppModel) pasteClipboardImage() tea.Cmd {
	if m.activeView != "shell" {
		return func() tea.Msg { return nil }
	}
	if m.state.Mode != "ai" {
		m.appendSystem(i18n.T("image.bash_mode", m.state.Language), "warning")
		return func() tea.Msg { return nil }
	}
	b, err := clip.ReadImage()
	if err != nil {
		if strings.Contains(err.Error(), "empty clipboard image") {
			m.appendSystem(i18n.T("image.clipboard_empty", m.state.Language), "warning")
		} else {
			m.appendSystem(i18n.T("image.paste_failed", m.state.Language)+err.Error(), "error")
		}
		return func() tea.Msg { return nil }
	}
	// 保存图片到 .eos/attachments 目录
	wd, err := os.Getwd()
	if err != nil || strings.TrimSpace(wd) == "" {
		m.appendSystem(i18n.T("image.paste_failed_cwd", m.state.Language), "error")
		return func() tea.Msg { return nil }
	}
	dir := filepath.Join(wd, ".eos", "attachments")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		m.appendSystem(i18n.T("image.paste_failed", m.state.Language)+err.Error(), "error")
		return func() tea.Msg { return nil }
	}
	path := filepath.Join(dir, fmt.Sprintf("clipboard-%d.png", time.Now().UnixNano()))
	if err := os.WriteFile(path, b, 0o600); err != nil {
		m.appendSystem(i18n.T("image.paste_failed", m.state.Language)+err.Error(), "error")
		return func() tea.Msg { return nil }
	}
	m.pendingImagePaths = append(m.pendingImagePaths, path)
	m.appendSystem(i18n.T("image.added", m.state.Language)+filepath.Base(path), "success")
	// 检查模型是否支持视觉
	modelName, _ := m.adapter.GetModelInfo()
	if strings.TrimSpace(modelName) != "" && !ai.SupportsVisionFromCatalog(modelName) {
		m.appendSystem(i18n.T("image.vision_warning", m.state.Language), "warning")
	}
	return func() tea.Msg { return nil }
}

// toggleThinkingExpand 切换思考过程的展开/折叠状态
func (m *AppModel) toggleThinkingExpand() tea.Cmd {
	if m == nil || !m.state.Thinking || strings.TrimSpace(m.thinkingLive.String()) == "" {
		return nil
	}
	m.thinkingExpanded = !m.thinkingExpanded
	if m.shell != nil {
		m.shell.SetThinkingExpanded(m.thinkingExpanded)
	}
	m.refreshAILive()
	return func() tea.Msg { return nil }
}

// updateHintsBasedOnInput 根据输入内容更新 hints
func (m *AppModel) updateHintsBasedOnInput() {
	text := m.shell.GetInputValue()

	// 如果以 ? 开头，显示帮助提示（但现在 ? 是打开帮助面板，所以这里不需要）
	// 实际处理在 handleGlobalKey 中

	// 如果以 / 开头，显示斜杠命令提示
	if cmdLine, ok := strings.CutPrefix(text, "/"); ok {
		// 检查是否有空格（有参数时不显示提示）
		if !strings.Contains(cmdLine, " ") {
			m.shell.ShowSlashHints(cmdLine)
		} else {
			m.shell.HideHints()
		}
		return
	}

	// 如果包含 @，显示路径提示
	if strings.Contains(text, "@") {
		// 检查 @ 后面是否有空格
		i := strings.LastIndex(text, "@")
		if i >= 0 {
			// @ 是最后一个字符，或者 @ 后面没有空格
			if i+1 >= len(text) {
				// @ 是最后一个字符，显示所有路径
				m.shell.ShowPathHints("")
				return
			}
			q := text[i+1:]
			if !strings.ContainsAny(q, " \n\t") {
				m.shell.ShowPathHints(q)
				return
			}
		}
	}

	// 其他情况隐藏 hints
	m.shell.HideHints()
}

// handleKeyMsg 处理 tea.KeyMsg（Update 中 KeyMsg case 的全部逻辑提取）。
// 该分支原为 fall-through（结束 switch 后跑尾部逻辑）。
func (m *AppModel) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	// 首先检查是否处于特殊视图
	if m.activeView == "confirm" && m.confirmView != nil {
		updated, cmd := m.confirmView.Update(msg)
		m.confirmView = updated
		return m, cmd
	}

	// 操作弹框拦截按键（覆盖在 shell 之上，不切换 activeView）
	if m.actionPopup != nil {
		updated, cmd := m.actionPopup.Update(msg)
		m.actionPopup = updated
		if cmd != nil {
			return m, cmd
		}
		return m, nil
	}

	if m.activeView == "help" {
		if msg.String() == "esc" || msg.String() == "q" {
			m.activeView = "shell"
			m.shell.ClearInput()
			return m, nil
		}
		updated, cmd := m.helpView.Update(msg)
		m.helpView = updated
		return m, cmd
	}

	if m.activeView == "setup" {
		switch sv := m.setupView.(type) {
		case *setup.SetupView:
			updated, cmd := sv.Update(msg)
			m.setupView = updated
			return m, cmd
		case *setup.ModelSetupView:
			updated, cmd := sv.Update(msg)
			m.setupView = updated
			return m, cmd
		case *setup.MCPConfigEditorView:
			updated, cmd := sv.Update(msg)
			m.setupView = updated
			return m, cmd
		}
	}

	if m.activeView == "panel" && m.activePanel != "" {
		if msg.String() == "esc" {
			if m.activePanel == "context" {
				if p, ok := m.panels[m.activePanel].(*panels.ContextPanel); ok && p != nil && p.IsViewing() {
					p.ResetView()
					m.panels[m.activePanel] = p
					return m, nil
				}
			}
			if m.activePanel == "tasks" {
				if p, ok := m.panels[m.activePanel].(*panels.TasksPanel); ok && p != nil && p.IsViewing() {
					p.ResetView()
					m.panels[m.activePanel] = p
					return m, nil
				}
			}
			if m.activePanel == "rules" {
				if p, ok := m.panels[m.activePanel].(*panels.RulesPanel); ok && p != nil && p.IsEditing() {
					p.CancelEdit()
					m.panels[m.activePanel] = p
					return m, nil
				}
			}
			if m.activePanel == "memory" {
				if p, ok := m.panels[m.activePanel].(*panels.MemoryPanel); ok && p != nil && p.IsEditing() {
					p.CancelEdit()
					m.panels[m.activePanel] = p
					return m, nil
				}
			}
			m.activeView = "shell"
			m.activePanel = ""
			m.shell.ClearInput()
			return m, nil
		}
		if panel, ok := m.panels[m.activePanel]; ok {
			updatedPanel, cmd := panel.Update(msg)
			m.panels[m.activePanel] = updatedPanel
			return m, m.handlePanelMsg(cmd)
		}
	}

	if m.activeView == "shell" && m.inlinePermissionReq != nil {
		if handled, cmd := m.handleInlinePermissionKey(msg); handled {
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
			return m, tea.Batch(cmds...)
		}
	}

	// 处理全局快捷键
	if cmd := m.handleGlobalKey(msg); cmd != nil {
		cmds = append(cmds, cmd)
	} else {
		inputBeforeKey := m.shell.GetInputValue()

		// 检查是否是 / 键（显示斜杠命令提示）- 只在输入框为空时触发
		if msg.String() == "/" && m.shell.GetInputValue() == "" {
			cmds = append(cmds, func() tea.Msg {
				return ShowHintsMsg{Type: "slash"}
			})
		}

		// 检查是否是 @ 键（显示路径提示）- 只在输入框为空时触发
		if msg.String() == "@" && m.shell.GetInputValue() == "" {
			cmds = append(cmds, func() tea.Msg {
				return ShowHintsMsg{Type: "path"}
			})
		}

		hintsVisibleBeforeKey := m.shell.IsHintsVisible()

		// 让 Shell 处理按键（处理 Enter, Esc, 历史导航等）
		handled, cmd := m.shell.HandleKey(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		m.syncPredictionState()

		// 如果 Shell 处理了非 Enter 键，不需要后续处理
		// 如果按 Enter 且 hints 显示，隐藏 hints 不发送
		// 如果按 Enter 且 hints 不显示，检查是否需要发送
		shouldUpdateShell := true
		shouldRefreshHints := true
		if hintsVisibleBeforeKey && handled {
			switch msg.String() {
			case "up", "down", "enter", "tab", "esc":
				shouldUpdateShell = false
				shouldRefreshHints = false
			}
		}
		if hintsVisibleBeforeKey && handled && (msg.String() == "enter" || msg.String() == "tab") {
			shouldUpdateShell = false
		}
		if msg.String() == "enter" {
			if hintsVisibleBeforeKey {
				shouldUpdateShell = false // hints 已处理，不需要再更新 shell
				shouldRefreshHints = false
			} else {
				shouldSend, exitCmd := m.shouldSendMessage()
				if exitCmd != nil {
					// /exit 命令，返回退出命令
					cmds = append(cmds, exitCmd)
					return m, tea.Batch(cmds...)
				}
				if shouldSend {
					cmds = append(cmds, m.sendMessage())
				} else if m.shell.GetMode() == shell.ModeBash && !m.state.Processing && strings.TrimSpace(m.shell.GetInputValue()) != "" {
					cmds = append(cmds, m.sendBashCommand())
				}
				// shouldSendMessage 返回 false 时，如果是斜杠命令已处理，输入框已清空，跳过 shell 更新
				// 否则（输入框为空等情况），继续更新 shell
				if m.shell.GetInputValue() == "" {
					shouldUpdateShell = false
				}
			}
		}

		// 更新 Shell 状态（确保输入能够显示）
		if shouldUpdateShell {
			updatedShell, shellCmd := m.shell.Update(msg)
			*m.shell = updatedShell
			m.syncPredictionState()
			if shellCmd != nil {
				cmds = append(cmds, shellCmd)
			}
		}

		// 输入变化后更新 hints
		if shouldRefreshHints {
			m.updateHintsBasedOnInput()
		}

		inputAfterKey := m.shell.GetInputValue()
		if inputAfterKey != inputBeforeKey {
			if cmd := m.schedulePrediction(inputAfterKey); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	}

	return m, m.finalizeUpdate(tea.Batch(cmds...))
}

// handlePredictionDebounceMsg 处理 predictionDebounceMsg（Update 分支提取）。
// 原行为：过期/不可预测/draft 不一致时早退，否则 fall-through。
func (m *AppModel) handlePredictionDebounceMsg(msg predictionDebounceMsg) (tea.Model, tea.Cmd) {
	if msg.Seq != m.predictionDebounceSeq {
		return m, nil
	}
	if !m.canPredict() {
		m.clearPrediction()
		return m, nil
	}
	if m.shell.GetInputValue() != msg.Draft {
		return m, nil
	}
	if cmd := m.requestPrediction(msg.Draft); cmd != nil {
		return m, m.finalizeUpdate(cmd)
	}
	return m, m.finalizeUpdate(nil)
}

// handlePredictionUpdateMsg 处理 PredictionUpdateMsg（Update 分支提取）。
// 原行为：多个早退分支（seq 不匹配 / 不可预测 / draft 不一致 / 空文本 / 前缀不符）。
func (m *AppModel) handlePredictionUpdateMsg(msg PredictionUpdateMsg) (tea.Model, tea.Cmd) {
	if msg.Seq != m.predictionSeq {
		return m, nil
	}
	if !m.canPredict() {
		m.clearPrediction()
		return m, nil
	}
	if m.shell.GetInputValue() != msg.Draft {
		return m, nil
	}
	text := strings.TrimSpace(msg.Text)
	if text == "" {
		m.clearPrediction()
		return m, nil
	}
	currentInput := m.shell.GetInputValue()
	if currentInput != "" {
		if !strings.HasPrefix(text, currentInput) || text == currentInput {
			m.clearPrediction()
			return m, nil
		}
	}
	m.predictionText = text
	m.shell.SetPrediction(text)
	return m, m.finalizeUpdate(nil)
}
