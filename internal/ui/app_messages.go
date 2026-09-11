package ui

// app_messages.go — 消息流渲染与历史记录维护。
//
// 本文件包含：
//   - 历史记录条目（historyEntry）/ 工具调用跟踪（toolTrack）类型定义
//   - 可点击消息区命中记录（bubbleActionHit）类型与判定
//   - 历史渲染与重建（renderHistoryEntry/appendHistory/rebuildHistoryContent 等）
//   - ANSI 清理与 rune 索引等渲染辅助
//   - AI 回复 / item.delta / reasoning / tool_call / tool_result /
//     agent.task / agent.final / error / thinking / invoke.done 等
//     Update 分支的处理方法（handleAIResponseMsg / handleItemStartedMsg / ...）。
//
// 这些代码原位于 app.go，仅做物理拆分，不改行为。

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/eosaios/eos/internal/i18n"
	"github.com/eosaios/eos/internal/state"
	"github.com/eosaios/eos/internal/ui/components/messages"
	"github.com/eosaios/eos/internal/ui/render"

	tea "charm.land/bubbletea/v2"
)

// ansiRe 匹配 ANSI 转义序列，用于清理文本中的终端样式代码
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

// clearCopiedMsg 清除复制成功提示的消息
type clearCopiedMsg struct {
	idx int // 要清除的历史记录索引
}

// toolTrack 跟踪正在执行的工具调用状态
type toolTrack struct {
	name    string         // 工具名称
	started time.Time      // 开始执行时间
	idx     int            // 在历史记录中的索引
	params  map[string]any // 工具调用参数
}

// historyEntry 历史记录条目，支持多种类型：用户输入、AI 回复、工具调用、系统消息等
type historyEntry struct {
	// 通用字段
	kind          string        // 条目类型："user", "ai", "tool", "system", "agent.task", "agent.final"
	content       string        // 条目内容
	timestamp     time.Time     // 时间戳
	tokens        int           // token 数量（用于 AI 回复）
	duration      time.Duration // 执行耗时
	level         string        // 消息级别（用于系统消息）："error", "warning", "success", "info"
	preStyled     bool          // 内容已含 ANSI 码（diff 高亮），渲染时跳过宽度折行
	executionMode string        // 执行模式（用于 AI 回复）："auto", "plan" 等
	rawMarkdown   string        // 原始 markdown 内容（用于计划下载）
	copiedAt      time.Time     // 复制时间（用于显示复制成功提示）

	// 工具调用相关字段
	toolID      string         // 工具调用唯一 ID
	toolName    string         // 工具名称
	toolParams  map[string]any // 工具调用参数
	toolOutput  string         // 工具执行输出
	toolSuccess bool           // 工具是否执行成功
	toolStatus  string         // 工具执行状态："running", "success", "error", "canceled"

	// Agent 相关字段
	agentName     string // Agent 名称
	agentID       string // Agent ID
	agentEvent    string // Agent 事件类型
	sourceAgent   string // 来源 Agent 名称
	sourceAgentID string // 来源 Agent ID
	task          string // Agent 任务描述
}

