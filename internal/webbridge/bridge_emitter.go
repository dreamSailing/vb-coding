package webbridge

import (
	"strings"
	"time"
)

func (s *BridgeService) emitHeartbeat() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			emitter := s.currentEmitter()
			if emitter == nil {
				continue
			}
			window := s.captureWindowSnapshot()
			emitter(heartbeatEventName, HeartbeatPayload{
				Time:             time.Now().Format(time.RFC3339),
				ActiveWorkspace:  s.activeWorkspaceValue(),
				BridgeMode:       s.bridgeMode(),
				CurrentSessionID: s.currentSessionValue(),
				Window:           window,
			})
		}
	}
}

func (s *BridgeService) emitShellUpdated() {
	s.emitShellUpdatedForSessionWithSource("", "event.runtime")
}

func (s *BridgeService) emitShellUpdatedForSession(sessionID string) {
	s.emitShellUpdatedForSessionWithSource(sessionID, "event.runtime")
}

func (s *BridgeService) emitShellUpdatedForSessionWithSource(sessionID, source string) {
	emitter := s.currentEmitter()
	if emitter == nil {
		return
	}
	go func() {
		payload := s.loadBootstrap(source)
		if strings.TrimSpace(sessionID) != "" {
			payload = s.bootstrapForSessionWithSource(sessionID, source)
		}
		emitter(shellUpdatedEventName, payload)
	}()
}

// emitConversationDelta 发送轻量增量事件（item/agentMessage/delta）。
// 零 RPC 往返——纯 Wails EventProcessor 调用，可在 stateMu 锁内安全调用（不阻塞、不开 goroutine）。
// 前端据此 patch 单条消息的单个 item，无需全量 loadBootstrap。
func (s *BridgeService) emitConversationDelta(payload ConversationDeltaPayload) {
	emitter := s.currentEmitter()
	if emitter == nil {
		return
	}
	emitter(conversationDeltaEventName, payload)
}

// emitTurnUsage 发送内核每步累计 token 用量的轻量转发（turn.token_usage →
// eos:bridge:usage-updated）。零 RPC，可在 stateMu 锁内安全调用。前端据此
// 实时刷新上下文用量/输出速率，不等 turn 收尾的全量快照。
func (s *BridgeService) emitTurnUsage(payload TurnUsagePayload) {
	emitter := s.currentEmitter()
	if emitter == nil {
		return
	}
	emitter(usageUpdatedEventName, payload)
}

func (s *BridgeService) currentEmitter() func(string, any) {
	// web 模式唯一发射通道是注入的 emitEvent（server 的 WS 事件扇出）；
	// 未注入（如纯单元测试）返回 nil，所有 emit 静默跳过。
	if s.emitEvent != nil {
		return s.emitEvent
	}
	return nil
}

func (s *BridgeService) emitStartupBootstrap() {
	emitter := s.currentEmitter()
	if emitter == nil {
		return
	}

	go emitter(shellUpdatedEventName, s.loadBootstrapWithOptions("event.startup.initial", BootstrapLoadImmediate))

	retries := []struct {
		delay  time.Duration
		source string
		scope  BootstrapLoadScope
	}{
		{delay: 1 * time.Second, source: "event.startup.retry.1", scope: BootstrapLoadIncludeDeferred},
		{delay: 3 * time.Second, source: "event.startup.retry.2", scope: BootstrapLoadIncludeDeferred},
	}
	for _, retry := range retries {
		go func() {
			timer := time.NewTimer(retry.delay)
			defer timer.Stop()
			select {
			case <-s.stopCh:
				return
			case <-timer.C:
				emitter(shellUpdatedEventName, s.loadBootstrapWithOptions(retry.source, retry.scope))
			}
		}()
	}
}
