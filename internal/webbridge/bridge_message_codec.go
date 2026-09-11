package webbridge

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/eosaios/eos/internal/webbridge/adapter"
	"github.com/eosaios/eos/pkg/coreapi"
)

// chatMessagesFromRuntime 把内核持久化的 SessionMessage 列表重建为前端 ChatMessage。
//
// 内核 items_to_session_messages 把一个 turn 的多个 TurnItem 存成多条
// SessionMessage（每条带 metadata.turn_id + metadata.item_id + metadata.kind）。
// 这里按 turn_id 分组：同一 turn 的 assistant/tool 消息合并成一个 ChatMessage，
// 按 metadata.kind 重建 []ThreadItem。无 turn_id 的消息（user/system/旧数据）
// 各成独立气泡（user 的 Content 仍是唯一渲染源）。
func chatMessagesFromRuntime(messages []adapter.SessionMessage) []ChatMessage {
	out := make([]ChatMessage, 0, len(messages))

	// pendingTurn 累积同一 turn_id 的消息，直到 turn_id 变化时 flush。
	var pendingTurnID string
	var pending []adapter.SessionMessage

	flushPending := func() {
		if len(pending) == 0 {
			return
		}
		out = append(out, chatMessageFromTurnGroup(pending))
		pending = pending[:0]
	}

	for _, msg := range messages {
		turnID := metadataTurnID(msg.Metadata)
		role := strings.TrimSpace(msg.Role)
		if strings.EqualFold(role, "tool") {
			// tool 结果消息归入当前 turn 组（它的 turn_id 与所属 tool_call 相同）。
			if turnID != "" && turnID == pendingTurnID {
				pending = append(pending, msg)
				continue
			}
		}
		if turnID == "" {
			// 无 turn_id（user/system/旧数据）：独立气泡。
			flushPending()
			pendingTurnID = ""
			out = append(out, chatMessageFromRuntime(msg))
			continue
		}
		if turnID != pendingTurnID {
			flushPending()
			pendingTurnID = turnID
		}
		pending = append(pending, msg)
	}
	flushPending()
	return out
}

// chatMessageFromTurnGroup 把同一 turn_id 的多条 SessionMessage 合并成一个
// ChatMessage，按 metadata.kind 重建 []ThreadItem。
func chatMessageFromTurnGroup(msgs []adapter.SessionMessage) ChatMessage {
	if len(msgs) == 0 {
		return ChatMessage{ID: newID("history"), Role: "assistant", State: "completed"}
	}

	first := msgs[0]
	createdAt := first.Time.Format(time.RFC3339)
	message := ChatMessage{
		ID:        newID("history"),
		Role:      "assistant",
		State:     "completed",
		CreatedAt: createdAt,
		UpdatedAt: createdAt,
	}
	// 继承 turn_id，使历史会话再次保存时 sessionMessagesFromChatMessage
	// 能按 turn_id 展开_items（读写对称）。
	message.turnID = metadataTurnID(first.Metadata)
	// 从第一条带 gui metadata 的消息恢复富字段（runtimeEvents/verification 等）。
	for _, m := range msgs {
		applyRuntimeMetadataToChatMessage(&message, m.Metadata)
		if strings.TrimSpace(message.ID) != "" && message.ID != "history" {
			break
		}
	}
	if strings.TrimSpace(message.ID) == "" {
		message.ID = newID("history")
	}
	// Top-level ChangeSet/Rollback (populated by the Rust core for the turn)
	// take precedence over the legacy metadata["eos_gui"] fallback, which only
	// applies to sessions persisted before the migration. The core attaches the
	// descriptor to the turn's messages, so scan for the first one present.
	for _, m := range msgs {
		if changeSet := chatMessageChangeSetFromCoreAPI(m.ChangeSet); changeSet != nil {
			message.ChangeSet = changeSet
			break
		}
	}
	for _, m := range msgs {
		if rollback := chatMessageTurnRollbackFromCoreAPI(m.Rollback); rollback != nil {
			message.Rollback = rollback
			break
		}
	}

	// 按 item 出现顺序重建 ThreadItem 列表。
	for _, m := range msgs {
		item, ok := threadItemFromSessionMessage(m)
		if !ok {
			continue
		}
		item.Status = "completed"
		message.Items = append(message.Items, item)
	}
	// 把 tool 结果消息（role=tool）的 content 回填到对应 tool_call item。
	backfillToolResults(msgs, message.Items)

	message.RuntimeEvents = nonNilSlice(message.RuntimeEvents)
	if strings.TrimSpace(message.RuntimeSummary) == "" {
		message.RuntimeSummary = runtimeSummaryForMessage(message)
	}
	return message
}

