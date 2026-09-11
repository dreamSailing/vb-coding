package webbridge

import (
	"strings"
	"time"

	"github.com/eosaios/eos/internal/webbridge/adapter"
)

type conversationEventFrame struct {
	session            *sessionState
	sessionID          string
	assistantMessageID string
	input              string
	source             string
	kind               string
	message            string
	event              adapter.Event
	assistantCompleted bool
}

type conversationEventResult struct {
	persist bool
	// emitBootstrap 指示 runConversation 在锁外立即触发一次全量 shell-updated。
	// 用于审批/问询/计划问题事件：handler 写入了 s.prompts，前端要尽快拿到
	// 带新 prompt 的快照，不能等 200ms debouncer（用户会看到"等待确认"状态
	// 文案但卡片迟迟不出现）。
	emitBootstrap bool
}

// handleConversationEventLocked 分发会话事件到 items 累积 + runtime 事件 + 状态提示。
//
// items 是 assistant 消息的唯一渲染源：思考/正文/工具调用/状态文案各是独立 item。
// content 拼接 + <think> 标签 + commentary flush 机制已删除——不再需要维护两套。
func (s *BridgeService) handleConversationEventLocked(frame conversationEventFrame) conversationEventResult {
	result := conversationEventResult{
		// token_usage 是计量事件：不落盘、不追加 runtime event、不触发全量快照，
		// 只走轻量 usage-updated 转发（同 conversation-delta 的零 RPC 路径）。
		persist: frame.kind != "turn.item_delta" && frame.kind != "turn.token_usage",
		// 审批/计划问题/resolved 事件写入了 s.prompts（或从中删除），
		// handler 内不 emit（持锁），由 runConversation 在锁外根据本标志立即
		// 全量同步，避免 200ms debouncer 造成"等待确认文案已显示但卡片迟到"。
		emitBootstrap: isApprovalPromptKind(frame.kind) ||
			isPromptResolvedEvent(frame.kind) ||
			frame.kind == "turn.request_user_input",
	}

	assistantMsg := findSessionMessageByID(frame.session, frame.assistantMessageID)
	if assistantMsg != nil {
		payload := payloadForItem(frame.event)
		switch frame.kind {
		case "turn.item_started":
			applyItemStarted(assistantMsg, payload)
			assistantMsg.IsPlaceholder = false
		case "turn.item_delta":
			applyItemDelta(assistantMsg, payload)
			assistantMsg.IsPlaceholder = false
		case "turn.item_completed":
			applyItemCompleted(assistantMsg, payload)
			assistantMsg.IsPlaceholder = false
		}
	}

	// 状态文案走 status item + message state，不再写 content。
	switch {
	case isApprovalPromptKind(frame.kind):
		s.handleConversationApprovalLocked(frame)
	case frame.kind == "turn.token_usage":
		s.handleConversationTokenUsageLocked(frame)
	case isPromptResolvedEvent(frame.kind):
		// 内核侧主动 resolved（用户在别处决策、无人值守自动 deny 等）。
		// 只清 s.prompts + 通知前端收起浮层，不重复发决策 RPC。
		resolution := s.handleConversationResolutionLocked(frame)
		result.emitBootstrap = resolution.emitBootstrap
	case frame.kind == "request.started" || frame.kind == "turn.started":
		s.handleConversationStartedLocked(frame)
	case frame.kind == "turn.item_delta" || frame.kind == "turn.plan_delta":
		// delta 已由 items 累积处理；流式态由 item.status="streaming" 表达。
		if assistantMsg != nil {
			assistantMsg.State = "streaming"
		}
	case frame.kind == "turn.request_user_input":
		s.handleConversationRequestUserInputLocked(frame)
	case frame.kind == "tool.result" || frame.kind == "turn.item_completed":
		s.handleConversationItemCompletedLocked(frame)
	case frame.kind == "request.failed" || frame.kind == "turn.error":
		s.handleConversationFailureLocked(frame)
	case frame.kind == "text.final":
		s.handleConversationTextFinalLocked(frame)
	case frame.kind == "turn.completed" || frame.kind == "request.completed":
		s.handleConversationTurnCompletedLocked(frame)
	default:
		s.handleConversationRuntimeEventLocked(frame)
	}

	return result
}

