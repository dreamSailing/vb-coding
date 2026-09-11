package webbridge

// Copyright (c) 2026 EOSAIOS
// SPDX-License-Identifier: EOS-NCL-1.1

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"
)

func (svc *ChatService) SendChat(sessionID, workspace, input string, attachments []string) (BootstrapState, error) {
	return svc.SendChatWithReasoning(sessionID, workspace, input, attachments, "")
}

func (svc *ChatService) SendChatWithReasoning(sessionID, workspace, input string, attachments []string, reasoningLevel string) (BootstrapState, error) {
	s := svc.bridge
	if s == nil {
		return BootstrapState{}, errors.New("bridge service is not available")
	}
	trimmed := strings.TrimSpace(input)
	if trimmed == "" && len(compactPaths(attachments)) == 0 {
		return s.LoadBootstrap(), errors.New("message content is required")
	}

	if level := strings.ToLower(strings.TrimSpace(reasoningLevel)); level != "" {
		if err := s.setReasoningLevelRPC(level); err != nil {
			return s.LoadBootstrap(), err
		}
	}

	s.ensureWorkspaceAndSession()

	s.stateMu.Lock()
	session, resolveErr := s.resolveSendSessionLocked(sessionID, workspace)
	if session == nil {
		s.stateMu.Unlock()
		if resolveErr != nil {
			return s.LoadBootstrap(), resolveErr
		}
		return s.LoadBootstrap(), errors.New("default workspace initialization failed: unable to create session")
	}
	if session.Running {
		s.stateMu.Unlock()
		return s.LoadBootstrap(), errors.New("current session is still processing")
	}
	workspace = strings.TrimSpace(session.WorkspacePath)
	if workspace == "" {
		if resolvedWorkspace := s.resolveSessionWorkspaceReadOnly(session.ID); resolvedWorkspace != "" {
			workspace = resolvedWorkspace
			session.WorkspacePath = workspace
		}
	}
	attachmentRefs, runtimeAttachments, attachmentErr := s.attachmentService().PrepareForSession(attachments, workspace)
	if attachmentErr != nil {
		s.stateMu.Unlock()
		return s.LoadBootstrap(), attachmentErr
	}
	runtimeInput := trimmed
	if runtimeInput == "" && len(attachmentRefs) > 0 {
		runtimeInput = "Read and process the attachments."
	}
	assistantID := s.prepareOutgoingMessagesLocked(session, runtimeInput, attachmentRefs)
	s.activateWorkspaceLocked(workspace, session.ID)
	if err := s.persistSessionLocked(session); err != nil {
		session.Running = false
		session.NeedsAttention = true
		session.UpdatedAt = time.Now()
		s.setMessageStatus(session, assistantID, requestFailureMessage(err.Error()), "error", "failed")
		s.pushNotificationLocked("Request Failed", err.Error(), "danger")
		s.stateMu.Unlock()
		response := s.bootstrapForSession(session.ID)
		s.emitShellUpdatedForSession(session.ID)
		return response, nil
	}
	s.pushNotificationLocked("Chat Sent", "Request entered the Wails backend bridge.", "info")
	// AGENTS.md §3：壳层不做业务裁决。原 needsInquiry 用硬编码中文关键词在壳层拦截
	// turn（违反规范）的逻辑已删除——所有审批/问询由内核裁决，壳层只渲染内核事件。
	ctx, cancel := context.WithCancel(context.Background())
	s.runningConversations[session.ID] = &runningConversationState{
		AssistantMessageID: assistantID,
		WorkspacePath:      workspace,
		Cancel:             cancel,
	}
	targetSessionID := session.ID
	s.stateMu.Unlock()

	response := s.bootstrapForSession(targetSessionID)
	s.emitShellUpdatedForSession(targetSessionID)
	s.conversationWG.Add(1)
	go s.runConversation(ctx, targetSessionID, assistantID, runtimeInput, runtimeAttachments, false)
	return response, nil
}