// threadItemFromSessionMessage 从一条 SessionMessage 的 metadata 重建 ThreadItem。
// 返回 ok=false 表示该消息不对应一个 item（如 tool 结果消息已被 tool_call 消费）。
func threadItemFromSessionMessage(msg adapter.SessionMessage) (ThreadItem, bool) {
	itemID := metadataString(msg.Metadata["item_id"])
	if itemID == "" {
		return ThreadItem{}, false
	}
	kind := metadataString(msg.Metadata["kind"])
	item := ThreadItem{ID: itemID}

	switch strings.TrimSpace(kind) {
	case "reasoning":
		item.Kind = "reasoning"
		item.Reasoning = strings.TrimSpace(msg.Content)
	case "plan":
		item.Kind = "plan"
		item.Text = strings.TrimSpace(msg.Content)
	case "status":
		item.Kind = "status"
		item.Text = strings.TrimSpace(msg.Content)
		item.Level = metadataString(msg.Metadata["level"])
	default:
		// agent_message（无 kind 标记）或 tool_call（有 tool_call metadata）。
		if tc, ok := msg.Metadata["tool_call"]; ok {
			item.Kind = "tool_call"
			populateToolCallFromMetadata(&item, tc)
			// 恢复审批/问询挂起态（与 sessionMessageFromThreadItem 写入对称）。
			if ap, ok := msg.Metadata["approval"]; ok {
				populateApprovalFromMetadata(&item, ap)
			}
		} else {
			item.Kind = "agent_message"
			item.Text = strings.TrimSpace(msg.Content)
		}
	}
	return item, true
}

// populateToolCallFromMetadata 从 metadata.tool_call 提取工具名/参数。
func populateToolCallFromMetadata(item *ThreadItem, raw any) {
	obj, ok := raw.(map[string]any)
	if !ok {
		return
	}
	item.ToolName, _ = obj["name"].(string)
	item.ToolArgs, _ = obj["arguments"].(string)
}

// populateApprovalFromMetadata 从 metadata.approval 重建审批/问询挂起态。
// 与 sessionMessageFromThreadItem 的写入对称：重启后让浮层能从 items 投影 pending 审批。
func populateApprovalFromMetadata(item *ThreadItem, raw any) {
	obj, ok := raw.(map[string]any)
	if !ok {
		return
	}
	state, _ := obj["state"].(string)
	if strings.TrimSpace(state) == "" {
		return
	}
	approval := &ItemApprovalState{
		ApprovalID: metadataString(obj["approvalId"]),
		Kind:       metadataString(obj["kind"]),
		State:      state,
		Title:      metadataString(obj["title"]),
		Message:    metadataString(obj["message"]),
		RiskLevel:  metadataString(obj["riskLevel"]),
		Reason:     metadataString(obj["reason"]),
		Diff:       metadataString(obj["diff"]),
		DiffPath:   metadataString(obj["diffPath"]),
		ResolvedAt: metadataString(obj["resolvedAt"]),
	}
	// 按钮选项不落盘也不从 metadata 恢复：前端按 kind 派生（见 ItemApprovalState
	// 注释），旧数据里的 options 数组（无论字符串还是对象形态）一律忽略。
	if qs, ok := obj["questions"].([]any); ok {
		approval.Questions = requestUserInputQuestionsFromAny(qs)
	}
	item.Approval = approval
}

// metadataTurnID 从 SessionMessage.metadata 提取 turn_id。
func metadataTurnID(metadata map[string]any) string {
	if len(metadata) == 0 {
		return ""
	}
	return metadataString(metadata["turn_id"])
}

