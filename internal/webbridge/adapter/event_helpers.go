package adapter

// Copyright (c) 2026 EOSAIOS
// SPDX-License-Identifier: EOS-NCL-1.1
// 本文件基于 EOS 非商用许可证 v1.1 发布，详见 LICENSE。
// 商业使用请联系版权人获得商业授权。

import (
	"strings"

	"github.com/eosaios/eos/pkg/protocol"
)

func newAdapterEvent(eventType protocol.EventType, legacyType, requestID, message string, payload map[string]any) Event {
	cloned := protocol.ClonePayload(payload)
	if cloned == nil {
		cloned = map[string]any{}
	}
	switch eventType {
	case protocol.EventTypeItemDelta, protocol.EventTypeTextFinal, protocol.EventTypeTextReasoning:
		if _, ok := cloned["text"]; !ok && strings.TrimSpace(message) != "" {
			cloned["text"] = message
		}
	case protocol.EventTypeApprovalReq:
		if _, ok := cloned["approval_id"]; !ok && strings.TrimSpace(requestID) != "" {
			cloned["approval_id"] = requestID
		}
		if _, ok := cloned["message"]; !ok && strings.TrimSpace(message) != "" {
			cloned["message"] = message
		}
	case protocol.EventTypeInquiryReq:
		if _, ok := cloned["inquiry_id"]; !ok && strings.TrimSpace(requestID) != "" {
			cloned["inquiry_id"] = requestID
		}
		if _, ok := cloned["question"]; !ok && strings.TrimSpace(message) != "" {
			cloned["question"] = message
		}
	case protocol.EventTypeRequestFailed:
		if _, ok := cloned["error"]; !ok && strings.TrimSpace(message) != "" {
			cloned["error"] = message
		}
	default:
		if _, ok := cloned["message"]; !ok && strings.TrimSpace(message) != "" {
			cloned["message"] = message
		}
	}

	ev := Event{
		Type:      strings.TrimSpace(legacyType),
		EventType: string(eventType),
		Version:   string(protocol.VersionV1),
		RequestID: strings.TrimSpace(requestID),
		Source:    string(protocol.SourceGUI),
		Message:   strings.TrimSpace(message),
		Data:      protocol.ClonePayload(cloned),
		Payload:   cloned,
	}
	if ev.Message == "" {
		ev.Message = eventMessageFromPayload(ev.Kind(), cloned)
	}
	if ev.Type == "" {
		ev.Type = legacyTypeFromProtocol(eventType)
	}
	return ev
}

func newRequestFailedEvent(requestID, message string) Event {
	return newAdapterEvent(protocol.EventTypeRequestFailed, "Error", requestID, message, nil)
}

func (e Event) Kind() string {
	if kind := normalizeEventKind(strings.TrimSpace(e.EventType)); kind != "" {
		return kind
	}
	return normalizeEventKind(strings.TrimSpace(e.Type))
}

func (e Event) EffectiveRequestID() string {
	if v := strings.TrimSpace(e.RequestID); v != "" {
		return v
	}
	payload := e.payloadMap()
	switch e.Kind() {
	case string(protocol.EventTypeApprovalReq):
		return stringValue(payload, "approval_id")
	case string(protocol.EventTypeInquiryReq):
		return stringValue(payload, "inquiry_id")
	default:
		return ""
	}
}

func (e Event) EffectiveMessage() string {
	if v := strings.TrimSpace(e.Message); v != "" {
		return v
	}
	return eventMessageFromPayload(e.Kind(), e.payloadMap())
}

func (e Event) payloadMap() map[string]any {
	if len(e.Payload) > 0 {
		return e.Payload
	}
	return e.Data
}

