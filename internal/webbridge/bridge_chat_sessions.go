package webbridge

import (
	"context"
	"errors"
	"os"
	"strings"
	"time"
)

func (svc *ChatService) CreateSession(workspacePath string) (BootstrapState, error) {
	s := svc.bridge
	if s == nil {
		return BootstrapState{}, errors.New("bridge service is not available")
	}
	s.ensureWorkspaceAndSession()
	workspace, resolveErr := s.resolveCreateSessionWorkspaceWithError(workspacePath, WorkspaceActivationForeground)
	if resolveErr != nil {
		return s.LoadBootstrap(), resolveErr
	}
	if workspace == "" {
		return s.LoadBootstrap(), errors.New("default workspace initialization failed: workspace path is empty")
	}
	if err := s.activateWorkspaceRPC(workspace); err != nil {
		return s.LoadBootstrap(), err
	}
	s.syncSandboxModeForWorkspace(workspace)
	s.stateMu.Lock()
	_, err := s.createWorkspaceSessionLocked(workspace, "New Chat", SessionCreateNotifyUser)
	if err != nil {
		s.stateMu.Unlock()
		return s.LoadBootstrap(), err
	}
	s.emitShellUpdated()
	s.stateMu.Unlock()
	return s.LoadBootstrap(), nil
}

func (svc *ChatService) EnsureWorkspaceSession(workspacePath string) (BootstrapState, error) {
	s := svc.bridge
	if s == nil {
		return BootstrapState{}, errors.New("bridge service is not available")
	}
	s.ensureWorkspaceAndSession()
	workspace, resolveErr := s.resolveCreateSessionWorkspaceWithError(workspacePath, WorkspaceActivationForeground)
	if resolveErr != nil {
		return s.LoadBootstrap(), resolveErr
	}
	if workspace == "" {
		return s.LoadBootstrap(), errors.New("default workspace initialization failed: workspace path is empty")
	}
	if err := s.activateWorkspaceRPC(workspace); err != nil {
		return s.LoadBootstrap(), err
	}

	s.stateMu.Lock()
	s.activeWorkspace = workspace
	s.tryCoreRPC("remember-workspace", workspace, "", func() error {
		return s.rememberWorkspaceRPC(workspace, WorkspaceActivationForeground)
	})

	currentID, _ := s.restoreSessionsFromRuntimeLocked(workspace)
	session := s.ensureWorkspaceSessionLocked(workspace, currentID)
	if session == nil {
		var err error
		session, err = s.createWorkspaceSessionLocked(workspace, "New Chat", SessionCreateSilent)
		if err != nil {
			s.stateMu.Unlock()
			return s.LoadBootstrap(), err
		}
	}

	s.currentSessionID = session.ID
	sessionID := session.ID
	s.tryCoreRPC("resume-session", workspace, session.ID, func() error {
		return s.resumeWorkspaceSessionRPC(workspace, session.ID)
	})
	s.emitShellUpdated()
	s.stateMu.Unlock()

	// 会话确定后再按会话维度恢复沙箱模式：新建会话无 metadata 记录 → 默认 workspace；
	// 历史会话有 sandbox_mode 记录 → 用会话值（per-session 持久化）。
	// 必须在锁外调用：applySandboxModeSemantics 恢复 full_access 时收口待审卡需要拿 stateMu。
	s.syncSandboxModeForSession(workspace, sessionID)
	return s.LoadBootstrap(), nil
}

func (svc *ChatService) SelectSession(workspacePath, sessionID string) (BootstrapState, error) {
	s := svc.bridge
	if s == nil {
		return BootstrapState{}, errors.New("bridge service is not available")
	}
	session := s.findOrRestoreSession(strings.TrimSpace(workspacePath), strings.TrimSpace(sessionID))
	if session == nil {
		return s.LoadBootstrap(), os.ErrNotExist
	}
	if err := s.activateWorkspaceRPC(session.WorkspacePath); err != nil {
		return s.LoadBootstrap(), err
	}
	s.syncSandboxModeForSession(session.WorkspacePath, session.ID)
	s.stateMu.Lock()
	s.currentSessionID = session.ID
	s.activeWorkspace = session.WorkspacePath
	targetSessionID := session.ID
	targetWorkspace := session.WorkspacePath
	s.stateMu.Unlock()
	go func() {
		s.tryCoreRPC("remember-workspace", targetWorkspace, "", func() error {
			return s.rememberWorkspaceRPC(targetWorkspace, WorkspaceActivationForeground)
		})
		s.tryCoreRPC("resume-session", targetWorkspace, targetSessionID, func() error {
			return s.resumeWorkspaceSessionRPC(targetWorkspace, targetSessionID)
		})
	}()
	response := s.bootstrapForSession(targetSessionID)
	s.emitShellUpdatedForSession(targetSessionID)
	return response, nil
}