func isApprovalPromptKind(kind string) bool {
	switch strings.TrimSpace(kind) {
	case "approval.required", "tool.approval_required":
		return true
	default:
		return false
	}
}

// approvalIDFromEvent 从 approval 相关事件的 payload 中读取 approval_id。
// tool.approval_required 事件的 RequestID 是 tool_call_id，而内核 pending 表用 approval_id
// 做 key；如果用 EffectiveRequestID() 会拿到错误的 id，导致 respond approval 时找不到 entry。
func approvalIDFromEvent(event adapter.Event) string {
	payload := event.Payload
	if len(payload) == 0 {
		payload = event.Data
	}
	if id := strings.TrimSpace(runtimeStringValue(payload, "approval_id")); id != "" {
		return id
	}
	return event.EffectiveRequestID()
}

func (s *BridgeService) handleConversationStartedLocked(frame conversationEventFrame) {
	if eventType, title, detail, status, ok := runtimeEventFromAdapterEvent(frame.event, frame.message); ok {
		s.appendRuntimeEventLocked(frame.session, frame.assistantMessageID, eventType, title, detail, status)
	}
}

// handleConversationItemCompletedLocked 处理工具/项完成事件：追加 runtime 事件。
// 写路径跟踪 + changeSet 构建已迁移到 Rust core，Go 侧不再需要。
func (s *BridgeService) handleConversationItemCompletedLocked(frame conversationEventFrame) {
	if frame.assistantCompleted {
		return
	}
	if eventType, title, detail, status, ok := runtimeEventFromAdapterEvent(frame.event, frame.message); ok {
		s.appendRuntimeEventLocked(frame.session, frame.assistantMessageID, eventType, title, detail, status)
	}
	if msg := findSessionMessageByID(frame.session, frame.assistantMessageID); msg != nil {
		msg.State = "streaming"
	}
}

// markItemApprovalLocked 在 ToolCall item 上设置审批/问询挂起态（单一数据源）。
// callID 是模型 tool call 的 id（= envelope.request_id = ToolCall item.ID），内核时序
// 保证 item_started 先于 approval_required/request_user_input 到达，故此处一定能找到。
// 返回找到的 item 指针（用于后续 delta emit），找不到返回 nil。
//
// 幂等：若 item.Approval 已存在且 state 相同，不重复覆盖。
func (s *BridgeService) markItemApprovalLocked(
	session *sessionState,
	assistantMessageID, callID string,
	approval *ItemApprovalState,
) *ThreadItem {
	callID = strings.TrimSpace(callID)
	if session == nil || callID == "" || approval == nil {
		return nil
	}
	msg := findSessionMessageByID(session, assistantMessageID)
	if msg == nil {
		return nil
	}
	idx := findItemIndex(msg.Items, callID)
	if idx < 0 {
		// 边界兜底：理论上 item_started 先到，不应发生。若发生则不标记，等全量快照兜底。
		return nil
	}
	item := &msg.Items[idx]
	if item.Approval != nil && item.Approval.ApprovalID == approval.ApprovalID && item.Approval.State == approval.State {
		return nil // 幂等，状态未变
	}
	item.Approval = approval
	return item
}