func chatMessageFromRuntime(item adapter.SessionMessage) ChatMessage {
	role := strings.TrimSpace(item.Role)
	if role == "" {
		role = strings.TrimSpace(item.Type)
	}
	if role == "" {
		role = "assistant"
	}
	createdAt := item.Time.Format(time.RFC3339)
	message := ChatMessage{
		ID:          newID("history"),
		Role:        role,
		Content:     strings.TrimSpace(item.Content),
		State:       "completed",
		CreatedAt:   createdAt,
		UpdatedAt:   createdAt,
		Attachments: makeAttachments(item.ImagePaths),
	}
	applyRuntimeMetadataToChatMessage(&message, item.Metadata)
	// Top-level ChangeSet/Rollback (populated by the Rust core) take
	// precedence over the legacy metadata["eos_gui"] fallback read above,
	// which only applies to sessions persisted before the migration.
	if changeSet := chatMessageChangeSetFromCoreAPI(item.ChangeSet); changeSet != nil {
		message.ChangeSet = changeSet
	}
	if rollback := chatMessageTurnRollbackFromCoreAPI(item.Rollback); rollback != nil {
		message.Rollback = rollback
	}
	if strings.TrimSpace(message.ID) == "" {
		message.ID = newID("history")
	}
	if strings.TrimSpace(message.State) == "" {
		message.State = "completed"
	}
	if strings.TrimSpace(message.UpdatedAt) == "" {
		message.UpdatedAt = message.CreatedAt
	}
	message.Attachments = nonNilSlice(message.Attachments)
	message.RuntimeEvents = nonNilSlice(message.RuntimeEvents)
	if strings.TrimSpace(message.RuntimeSummary) == "" {
		message.RuntimeSummary = runtimeSummaryForMessage(message)
	}
	return message
}

// sessionMessagesFromChatMessage 把一条 ChatMessage 展开为持久化的 SessionMessage 列表。
//
// user 消息 → 单条（Content 是唯一载体，无 turn_id）。
// assistant 消息 → 按 Items 逐条展开，每条带 metadata.item_id + metadata.turn_id +
// metadata.kind（与内核 items_to_session_messages 输出格式对齐），使重载时
// chatMessageFromTurnGroup 能按 turn_id 合并、按 kind 重建 Items。
// tool_call item 若带 result，额外产出一条 role=tool 的结果消息
// （metadata.tool_call_id = item_id），供 backfillToolResults 回填。
//
// assistant 占位消息（IsPlaceholder 且无 Items）退化为单条 Content，
// 与历史行为兼容（流式中的中间态，正常完成的 turn 不会落到此分支）。
func sessionMessagesFromChatMessage(item ChatMessage) []adapter.SessionMessage {
	role := strings.TrimSpace(item.Role)
	// user / system / 占位 assistant：Content 是唯一载体，单条持久化。
	if role != "assistant" || len(item.Items) == 0 {
		return []adapter.SessionMessage{sessionMessageContentFromChat(item)}
	}

	guiMeta := runtimeMetadataFromChatMessage(item)
	changeSet := coreAPIChangeSetFromChatMessage(item.ChangeSet)
	rollback := coreAPITurnRollbackFromChatMessage(item.Rollback)
	turnID := strings.TrimSpace(item.turnID)
	out := make([]adapter.SessionMessage, 0, len(item.Items)*2)
	for _, ti := range item.Items {
		out = append(out, sessionMessageFromThreadItem(ti, turnID, item.CreatedAt, guiMeta, changeSet, rollback))
		// tool_call 带 result：追加一条 role=tool 的结果消息，
		// metadata.tool_call_id = item_id，供重载时 backfillToolResults 回填到该 tool_call。
		if ti.Kind == "tool_call" && ti.ToolResult != nil {
			out = append(out, sessionMessageToolResult(ti, turnID, item.CreatedAt))
		}
	}
	// 无 Items 展开时（例如全部 item 提取失败），回退单条 Content，避免丢消息。
	if len(out) == 0 {
		return []adapter.SessionMessage{sessionMessageContentFromChat(item)}
	}
	return out
}

// sessionMessageContentFromChat 构造以 Content 为唯一载体的单条 SessionMessage
// （user 消息、无 Items 的 assistant 消息走此路径）。
func sessionMessageContentFromChat(item ChatMessage) adapter.SessionMessage {
	return adapter.SessionMessage{
		Role:       strings.TrimSpace(item.Role),
		Type:       "text",
		Content:    item.Content,
		Time:       parseTime(item.CreatedAt),
		ImagePaths: imagePathsFromAttachments(item.Attachments),
		Metadata:   runtimeMetadataFromChatMessage(item),
		ChangeSet:  coreAPIChangeSetFromChatMessage(item.ChangeSet),
		Rollback:   coreAPITurnRollbackFromChatMessage(item.Rollback),
	}
}

