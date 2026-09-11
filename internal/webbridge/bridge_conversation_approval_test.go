package webbridge

import (
	"testing"

	"github.com/eosaios/eos/internal/webbridge/adapter"
	"github.com/eosaios/eos/pkg/coreapi"
)

func TestIsApprovalPromptKind(t *testing.T) {
	cases := map[string]bool{
		"approval.required":      true,
		"tool.approval_required": true,
		"turn.waiting_approval":  false,
		"agent.waiting_approval": false,
		"turn.started":           false,
		"tool.executing":         false,
	}
	for kind, want := range cases {
		if got := isApprovalPromptKind(kind); got != want {
			t.Fatalf("isApprovalPromptKind(%q) = %v, want %v", kind, got, want)
		}
	}
}

func TestApprovalDecisionFromToken(t *testing.T) {
	// token 即 coreapi.ApprovalDecision wire 值：acceptForSession 是 web 前端
	// "本次会话不再询问"按钮的回传值，直达内核 SessionApprovalCache。
	cases := map[string]coreapi.ApprovalDecision{
		"accept":           coreapi.ApprovalAccept,
		"acceptForSession": coreapi.ApprovalAcceptForSession,
		"decline":          coreapi.ApprovalDecline,
		"cancel":           coreapi.ApprovalCancel,
	}
	for token, want := range cases {
		got, err := approvalDecisionFromToken(token)
		if err != nil {
			t.Fatalf("approvalDecisionFromToken(%q) unexpected error: %v", token, err)
		}
		if got != want {
			t.Fatalf("approvalDecisionFromToken(%q) = %q, want %q", token, got, want)
		}
	}
	// 未知 token 必须报错（fail-fast），不得静默重解释为 accept。
	if _, err := approvalDecisionFromToken("允许"); err == nil {
		t.Fatal("approvalDecisionFromToken(允许) must fail: localized labels are not tokens")
	}
	if _, err := approvalDecisionFromToken("bogus"); err == nil {
		t.Fatal("approvalDecisionFromToken(bogus) must fail")
	}
}

func TestApprovalStateBroadcastDoesNotDuplicatePromptOrStatus(t *testing.T) {
	s := &BridgeService{prompts: map[string]*promptState{}}
	session := &sessionState{
		ID: "session-1",
		Messages: []ChatMessage{
			{ID: "msg-1", Role: "assistant", State: "streaming"},
		},
	}

	required := conversationEventFrame{
		session:            session,
		sessionID:          session.ID,
		assistantMessageID: "msg-1",
		kind:               "tool.approval_required",
		message:            "检测到高风险操作",
		event: adapter.Event{
			Payload: map[string]any{
				"approval_id": "approval-1",
				"preview": map[string]any{
					"level":  "high",
					"reason": "写入工作区外文件",
				},
			},
		},
	}
	s.handleConversationEventLocked(required)

	waiting := conversationEventFrame{
		session:            session,
		sessionID:          session.ID,
		assistantMessageID: "msg-1",
		kind:               "turn.waiting_approval",
		event: adapter.Event{
			Payload: map[string]any{
				"approval_id": "approval-1",
			},
		},
	}
	s.handleConversationEventLocked(waiting)

	if len(s.prompts) != 1 {
		t.Fatalf("prompt count = %d, want 1", len(s.prompts))
	}
	msg := findSessionMessageByID(session, "msg-1")
	if msg == nil {
		t.Fatal("message not found")
	}
	waitingCount := 0
	for _, item := range msg.Items {
		if item.Kind == "status" && item.Status == "waiting" {
			waitingCount++
		}
	}
	if waitingCount != 1 {
		t.Fatalf("waiting status count = %d, want 1", waitingCount)
	}
	// 审批等待行用确定性 id（prompt:<approval_id>），resolved 时按此 id 精确 delta 推送。
	if findStatusItemIndex(msg.Items, promptStatusKey("approval-1")) < 0 {
		t.Fatalf("approval status item must carry deterministic id %q", promptStatusKey("approval-1"))
	}
}