func (m *AppModel) renderHistoryEntry(e historyEntry) string {
	if m.msgRenderer == nil {
		switch e.kind {
		case "user", "ai", "system":
			return e.content
		case "tool":
			return e.toolOutput
		case "agent.final":
			return e.content
		case "reasoning":
			// Archived thinking block: render as a dim one-line summary (the
			// last non-empty line, truncated). Matches the persisted
			// reasoning summary and the eos-app collapsed "思考过程" block.
			return messages.LastNonEmptyLine(e.content)
		default:
			return e.content
		}
	}

	switch e.kind {
	case "user":
		return m.msgRenderer.RenderUserInputAt(e.content, e.timestamp)
	case "ai":
		return m.msgRenderer.RenderAIResponseAtWithActions(e.content, e.tokens, e.duration, true, e.timestamp, m.bubbleActionsForEntry(e))
	case "agent.task":
		return m.msgRenderer.RenderAgentTaskAt(e.agentName, e.agentID, e.sourceAgent, e.sourceAgentID, e.agentEvent, e.task, e.timestamp)
	case "tool":
		status := e.toolStatus
		if status == "" {
			if e.toolSuccess {
				status = "success"
			} else if e.toolOutput != "" {
				status = "error"
			} else {
				status = "running"
			}
		}
		return m.msgRenderer.RenderToolEvent(e.toolName, e.toolParams, status, e.toolOutput, e.duration)
	case "agent.final":
		return m.msgRenderer.RenderAgentFinalAtWithActions(e.agentName, e.agentID, e.sourceAgent, e.sourceAgentID, e.agentEvent, e.content, e.timestamp, m.bubbleActionsForEntry(e))
	case "system":
		if e.preStyled {
			return m.msgRenderer.RenderSystemPreStyled(e.content, e.level)
		}
		return m.msgRenderer.RenderSystem(e.content, e.level)
	case "reasoning":
		// Archived thinking block, collapsed: a header line ("💭 Thinking · Xs")
		// followed by a single dim summary line (last non-empty line, truncated
		// to 160 chars). Mirrors the live block's collapsed state.
		return m.msgRenderer.RenderThinkingWithHint(e.content, e.duration, false, nil, "")
	default:
		return e.content
	}
}

func (m *AppModel) bubbleActionsForEntry(e historyEntry) []messages.BubbleAction {
	// AI / 子 Agent 回复与用户消息均可点击复制；计划消息额外提供下载。
	if (e.kind != "ai" && e.kind != "agent.final" && e.kind != "user") || strings.TrimSpace(e.content) == "" {
		return nil
	}
	// 文本流布局下 Label 不再用于内联按钮；弹框展示文案由 actionLabel(kind) 解析。
	actions := []messages.BubbleAction{{Kind: "copy"}}
	if strings.EqualFold(strings.TrimSpace(e.executionMode), "plan") && strings.TrimSpace(e.rawMarkdown) != "" {
		actions = append(actions, messages.BubbleAction{Kind: "download"})
	}
	return actions
}

func (m *AppModel) appendHistory(e historyEntry) {
	m.history = append(m.history, e)
	rendered := m.renderHistoryEntry(e)
	m.trackBubbleActionsAt(m.shell.ContentLineCount(), len(m.history)-1, e, rendered)
	block := "\n" + rendered + "\n\n"
	m.shell.AppendContent(block)
}

func (m *AppModel) appendHistoryIndex(e historyEntry) int {
	idx := len(m.history)
	m.appendHistory(e)
	return idx
}

func (m *AppModel) rebuildHistoryContent() {
	if len(m.history) == 0 {
		return
	}
	m.actionHits = nil
	var sb strings.Builder
	lineCount := 1
	for idx, e := range m.history {
		rendered := m.renderHistoryEntry(e)
		m.trackBubbleActionsAt(lineCount, idx, e, rendered)
		sb.WriteString("\n")
		sb.WriteString(rendered)
		sb.WriteString("\n\n")
		lineCount += strings.Count(rendered, "\n") + 3
	}
	m.shell.SetContentPreserveOffset(sb.String())
}

// stripANSI 清除字符串中的 ANSI 转义序列
func stripANSI(s string) string {
	if strings.IndexByte(s, 0x1b) < 0 {
		return s
	}
	return ansiRe.ReplaceAllString(s, "")
}

// runeIndex 将字节索引转换为 rune 索引
func runeIndex(s string, byteIdx int) int {
	if byteIdx <= 0 {
		return 0
	}
	if byteIdx >= len(s) {
		return len([]rune(s))
	}
	return len([]rune(s[:byteIdx]))
}