// sessionMessageFromThreadItem 把一个 ThreadItem 转成 SessionMessage，
// 字段映射与读取端 threadItemFromSessionMessage 严格对称。
// 若 item 是带 result 的 tool_call，追加一条 role=tool 的结果消息。
func sessionMessageFromThreadItem(
	ti ThreadItem,
	turnID, createdAt string,
	guiMeta map[string]any,
	changeSet *coreapi.MessageChangeSet,
	rollback *coreapi.TurnRollback,
) adapter.SessionMessage {
	meta := buildItemMetadata(ti, turnID, guiMeta)
	msg := adapter.SessionMessage{
		Role:      "assistant",
		Type:      "assistant",
		Time:      parseTime(createdAt),
		Metadata:  meta,
		ChangeSet: changeSet,
		Rollback:  rollback,
	}
	switch strings.TrimSpace(ti.Kind) {
	case "reasoning":
		msg.Metadata["kind"] = "reasoning"
		msg.Content = ti.Reasoning
	case "plan":
		msg.Metadata["kind"] = "plan"
		msg.Content = ti.Text
	case "status":
		msg.Metadata["kind"] = "status"
		if strings.TrimSpace(ti.Level) != "" {
			msg.Metadata["level"] = ti.Level
		}
		msg.Content = ti.Text
	default: // agent_message 或 tool_call
		if ti.Kind == "tool_call" || strings.TrimSpace(ti.ToolName) != "" {
			msg.Metadata["tool_call"] = map[string]any{
				"id":        ti.ID,
				"name":      ti.ToolName,
				"arguments": ti.ToolArgs,
			}
			// 审批/问询挂起态对称落盘：重启后从持久化重建时恢复 Approval 字段，
			// 让浮层能从 items 投影 pending 审批（单一数据源，不依赖内存 s.prompts）。
			if ti.Approval != nil {
				msg.Metadata["approval"] = ti.Approval
			}
			msg.Content = ""
		} else {
			msg.Content = ti.Text
		}
	}
	return msg
}

// buildItemMetadata 构造单条 item 消息的 metadata：item_id + turn_id + gui 富字段。
func buildItemMetadata(ti ThreadItem, turnID string, guiMeta map[string]any) map[string]any {
	meta := map[string]any{
		"item_id": ti.ID,
	}
	if turnID != "" {
		meta["turn_id"] = turnID
	}
	// gui 富字段（runtimeEvents/verification 等）附在首条 item 上即可，
	// 重载时 chatMessageFromTurnGroup 会从组内任一消息恢复。
	if len(guiMeta) > 0 {
		meta[guiRuntimeMetadataKey] = guiMeta[guiRuntimeMetadataKey]
	}
	return meta
}

// requestUserInputQuestionsFromAny 把持久化读回的 []any 重建为 question 列表。
func requestUserInputQuestionsFromAny(raw []any) []bridgeRequestUserInputQuestion {
	questions := make([]bridgeRequestUserInputQuestion, 0, len(raw))
	for _, item := range raw {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		q := bridgeRequestUserInputQuestion{
			ID:       metadataString(obj["id"]),
			Header:   metadataString(obj["header"]),
			Question: metadataString(obj["question"]),
		}
		if opts, ok := obj["options"].([]any); ok {
			for _, opt := range opts {
				if om, ok := opt.(map[string]any); ok {
					q.Options = append(q.Options, bridgeRequestUserInputOption{
						Label:       metadataString(om["label"]),
						Description: metadataString(om["description"]),
					})
				}
			}
		}
		questions = append(questions, q)
	}
	return questions
}

// sessionMessageToolResult 构造 tool_call 结果的 role=tool 消息。
// metadata.tool_call_id = tool_call 的 item_id，与 backfillToolResults 对称。
func sessionMessageToolResult(ti ThreadItem, turnID, createdAt string) adapter.SessionMessage {
	meta := map[string]any{
		"tool_call_id": ti.ID,
	}
	if turnID != "" {
		meta["turn_id"] = turnID
	}
	content := ""
	if ti.ToolResult != nil {
		content = strings.TrimSpace(ti.ToolResult.Output)
	}
	return adapter.SessionMessage{
		Role:     "tool",
		Type:     "tool",
		Content:  content,
		Time:     parseTime(createdAt),
		Metadata: meta,
	}
}