// TestSettlePromptLockedEmitsStatusDelta 验证审批 resolved 后通过 conversation-delta
// 增量通道推送 status item 的状态翻转——这是"点允许后等待确认立即变绿"的权威路径，
// 绕开全量快照 + 时间戳守卫竞态。
func TestSettlePromptLockedEmitsStatusDelta(t *testing.T) {
	var emitted []ConversationDeltaPayload
	s := &BridgeService{
		prompts: map[string]*promptState{},
		emitEvent: func(name string, data any) {
			if name == conversationDeltaEventName {
				if payload, ok := data.(ConversationDeltaPayload); ok {
					emitted = append(emitted, payload)
				}
			}
		},
	}
	session := &sessionState{
		ID: "session-1",
		Messages: []ChatMessage{
			{ID: "msg-1", Role: "assistant", State: "waiting"},
		},
	}
	// 模拟 handleConversationApprovalLocked 已写入的等待行（带确定性 key）。
	s.beginMessageStatusWithKey(session, "msg-1", promptStatusKey("approval-1"), "等待确认…", "warning")
	prompt := &promptState{
		PromptCard:         PromptCard{ID: "approval-1", Kind: "approval", SessionID: "session-1"},
		AssistantMessageID: "msg-1",
	}
	s.prompts["approval-1"] = prompt

	// resolved：settlePromptLocked 应把 waiting 行改成 completed，并发一条 delta。
	s.settlePromptLocked(session, prompt, "已允许", "success", "streaming")

	msg := findSessionMessageByID(session, "msg-1")
	if msg == nil {
		t.Fatal("message not found")
	}
	idx := findStatusItemIndex(msg.Items, promptStatusKey("approval-1"))
	if idx < 0 {
		t.Fatalf("approval status item not found after settle")
	}
	if msg.Items[idx].Status != "completed" {
		t.Fatalf("status item status = %q, want completed", msg.Items[idx].Status)
	}
	if msg.Items[idx].Text != "已允许" {
		t.Fatalf("status item text = %q, want 已允许", msg.Items[idx].Text)
	}
	if len(emitted) != 1 {
		t.Fatalf("delta emit count = %d, want 1", len(emitted))
	}
	delta := emitted[0]
	if delta.ItemID != promptStatusKey("approval-1") {
		t.Fatalf("delta itemId = %q, want %q", delta.ItemID, promptStatusKey("approval-1"))
	}
	if delta.Item == nil || delta.Item.Status != "completed" || delta.Item.Text != "已允许" {
		t.Fatalf("delta item = %+v, want completed/已允许", delta.Item)
	}
	if delta.MessageID != "msg-1" || delta.SessionID != "session-1" {
		t.Fatalf("delta routing = %q/%q, want msg-1/session-1", delta.MessageID, delta.SessionID)
	}
}

// TestApprovalMarksToolCallItem 验证审批挂起时 Approval 态挂到 ToolCall item 上
// （单一数据源）：内核 item_started 先到的 ToolCall item 应被标记。
func TestApprovalMarksToolCallItem(t *testing.T) {
	s := &BridgeService{prompts: map[string]*promptState{}}
	session := &sessionState{
		ID: "session-1",
		Messages: []ChatMessage{
			{ID: "msg-1", Role: "assistant", State: "streaming"},
		},
	}
	// 模拟内核 item_started 先到：ToolCall item（id=call.id）已在 message.Items。
	applyItemStarted(&session.Messages[0], map[string]any{
		"item": map[string]any{
			"kind": "tool_call",
			"id":   "call_abc",
			"name": "shell",
		},
	})

	// approval_required 到达：envelope.request_id = call.id，payload.approval_id = approval_{n}。
	required := conversationEventFrame{
		session:            session,
		sessionID:          session.ID,
		assistantMessageID: "msg-1",
		kind:               "tool.approval_required",
		message:            "高风险",
		event: adapter.Event{
			RequestID: "call_abc",
			Payload: map[string]any{
				"approval_id": "approval_1",
				"preview":     map[string]any{"level": "high", "reason": "写入工作区外"},
			},
		},
	}
	s.handleConversationEventLocked(required)

	msg := findSessionMessageByID(session, "msg-1")
	if msg == nil {
		t.Fatal("message not found")
	}
	// ToolCall item 应被标记 Approval 态。
	idx := findItemIndex(msg.Items, "call_abc")
	if idx < 0 {
		t.Fatal("tool_call item not found")
	}
	approval := msg.Items[idx].Approval
	if approval == nil {
		t.Fatalf("tool_call item.Approval not set (single source of truth)")
	}
	if approval.ApprovalID != "approval_1" {
		t.Fatalf("approval.approvalId = %q, want approval_1", approval.ApprovalID)
	}
	if approval.Kind != "approval" || approval.State != "pending" {
		t.Fatalf("approval kind/state = %q/%q, want approval/pending", approval.Kind, approval.State)
	}
	if approval.RiskLevel != "high" {
		t.Fatalf("approval.riskLevel = %q, want high", approval.RiskLevel)
	}
	// prompt.CallID 应记录 call.id，供 ResolvePrompt 反查 item。
	if prompt, ok := s.prompts["approval_1"]; !ok || prompt.CallID != "call_abc" {
		t.Fatalf("prompt.CallID = %q, want call_abc", prompt.CallID)
	}
}

