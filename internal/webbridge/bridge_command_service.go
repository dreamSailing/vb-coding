package webbridge

// Copyright (c) 2026 EOSAIOS
// SPDX-License-Identifier: EOS-NCL-1.1

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/eosaios/eos/pkg/coreapi"
)

type CommandService struct {
	bridge *BridgeService
}

func NewCommandService(bridge *BridgeService) *CommandService {
	return &CommandService{bridge: bridge}
}

func (s *BridgeService) commandService() *CommandService {
	if s == nil {
		return NewCommandService(nil)
	}
	if s.commandSvc == nil {
		s.commandSvc = NewCommandService(s)
	}
	return s.commandSvc
}

func (svc *CommandService) DefaultCommandPalette() []CommandAction {
	return []CommandAction{
		{ID: "cmd-page-sessions", Command: "page.open.sessions", Label: "Open Sessions", Description: "Switch to the session management page", Target: "sessions"},
		{ID: "cmd-page-models", Command: "page.open.models", Label: "Open Models", Description: "Switch to the connections and models page", Target: "models"},
		{ID: "cmd-page-rules", Command: "page.open.rules", Label: "Open Rules", Description: "Switch to the rules page", Target: "rules"},
		{ID: "cmd-tasks-clean", Command: "tasks.cleanup", Label: "Clean Tasks", Description: "Remove finished background tasks", Target: "tasks"},
	}
}

func (svc *CommandService) ResolvePrompt(promptID, decision, note string) (BootstrapState, error) {
	s := svc.bridge
	if s == nil {
		return BootstrapState{}, errors.New("bridge service is not available")
	}
	s.stateMu.Lock()
	promptID = strings.TrimSpace(promptID)
	prompt, ok := s.prompts[promptID]
	if !ok {
		// 该审批已被内核/旁路 resolved（切 never 模式 auto-approve、resolved 事件
		// 先删）：用户的放行意图已达成，幂等成功返回，不当错误抛给前端。
		// 消息状态行由 resolved 事件路径（handleConversationResolutionLocked）更新。
		s.stateMu.Unlock()
		return s.LoadBootstrap(), nil
	}
	resolvedText, resolvedLevel := s.resolvedStatusTextAndLevel(prompt, decision)
	session := s.settlePromptLocked(nil, prompt, resolvedText, resolvedLevel, "streaming")
	if session == nil {
		s.stateMu.Unlock()
		return s.LoadBootstrap(), nil
	}
	session.Running = true
	// 把消息流中的"等待确认…"占位更新为用户决策结果，避免决策后还显示旧状态。
	// 允许/拒绝后 turn 会恢复执行，因此消息整体保持 streaming；但这条审批状态行
	// 已经收尾，必须标记为 completed，避免前端继续按 waiting/streaming 脉冲显示。
	if err := s.persistSessionLocked(session); err != nil {
		s.pushNotificationLocked("Approval Save Failed", err.Error(), "danger")
		s.stateMu.Unlock()
		return s.LoadBootstrap(), err
	}
	s.pushNotificationLocked(s.resolvePromptNotificationTitle(prompt), prompt.Title, "info")
	// 会话作用域 emit：针对审批所在的 session 构建快照，避免全局 emit 在多会话时
	// 把 currentSessionID 解析到别的会话上，导致本次审批的 status 更新（等待确认→已允许）
	// 不被前端应用到当前查看的会话。
	s.emitShellUpdatedForSession(prompt.SessionID)
	s.stateMu.Unlock()

	if err := s.respondPromptRPC(prompt, decision, note); err != nil {
		slog.Warn("bridge.resolve_prompt_failed", "prompt_id", promptID, "error", err)
		s.stateMu.Lock()
		s.pushNotificationLocked("审批响应失败", err.Error(), "danger")
		// 回滚乐观落账：prompt 翻回 pending（s.prompts 逆索引 + ToolCall item 的
		// Approval.State + 状态行挂起文案），前端时间线内联卡与底部浮层随之重现，
		// 用户可以重试。若内核实际已落账（响应丢失场景），后续 resolved 事件会经
		// handleConversationResolutionLocked 再次收口，状态自愈。
		s.unsettlePromptLocked(session, prompt)
		// 失败留痕：追加一条 error 级别状态行（completed 终态，不脉冲），消息整体
		// 保持 waiting——prompt 仍挂起等待重试，不能把整条消息标 failed。
		s.setMessageStatusWithItemState(session, prompt.AssistantMessageID, "审批响应失败："+err.Error(), "error", "waiting", "completed")
		// 不动 session.Running：respond 没送达内核时轮次仍在等审批，Running 保持 true
		// 与内核实际状态一致；终态由看门狗/流错误路径负责。
		session.NeedsAttention = true
		if persistErr := s.persistSessionLocked(session); persistErr != nil {
			s.pushNotificationLocked("Approval Save Failed", persistErr.Error(), "danger")
		}
		s.stateMu.Unlock()
		s.emitShellUpdated()
		return s.LoadBootstrap(), err
	}
	return s.LoadBootstrap(), nil
}