// ResumeFailedTurn 续跑当前会话最后一个失败的 turn（resume 语义）。
// 不追加用户消息：内核按已提交历史重建请求续写，GUI 只追加一个 assistant 占位
// 消息承接续写输出。取代旧的「把最后一条用户输入塞回输入框重发」重试路径。
func (svc *ChatService) ResumeFailedTurn(sessionID string) (BootstrapState, error) {
	s := svc.bridge
	if s == nil {
		return BootstrapState{}, errors.New("bridge service is not available")
	}
	s.ensureWorkspaceAndSession()

	s.stateMu.Lock()
	session, resolveErr := s.resolveSendSessionLocked(sessionID, "")
	if session == nil {
		s.stateMu.Unlock()
		if resolveErr != nil {
			return s.LoadBootstrap(), resolveErr
		}
		return s.LoadBootstrap(), errors.New("default workspace initialization failed: unable to create session")
	}
	if session.Running {
		s.stateMu.Unlock()
		return s.LoadBootstrap(), errors.New("current session is still processing")
	}
	assistantID := s.prepareResumeMessagesLocked(session)
	if err := s.persistSessionLocked(session); err != nil {
		session.Running = false
		session.NeedsAttention = true
		session.UpdatedAt = time.Now()
		s.setMessageStatus(session, assistantID, requestFailureMessage(err.Error()), "error", "failed")
		s.pushNotificationLocked("请求失败", err.Error(), "danger")
		s.stateMu.Unlock()
		response := s.bootstrapForSession(session.ID)
		s.emitShellUpdatedForSession(session.ID)
		return response, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.runningConversations[session.ID] = &runningConversationState{
		AssistantMessageID: assistantID,
		WorkspacePath:      strings.TrimSpace(session.WorkspacePath),
		Cancel:             cancel,
	}
	targetSessionID := session.ID
	s.stateMu.Unlock()

	response := s.bootstrapForSession(targetSessionID)
	s.emitShellUpdatedForSession(targetSessionID)
	s.conversationWG.Add(1)
	go s.runConversation(ctx, targetSessionID, assistantID, "", nil, true)
	return response, nil
}

// prepareResumeMessagesLocked appends only an assistant placeholder (no user
// message — the original request is already in the kernel's committed history),
// flips the session to running, and records it as the current session. Caller
// must hold stateMu. Returns the assistant message id.
func (s *BridgeService) prepareResumeMessagesLocked(session *sessionState) string {
	now := time.Now()
	nowText := now.Format(time.RFC3339)
	assistantID := newID("assistant")
	assistant := ChatMessage{
		ID:            assistantID,
		Role:          "assistant",
		Content:       "思考中",
		State:         "streaming",
		CreatedAt:     nowText,
		UpdatedAt:     nowText,
		IsPlaceholder: true,
	}
	session.Messages = append(session.Messages, assistant)
	session.Running = true
	session.NeedsAttention = false
	session.UpdatedAt = now
	s.currentSessionID = session.ID
	return assistantID
}

// prepareOutgoingMessagesLocked appends the user message plus a placeholder
// assistant message to the session, flips it to running, and records it as the
// current session. Caller must hold stateMu. Returns the assistant message id
// so the caller can drive persistence, inquiry, and the turn stream.
func (s *BridgeService) prepareOutgoingMessagesLocked(session *sessionState, runtimeInput string, attachmentRefs []AttachmentRef) string {
	now := time.Now()
	nowText := now.Format(time.RFC3339)
	if session != nil && isAutoSessionPlaceholderTitle(session.Title) {
		session.Title = autoSessionTitle(session, runtimeInput)
	}
	userMessage := ChatMessage{
		ID:          newID("user"),
		Role:        "user",
		Content:     runtimeInput,
		State:       "completed",
		CreatedAt:   nowText,
		UpdatedAt:   nowText,
		Attachments: attachmentRefs,
	}
	assistantID := newID("assistant")
	assistant := ChatMessage{
		ID:            assistantID,
		Role:          "assistant",
		Content:       "思考中",
		State:         "streaming",
		CreatedAt:     nowText,
		UpdatedAt:     nowText,
		IsPlaceholder: true,
	}
	session.Messages = append(session.Messages, userMessage, assistant)
	session.Running = true
	session.NeedsAttention = false
	session.UpdatedAt = now
	s.currentSessionID = session.ID
	return assistantID
}

// activateWorkspaceLocked activates the workspace through the runtime core.
// Caller must hold stateMu; the three RPCs only touch the gateway (not
// stateMu), so calling them under the lock is safe. Their errors are logged
// rather than surfaced: the chat turn should still proceed when workspace
// bookkeeping is degraded, but the failure must stay observable.
//
// The Rust core now captures baseline fingerprints + snapshots at turn start
// and builds changeSet + rollback itself, so the Go shell no longer needs to.
func (s *BridgeService) activateWorkspaceLocked(workspace, sessionID string) {
	if strings.TrimSpace(workspace) == "" {
		return
	}
	s.activeWorkspace = workspace
	if err := s.activateWorkspaceRPC(workspace); err != nil {
		slog.Warn("bridge.core_rpc.write_failed", "domain", "activate-workspace", "workspace", workspace, "error", err)
	}
	if err := s.rememberWorkspaceRPC(workspace, WorkspaceActivationForeground); err != nil {
		slog.Warn("bridge.core_rpc.write_failed", "domain", "remember-workspace", "workspace", workspace, "error", err)
	}
	if err := s.setWorkspaceCurrentSessionRPC(workspace, sessionID); err != nil {
		slog.Warn("bridge.core_rpc.write_failed", "domain", "set-current-session", "workspace", workspace, "session_id", sessionID, "error", err)
	}
}