// trackBubbleActionsAt 登记一条可点击消息文本在内容区中的行范围。
// 文本流布局下不再有内联按钮，改为把整条 AI/子 Agent 回复文本登记为
// 可点击区，点击时弹出操作选择框（复制/下载）。
func (m *AppModel) trackBubbleActionsAt(startLine int, idx int, e historyEntry, rendered string) {
	if m.msgRenderer == nil {
		return
	}
	actions := m.bubbleActionsForEntry(e)
	if len(actions) == 0 {
		return
	}
	payload := strings.TrimSpace(e.content)
	if payload == "" {
		return
	}
	kinds := make([]string, 0, len(actions))
	for _, a := range actions {
		if k := strings.TrimSpace(a.Kind); k != "" {
			kinds = append(kinds, k)
		}
	}
	if len(kinds) == 0 {
		return
	}
	lineCount := strings.Count(rendered, "\n") + 1
	m.actionHits = append(m.actionHits, bubbleActionHit{
		y:       startLine,
		lines:   lineCount,
		idx:     idx,
		actions: kinds,
		text:    payload,
	})
}

// refreshAILive 刷新 AI 实时响应区域，包括思考过程和流式回复
func (m *AppModel) refreshAILive() {
	if m == nil || m.shell == nil {
		return
	}
	// 非处理状态时清除实时显示
	if !m.state.Processing {
		m.shell.ClearLive()
		m.shell.SetStatusHints(false, false)
		return
	}
	var blocks []string
	thinking := strings.TrimSpace(m.thinkingLive.String())
	thinkingShown := m.state.Thinking && state.Thinking() && thinking != ""
	// 渲染思考过程块
	if thinkingShown {
		if m.msgRenderer != nil {
			hint := i18n.T("status.hint.thinking_expand", m.state.Language)
			blocks = append(blocks, m.msgRenderer.RenderThinkingWithHint(thinking, time.Since(m.currentAIStartTime), m.thinkingExpanded, nil, hint))
		} else {
			blocks = append(blocks, thinking)
		}
	}
	// 渲染流式回复块
	live := strings.TrimSpace(m.aiLive.String())
	liveShown := live != ""
	if live != "" {
		if m.msgRenderer != nil {
			ts := m.currentAIStartTime
			if ts.IsZero() {
				ts = time.Now()
			}
			blocks = append(blocks, m.msgRenderer.RenderAIResponseAt(live, m.currentAITokens, 0, false, ts))
		} else {
			blocks = append(blocks, live)
		}
	}
	if len(blocks) == 0 {
		m.shell.SetStatusHints(false, false)
		m.shell.ClearLive()
		return
	}
	m.shell.SetStatusHints(liveShown, thinkingShown)
	m.shell.SetLive(strings.Join(blocks, "\n\n"))
}

// clearCurrentThinking 清除当前思考状态，包括内容缓冲区和展开状态
func (m *AppModel) clearCurrentThinking() {
	if m == nil {
		return
	}
	m.thinkingLive.Reset()
	m.state.Thinking = false
	m.thinkingExpanded = false
	if m.shell != nil {
		m.shell.SetThinking(false, "")
		m.shell.SetThinkingExpanded(false)
	}
}

func (m *AppModel) appendSystem(text, level string) {
	m.appendHistory(historyEntry{kind: "system", content: text, level: level})
}

// appendSystemStyled 追加已含 ANSI 码的系统消息（如 diff 高亮）。
func (m *AppModel) appendSystemStyled(text, level string) {
	m.appendHistory(historyEntry{kind: "system", content: text, level: level, preStyled: true})
}

// diffHighlightTheme 返回当前 diff/代码块高亮主题（零值回默认）。
func (m *AppModel) diffHighlightTheme() string {
	if theme := strings.TrimSpace(m.diffTheme); theme != "" {
		return theme
	}
	return render.DefaultChromaTheme
}

// highlightDiffBlock 先截断（避免截断 ANSI 序列）再高亮，供 /diff、/review 输出。
func (m *AppModel) highlightDiffBlock(diff string, maxLines int, maxBytes int) string {
	return render.HighlightDiffANSI(truncateBlock(diff, maxLines, maxBytes), m.diffHighlightTheme())
}

// handleAIResponseMsg 处理 AIResponseMsg（Update 分支提取）。
// 原 case 为 fall-through（仅 delta 提前返回，final/error fall through 走尾部逻辑），
// 因此 fall-through 时调用 finalizeUpdate。
func (m *AppModel) handleAIResponseMsg(msg AIResponseMsg) (tea.Model, tea.Cmd) {
	if msg.Type == "delta" && !m.state.Processing {
		return m, nil
	}
	if m.delegatedThisRound && msg.Type == "delta" {
		return m, nil
	}
	cmd := m.handleAIResponse(msg)
	return m, m.finalizeUpdate(cmd)
}