func (s *BridgeService) handleConversationApprovalLocked(frame conversationEventFrame) {
	review := s.pendingReviewReadOnly()
	promptID := approvalIDFromEvent(frame.event)
	// Codex 语义里，`tool.approval_required` 才是创建待审批对象的主事件；
	// `turn.waiting_approval` / `agent.waiting_approval` 只是"当前 turn/agent 正在等待"
	// 的状态广播，不应再次生成审批态。
	//
	// 按 approval_id 去重：同一条审批只标记一次。
	if _, exists := s.prompts[promptID]; exists {
		return
	}
	// 内核 tool.approval_required 事件内联了 ApprovalPreviewResponse（含 level/
	// reason/tags）。壳层优先用内核的风险分类渲染卡片（AGENTS.md §3：壳层渲染，内核裁决）。
	level, previewReason := approvalPreviewFromEvent(frame.event)
	message := firstNonEmptyString(previewReason, frame.message, s.t("approval.card.message_default"))
	// 单一数据源：审批态挂到 ToolCall item 上（call.id = envelope.request_id）。
	// 按钮选项不在壳层携带：前端按 kind 派生 token+label（见 ItemApprovalState 注释）。
	approval := &ItemApprovalState{
		ApprovalID: promptID,
		Kind:       "approval",
		State:      "pending",
		Title:      s.t("approval.card.title"),
		Message:    message,
		RiskLevel:  level,
		Reason:     previewReason,
		DiffPath:   review.Path,
		Diff:       review.Diff,
	}
	callID := strings.TrimSpace(frame.event.EffectiveRequestID())
	s.markItemApprovalLocked(frame.session, frame.assistantMessageID, callID, approval)
	// 保留 s.prompts 作为 approval_id → 定位信息的反向索引（ResolvePrompt 用），
	// 步骤 E 彻底废弃前过渡期并存。UI 数据以 item.Approval 为唯一真相源。
	prompt := &promptState{
		PromptCard: PromptCard{
			ID:                 promptID,
			Kind:               "approval",
			Title:              approval.Title,
			Message:            message,
			RiskLevel:          level,
			SessionID:          frame.sessionID,
			AssistantMessageID: frame.assistantMessageID,
			WorkspacePath:      frame.session.WorkspacePath,
			DiffPath:           review.Path,
			Diff:               review.Diff,
			Status:             "pending",
			CreatedAt:          time.Now().Format(time.RFC3339),
			CallID:             callID,
		},
		AssistantMessageID: frame.assistantMessageID,
		Source:             frame.source,
		Input:              frame.input,
	}
	s.prompts[prompt.ID] = prompt
	s.appendRuntimeEventLocked(frame.session, frame.assistantMessageID, "approval", fallbackText(frame.message, s.t("approval.notification.message_default")), s.t("approval.runtime_event.detail"), "waiting")
	text, level := s.pendingPromptStatusTextAndLevel(prompt)
	s.beginMessageStatusWithKey(frame.session, frame.assistantMessageID, promptStatusKey(promptID), text, level)
	s.pushNotificationLocked(s.t("approval.notification.title"), prompt.Message, "warning")
}

// approvalPreviewFromEvent extracts the kernel-side risk preview (level + reason)
// from a tool.approval_required event payload. Returns ("","") when the kernel
// did not inline a preview (e.g. MCP/external tools); callers fall back to i18n.
func approvalPreviewFromEvent(event adapter.Event) (level, reason string) {
	payload := event.Payload
	if len(payload) == 0 {
		payload = event.Data
	}
	if len(payload) == 0 {
		return "", ""
	}
	raw, ok := payload["preview"]
	if !ok {
		return "", ""
	}
	obj, ok := raw.(map[string]any)
	if !ok {
		return "", ""
	}
	if l, ok := obj["level"].(string); ok {
		level = strings.TrimSpace(l)
	}
	if r, ok := obj["reason"].(string); ok {
		reason = strings.TrimSpace(r)
	}
	return level, reason
}

