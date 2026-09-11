package webbridge

import (
	"strings"

	"github.com/eosaios/eos/internal/webbridge/adapter"
)

// bridge_conversation_items.go 处理结构化 ThreadItem 列表的累积。
//
// 结构化 ThreadItem 模型：内核发 turn.item_started / turn.item_delta /
// turn.item_completed 事件（带 item_id + kind + delta_type），bridge 按 item_id
// 在 ChatMessage.Items 里累积，思考/正文/工具调用各是独立 item，一旦显示不被覆盖。
//
// 这取代了旧的「content 字符串拼接 + <think> 标签 + flush commentary」机制。
// Content 字段仍保留（复制/回退/历史会话兼容），但不再是渲染的唯一来源。

// applyItemStarted 在 message.Items 里插入新 item（若同 id 不存在）。
func applyItemStarted(message *ChatMessage, payload map[string]any) {
	if message == nil || payload == nil {
		return
	}
	item, ok := extractThreadItem(payload["item"])
	if !ok {
		return
	}
	// 按 id 去重：已存在则更新（item_started 可能重发），否则追加。
	if idx := findItemIndex(message.Items, item.ID); idx >= 0 {
		item.Status = "streaming"
		message.Items[idx] = item
	} else {
		item.Status = "streaming"
		message.Items = append(message.Items, item)
	}
}

// applyItemDelta 按 item_id + delta_type 累积增量到对应 item 字段。
func applyItemDelta(message *ChatMessage, payload map[string]any) {
	if message == nil || payload == nil {
		return
	}
	itemID, _ := payload["item_id"].(string)
	deltaType, _ := payload["delta_type"].(string)
	delta, _ := payload["delta"].(string)
	if strings.TrimSpace(delta) == "" {
		return
	}
	idx := findItemIndex(message.Items, itemID)
	if idx < 0 {
		return
	}
	item := &message.Items[idx]
	switch strings.TrimSpace(deltaType) {
	case "reasoning":
		item.Reasoning += delta
	case "tool_args":
		item.ToolArgs += delta
	default: // "text" 或空
		item.Text += delta
	}
}

// applyItemCompleted 用完整 item 替换流式累积的（内核给出最终内容）。
func applyItemCompleted(message *ChatMessage, payload map[string]any) {
	if message == nil || payload == nil {
		return
	}
	item, ok := extractThreadItem(payload["item"])
	if !ok {
		return
	}
	item.Status = "completed"
	if idx := findItemIndex(message.Items, item.ID); idx >= 0 {
		message.Items[idx] = item
	} else {
		message.Items = append(message.Items, item)
	}
}

// finalizeMessageItems 标记所有 streaming 的 item 为 completed（turn 结束时调用）。
func finalizeMessageItems(message *ChatMessage) {
	if message == nil {
		return
	}
	for i := range message.Items {
		if message.Items[i].Status == "" || message.Items[i].Status == "streaming" {
			message.Items[i].Status = "completed"
		}
	}
}

// appendStatusItem 往 message.Items 追加/更新一个 status item（取消/等待/失败/信息等生命周期提示）。
// text 是一句话文案，level 决定前端样式（info/warning/error），status 是 item 级状态。
// 策略：
// 1. 连续相同文案的 status 直接替换，避免重复 emit 时堆叠。
// 2. 如果最后一个 status 仍处于 waiting/streaming 状态，用新状态替换它（例如“等待确认…”→“允许”）。
// 3. 否则追加新 status。
func appendStatusItem(message *ChatMessage, text, level, status string) {
	appendStatusItemWithID(message, "", text, level, status)
}