// handleAIResponse 处理 AI 响应消息，支持三种类型：
// - "delta": 流式回复片段，追加到实时显示区域
// - "final": 完整回复，保存到历史记录并结束处理状态
// - "error": 错误消息，作为系统消息记录
func (m *AppModel) handleAIResponse(msg AIResponseMsg) tea.Cmd {
	switch msg.Type {
	case "delta":
		// 流式回复：清除预测、清除思考过程、追加内容到实时显示
		m.clearPrediction()
		if strings.TrimSpace(msg.Content) != "" && strings.TrimSpace(m.thinkingLive.String()) != "" {
			m.clearCurrentThinking()
		}
		m.aiLive.WriteString(msg.Content)
		m.currentAITokens += len(msg.Content) / 4
		m.refreshAILive()
	case "final":
		// 完整回复：item 模型已在 item.completed 落 history 时跳过，避免双写。
		duration := time.Since(m.currentAIStartTime)
		m.shell.ClearLive()
		m.aiLive.Reset()
		m.clearCurrentThinking()
		m.shell.SetStatusHints(false, false)
		mainContent := strings.TrimSpace(msg.Content)
		agentContent := strings.TrimSpace(m.lastAgentFinal)
		alreadyCommitted := m.agentTextCommittedThisTurn ||
			(len(m.history) > 0 && m.history[len(m.history)-1].kind == "ai" &&
				strings.TrimSpace(m.history[len(m.history)-1].content) == mainContent)
		// 避免重复记录：如果本轮有委派且内容与 agent final 相同则跳过
		if !alreadyCommitted &&
			(!m.delegatedThisRound || mainContent == "" || agentContent == "" || mainContent != agentContent) {
			m.appendHistory(historyEntry{
				kind:          "ai",
				content:       msg.Content,
				rawMarkdown:   msg.Content,
				executionMode: m.state.ExecutionMode,
				timestamp:     time.Now(),
				tokens:        m.currentAITokens,
				duration:      duration,
			})
		}
		m.agentTextCommittedThisTurn = false
		m.state.Processing = false
		m.shell.SetProcessing(false)
		m.activeCancel = nil
		m.stopRequested = false
		return m.schedulePrediction(m.shell.GetInputValue())
	case "error":
		// 错误：清除所有状态，记录错误消息
		m.clearPrediction()
		m.shell.ClearLive()
		m.aiLive.Reset()
		m.clearCurrentThinking()
		m.shell.SetStatusHints(false, false)
		m.appendHistory(historyEntry{kind: "system", content: msg.Content, level: "error"})
		m.state.Processing = false
		m.shell.SetProcessing(false)
		m.activeCancel = nil
		m.stopRequested = false
	}
	return nil
}

// startAgentMessageItem begins a new text-segment item. Streaming updates stay
// in aiLive until item_completed or a tool_call archives them (ItemStarted for
// AgentMessage does not commit partial text).
func (m *AppModel) startAgentMessageItem(itemID string) {
	itemID = strings.TrimSpace(itemID)
	if itemID != "" && itemID == m.activeItemID {
		return
	}
	m.activeItemID = itemID
	m.aiLive.Reset()
	m.currentAITokens = 0
	m.refreshAILive()
}

// archiveAgentMessage saves the current aiLive content as a finalized history
// entry (an "ai" paragraph). Called when a segment ends (item_completed) or
// when a new segment/tool starts.
func (m *AppModel) archiveAgentMessage() {
	text := strings.TrimSpace(m.aiLive.String())
	if text == "" {
		return
	}
	if n := len(m.history); n > 0 {
		last := m.history[n-1]
		if last.kind == "ai" && strings.TrimSpace(last.content) == text {
			return
		}
	}
	duration := time.Since(m.currentAIStartTime)
	m.appendHistory(historyEntry{
		kind:          "ai",
		content:       m.aiLive.String(),
		rawMarkdown:   m.aiLive.String(),
		executionMode: m.state.ExecutionMode,
		timestamp:     time.Now(),
		tokens:        m.currentAITokens,
		duration:      duration,
	})
	m.agentTextCommittedThisTurn = true
}