func (svc *CommandService) KillTask(taskID string) (BootstrapState, error) {
	s := svc.bridge
	if s == nil {
		return BootstrapState{}, errors.New("bridge service is not available")
	}
	if err := s.killTaskRPC(taskID); err != nil {
		return s.LoadBootstrap(), err
	}
	s.stateMu.Lock()
	s.pushNotificationLocked("Task Stopped", taskID, "warning")
	s.emitShellUpdated()
	s.stateMu.Unlock()
	return s.LoadBootstrap(), nil
}

// resolvedStatusTextAndLevel 把用户决策 token 映射为消息流状态行的显示文本与级别。
// decision 是前端回传的 canonical token（= coreapi.ApprovalDecision wire 值），
// 与 approvalDecisionFromToken 的校验集合一致。允许类用 success（绿色），
// 拒绝/取消类用 warning（橙色）。AGENTS.md L109：用户可见文案用 i18n key。
func (s *BridgeService) resolvedStatusTextAndLevel(prompt *promptState, decision string) (string, string) {
	if prompt != nil && prompt.Source == "request-user-input" {
		return s.t("request_user_input.resolved.answered"), "info"
	}
	switch coreapi.ApprovalDecision(strings.TrimSpace(decision)) {
	case coreapi.ApprovalAccept:
		return s.t("approval.resolved.allowed"), "success"
	case coreapi.ApprovalAcceptForSession:
		return s.t("approval.resolved.allowed_session"), "success"
	case coreapi.ApprovalDecline:
		return s.t("approval.resolved.denied"), "warning"
	case coreapi.ApprovalCancel:
		return s.t("approval.resolved.cancelled"), "warning"
	default:
		return s.t("approval.resolved.default"), "info"
	}
}

func (s *BridgeService) resolvePromptNotificationTitle(prompt *promptState) string {
	if prompt != nil && prompt.Source == "request-user-input" {
		return s.t("request_user_input.notification.submitted")
	}
	return "Approval Submitted"
}

func (svc *CommandService) DismissNotification(notificationID string) BootstrapState {
	s := svc.bridge
	if s == nil {
		return BootstrapState{}
	}
	s.stateMu.Lock()
	notificationID = strings.TrimSpace(notificationID)
	next := s.notifications[:0]
	for _, item := range s.notifications {
		if item.ID == notificationID {
			continue
		}
		next = append(next, item)
	}
	s.notifications = append([]NotificationItem(nil), next...)
	s.emitShellUpdated()
	s.stateMu.Unlock()
	return s.LoadBootstrap()
}

func (svc *CommandService) RunCommandPalette(command string) (BootstrapState, error) {
	s := svc.bridge
	if s == nil {
		return BootstrapState{}, errors.New("bridge service is not available")
	}
	switch strings.ToLower(strings.TrimSpace(command)) {
	case "session.new":
		return s.chatService().CreateSession("")
	case "tasks.cleanup":
		count := s.cleanupTasksRPC()
		s.stateMu.Lock()
		s.pushNotificationLocked("Tasks Cleaned", fmt.Sprintf("Removed %d finished tasks", count), "info")
		s.emitShellUpdated()
		s.stateMu.Unlock()
		return s.LoadBootstrap(), nil
	case "clipboard.copy-diagnostics":
		report := s.buildDiagnosticsReport()
		s.stateMu.Lock()
		if s.writeClipboardText(report) {
			s.pushNotificationLocked("Diagnostics Copied", "Diagnostics summary copied to clipboard", "success")
		} else {
			s.pushNotificationLocked("Clipboard Unavailable", "Diagnostics summary could not be copied", "warning")
		}
		s.emitShellUpdated()
		s.stateMu.Unlock()
		return s.LoadBootstrap(), nil
	case "notifications.clear":
		s.stateMu.Lock()
		s.notifications = nil
		s.emitShellUpdated()
		s.stateMu.Unlock()
		return s.LoadBootstrap(), nil
	default:
		return s.LoadBootstrap(), nil
	}
}
