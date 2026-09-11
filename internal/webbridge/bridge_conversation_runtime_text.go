package webbridge

import (
	"fmt"
	"strings"

	"github.com/eosaios/eos/internal/webbridge/adapter"
)

// runtimeItemKind 从 turn.item_* 事件的 payload 中提取 item.kind。
// Rust 核心的 TurnItem 序列化为 {kind: "agent_message"|"tool_call"|"plan", ...}。
func runtimeItemKind(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	item, ok := metadataMap(payload["item"])
	if !ok {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", item["kind"])))
}

// isContentItemKind reports whether an item kind is conversational content
// (agent_message or plan) rather than a tool invocation. Content items flow
// into the message stream; tool items render as tool cards.
func isContentItemKind(kind string) bool {
	return kind == "agent_message" || kind == "plan"
}

// runtimeDeltaType 的语义（区分 text/reasoning/tool_args delta）已由 items 子系统
// 接管：applyItemDelta 按 delta_type 路由到 ThreadItem 的对应字段，不再需要把
// reasoning 塞进 <think> 标签。

func runtimeEventFromAdapterEvent(event adapter.Event, message string) (eventType, title, detail, status string, ok bool) {
	kind := strings.TrimSpace(event.EventType)
	message = strings.TrimSpace(message)
	payload := event.Payload
	if len(payload) == 0 {
		payload = event.Data
	}
	detail = runtimeEventDetail(payload, message)
	switch kind {
	case "turn.item_started":
		if isContentItemKind(runtimeItemKind(payload)) {
			return "", "", "", "", false
		}
		return "tool", fallbackText(runtimeToolTitle(payload, "调用工具"), fallbackText(message, "调用工具")), detail, "running", true
	case "turn.item_delta", "tool.executing":
		if kind == "turn.item_delta" && isContentItemKind(runtimeItemKind(payload)) {
			return "", "", "", "", false
		}
		return "tool", fallbackText(message, "执行工具步骤"), detail, "running", true
	case "turn.item_completed", "turn.tool_observation", "tool.executed":
		if kind == "turn.item_completed" && isContentItemKind(runtimeItemKind(payload)) {
			return "", "", "", "", false
		}
		return "tool", fallbackText(runtimeToolTitle(payload, "工具完成"), fallbackText(message, "工具完成")), detail, "completed", true
	case "turn.plan_delta":
		// Plan deltas stream the <proposed_plan> block; render as a planning
		// content card, not a tool event (plan delta notification).
		return "plan", fallbackText(message, "正在生成计划"), detail, "running", true
	case "turn.request_user_input":
		// The request_user_input tool suspended the turn; surface as an
		// interaction prompt. Resolution goes via approval/respond.
		return "interaction", fallbackText(message, "等待用户输入"), detail, "waiting", true
	case "turn.tool_loop_exhausted":
		return "tool", fallbackText(message, "检测到重复步骤"), detail, "blocked", true
	case "turn.mid_compact", "turn.pre_compact":
		return "reasoning", fallbackText(message, "正在总结现有结果"), detail, "running", true
	case "turn.reasoning_delta":
		return "reasoning", fallbackText(message, "正在推理"), detail, "running", true
	case "agent.started":
		return "agent", fallbackText(message, "Agent 已启动"), detail, "running", true
	case "agent.progress":
		return "agent", fallbackText(message, "Agent 正在运行"), detail, "running", true
	case "agent.done":
		return "agent", fallbackText(message, "Agent 已完成"), detail, "completed", true
	case "agent.failed":
		return "agent", fallbackText(message, "Agent 执行失败"), detail, "failed", true
	case "turn.retry":
		// 内核模型流瞬态失败自动重试（payload: attempt/max_attempts/delay_ms/
		// message）。展示为运行中状态的生命周期事件，让用户看到「正在重试」
		// 而不是无解释的转圈；标题带 "Reconnecting... n/N" 样式的进度提示。
		attempt := metadataInt64(payload["attempt"])
		maxAttempts := metadataInt64(payload["max_attempts"])
		title := "模型连接中断，自动重试中"
		if attempt > 0 && maxAttempts > 0 {
			title = fmt.Sprintf("模型连接中断，自动重试中（%d/%d）", attempt, maxAttempts)
		}
		return "lifecycle", fallbackText(message, title), detail, "running", true
	case "request.failed", "turn.error":
		return "lifecycle", fallbackText(message, "请求失败"), detail, "failed", true
	default:
		return "", "", "", "", false
	}
}

func runtimeCompletionEvent(payload map[string]any, fallback string) (title string, detail string) {
	verdict := strings.ToUpper(strings.TrimSpace(runtimeNestedStringValue(payload, "verification_verdict")))
	if verdict != "" {
		title = "验收 " + verdict
		detail = fallbackText(
			runtimeNestedStringValue(payload, "summary", "verification_result", "message", "result"),
			fallbackText(strings.TrimSpace(fallback), "请求已完成"),
		)
		return title, detail
	}
	title = fallbackText(strings.TrimSpace(fallback), "请求已完成")
	detail = runtimeEventDetail(payload, fallback)
	return title, detail
}

func requestCompletedText(payload map[string]any) string {
	return runtimeStringValue(payload, "text")
}

func runtimeToolTitle(payload map[string]any, fallback string) string {
	name := runtimeStringValue(payload, "tool_name", "name", "tool")
	if name == "" {
		return fallback
	}
	return fallback + "：" + name
}

func runtimeEventDetail(payload map[string]any, fallback string) string {
	if detail := runtimeNestedStringValue(payload, "summary", "display", "detail", "verification_result", "message", "text", "command", "path", "title"); detail != "" {
		return detail
	}
	return strings.TrimSpace(fallback)
}