func protocolEnvelopeToEvent(ev protocol.Envelope) Event {
	payload := protocol.ClonePayload(ev.Payload)
	if payload == nil {
		payload = map[string]any{}
	}
	rawEventType := ev.EventType
	eventType := protocol.NormalizeEventType(rawEventType)
	if rawEventType != eventType {
		payload["original_event_type"] = string(rawEventType)
		normalizeTurnEventPayload(payload, rawEventType, eventType)
	}
	turnID := strings.TrimSpace(ev.TurnID)
	if turnID == "" {
		turnID = stringValue(payload, "turn_id")
	}
	agentID := strings.TrimSpace(ev.AgentID)
	if agentID == "" {
		agentID = stringValue(payload, "agent_id")
	}
	requestID := strings.TrimSpace(ev.RequestID)
	if requestID == "" {
		switch eventType {
		case protocol.EventTypeApprovalReq:
			requestID = stringValue(payload, "approval_id")
		case protocol.EventTypeInquiryReq:
			requestID = stringValue(payload, "inquiry_id")
		}
	}

	out := Event{
		Type:          legacyTypeFromProtocol(eventType),
		EventType:     string(rawEventType),
		Version:       string(ev.Version),
		EventID:       strings.TrimSpace(ev.EventID),
		RequestID:     requestID,
		SessionID:     strings.TrimSpace(ev.SessionID),
		ThreadID:      strings.TrimSpace(ev.ThreadID),
		TurnID:        turnID,
		AgentID:       agentID,
		CorrelationID: strings.TrimSpace(ev.CorrelationID),
		Source:        strings.TrimSpace(string(ev.Source)),
		Message:       eventMessageFromPayload(string(rawEventType), payload),
		Data:          protocol.ClonePayload(payload),
		Payload:       payload,
	}
	return out
}

func normalizeTurnEventPayload(payload map[string]any, rawEventType, eventType protocol.EventType) {
	if payload == nil {
		return
	}
	switch rawEventType {
	case protocol.EventTypeTurnItemStarted,
		protocol.EventTypeTurnItemDelta,
		protocol.EventTypeTurnItemCompleted,
		protocol.EventTypeTurnWaitingApproval:
		// eos-core-rs 的 turn.item_* 事件把工具信息放在 payload.item 里，
		// 需要把工具名提升到顶层，让前端时间线能显示"工具完成：xxx"。
		if stringValue(payload, "tool_name") == "" {
			name := stringValue(payload, "name", "tool")
			if name == "" {
				name = nestedItemString(payload, "name", "tool")
			}
			if name != "" {
				payload["tool_name"] = name
			}
		}
		// turn.item_completed 的自然语言说明在 item.result.display，
		// 提升到顶层 message，使前端时间线能为每个工具卡片显示对应说明。
		if rawEventType == protocol.EventTypeTurnItemCompleted {
			if stringValue(payload, "message", "display") == "" {
				if display := nestedItemResultString(payload, "display"); display != "" {
					payload["message"] = display
				}
			}
		}
	}
	if eventType == protocol.EventTypeRequestFailed && stringValue(payload, "error", "message", "text") == "" {
		switch rawEventType {
		case protocol.EventTypeTurnCancelled:
			payload["error"] = "request cancelled"
		case protocol.EventTypeTurnInterrupted:
			payload["error"] = "request interrupted"
		}
	}
}

func legacyTypeFromProtocol(eventType protocol.EventType) string {
	switch eventType {
	case protocol.EventTypeItemDelta:
		return "TextDelta"
	case protocol.EventTypeTextFinal:
		return "TextFinal"
	case protocol.EventTypeApprovalReq:
		return "ConfirmRequired"
	case protocol.EventTypeInquiryReq:
		return "Inquiry"
	case protocol.EventTypeRequestFailed:
		return "Error"
	case protocol.EventTypeItemStarted,
		protocol.EventTypeItemCompleted,
		protocol.EventTypeTextReasoning,
		protocol.EventTypeAgentStarted,
		protocol.EventTypeAgentProgress,
		protocol.EventTypeAgentDone,
		protocol.EventTypeAgentFailed,
		protocol.EventTypeAgentCancelled,
		protocol.EventTypeTaskStarted,
		protocol.EventTypeTaskUpdated,
		protocol.EventTypeTaskDone,
		protocol.EventTypeTaskFailed:
		return "ToolStep"
	default:
		return ""
	}
}