// handleItemDelta appends an incremental chunk to the current item's live
// buffer and refreshes the display.
func (m *AppModel) handleItemDelta(msg ItemDeltaMsg) {
	itemID := strings.TrimSpace(msg.ItemID)
	switch msg.DeltaType {
	case "text", "":
		if !m.acceptsAgentTextDelta(itemID) {
			return
		}
		m.clearPrediction()
		if strings.TrimSpace(msg.Delta) != "" && strings.TrimSpace(m.thinkingLive.String()) != "" {
			m.clearCurrentThinking()
		}
		m.aiLive.WriteString(msg.Delta)
		m.currentAITokens += len(msg.Delta) / 4
		m.refreshAILive()
	case "reasoning":
		if itemID != "" && m.activeItemID != "" && itemID != m.activeItemID {
			return
		}
		// Reasoning deltas stream into the collapsible thinking block. This
		// mirrors the legacy ThinkingMsg path: light up the live thinking
		// region (rendered by ThinkingMessage via refreshAILive).
		m.state.Thinking = true
		m.thinkingLive.WriteString(msg.Delta)
		if m.shell != nil {
			m.shell.SetThinking(true, "")
		}
		m.refreshAILive()
	}
}

// acceptsAgentTextDelta reports whether a text delta should append to aiLive.
// After item_completed clears activeItemID, late/out-of-order deltas are ignored
// so a finalized paragraph is not duplicated by stale stream chunks.
func (m *AppModel) acceptsAgentTextDelta(itemID string) bool {
	if !m.state.Processing {
		return false
	}
	if m.activeItemID == "" {
		return false
	}
	if itemID == "" {
		return true
	}
	return itemID == m.activeItemID
}

// startReasoningItem begins a new reasoning (thinking) block. Does not archive
// in-progress agent text — reasoning may precede or interleave with streaming
// reply; premature archive caused duplicate assistant paragraphs.
func (m *AppModel) startReasoningItem(itemID string) {
	itemID = strings.TrimSpace(itemID)
	if itemID != "" && itemID == m.activeItemID {
		return
	}
	m.activeItemID = itemID
	m.thinkingLive.Reset()
	m.thinkingExpanded = false
	m.state.Thinking = true
	m.reasoningStartTime = time.Now()
	m.refreshAILive()
}

// handleReasoningCompleted finalizes a reasoning item: archive the thinking
// block as a history entry (rendered as a dim one-line summary by
// renderHistoryEntry), then clear the live thinking state.
func (m *AppModel) handleReasoningCompleted(msg ItemCompletedMsg) {
	content := strings.TrimSpace(msg.Reasoning)
	if content == "" {
		content = strings.TrimSpace(m.thinkingLive.String())
	}
	if content != "" {
		duration := time.Since(m.reasoningStartTime)
		if m.reasoningStartTime.IsZero() {
			duration = time.Since(m.currentAIStartTime)
		}
		m.appendHistory(historyEntry{
			kind:      "reasoning",
			content:   content,
			duration:  duration,
			timestamp: time.Now(),
		})
	}
	m.clearCurrentThinking()
	m.activeItemID = ""
}

// handleItemCompleted finalizes an AgentMessage item: archive the live text
// into a history entry and clear the buffer for the next segment.
func (m *AppModel) handleItemCompleted(msg ItemCompletedMsg) {
	switch msg.ItemType {
	case "reasoning":
		m.handleReasoningCompleted(msg)
		return
	case "agent_message", "":
		// fall through to AgentMessage finalization below.
	default:
		return
	}
	// If the completed event carries full text, prefer it over the buffer.
	if strings.TrimSpace(msg.Text) != "" {
		m.aiLive.Reset()
		m.aiLive.WriteString(msg.Text)
	}
	m.archiveAgentMessage()
	m.aiLive.Reset()
	m.activeItemID = ""
	m.shell.ClearLive()
}

