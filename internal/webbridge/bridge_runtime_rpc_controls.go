package webbridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/eosaios/eos/internal/webbridge/adapter"
	"github.com/eosaios/eos/pkg/coreapi"
)

func (s *BridgeService) runBashRPC(ctx context.Context, command string) (<-chan adapter.Event, error) {
	if s == nil || s.runtimeGatewayClient() == nil {
		return nil, errors.New("runtime core unavailable")
	}
	if ctx == nil {
		ctx = coreCtx()
	}
	command = strings.TrimSpace(command)
	if stream, err := s.runtimeGatewayClient().CoreRunBashStreamRPC(ctx, command); err == nil {
		return stream, nil
	}
	return s.runtimeGatewayClient().RunBash(ctx, command)
}

func (s *BridgeService) killTaskRPC(taskID string) error {
	taskID = strings.TrimSpace(taskID)
	return coreErrOrRequire(
		s,
		func(g bridgeRuntimeGateway) error { return g.CoreKillTaskRPC(coreCtx(), taskID) },
	)
}

func (s *BridgeService) cleanupTasksRPC() int {
	return coreValueOrNil(
		s,
		0,
		func(g bridgeRuntimeGateway) (int, error) { return g.CoreCleanupTasksRPC(coreCtx()) },
	)
}

func (s *BridgeService) listTasksRPC() ([]coreapi.TaskSnapshot, error) {
	return coreValueOrRequire(
		s,
		func(g bridgeRuntimeGateway) ([]coreapi.TaskSnapshot, error) {
			return g.CoreTaskListRPC(coreCtx())
		},
	)
}

func (s *BridgeService) listPendingApprovalsRPC(sessionID string) ([]coreapi.PendingApprovalItem, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, errors.New("session id required")
	}
	list, err := coreValueOrRequire(
		s,
		func(g bridgeRuntimeGateway) (coreapi.PendingApprovalList, error) {
			return g.CoreApprovalListRPC(coreCtx(), coreapi.PendingApprovalListRequest{SessionID: sessionID})
		},
	)
	if err != nil {
		return nil, err
	}
	return append([]coreapi.PendingApprovalItem(nil), list.Approvals...), nil
}

func (s *BridgeService) respondApprovalDecisionRPC(approvalID string, decision coreapi.ApprovalDecision) error {
	approvalID = strings.TrimSpace(approvalID)
	if approvalID == "" {
		return errors.New("approval id required")
	}
	return coreErrOrRequire(s, func(g bridgeRuntimeGateway) error {
		return g.CoreRespondApprovalRPC(coreCtx(), approvalID, decision)
	})
}

// approvalDecisionFromToken maps the canonical decision token (echoed by the
// frontend from the option's `token` field) to the typed ApprovalDecision.
// Token values ARE the coreapi.ApprovalDecision wire values, so this is a
// validated cast, not a translation: unknown tokens are rejected fail-fast
// instead of silently reinterpreted (AGENTS.md 原则 1).
func approvalDecisionFromToken(token string) (coreapi.ApprovalDecision, error) {
	switch coreapi.ApprovalDecision(strings.TrimSpace(token)) {
	case coreapi.ApprovalAccept:
		return coreapi.ApprovalAccept, nil
	case coreapi.ApprovalAcceptForSession:
		return coreapi.ApprovalAcceptForSession, nil
	case coreapi.ApprovalDecline:
		return coreapi.ApprovalDecline, nil
	case coreapi.ApprovalCancel:
		return coreapi.ApprovalCancel, nil
	default:
		return "", fmt.Errorf("unknown approval decision token: %q", token)
	}
}

func (s *BridgeService) respondPromptRPC(prompt *promptState, decision, note string) error {
	gateway := runtimeGatewayOrNil(s)
	if gateway == nil || prompt == nil {
		return errors.New("runtime gateway is not available")
	}
	promptID := strings.TrimSpace(prompt.ID)
	sessionID := strings.TrimSpace(prompt.SessionID)
	if promptID == "" || sessionID == "" {
		return errors.New("prompt id or session id is empty")
	}
	decision = strings.TrimSpace(decision)
	workspace := strings.TrimSpace(prompt.WorkspacePath)
	if workspace == "" {
		if resolvedWorkspace, err := gateway.ResolveSessionWorkspace(sessionID); err == nil {
			workspace = strings.TrimSpace(resolvedWorkspace)
		}
	}
	if workspace != "" {
		if err := s.activateWorkspaceRPC(workspace); err != nil {
			slog.Warn("bridge.core_rpc.write_failed", "domain", "activate-workspace", "workspace", workspace, "error", err)
		}
		if err := s.setWorkspaceCurrentSessionRPC(workspace, sessionID); err != nil {
			slog.Warn("bridge.core_rpc.write_failed", "domain", "set-current-session", "workspace", workspace, "session_id", sessionID, "error", err)
		}
	}
	if prompt.Source == "request-user-input" {
		// request_user_input resolution: answers are fed back via
		// approval/respond with decision=accept and
		// reason=JSON(RequestUserInputResponse). New frontends send the full
		// answer map in note; old callers still pass a single selected label via
		// decision and fall back to the first question.
		reasonJSON, err := requestUserInputReasonJSON(prompt, decision, note)
		if err != nil {
			return fmt.Errorf("marshal request_user_input answers: %w", err)
		}
		if err := gateway.CoreRespondApprovalWithReasonRPC(coreCtx(), promptID, coreapi.ApprovalAccept, reasonJSON); err != nil {
			return fmt.Errorf("respond request_user_input: %w", err)
		}
		return nil
	}
	approvalDecision, err := approvalDecisionFromToken(decision)
	if err != nil {
		return err
	}
	if err := gateway.CoreRespondApprovalRPC(coreCtx(), promptID, approvalDecision); err != nil {
		return fmt.Errorf("respond approval: %w", err)
	}
	return nil
}

func (s *BridgeService) subscribeRuntimeEventsRPC(ctx context.Context, sessionID, turnID, agentID string, buffer int) (<-chan adapter.Event, func(), error) {
	gateway, err := requireRuntimeGateway(s)
	if err != nil {
		return nil, nil, err
	}
	if ctx == nil {
		ctx = coreCtx()
	}
	return gateway.CoreSubscribeEventsRPC(ctx, sessionID, turnID, agentID, buffer)
}

// firstQuestionID returns the id of the first question on a request_user_input
// PromptCard, falling back to the call_id. Used to key the answer map in the
// RequestUserInputResponse sent back to core.
func firstQuestionID(prompt *promptState) string {
	if len(prompt.Questions) > 0 && prompt.Questions[0].ID != "" {
		return prompt.Questions[0].ID
	}
	return prompt.CallID
}

func requestUserInputReasonJSON(prompt *promptState, decision, note string) (string, error) {
	note = strings.TrimSpace(note)
	if note != "" {
		var payload coreapi.RequestUserInputResponse
		if err := json.Unmarshal([]byte(note), &payload); err != nil {
			return "", fmt.Errorf("invalid request_user_input response JSON: %w", err)
		}
		if len(payload.Answers) == 0 {
			return "", errors.New("request_user_input answers are empty")
		}
		return note, nil
	}
	answers := coreapi.RequestUserInputResponse{
		Answers: map[string]coreapi.RequestUserInputAnswer{
			// 旧单题路径：把选中的 option 映射到首题 id（或 call_id）。
			firstQuestionID(prompt): {Answers: []string{strings.TrimSpace(decision)}},
		},
	}
	reasonJSON, err := json.Marshal(answers)
	if err != nil {
		return "", err
	}
	return string(reasonJSON), nil
}