func eventMessageFromPayload(kind string, payload map[string]any) string {
	switch normalizeEventKind(kind) {
	case string(protocol.EventTypeItemDelta),
		string(protocol.EventTypeTextFinal),
		string(protocol.EventTypeTextReasoning):
		return stringValue(payload, "delta", "text", "message")
	case string(protocol.EventTypeApprovalReq):
		return stringValue(payload, "message", "title", "text")
	case string(protocol.EventTypeInquiryReq):
		return stringValue(payload, "question", "message", "text")
	case string(protocol.EventTypeRequestFailed):
		return stringValue(payload, "error", "message", "text")
	case string(protocol.EventTypeItemStarted):
		if msg := stringValue(payload, "message"); msg != "" {
			return msg
		}
		if toolName := stringValue(payload, "tool_name"); toolName != "" {
			return "调用工具: " + toolName
		}
	case string(protocol.EventTypeItemCompleted):
		if msg := stringValue(payload, "display", "message"); msg != "" {
			return msg
		}
		if toolName := stringValue(payload, "tool_name"); toolName != "" {
			return "工具完成: " + toolName
		}
	case string(protocol.EventTypeAgentStarted),
		string(protocol.EventTypeAgentProgress),
		string(protocol.EventTypeAgentDone),
		string(protocol.EventTypeAgentFailed),
		string(protocol.EventTypeAgentCancelled),
		string(protocol.EventTypeTaskStarted),
		string(protocol.EventTypeTaskUpdated),
		string(protocol.EventTypeTaskDone),
		string(protocol.EventTypeTaskFailed):
		return stringValue(payload, "message", "display", "text", "label", "title", "tool_name")
	}
	return stringValue(payload, "message", "text", "question", "display", "error", "tool_name", "title")
}

func normalizeEventKind(kind string) string {
	normalized := protocol.NormalizeEventType(protocol.EventType(strings.TrimSpace(kind)))
	switch string(normalized) {
	case "TextDelta", string(protocol.EventTypeItemDelta):
		return string(protocol.EventTypeItemDelta)
	case "TextFinal", string(protocol.EventTypeTextFinal):
		return string(protocol.EventTypeTextFinal)
	case "TextReasoning", string(protocol.EventTypeTextReasoning):
		return string(protocol.EventTypeTextReasoning)
	case "ConfirmRequired", string(protocol.EventTypeApprovalReq):
		return string(protocol.EventTypeApprovalReq)
	case "Inquiry", string(protocol.EventTypeInquiryReq):
		return string(protocol.EventTypeInquiryReq)
	case "Error", string(protocol.EventTypeRequestFailed):
		return string(protocol.EventTypeRequestFailed)
	case "ToolStep", string(protocol.EventTypeItemStarted), string(protocol.EventTypeItemCompleted):
		return string(protocol.EventTypeItemDelta)
	default:
		return strings.TrimSpace(kind)
	}
}

func stringValue(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		raw, ok := payload[key]
		if !ok {
			continue
		}
		if text, ok := raw.(string); ok {
			text = strings.TrimSpace(text)
			if text != "" {
				return text
			}
		}
	}
	return ""
}

// nestedItemString 从 payload.item 中提取指定字段。
func nestedItemString(payload map[string]any, keys ...string) string {
	item, ok := payload["item"].(map[string]any)
	if !ok {
		return ""
	}
	return stringValue(item, keys...)
}

// nestedItemResultString 从 payload.item.result 中提取指定字段。
func nestedItemResultString(payload map[string]any, keys ...string) string {
	item, ok := payload["item"].(map[string]any)
	if !ok {
		return ""
	}
	result, ok := item["result"].(map[string]any)
	if !ok {
		return ""
	}
	return stringValue(result, keys...)
}