// handleConversationRequestUserInputLocked builds an approval state from a
// turn.request_user_input event. The questions are parsed from the payload
// (mirroring core's RequestUserInputEvent). Resolution goes via
// approval/respond with decision=accept and reason=JSON(RequestUserInputResponse).
//
// 单一数据源：问询态挂到 ToolCall item 上（call_id = payload.call_id = item.ID）。
// prompt.ID 用内核 approval_id（payload.approval_id），respond RPC 必须用它——
// 内核 pending 表只认 approval_id，不认 call_id（修复 latent bug）。
func (s *BridgeService) handleConversationRequestUserInputLocked(frame conversationEventFrame) {
	payload := frame.event.Payload
	if len(payload) == 0 {
		payload = frame.event.Data
	}
	callID := runtimeStringValue(payload, "call_id")
	if callID == "" {
		callID = frame.event.EffectiveRequestID()
	}
	// approval_id 来自内核 RequestUserInputEvent.approval_id（= input_id = approval_{n}）。
	// respond RPC 必须用它。fallback 到 callID 仅用于兼容旧内核（不带 approval_id 时）。
	approvalID := strings.TrimSpace(runtimeStringValue(payload, "approval_id"))
	if approvalID == "" {
		approvalID = callID
	}
	if _, exists := s.prompts[approvalID]; exists {
		return
	}
	questions := parseRequestUserInputQuestions(payload)
	title := "需要确认计划问题"
	message := "请回答以下问题以继续规划。"
	if len(questions) > 0 {
		message = questions[0].Question
	}
	// 单一数据源：问询态挂到 ToolCall item 上。
	approval := &ItemApprovalState{
		ApprovalID: approvalID,
		Kind:       "request_user_input",
		State:      "pending",
		Title:      title,
		Message:    message,
		Questions:  questions,
	}
	s.markItemApprovalLocked(frame.session, frame.assistantMessageID, callID, approval)
	prompt := &promptState{
		PromptCard: PromptCard{
			ID:                 approvalID,
			Kind:               "request_user_input",
			Title:              title,
			Message:            message,
			AllowText:          false,
			SessionID:          frame.sessionID,
			AssistantMessageID: frame.assistantMessageID,
			WorkspacePath:      frame.session.WorkspacePath,
			Status:             "pending",
			CreatedAt:          time.Now().Format(time.RFC3339),
			Questions:          questions,
			CallID:             callID,
		},
		AssistantMessageID: frame.assistantMessageID,
		Source:             "request-user-input",
		Input:              frame.input,
	}
	s.prompts[prompt.ID] = prompt
	s.appendRuntimeEventLocked(frame.session, frame.assistantMessageID, "interaction", title, message, "waiting")
	text, level := s.pendingPromptStatusTextAndLevel(prompt)
	s.beginMessageStatusWithKey(frame.session, frame.assistantMessageID, promptStatusKey(prompt.ID), text, level)
	s.pushNotificationLocked("需要确认", message, "warning")
}

// parseRequestUserInputQuestions extracts the questions array from a
// turn.request_user_input event payload, mirroring core's
// RequestUserInputEvent.questions → RequestUserInputQuestion.
func parseRequestUserInputQuestions(payload map[string]any) []bridgeRequestUserInputQuestion {
	if payload == nil {
		return nil
	}
	raw, ok := payload["questions"].([]any)
	if !ok {
		return nil
	}
	questions := make([]bridgeRequestUserInputQuestion, 0, len(raw))
	for _, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		q := bridgeRequestUserInputQuestion{
			ID:       runtimeStringValue(m, "id"),
			Header:   runtimeStringValue(m, "header"),
			Question: runtimeStringValue(m, "question"),
		}
		if opts, ok := m["options"].([]any); ok {
			for _, opt := range opts {
				if om, ok := opt.(map[string]any); ok {
					q.Options = append(q.Options, bridgeRequestUserInputOption{
						Label:       runtimeStringValue(om, "label"),
						Description: runtimeStringValue(om, "description"),
					})
				}
			}
		}
		questions = append(questions, q)
	}
	return questions
}