// TestSettlePromptFlipsItemApprovalAndEmitsDelta 验证审批 resolved 时：
//  1. ToolCall item 的 Approval.State 翻转；
//  2. 通过 delta 推送完整 item 快照（含翻转后的 Approval）——浮层据此收起/变色。
func TestSettlePromptFlipsItemApprovalAndEmitsDelta(t *testing.T) {
	var emitted []ConversationDeltaPayload
	s := &BridgeService{
		prompts: map[string]*promptState{},
		emitEvent: func(name string, data any) {
			if name == conversationDeltaEventName {
				if payload, ok := data.(ConversationDeltaPayload); ok {
					emitted = append(emitted, payload)
				}
			}
		},
	}
	session := &sessionState{
		ID: "session-1",
		Messages: []ChatMessage{
			{ID: "msg-1", Role: "assistant", State: "streaming"},
		},
	}
	// ToolCall item + 已标记的 pending approval。
	session.Messages[0].Items = []ThreadItem{
		{ID: "call_abc", Kind: "tool_call", ToolName: "shell", Status: "streaming",
			Approval: &ItemApprovalState{ApprovalID: "approval_1", Kind: "approval", State: "pending"}},
	}
	prompt := &promptState{
		PromptCard:         PromptCard{ID: "approval_1", Kind: "approval", SessionID: "session-1", CallID: "call_abc"},
		AssistantMessageID: "msg-1",
	}
	s.prompts["approval_1"] = prompt

	s.settlePromptLocked(session, prompt, "已允许", "success", "streaming")

	msg := findSessionMessageByID(session, "msg-1")
	idx := findItemIndex(msg.Items, "call_abc")
	if msg.Items[idx].Approval == nil || msg.Items[idx].Approval.State != "approved" {
		t.Fatalf("item approval state = %+v, want approved", msg.Items[idx].Approval)
	}
	// 应有两条 delta：status item 翻转 + tool_call item 翻转。
	var itemDelta *ConversationDeltaPayload
	for i, d := range emitted {
		if d.ItemID == "call_abc" {
			itemDelta = &emitted[i]
		}
	}
	if itemDelta == nil {
		t.Fatalf("no delta emitted for tool_call item; got %d deltas", len(emitted))
	}
	if itemDelta.Item == nil || itemDelta.Item.Approval == nil || itemDelta.Item.Approval.State != "approved" {
		t.Fatalf("delta item approval = %+v, want approved", itemDelta.Item)
	}
}