// appendStatusItemWithID 同 appendStatusItem，但允许为 status item 指定确定性 id。
//
// 审批/问询类状态行必须用确定性 id（statusKey 形如 "approval:<approval_id>"），
// 这样 resolved 时（settlePromptLocked）能按同一个 id 精确定位并改写该 item，再通过
// conversation-delta 增量通道推送给前端——彻底绕开"全量快照 + 时间戳守卫"的竞态路径
// （该竞态是"点允许后等待确认不更新"的根因）。
//
// statusKey 为空时退化为旧的随机 id 行为（向后兼容普通生命周期提示）。
func appendStatusItemWithID(message *ChatMessage, statusKey, text, level, status string) {
	if message == nil {
		return
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	status = strings.TrimSpace(status)
	if status == "" {
		status = "completed"
	}
	statusKey = strings.TrimSpace(statusKey)
	items := message.Items
	// 带确定性 key 的 status item：按 key 原位更新（审批 resolved 的权威路径）。
	// 这优先于"末尾/回扫"启发式，保证同一审批的 waiting→completed 一定落在同一个 item 上。
	if statusKey != "" {
		if idx := findStatusItemIndex(items, statusKey); idx >= 0 {
			items[idx].Text = text
			items[idx].Level = level
			items[idx].Status = status
			return
		}
	}
	if len(items) > 0 {
		last := &items[len(items)-1]
		if strings.TrimSpace(strings.ToLower(last.Kind)) == "status" {
			// 相同文案：仅刷新级别/状态，避免重复追加。
			if strings.TrimSpace(last.Text) == text {
				last.Level = level
				last.Status = status
				return
			}
			// 前一条状态仍在等待/流式中：表示是同一次生命周期，用新结果替换。
			if last.Status == "waiting" || last.Status == "streaming" {
				last.Text = text
				last.Level = level
				last.Status = status
				return
			}
		}
	}
	// 末尾不是可替换的状态行时，回扫最近一条仍处于 waiting/streaming 的 status 原位替换。
	// 场景：审批悬起期间内核又推了 tool_call/reasoning，把"等待确认…"压在下面；
	// resolved 到达时若只看末尾会误判追加，导致旧 waiting 行残留（用户确认后状态不变）。
	if idx := findPendingStatusIndex(items); idx >= 0 {
		items[idx].Text = text
		items[idx].Level = level
		items[idx].Status = status
		return
	}
	id := newID("status")
	if statusKey != "" {
		id = statusKey
	}
	item := ThreadItem{
		ID:     id,
		Kind:   "status",
		Text:   text,
		Level:  level,
		Status: status,
	}
	message.Items = append(message.Items, item)
}

// findStatusItemIndex 返回指定确定性 id 的 status item 下标，不存在返回 -1。
// 用于审批 resolved 时按 approval key 精确定位待更新的状态行。
func findStatusItemIndex(items []ThreadItem, statusKey string) int {
	statusKey = strings.TrimSpace(statusKey)
	if statusKey == "" {
		return -1
	}
	for i, item := range items {
		if item.ID == statusKey && strings.TrimSpace(strings.ToLower(item.Kind)) == "status" {
			return i
		}
	}
	return -1
}

// findPendingStatusIndex 回扫 items，返回最后一条悬起待确认（waiting）status 的下标。
// 只匹配 waiting：streaming 表示该轮审批已 resolved、turn 恢复执行，是"已完成"的
// 生命周期，绝不能被下一轮审批/resolved 再次改写。没有悬起 status 时返回 -1。
func findPendingStatusIndex(items []ThreadItem) int {
	for i := len(items) - 1; i >= 0; i-- {
		if strings.TrimSpace(strings.ToLower(items[i].Kind)) != "status" {
			continue
		}
		if items[i].Status == "waiting" {
			return i
		}
	}
	return -1
}

// extractThreadItem 从内核 payload 的 item 字段提取 ThreadItem。
// payload["item"] 是序列化的 TurnItem（{kind, id, text/reasoning/...}）。
func extractThreadItem(raw any) (ThreadItem, bool) {
	obj, ok := raw.(map[string]any)
	if !ok {
		return ThreadItem{}, false
	}
	kind, _ := obj["kind"].(string)
	id, _ := obj["id"].(string)
	item := ThreadItem{ID: id, Kind: kind}
	switch kind {
	case "agent_message":
		item.Text, _ = obj["text"].(string)
	case "reasoning":
		// content 可能是数组（多段）或字符串
		item.Reasoning = joinStringSlice(obj["content"])
		// summary 优先级低，暂不单独存
	case "tool_call":
		item.ToolName, _ = obj["name"].(string)
		item.ToolArgs, _ = obj["arguments"].(string)
		item.Category, _ = obj["category"].(string)
		if resultRaw, ok := obj["result"].(map[string]any); ok {
			item.ToolResult = extractItemToolResult(resultRaw)
		}
	case "plan":
		item.Text, _ = obj["text"].(string)
	case "status":
		item.Text, _ = obj["text"].(string)
		item.Level, _ = obj["level"].(string)
	}
	return item, true
}

func extractItemToolResult(obj map[string]any) *ItemToolResult {
	result := &ItemToolResult{}
	result.Status, _ = obj["status"].(string)
	result.Error, _ = obj["error"].(string)
	// Rust core reports "ok" for success; the shell expects "completed".
	// Normalize non-success statuses to "failed" so the trace UI maps them
	// to the defined completed/failed/running states.
	switch result.Status {
	case "ok", "":
		result.Status = "completed"
	case "error", "denied", "failed":
		result.Status = "failed"
	}
	result.Output = formatToolResultOutput(obj)
	if dur, ok := obj["duration_ms"].(float64); ok {
		result.DurationMS = int64(dur)
	}
	return result
}

// formatToolResultOutput 从 ToolResult payload 提取可读输出。
func formatToolResultOutput(obj map[string]any) string {
	// display 字段是内核格式化好的可读文本，优先用
	if display, ok := obj["display"].(string); ok && strings.TrimSpace(display) != "" {
		return display
	}
	// output 字段可能是任意 JSON value
	if raw, ok := obj["output"]; ok {
		if text, ok := raw.(string); ok {
			return text
		}
	}
	return ""
}

// joinStringSlice 把 content/summary（可能是 []any 或 string）拼成单个字符串。
func joinStringSlice(raw any) string {
	if raw == nil {
		return ""
	}
	switch v := raw.(type) {
	case string:
		return v
	case []any:
		var sb strings.Builder
		for _, item := range v {
			if text, ok := item.(string); ok {
				sb.WriteString(text)
			}
		}
		return sb.String()
	}
	return ""
}

func findItemIndex(items []ThreadItem, id string) int {
	for i, item := range items {
		if item.ID == id {
			return i
		}
	}
	return -1
}

// promptStatusKey 构造待确认状态行（审批/问询/计划问题）的确定性 item id。
// prompt.ID 对 approval 是 approval_id、对 inquiry 是 request_id、对 request_user_input
// 是 call_id——都是稳定主键。由此派生的 statusKey 让"等待确认/等待回答…"状态行在
// resolved 时能被精确定位 + 通过 conversation-delta 增量推送，绕开全量快照时间戳竞态。
func promptStatusKey(promptID string) string {
	promptID = strings.TrimSpace(promptID)
	if promptID == "" {
		return ""
	}
	return "prompt:" + promptID
}

// payloadForItem 提取事件的 payload（兼容 event.Payload 和 event.Data 两个字段）。
func payloadForItem(event adapter.Event) map[string]any {
	payload := event.Payload
	if len(payload) == 0 {
		payload = event.Data
	}
	return payload
}