func (s *BridgeService) handleConversationFailureLocked(frame conversationEventFrame) {
	if frame.assistantCompleted {
		return
	}
	if s.conversationCancelledLocked(frame.sessionID, frame.assistantMessageID) {
		frame.session.Running = false
		frame.session.NeedsAttention = false
		stoppedText := s.t("conversation.stopped_manual")
		s.appendRuntimeEventLocked(frame.session, frame.assistantMessageID, "lifecycle", stoppedText, "", "failed")
		s.setMessageStatus(frame.session, frame.assistantMessageID, stoppedText, "warning", "failed")
		return
	}
	message := requestFailureMessage(frame.message)
	frame.session.Running = false
	frame.session.NeedsAttention = true
	s.appendRuntimeEventLocked(frame.session, frame.assistantMessageID, "error", message, lastRuntimeEventTitle(frame.session, frame.assistantMessageID), "failed")
	s.setMessageStatus(frame.session, frame.assistantMessageID, message, "error", "failed")
	s.pushNotificationLocked("请求失败", message, "danger")
}

func (s *BridgeService) handleConversationTextFinalLocked(frame conversationEventFrame) {
	frame.session.Running = false
	frame.session.NeedsAttention = false
	s.appendRuntimeEventLocked(frame.session, frame.assistantMessageID, "lifecycle", "请求已完成", "", "completed")
	s.completeAssistantMessage(frame.session, frame.assistantMessageID)
	if isAutoSessionPlaceholderTitle(frame.session.Title) {
		frame.session.Title = autoSessionTitle(frame.session, frame.input)
	}
	s.pushNotificationLocked("请求完成", "聊天内容与任务状态已同步。", "success")
}

func (s *BridgeService) handleConversationTurnCompletedLocked(frame conversationEventFrame) {
	frame.session.Running = false
	frame.session.NeedsAttention = false
	payload := frame.event.Payload
	if len(payload) == 0 {
		payload = frame.event.Data
	}
	completionTitle, completionDetail := runtimeCompletionEvent(payload, frame.message)
	if !frame.assistantCompleted {
		s.appendRuntimeEventLocked(frame.session, frame.assistantMessageID, "lifecycle", completionTitle, completionDetail, "completed")
	}
	s.completeAssistantMessage(frame.session, frame.assistantMessageID)
	s.applyStructuredResultToMessageLocked(frame.session, frame.assistantMessageID, payload)
	if isAutoSessionPlaceholderTitle(frame.session.Title) {
		frame.session.Title = autoSessionTitle(frame.session, frame.input)
	}
	s.pushNotificationLocked("请求完成", "聊天内容与任务状态已同步。", "success")
}

func (s *BridgeService) handleConversationRuntimeEventLocked(frame conversationEventFrame) {
	if frame.assistantCompleted {
		return
	}
	if eventType, title, detail, status, ok := runtimeEventFromAdapterEvent(frame.event, frame.message); ok {
		s.appendRuntimeEventLocked(frame.session, frame.assistantMessageID, eventType, title, detail, status)
		if msg := findSessionMessageByID(frame.session, frame.assistantMessageID); msg != nil {
			msg.State = "streaming"
		}
	}
}

// handleConversationTokenUsageLocked 转发内核每步的累计 token 用量给前端。
// 锁内直接 emit（零 RPC，同 conversation-delta），前端实时刷新上下文用量浮层，
// 不必等 turn 收尾的全量快照——那期间 usage 浮层数字全程静止。
func (s *BridgeService) handleConversationTokenUsageLocked(frame conversationEventFrame) {
	payload := frame.event.Payload
	if len(payload) == 0 {
		payload = frame.event.Data
	}
	s.emitTurnUsage(TurnUsagePayload{
		SessionID:        frame.sessionID,
		MessageID:        frame.assistantMessageID,
		TurnID:           frame.event.TurnID,
		PromptTokens:     metadataInt64(payload["prompt_tokens"]),
		CompletionTokens: metadataInt64(payload["completion_tokens"]),
		TotalTokens:      metadataInt64(payload["total_tokens"]),
		LastPromptTokens: metadataInt64(payload["last_prompt_tokens"]),
		ContextBreakdown: metadataBreakdown(payload["context_breakdown"]),
	})
}

