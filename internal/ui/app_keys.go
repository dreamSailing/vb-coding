package ui

// app_keys.go — 全局键盘快捷键处理（handleGlobalKey）。
//
// handleGlobalKey 在 Update 的 KeyMsg 分支里被调用，处理那些跨视图
// 全局生效的快捷键（Ctrl+C / F2 / ? / Esc / Tab / Ctrl+O / Alt+V / Alt+H）。
// 这些快捷键的优先级高于 shell 自身的按键处理。
//
// 代码原位于 app.go，仅做物理拆分，不改行为。

import (
	"strings"

	"github.com/eosaios/eos/internal/i18n"
	"github.com/eosaios/eos/internal/state"
	"github.com/eosaios/eos/internal/ui/views/shell"

	tea "charm.land/bubbletea/v2"
)

// handleGlobalKey 处理全局快捷键，这些快捷键在所有视图中都有效
func (m *AppModel) handleGlobalKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "ctrl+c":
		// Ctrl+C：如果正在处理则取消，否则退出应用
		if m.state.Processing {
			m.cancelProcessingUI()
			return nil
		}
		return tea.Quit

	case "f2":
		// F2：切换 AI/Bash 模式
		m.shell.ToggleMode()
		if m.shell.GetMode() == shell.ModeAI {
			m.state.Mode = "ai"
		} else {
			m.state.Mode = "bash"
		}
		return nil

	case "?":
		// ?：打开帮助面板
		m.clearPrediction()
		m.activeView = "help"
		if m.helpView != nil {
			m.helpView.ResetScroll()
			m.helpView.SetSize(m.width, m.height)
		}
		return nil

	case "esc":
		// Esc：如果正在处理则取消当前请求，否则隐藏提示并清空输入
		if m.state.Processing {
			if m.cancelActiveRequest() {
				m.markInflightToolsCanceled(i18n.T("toast.stopped", m.state.Language))
				m.cancelProcessingUI()
				m.appendSystem(i18n.T("toast.stopped", m.state.Language), "warning")
				return func() tea.Msg { return nil }
			}
		}
		m.shell.HideHints()
		m.clearPrediction()
		m.shell.ClearInput()
		m.clearSelection()
		return func() tea.Msg { return nil } // 返回非nil阻止进入else分支

	case "tab":
		// Tab：如果有预测则接受预测，否则切换思考状态
		if m.activeView == "shell" && m.shell.GetMode() == shell.ModeAI && !m.shell.IsHintsVisible() {
			if m.shell.CanAcceptPrediction() {
				m.shell.HandleKey(msg)
				m.syncPredictionState()
				return func() tea.Msg { return nil }
			}
			state.SetThinking(!state.Thinking())
			m.refreshAILive()
			return func() tea.Msg { return nil }
		}
	case "ctrl+o":
		// Ctrl+O：循环切换实时面板显示模式
		if m.activeView == "shell" && m.shell != nil && m.shell.GetMode() == shell.ModeAI {
			m.shell.CycleLivePanelMode()
			return func() tea.Msg { return nil }
		}
	case "alt+v":
		// Alt+V：粘贴剪贴板中的图片
		return m.pasteClipboardImage()
	case "alt+h":
		// Alt+H：切换思考过程展开/折叠
		if m.activeView == "shell" && m.shell != nil && m.shell.GetMode() == shell.ModeAI && m.state.Thinking && strings.TrimSpace(m.thinkingLive.String()) != "" {
			return m.toggleThinkingExpand()
		}
	}
	return nil
}
