package webbridge

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/eosaios/eos/internal/webbridge/adapter"
	"github.com/eosaios/eos/pkg/coreapi"
)

type bridgeRuntimeGatewayRequestTurnStreamer interface {
	CoreStartTurnStreamWithRequestRPC(context.Context, coreapi.StartTurnRequest) (<-chan adapter.Event, coreapi.Turn, error)
}

type bridgeRuntimeGatewayResumeTurnStreamer interface {
	CoreResumeTurnStreamRPC(context.Context, string, string) (<-chan adapter.Event, coreapi.Turn, error)
}

// startResumeConversationTurnRPC 续跑失败 turn：不发送任何用户输入，直接调
// turn/resume——内核按已提交历史重建请求续写（resume 语义）。turnID 由
// 调用方预生成，供事件订阅过滤。
func (s *BridgeService) startResumeConversationTurnRPC(ctx context.Context, sessionID, turnID string) (conversationStreamHandle, error) {
	if s == nil || s.runtimeGatewayClient() == nil {
		return conversationStreamHandle{}, errors.New("runtime core unavailable")
	}
	if ctx == nil {
		ctx = coreCtx()
	}
	sessionID = strings.TrimSpace(sessionID)
	turnID = strings.TrimSpace(turnID)
	if sessionID == "" {
		sessionID = s.resolveOrCreateSessionRPC()
		if sessionID == "" {
			return conversationStreamHandle{}, errors.New("session_id is required: failed to resolve session for turn resume")
		}
	}
	resumer, ok := s.runtimeGatewayClient().(bridgeRuntimeGatewayResumeTurnStreamer)
	if !ok {
		return conversationStreamHandle{}, errors.New("runtime gateway does not support turn/resume streaming")
	}
	stream, turn, err := resumer.CoreResumeTurnStreamRPC(ctx, sessionID, turnID)
	if err != nil {
		return conversationStreamHandle{}, err
	}
	handle := conversationGatewayTurnHandle(s.runtimeGatewayClient(), sessionID, stream, strings.TrimSpace(turn.ID))
	handle.SessionID = sessionID
	return handle, nil
}

func (s *BridgeService) startConversationTurnRPC(ctx context.Context, sessionID, input string, attachments []adapter.Attachment, turnID string) (conversationStreamHandle, error) {
	if s == nil || s.runtimeGatewayClient() == nil {
		return conversationStreamHandle{}, errors.New("runtime core unavailable")
	}
	if ctx == nil {
		ctx = coreCtx()
	}
	sessionID = strings.TrimSpace(sessionID)
	turnID = strings.TrimSpace(turnID)
	imagePaths := imagePathsFromRuntimeAttachments(attachments)

	// If sessionID is empty, resolve or create a session before calling
	// turn/start so that we never pass an empty session_id to the Rust core.
	if sessionID == "" {
		sessionID = s.resolveOrCreateSessionRPC()
		if sessionID == "" {
			return conversationStreamHandle{}, errors.New("session_id is required: failed to resolve or create a session")
		}
	}

	stream, turn, err := s.callStartTurnStreamRPC(ctx, sessionID, input, attachments, turnID, imagePaths)
	if err != nil {
		if newID := s.ensureSessionForTurnRPC(sessionID); newID != "" {
			sessionID = newID
			stream, turn, err = s.callStartTurnStreamRPC(ctx, sessionID, input, attachments, turnID, imagePaths)
		}
	}
	if err != nil {
		return conversationStreamHandle{}, err
	}
	handle := conversationGatewayTurnHandle(s.runtimeGatewayClient(), sessionID, stream, strings.TrimSpace(turn.ID))
	handle.SessionID = sessionID
	return handle, nil
}

// resolveOrCreateSessionRPC resolves the current session for the active
// workspace, creating a new one only when the core reports no current session
// (the normal first-turn path). A current-session query *error* is not masked
// by silently creating a fresh session — that would discard the user's real
// conversation history and duplicate sessions. Instead the error is logged and
// an empty id is returned so the caller surfaces "请求失败" to the user.
// Returns empty string on failure.
func (s *BridgeService) resolveOrCreateSessionRPC() string {
	gateway := s.runtimeGatewayClient()
	if gateway == nil {
		return ""
	}
	workspace := strings.TrimSpace(s.activeWorkspace)
	if workspace == "" {
		return ""
	}
	// Distinguish a query error (core unreachable / workspace mismatch) from a
	// successful "no current session" — only the latter justifies creating one.
	meta, err := gateway.CoreCurrentSessionRPC(coreCtx(), workspace)
	if err != nil {
		slog.Warn("bridge.core_rpc.read_failed", "domain", "current-session", "workspace", workspace, "error", err)
		return ""
	}
	if id := strings.TrimSpace(meta.ID); id != "" {
		return id
	}
	// No current session for this workspace — create one (normal first-turn path).
	meta, err = gateway.CoreCreateSessionRPC(coreCtx(), workspace, "", "gui", nil)
	if err != nil {
		slog.Warn("bridge.core_rpc.write_failed", "domain", "create-session", "workspace", workspace, "error", err)
		return ""
	}
	return strings.TrimSpace(meta.ID)
}