// handleToolCall 处理工具调用消息
// 工具调用分两个阶段：
// 1. tool_call_start：创建工具卡片（可能无参数）
// 2. tool_call_done：补充真实参数
// 如果已有同 ID 的进行中卡片，只更新参数而非新建
func (m *AppModel) handleToolCall(msg ToolCallMsg) tea.Cmd {
	// A tool call starts: archive any in-progress text segment so the tool
	// card appears after it ([text]→[tool] interleaving).
	m.archiveAgentMessage()
	m.aiLive.Reset()
	m.activeItemID = ""
	m.shell.ClearLive()
	m.clearCurrentThinking()
	// 如果已有同 ID 的进行中卡片，更新参数
	if track, ok := m.toolInflight[msg.ID]; ok {
		if len(msg.Params) > 0 {
			track.params = msg.Params
			m.toolInflight[msg.ID] = track
			if track.idx >= 0 && track.idx < len(m.history) {
				e := m.history[track.idx]
				if len(e.toolParams) == 0 {
					e.toolParams = msg.Params
					m.history[track.idx] = e
					m.rebuildHistoryContent()
				}
			}
			if m.msgRenderer != nil {
				m.shell.SetLive(m.msgRenderer.RenderToolCall(track.name, msg.Params))
			}
		}
		return nil
	}
	// 新建工具卡片
	entry := historyEntry{
		kind:       "tool",
		toolID:     msg.ID,
		toolName:   msg.Name,
		toolParams: msg.Params,
		toolStatus: "running",
		timestamp:  time.Now(),
	}
	idx := m.appendHistoryIndex(entry)
	m.toolInflight[msg.ID] = toolTrack{name: msg.Name, started: time.Now(), idx: idx, params: msg.Params}
	if m.msgRenderer != nil {
		m.shell.SetLive(m.msgRenderer.RenderToolCall(msg.Name, msg.Params))
	} else {
		m.shell.SetLive(fmt.Sprintf("[Tool Call] %s", msg.Name))
	}
	return nil
}

// handleToolResult 处理工具执行结果
// 如果有对应的进行中卡片，更新其状态；否则创建新记录
// 对于 bash 工具，完成后会结束处理状态
func (m *AppModel) handleToolResult(msg ToolResultMsg) tea.Cmd {
	success := msg.Status == "success"
	track, ok := m.toolInflight[msg.ID]
	if ok {
		delete(m.toolInflight, msg.ID)
	}
	m.shell.ClearLive()
	name := msg.ID
	var duration time.Duration
	if ok {
		name = track.name
		duration = time.Since(track.started)
	}
	// 更新已存在的工具卡片
	if ok && track.idx >= 0 && track.idx < len(m.history) {
		e := m.history[track.idx]
		e.kind = "tool"
		e.toolID = msg.ID
		e.toolName = name
		if e.toolParams == nil {
			e.toolParams = track.params
		}
		e.toolOutput = msg.Output
		e.toolSuccess = success
		e.toolStatus = msg.Status
		e.duration = duration
		m.history[track.idx] = e
		m.rebuildHistoryContent()
		// bash 工具完成后恢复空闲状态
		if strings.EqualFold(name, "bash") {
			m.state.Processing = false
			m.shell.SetProcessing(false)
			m.activeCancel = nil
			m.stopRequested = false
		}
		return nil
	}
	// 处理取消的 bash 工具
	if msg.Status == "canceled" {
		if strings.EqualFold(name, "bash") {
			m.activeCancel = nil
			m.stopRequested = false
		}
		return nil
	}
	// 创建新的工具记录（无对应卡片的情况）
	m.appendHistory(historyEntry{kind: "tool", toolID: msg.ID, toolName: name, toolOutput: msg.Output, toolSuccess: success, toolStatus: msg.Status, duration: duration, timestamp: time.Now()})
	if strings.EqualFold(name, "bash") {
		m.state.Processing = false
		m.shell.SetProcessing(false)
		m.activeCancel = nil
		m.stopRequested = false
	}
	return nil
}
