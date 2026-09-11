package adapter

// Copyright (c) 2026 EOSAIOS
// SPDX-License-Identifier: EOS-NCL-1.1
// 本文件基于 EOS 非商用许可证 v1.1 发布，详见 LICENSE。
// 商业使用请联系版权人获得商业授权。

import (
	"context"
	"encoding/json"
	"log/slog"
	"slices"
	"strings"
	"sync"

	"github.com/eosaios/eos/pkg/protocol"
	protocoljsonrpc "github.com/eosaios/eos/pkg/protocol/jsonrpc"
)

type runtimeJSONRPCEventFilter struct {
	EventTypes []string
	SessionID  string
	TurnID     string
	AgentID    string
}

type runtimeJSONRPCEventSubscriber struct {
	filter runtimeJSONRPCEventFilter
	ch     chan Event
}

type runtimeJSONRPCEventSink struct {
	mu          sync.RWMutex
	subscribers map[int]runtimeJSONRPCEventSubscriber
	nextID      int
	closed      bool
}

func newRuntimeJSONRPCEventSink() *runtimeJSONRPCEventSink {
	return &runtimeJSONRPCEventSink{subscribers: map[int]runtimeJSONRPCEventSubscriber{}}
}

func (s *runtimeJSONRPCEventSink) Notify(ctx context.Context, notification protocoljsonrpc.Notification) error {
	if s == nil {
		return nil
	}
	if err := notification.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(notification.Method) != protocoljsonrpc.NotificationEvent {
		return nil
	}
	var envelope protocol.Envelope
	if err := json.Unmarshal(notification.Params, &envelope); err != nil {
		return err
	}
	if err := ctxErr(ctx); err != nil {
		return err
	}
	return s.publish(ctx, protocolEnvelopeToEvent(envelope))
}

func (s *runtimeJSONRPCEventSink) Subscribe(ctx context.Context, filter runtimeJSONRPCEventFilter, buffer int) (<-chan Event, func()) {
	if buffer <= 0 {
		buffer = 64
	}
	ch := make(chan Event, buffer)
	if s == nil {
		close(ch)
		return ch, func() {}
	}
	filter = normalizeRuntimeJSONRPCEventFilter(filter)

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		close(ch)
		return ch, func() {}
	}
	id := s.nextID
	s.nextID++
	s.subscribers[id] = runtimeJSONRPCEventSubscriber{filter: filter, ch: ch}
	s.mu.Unlock()

	var once sync.Once
	unsubscribe := func() {
		once.Do(func() {
			s.mu.Lock()
			if sub, ok := s.subscribers[id]; ok {
				delete(s.subscribers, id)
				close(sub.ch)
			}
			s.mu.Unlock()
		})
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if done := ctx.Done(); done != nil {
		go func() {
			<-done
			unsubscribe()
		}()
	}
	return ch, unsubscribe
}

// publish 将事件分发给所有匹配的订阅者。
//
// 关键约束：必须非阻塞。readLoop（stream_client.go:158）同步调用 Notify → publish，
// 如果 publish 在某个 subscriber channel 满时阻塞，会卡死 readLoop → stdio 管道
// 全局死锁（core 写 stdio 阻塞 → 不响应 Go 的 RPC → 所有 Call 阻塞）。
// 因此 channel 满时丢弃事件（default 分支），绝不阻塞读循环（notification
// handler 不阻塞 readLoop 原则）。丢弃数经 slog.Warn 暴露，
// 便于排查订阅者消费过慢导致的事件丢失。
func (s *runtimeJSONRPCEventSink) publish(ctx context.Context, event Event) error {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return nil
	}
	delivered := 0
	dropped := 0
	for _, sub := range s.subscribers {
		if !runtimeJSONRPCEventMatchesFilter(event, sub.filter) {
			continue
		}
		select {
		case sub.ch <- event:
			delivered++
		default:
			dropped++
		}
	}
	if dropped > 0 {
		slog.Warn("runtime_events.dropped",
			"eventType", event.EventType,
			"delivered", delivered,
			"dropped", dropped)
	}
	return nil
}

func (s *runtimeJSONRPCEventSink) close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	for id, sub := range s.subscribers {
		delete(s.subscribers, id)
		close(sub.ch)
	}
}