func (s *BridgeService) callStartTurnStreamRPC(ctx context.Context, sessionID, input string, attachments []adapter.Attachment, turnID string, imagePaths []string) (<-chan adapter.Event, coreapi.Turn, error) {
	// Map the global execution_mode ("plan" / "auto") to the per-turn
	// collaboration_mode carried on turn/start. When
	// the user selects "plan", each turn carries Mode=Plan so the core injects
	// plan.md, parses <proposed_plan>, and gates request_user_input.
	req := coreapi.StartTurnRequest{
		SessionID:   sessionID,
		TurnID:      turnID,
		Input:       input,
		ImagePaths:  imagePaths,
		Attachments: adapter.CoreAPIAttachments(attachments),
	}
	if s.executionModeReadOnly() == "plan" {
		req.CollaborationMode = &coreapi.CollaborationMode{Mode: coreapi.ModePlan}
	}
	// 请求级记忆注入开关：壳层只透传，注入裁决在内核
	//（use_memory.unwrap_or(true) && 全局 use_memories）。
	useMemory := s.useMemoryInjectionReadOnly()
	req.UseMemory = &useMemory
	slog.Debug("bridge.core_rpc.turn_start", "use_memory", useMemory, "source", "app")

	requestStreamer, ok := s.runtimeGatewayClient().(bridgeRuntimeGatewayRequestTurnStreamer)
	if !ok {
		return nil, coreapi.Turn{}, errors.New("runtime gateway does not support turn/start request streaming")
	}
	return requestStreamer.CoreStartTurnStreamWithRequestRPC(ctx, req)
}

func (s *BridgeService) ensureSessionForTurnRPC(sessionID string) string {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" || s == nil {
		return ""
	}
	gateway := s.runtimeGatewayClient()
	if gateway == nil {
		return ""
	}
	s.stateMu.RLock()
	workspace := ""
	title := "New Chat"
	var messages []adapter.SessionMessage
	if session := s.sessions[sessionID]; session != nil {
		workspace = strings.TrimSpace(session.WorkspacePath)
		title = session.Title
		for _, item := range session.Messages {
			messages = append(messages, sessionMessagesFromChatMessage(item)...)
		}
	}
	s.stateMu.RUnlock()
	if workspace == "" {
		workspace = strings.TrimSpace(s.activeWorkspace)
	}
	if workspace == "" {
		return ""
	}
	if _, err := gateway.CoreResumeSessionRPC(coreCtx(), workspace, sessionID); err == nil {
		return sessionID
	} else {
		// resume failed (session archived / workspace mismatch / concurrent
		// delete). The adapter cannot distinguish these yet, so we keep the
		// degrade-to-create path to let this turn go out, but the swallowed
		// error is logged for observability.
		slog.Warn("bridge.core_rpc.read_failed", "domain", "resume-session", "session_id", sessionID, "workspace", workspace, "error", err)
	}
	meta, err := gateway.CoreCreateSessionRPC(coreCtx(), workspace, title, "gui", messages)
	if err != nil {
		return ""
	}
	newID := strings.TrimSpace(meta.ID)
	if newID == "" {
		return ""
	}
	if newID == sessionID {
		return newID
	}
	s.stateMu.Lock()
	s.rekeySessionLocked(sessionID, newID, s.sessions[sessionID])
	s.stateMu.Unlock()
	return newID
}

func conversationGatewayTurnHandle(gateway bridgeRuntimeGateway, sessionID string, stream <-chan adapter.Event, turnID string) conversationStreamHandle {
	handle := conversationStreamHandle{
		Events: stream,
		TurnID: strings.TrimSpace(turnID),
	}
	if gateway == nil || handle.TurnID == "" {
		return handle
	}
	sessionID = strings.TrimSpace(sessionID)
	turnID = handle.TurnID
	handle.Interrupt = func(ctx context.Context) error {
		if ctx == nil {
			ctx = coreCtx()
		}
		return gateway.CoreInterruptTurnRPC(ctx, sessionID, turnID)
	}
	return handle
}