// TestSyncApprovedPromptsAfterFullAccessLockedFlipsItemAndEmitsDelta 验证切「完全访问」
// 后立即收口 pending 审批的完整路径：不只是删 s.prompts，还要翻转 ToolCall item 的
// Approval.State 并推送 conversation-delta。否则前端浮层卡片（靠 approval.state===
// 'pending' 投影）会永久残留「等待确认…」，而内核已自动放行继续执行——表现为
// 「工具跑下去了，但等待确认字样不变」。
func TestSyncApprovedPromptsAfterFullAccessLockedFlipsItemAndEmitsDelta(t *testing.T) {
	var emitted []ConversationDeltaPayload
	s := &BridgeService{
		prompts:  map[string]*promptState{},
		sessions: map[string]*sessionState{},
		emitEvent: func(name string, data any) {
			if name == conversationDeltaEventName {
				if payload, ok := data.(ConversationDeltaPayload); ok {
					emitted = append(emitted, payload)
				}
			}
		},
	}
	session := &sessionState{
		ID: "session-1",
		Messages: []ChatMessage{
			{ID: "msg-1", Role: "assistant", State: "waiting"},
		},
	}
	s.sessions["session-1"] = session
	// 模拟 handleConversationApprovalLocked 已写入的 pending ToolCall item + 等待行。
	session.Messages[0].Items = []ThreadItem{
		{ID: "call_abc", Kind: "tool_call", ToolName: "shell", Status: "streaming",
			Approval: &ItemApprovalState{ApprovalID: "approval_1", Kind: "approval", State: "pending"}},
	}
	s.beginMessageStatusWithKey(session, "msg-1", promptStatusKey("approval_1"), "等待确认…", "warning")
	s.prompts["approval_1"] = &promptState{
		PromptCard:         PromptCard{ID: "approval_1", Kind: "approval", SessionID: "session-1", CallID: "call_abc"},
		AssistantMessageID: "msg-1",
	}

	s.syncApprovedPromptsAfterFullAccessLocked()

	// prompts 必须被清空。
	if len(s.prompts) != 0 {
		t.Fatalf("prompts = %d items, want 0 (cleared after full access)", len(s.prompts))
	}
	msg := findSessionMessageByID(session, "msg-1")
	if msg == nil {
		t.Fatal("message not found")
	}
	// 消息整体回到 streaming（内核已放行继续执行）。
	if msg.State != "streaming" {
		t.Fatalf("message state = %q, want streaming", msg.State)
	}
	// ToolCall item 的 Approval.State 必须翻转——这是前端浮层卡片收起的唯一依据。
	idx := findItemIndex(msg.Items, "call_abc")
	if idx < 0 {
		t.Fatal("tool_call item not found")
	}
	if msg.Items[idx].Approval == nil || msg.Items[idx].Approval.State != "approved" {
		t.Fatalf("item approval state = %+v, want approved", msg.Items[idx].Approval)
	}
	// 「等待确认…」状态行必须翻成 completed/已允许。
	statusIdx := findStatusItemIndex(msg.Items, promptStatusKey("approval_1"))
	if statusIdx < 0 {
		t.Fatalf("approval status item not found")
	}
	if msg.Items[statusIdx].Status != "completed" {
		t.Fatalf("status item status = %q, want completed", msg.Items[statusIdx].Status)
	}
	if msg.Items[statusIdx].Text != "已允许" {
		t.Fatalf("status item text = %q, want 已允许", msg.Items[statusIdx].Text)
	}
	// 必须推送 tool_call item 的 conversation-delta：前端据此增量收起卡片，
	// 而非只靠全量 bootstrap 回传（避免内核异步事件被幂等吞掉后无人补发 delta）。
	var itemDelta *ConversationDeltaPayload
	for i, d := range emitted {
		if d.ItemID == "call_abc" {
			itemDelta = &emitted[i]
		}
	}
	if itemDelta == nil {
		t.Fatalf("no delta emitted for tool_call item; got %d deltas", len(emitted))
	}
	if itemDelta.Item == nil || itemDelta.Item.Approval == nil || itemDelta.Item.Approval.State != "approved" {
		t.Fatalf("delta item approval = %+v, want approved", itemDelta.Item)
	}
}

// TestSyncApprovedPromptsAfterFullAccessLockedSkipsRequestUserInput 验证切「完全访问」
// 只收口 approval 类 prompt，不影响 request_user_input（计划问题）——后者语义上不该被
// 「完全访问」自动放行。
func TestSyncApprovedPromptsAfterFullAccessLockedSkipsRequestUserInput(t *testing.T) {
	var emitted []ConversationDeltaPayload
	s := &BridgeService{
		prompts:  map[string]*promptState{},
		sessions: map[string]*sessionState{},
		emitEvent: func(name string, data any) {
			if name == conversationDeltaEventName {
				if payload, ok := data.(ConversationDeltaPayload); ok {
					emitted = append(emitted, payload)
				}
			}
		},
	}
	session := &sessionState{
		ID: "session-1",
		Messages: []ChatMessage{
			{ID: "msg-1", Role: "assistant", State: "waiting"},
		},
	}
	s.sessions["session-1"] = session
	session.Messages[0].Items = []ThreadItem{
		{ID: "call_def", Kind: "tool_call", ToolName: "shell", Status: "streaming",
			Approval: &ItemApprovalState{ApprovalID: "inquiry_1", Kind: "request_user_input", State: "pending"}},
	}
	s.beginMessageStatusWithKey(session, "msg-1", promptStatusKey("inquiry_1"), "等待回答计划问题…", "info")
	s.prompts["inquiry_1"] = &promptState{
		PromptCard:         PromptCard{ID: "inquiry_1", Kind: "request_user_input", SessionID: "session-1", CallID: "call_def"},
		AssistantMessageID: "msg-1",
	}

	s.syncApprovedPromptsAfterFullAccessLocked()

	// request_user_input 不应被「完全访问」收口——仍 pending。
	if _, ok := s.prompts["inquiry_1"]; !ok {
		t.Fatal("request_user_input prompt must NOT be cleared by full access")
	}
	idx := findItemIndex(session.Messages[0].Items, "call_def")
	if session.Messages[0].Items[idx].Approval.State != "pending" {
		t.Fatalf("inquiry approval state = %q, want pending (untouched)", session.Messages[0].Items[idx].Approval.State)
	}
	if len(emitted) != 0 {
		t.Fatalf("delta emit count = %d, want 0 (request_user_input untouched)", len(emitted))
	}
}