// coreAPIChangeSetFromChatMessage converts the Go shell's local MessageChangeSet
// (package main) into coreapi.MessageChangeSet. The two types share identical
// camelCase JSON tags (differing only in int vs int64 widths), so a JSON
// round-trip is the simplest loss-free conversion.
func coreAPIChangeSetFromChatMessage(changeSet *MessageChangeSet) *coreapi.MessageChangeSet {
	if changeSet == nil {
		return nil
	}
	data, err := json.Marshal(changeSet)
	if err != nil {
		return nil
	}
	var out coreapi.MessageChangeSet
	if err := json.Unmarshal(data, &out); err != nil {
		return nil
	}
	return &out
}

// coreAPITurnRollbackFromChatMessage converts the Go shell's local TurnRollback
// (package main) into coreapi.TurnRollback via a JSON round-trip.
func coreAPITurnRollbackFromChatMessage(rollback *TurnRollback) *coreapi.TurnRollback {
	if rollback == nil {
		return nil
	}
	data, err := json.Marshal(rollback)
	if err != nil {
		return nil
	}
	var out coreapi.TurnRollback
	if err := json.Unmarshal(data, &out); err != nil {
		return nil
	}
	return &out
}

// chatMessageChangeSetFromCoreAPI converts coreapi.MessageChangeSet back into
// the Go shell's local MessageChangeSet via a JSON round-trip.
func chatMessageChangeSetFromCoreAPI(changeSet *coreapi.MessageChangeSet) *MessageChangeSet {
	if changeSet == nil {
		return nil
	}
	data, err := json.Marshal(changeSet)
	if err != nil {
		return nil
	}
	var out MessageChangeSet
	if err := json.Unmarshal(data, &out); err != nil {
		return nil
	}
	out.Files = nonNilSlice(out.Files)
	if strings.TrimSpace(out.ID) == "" || len(out.Files) == 0 {
		return nil
	}
	return &out
}

// chatMessageTurnRollbackFromCoreAPI converts coreapi.TurnRollback back into
// the Go shell's local TurnRollback via a JSON round-trip.
func chatMessageTurnRollbackFromCoreAPI(rollback *coreapi.TurnRollback) *TurnRollback {
	if rollback == nil {
		return nil
	}
	data, err := json.Marshal(rollback)
	if err != nil {
		return nil
	}
	var out TurnRollback
	if err := json.Unmarshal(data, &out); err != nil {
		return nil
	}
	out.Files = nonNilSlice(out.Files)
	if strings.TrimSpace(out.AssistantMessageID) == "" && strings.TrimSpace(out.UserMessageID) == "" && len(out.Files) == 0 {
		return nil
	}
	return &out
}

func runtimeMetadataFromChatMessage(message ChatMessage) map[string]any {
	gui := map[string]any{}
	if value := strings.TrimSpace(message.ID); value != "" {
		gui["id"] = value
	}
	if value := strings.TrimSpace(message.State); value != "" {
		gui["state"] = value
	}
	if value := strings.TrimSpace(message.UpdatedAt); value != "" {
		gui["updatedAt"] = value
	}
	if value := strings.TrimSpace(message.RuntimeSummary); value != "" {
		gui["runtimeSummary"] = value
	}
	if value := strings.TrimSpace(message.ImplementationResult); value != "" {
		gui["implementationResult"] = value
	}
	if value := strings.TrimSpace(message.VerificationResult); value != "" {
		gui["verificationResult"] = value
	}
	if value := strings.TrimSpace(message.VerificationVerdict); value != "" {
		gui["verificationVerdict"] = value
	}
	if value := strings.TrimSpace(message.VerificationSummary); value != "" {
		gui["verificationSummary"] = value
	}
	if len(message.VerificationCovered) > 0 {
		gui["verificationCoveredChecks"] = append([]string(nil), compactStrings(message.VerificationCovered)...)
	}
	if len(message.VerificationOpenRisks) > 0 {
		gui["verificationOpenRisks"] = append([]string(nil), compactStrings(message.VerificationOpenRisks)...)
	}
	if len(message.VerificationEvidence) > 0 {
		gui["verificationEvidence"] = append([]string(nil), compactStrings(message.VerificationEvidence)...)
	}
	if len(message.RuntimeEvents) > 0 {
		gui["runtimeEvents"] = runtimeEventsToMetadata(message.RuntimeEvents)
	}
	if len(gui) == 0 {
		return nil
	}
	return map[string]any{
		guiRuntimeMetadataKey: gui,
	}
}