// setMessageStatus 设置消息状态 + 追加 status item（取消/等待/失败等生命周期提示）。
// text 是一句话文案（写入 status item），state 是消息级状态（streaming/waiting/failed/completed）。
func (s *BridgeService) setMessageStatus(session *sessionState, messageID, text, level, state string) {
	s.setMessageStatusWithItemState(session, messageID, text, level, state, state)
}

// setMessageStatusWithItemState 允许消息整体状态与 status item 状态分离。
// 用于审批已确认但消息仍继续运行的场景：消息保持 streaming，让正文/工具继续流式，
// 同时把"已允许/已拒绝"这条 status 行收尾为 completed，避免视觉上还像"等待确认…"。
func (s *BridgeService) setMessageStatusWithItemState(
	session *sessionState,
	messageID, text, level, messageState, itemState string,
) {
	s.setMessageStatusWithItemStateKey(session, messageID, "", text, level, messageState, itemState)
}

// setMessageStatusWithItemStateKey 同 setMessageStatusWithItemState，但给 status item
// 指定确定性 id（statusKey）。审批 resolved 路径用它把"等待确认…"改写成"已允许"，
// 再由调用方（settlePromptLocked）通过同一 statusKey 走 conversation-delta 增量推送，
// 绕开全量快照时间戳竞态。
func (s *BridgeService) setMessageStatusWithItemStateKey(
	session *sessionState,
	messageID, statusKey, text, level, messageState, itemState string,
) {
	if session == nil {
		return
	}
	msg := findSessionMessageByID(session, messageID)
	if msg == nil {
		return
	}
	msg.State = messageState
	// 到达终态（failed/completed）的消息不再是「等待模型首字」的占位：
	// 403 等错误路径没有任何 turn.item_* 事件，若不在这里翻转，前端会一直渲染「思考中」。
	if messageState == "failed" || messageState == "completed" {
		msg.IsPlaceholder = false
	}
	if strings.TrimSpace(text) != "" {
		appendStatusItemWithID(msg, statusKey, text, level, itemState)
	}
}

// beginMessageStatus 开始一条新的待确认状态行（等待确认…/等待选择…）。
// 内核串行处理审批：同一时刻只应有一个活跃待确认。新审批触发说明上一个
// 审批已被处理（turn 已恢复执行），因此先把残留的 waiting 行收尾（标记为
// completed，不再显示为待确认），再追加本次的新 waiting 行。这样每个审批
// 独立、一次只呈现一个待确认。
func (s *BridgeService) beginMessageStatus(session *sessionState, messageID, text, level string) {
	s.beginMessageStatusWithKey(session, messageID, "", text, level)
}

// beginMessageStatusWithKey 同 beginMessageStatus，但给 status item 指定确定性 id。
// 审批/问询类用 "approval:<approval_id>" 作为 key，以便 resolved 时按 key 精确 delta 推送。
func (s *BridgeService) beginMessageStatusWithKey(session *sessionState, messageID, statusKey, text, level string) {
	if session == nil {
		return
	}
	msg := findSessionMessageByID(session, messageID)
	if msg == nil {
		return
	}
	msg.State = "waiting"
	// 收尾所有残留 waiting 行（对应已完成的上一轮审批）。
	for i := range msg.Items {
		if strings.TrimSpace(strings.ToLower(msg.Items[i].Kind)) != "status" {
			continue
		}
		if msg.Items[i].Status == "waiting" {
			msg.Items[i].Status = "completed"
		}
	}
	if strings.TrimSpace(text) != "" {
		appendStatusItemWithID(msg, statusKey, text, level, "waiting")
	}
}

// completeAssistantMessage 标记消息完成（所有 item 完成 + state=completed）。
func (s *BridgeService) completeAssistantMessage(session *sessionState, messageID string) {
	msg := findSessionMessageByID(session, messageID)
	if msg == nil {
		return
	}
	msg.State = "completed"
	// 覆盖「无 item 事件直接完成」的边界：占位标志必须随终态清除。
	msg.IsPlaceholder = false
	finalizeMessageItems(msg)
}