func normalizeRuntimeJSONRPCEventFilter(filter runtimeJSONRPCEventFilter) runtimeJSONRPCEventFilter {
	filter.EventTypes = runtimeJSONRPCEventTypes(filter.EventTypes)
	filter.SessionID = strings.TrimSpace(filter.SessionID)
	filter.TurnID = strings.TrimSpace(filter.TurnID)
	filter.AgentID = strings.TrimSpace(filter.AgentID)
	return filter
}

func runtimeJSONRPCEventMatchesFilter(event Event, filter runtimeJSONRPCEventFilter) bool {
	if len(filter.EventTypes) > 0 {
		eventType := strings.TrimSpace(event.EventType)
		if eventType == "" {
			eventType = strings.TrimSpace(event.Kind())
		}
		if eventType == "" {
			eventType = strings.TrimSpace(event.Type)
		}
		if !slices.Contains(filter.EventTypes, eventType) {
			return false
		}
	}
	if filter.SessionID != "" && filter.SessionID != strings.TrimSpace(event.SessionID) && filter.SessionID != stringValue(event.payloadMap(), "session_id") {
		return false
	}
	if filter.TurnID != "" &&
		filter.TurnID != strings.TrimSpace(event.TurnID) &&
		filter.TurnID != strings.TrimSpace(event.RequestID) &&
		filter.TurnID != strings.TrimSpace(event.CorrelationID) &&
		filter.TurnID != stringValue(event.payloadMap(), "turn_id") &&
		filter.TurnID != stringValue(event.payloadMap(), "request_id") {
		return false
	}
	if filter.AgentID != "" &&
		filter.AgentID != strings.TrimSpace(event.AgentID) &&
		filter.AgentID != stringValue(event.payloadMap(), "agent_id") &&
		filter.AgentID != stringValue(event.payloadMap(), "agent_name") {
		return false
	}
	return true
}

func runtimeJSONRPCEventTypes(eventTypes []string) []string {
	if len(eventTypes) == 0 {
		return nil
	}
	normalized := make([]string, 0, len(eventTypes))
	seen := make(map[string]struct{}, len(eventTypes))
	for _, eventType := range eventTypes {
		eventType = strings.TrimSpace(eventType)
		if eventType == "" {
			continue
		}
		if _, ok := seen[eventType]; ok {
			continue
		}
		seen[eventType] = struct{}{}
		normalized = append(normalized, eventType)
	}
	return normalized
}

func runtimeStateSyncEventTypes() []string {
	return []string{
		"session.created",
		"session.history_replay",
		"turn.started",
		"turn.completed",
		"turn.context_window_exceeded",
		"turn.tool_loop_exhausted",
		"turn.waiting_approval",
		"turn.mid_compact",
		"turn.error",
		"turn.retry",
		"turn.cancelled",
		"turn.tool_observation",
		"turn.model_downshift",
		"turn.pre_compact",
		"turn.interrupted",
		"agent.started",
		"agent.progress",
		"agent.resumed",
		"agent.done",
		"agent.waiting_approval",
		"agent.cancelled",
		"agent.failed",
		"agent.spawned",
		"tool.denied",
		"tool.validation_failed",
		"tool.loop_detected",
		"tool.approval_required",
		"tool.executing",
		"tool.executed",
		"tool.sandbox_denied",
		"approval.responded",
		"agent.input",
		"inquiry.responded",
		"goal.updated",
		"goal.cleared",
		// 浏览器子系统（内核 eos-core-browser 发布；协议常量 BROWSER_EVENT_TYPES）
		"browser.state", "browser.action",
		"browser.takeover.started", "browser.takeover.ended",
		"browser.pick.started", "browser.pick.stopped", "browser.pick.selected",
		"browser.download.started", "browser.download.progress", "browser.download.completed",
		"browser.page.updated", "browser.frame",
		"browser.dialog.opened", "browser.upload.needed",
	}
}

func runtimeRPCSubscriptionEventTypes(filter runtimeJSONRPCEventFilter) []string {
	if strings.TrimSpace(filter.SessionID) != "" || strings.TrimSpace(filter.TurnID) != "" || strings.TrimSpace(filter.AgentID) != "" {
		return nil
	}
	return runtimeStateSyncEventTypes()
}

func ctxErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}