func (svc *ChatService) RenameSession(sessionID, title string) (BootstrapState, error) {
	s := svc.bridge
	if s == nil {
		return BootstrapState{}, errors.New("bridge service is not available")
	}
	session := s.findOrRestoreSession("", sessionID)
	if session == nil {
		return s.LoadBootstrap(), os.ErrNotExist
	}
	s.stateMu.Lock()
	title = strings.TrimSpace(title)
	if title == "" {
		title = "Untitled Chat"
	}
	if err := s.withSessionLocked(session.ID, func(session *sessionState) error {
		session.Title = title
		session.UpdatedAt = time.Now()
		return s.persistSessionOrNotifyLocked(session, "Session Rename Failed")
	}); err != nil {
		s.stateMu.Unlock()
		return s.LoadBootstrap(), err
	}
	s.pushNotificationLocked("Session Renamed", title, "info")
	s.emitShellUpdated()
	s.stateMu.Unlock()
	return s.LoadBootstrap(), nil
}

func (svc *ChatService) DeleteSession(workspacePath, sessionID string) (BootstrapState, error) {
	s := svc.bridge
	if s == nil {
		return BootstrapState{}, errors.New("bridge service is not available")
	}
	s.stateMu.Lock()
	workspacePath = strings.TrimSpace(workspacePath)
	sessionID = strings.TrimSpace(sessionID)
	session, ok := s.sessions[sessionID]
	if !ok || (workspacePath != "" && !sameWorkspacePath(session.WorkspacePath, workspacePath)) {
		s.stateMu.Unlock()
		session = s.findOrRestoreSession(workspacePath, sessionID)
		if session == nil {
			return s.LoadBootstrap(), os.ErrNotExist
		}
		s.stateMu.Lock()
	}
	deleteErr := s.deleteWorkspaceSessionRPC(session.WorkspacePath, sessionID)
	if deleteErr != nil && !errors.Is(deleteErr, os.ErrNotExist) {
		s.stateMu.Unlock()
		return s.LoadBootstrap(), deleteErr
	}
	delete(s.sessions, sessionID)
	if s.currentSessionID == sessionID {
		s.currentSessionID = ""
		if next := s.latestSessionForWorkspaceLocked(session.WorkspacePath); next != nil {
			s.currentSessionID = next.ID
			s.activeWorkspace = next.WorkspacePath
		}
	}
	s.pushNotificationLocked("Session Deleted", session.Title, "warning")
	if s.currentSessionID == "" {
		if next := s.latestSessionForWorkspaceLocked(""); next != nil {
			s.currentSessionID = next.ID
			s.activeWorkspace = next.WorkspacePath
		}
	}
	if s.currentSessionID != "" {
		s.tryCoreRPC("activate-workspace", s.activeWorkspace, "", func() error {
			return s.activateWorkspaceRPC(s.activeWorkspace)
		})
		s.tryCoreRPC("set-current-session", s.activeWorkspace, s.currentSessionID, func() error {
			return s.setWorkspaceCurrentSessionRPC(s.activeWorkspace, s.currentSessionID)
		})
	} else {
		s.tryCoreRPC("set-current-session", s.activeWorkspace, "", func() error {
			return s.setWorkspaceCurrentSessionRPC(s.activeWorkspace, "")
		})
	}
	s.ensureActiveSessionLocked("")
	s.emitShellUpdated()
	s.stateMu.Unlock()
	return s.LoadBootstrap(), nil
}

func (svc *ChatService) ArchiveSession(sessionID string, archived bool) (BootstrapState, error) {
	s := svc.bridge
	if s == nil {
		return BootstrapState{}, errors.New("bridge service is not available")
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return s.LoadBootstrap(), errors.New("session id is required")
	}
	if err := s.archiveSessionRPC(sessionID, archived); err != nil {
		return s.LoadBootstrap(), err
	}
	s.stateMu.Lock()
	delete(s.sessions, sessionID)
	if archived && s.currentSessionID == sessionID {
		s.currentSessionID = ""
		if next := s.latestSessionForWorkspaceLocked(""); next != nil {
			s.currentSessionID = next.ID
			s.activeWorkspace = next.WorkspacePath
		}
		if s.currentSessionID != "" {
			s.tryCoreRPC("activate-workspace", s.activeWorkspace, "", func() error {
				return s.activateWorkspaceRPC(s.activeWorkspace)
			})
			s.tryCoreRPC("set-current-session", s.activeWorkspace, s.currentSessionID, func() error {
				return s.setWorkspaceCurrentSessionRPC(s.activeWorkspace, s.currentSessionID)
			})
		}
	}
	if archived {
		s.pushNotificationLocked("Session Archived", sessionID, "info")
	} else {
		s.pushNotificationLocked("Session Restored", sessionID, "info")
	}
	s.ensureActiveSessionLocked("")
	s.emitShellUpdated()
	s.stateMu.Unlock()
	return s.LoadBootstrap(), nil
}

func (svc *ChatService) PredictNextUserMessage(draft string) (string, error) {
	s := svc.bridge
	if s == nil {
		return "", errors.New("bridge service is not available")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	return s.predictNextUserMessageRPC(ctx, draft)
}

func (svc *ChatService) RefineInput(draft string) (string, error) {
	s := svc.bridge
	if s == nil {
		return "", errors.New("bridge service is not available")
	}
	// 内核侧模型调用超时 20s，壳层留足余量。
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return s.refineInputRPC(ctx, draft)
}