func runtimeEventsToMetadata(events []RuntimeEvent) []any {
	out := make([]any, 0, len(events))
	for _, event := range events {
		item := map[string]any{}
		if value := strings.TrimSpace(event.ID); value != "" {
			item["id"] = value
		}
		if value := strings.TrimSpace(event.Type); value != "" {
			item["type"] = value
		}
		if value := strings.TrimSpace(event.Title); value != "" {
			item["title"] = value
		}
		if value := strings.TrimSpace(event.Detail); value != "" {
			item["detail"] = value
		}
		if value := strings.TrimSpace(event.Status); value != "" {
			item["status"] = value
		}
		if value := strings.TrimSpace(event.Timestamp); value != "" {
			item["timestamp"] = value
		}
		if event.DurationMS > 0 {
			item["durationMs"] = event.DurationMS
		}
		out = append(out, item)
	}
	return out
}

func applyRuntimeMetadataToChatMessage(message *ChatMessage, metadata map[string]any) {
	if message == nil || len(metadata) == 0 {
		return
	}
	gui, ok := metadataMap(metadata[guiRuntimeMetadataKey])
	if !ok {
		return
	}
	if value := metadataString(gui["id"]); value != "" {
		message.ID = value
	}
	if value := metadataString(gui["state"]); value != "" {
		message.State = value
	}
	if value := metadataString(gui["updatedAt"]); value != "" {
		message.UpdatedAt = value
	}
	if value := metadataString(gui["runtimeSummary"]); value != "" {
		message.RuntimeSummary = value
	}
	if value := metadataString(gui["implementationResult"]); value != "" {
		message.ImplementationResult = value
	}
	if value := metadataString(gui["verificationResult"]); value != "" {
		message.VerificationResult = value
	}
	if value := metadataString(gui["verificationVerdict"]); value != "" {
		message.VerificationVerdict = value
	}
	if value := metadataString(gui["verificationSummary"]); value != "" {
		message.VerificationSummary = value
	}
	if values := metadataStringSlice(gui["verificationCoveredChecks"]); len(values) > 0 {
		message.VerificationCovered = values
	}
	if values := metadataStringSlice(gui["verificationOpenRisks"]); len(values) > 0 {
		message.VerificationOpenRisks = values
	}
	if values := metadataStringSlice(gui["verificationEvidence"]); len(values) > 0 {
		message.VerificationEvidence = values
	}
	if events := metadataRuntimeEvents(gui["runtimeEvents"]); len(events) > 0 {
		message.RuntimeEvents = events
	}
	if changeSet := metadataChangeSet(gui["changeSet"]); changeSet != nil {
		message.ChangeSet = changeSet
	}
	if rollback := metadataTurnRollback(gui["rollback"]); rollback != nil {
		message.Rollback = rollback
	}
}

// backfillToolResults 把 role=tool 的结果消息按 tool_call_id 回填到对应 tool_call item。
func backfillToolResults(msgs []adapter.SessionMessage, items []ThreadItem) {
	// 建立 item_id → items 索引（仅 tool_call）。
	toolItems := make(map[string]int)
	for i, item := range items {
		if item.Kind == "tool_call" {
			toolItems[item.ID] = i
		}
	}
	for _, msg := range msgs {
		if !strings.EqualFold(strings.TrimSpace(msg.Role), "tool") {
			continue
		}
		callID := metadataString(msg.Metadata["tool_call_id"])
		if callID == "" {
			continue
		}
		idx, ok := toolItems[callID]
		if !ok {
			continue
		}
		items[idx].ToolResult = &ItemToolResult{
			Status: "completed",
			Output: strings.TrimSpace(msg.Content),
		}
	}
}
