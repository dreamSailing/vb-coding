package ui

// app_confirm.go — 确认框 / 内联权限审批 / 操作弹框 / 计划下载。
//
// 本文件包含：
//   - 内联权限确认框：showInlinePermission / clearInlinePermission /
//     updateInlinePermissionUI / buildInlinePermissionResult / handleInlinePermissionKey
//   - 通用确认框：openConfirm
//   - 操作弹框（点击消息文本后弹出复制/下载）：openActionPopup / actionLabel /
//     handleActionResult
//   - 计划下载：handlePlanDownloadAction / planDownloadEntry /
//     savePlanHistoryEntryToDir / nextPlanDownloadFileName /
//     sanitizePlanFileNameSegment / uniqueAvailablePath
//   - 可点击消息命中类型 bubbleActionHit、planDownloadRequest 类型与
//     choosePlanDownloadDirectory / writePlanDownloadFile / planDownloadNow 变量
//   - Update 中 PromptRequestMsg / confirm.ResultMsg / confirm.ActionResultMsg 三个分支
//     的处理方法（handlePromptRequestMsg / handleConfirmResultMsg / handleActionResultMsg）
//
// 代码原位于 app.go，仅做物理拆分，不改行为。

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/eosaios/eos/internal/i18n"
	"github.com/eosaios/eos/internal/pkg/filedialog"
	"github.com/eosaios/eos/internal/ui/adapter"
	"github.com/eosaios/eos/internal/ui/panels"
	"github.com/eosaios/eos/internal/ui/views/confirm"

	tea "charm.land/bubbletea/v2"
	"github.com/atotto/clipboard"
)

// bubbleActionHit 记录一条可点击消息文本在内容区中的行范围，
// 用于点击 AI/子 Agent 回复文本时弹出操作选择框（复制/下载）。
type bubbleActionHit struct {
	y       int      // 消息起始行号
	lines   int      // 消息占用的行数（含空行）
	idx     int      // 对应历史记录条目的索引
	actions []string // 该条目可用动作（如 "copy"、"download"）
	text    string   // 待复制/下载的文本内容
}

// hasAction 报告该命中区是否提供指定动作。
func (r bubbleActionHit) hasAction(action string) bool {
	for _, a := range r.actions {
		if strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(action)) {
			return true
		}
	}
	return false
}

// planDownloadRequest 记录待下载的计划文件信息
type planDownloadRequest struct {
	HistoryIndex int // 历史记录中该计划的索引
}

var choosePlanDownloadDirectory = filedialog.ChooseDirectory
var writePlanDownloadFile = os.WriteFile
var planDownloadNow = time.Now

// updateInlinePermissionUI 更新内联权限确认框的显示
func (m *AppModel) updateInlinePermissionUI() {
	if m == nil || m.shell == nil {
		return
	}
	if m.inlinePermissionReq == nil {
		m.shell.ClearPromptOverlay()
		return
	}
	m.shell.SetPromptOverlay(confirm.RenderInlinePermission(
		m.styles,
		m.state.Language,
		m.diffHighlightTheme(),
		*m.inlinePermissionReq,
		m.inlinePermissionSelected,
		m.width,
	))
}

// showInlinePermission 显示内联权限确认框，用于 AI 请求执行敏感操作时的用户确认
func (m *AppModel) showInlinePermission(req confirm.Request) {
	reqCopy := req
	m.inlinePermissionReq = &reqCopy
	m.inlinePermissionSelected = 0
	m.updateInlinePermissionUI()
	if m.shell != nil {
		m.shell.BlurInput()
	}
}

// clearInlinePermission 清除内联权限确认框状态
func (m *AppModel) clearInlinePermission() {
	m.inlinePermissionReq = nil
	m.inlinePermissionSelected = 0
	if m.shell != nil {
		m.shell.ClearPromptOverlay()
	}
}

func (m *AppModel) buildInlinePermissionResult(decision string) confirm.ResultMsg {
	if m.inlinePermissionReq == nil {
		return confirm.ResultMsg{Kind: "permission", Decision: decision, OptionIndex: -1}
	}
	req := m.inlinePermissionReq
	option := ""
	idx := m.inlinePermissionSelected
	if idx >= 0 && idx < len(req.Options) {
		option = req.Options[idx]
	}
	// Option keys are canonical decision values; the selected option IS the
	// decision. Esc passes "decline" explicitly.
	if decision == "" {
		decision = option
		if decision == "" {
			decision = "decline"
		}
	}
	return confirm.ResultMsg{
		ID:          req.ID,
		Kind:        req.Kind,
		Decision:    decision,
		Option:      option,
		OptionIndex: idx,
	}
}

func (m *AppModel) handleInlinePermissionKey(msg tea.KeyMsg) (bool, tea.Cmd) {
	if m == nil || m.inlinePermissionReq == nil {
		return false, nil
	}
	switch msg.String() {
	case "up":
		if m.inlinePermissionSelected > 0 {
			m.inlinePermissionSelected--
			m.updateInlinePermissionUI()
		}
		return true, nil
	case "down":
		if m.inlinePermissionSelected < len(m.inlinePermissionReq.Options)-1 {
			m.inlinePermissionSelected++
			m.updateInlinePermissionUI()
		}
		return true, nil
	case "enter":
		result := m.buildInlinePermissionResult("")
		return true, func() tea.Msg { return result }
	case "esc":
		// Esc 决策不硬编码 "decline"（旧注释明写 "Esc passes decline explicitly"，
		// 是壳层凭 permission 类型推断）。改为基于 options 推断，与 eos-app 一致。
		// 直接用 EscDecision 的 decision + idx 构造 ResultMsg，确保 Decision / Option /
		// OptionIndex 三者一致（不走 buildInlinePermissionResult 的光标逻辑，光标位置
		// 在 esc 时不代表用户选择）。
		decision, idx := confirm.EscDecision(m.inlinePermissionReq.Options)
		option := ""
		if idx >= 0 && idx < len(m.inlinePermissionReq.Options) {
			option = m.inlinePermissionReq.Options[idx]
		}
		result := confirm.ResultMsg{
			ID:          m.inlinePermissionReq.ID,
			Kind:        m.inlinePermissionReq.Kind,
			Decision:    decision,
			Option:      option,
			OptionIndex: idx,
		}
		return true, func() tea.Msg { return result }
	default:
		if len(msg.String()) == 1 {
			k := msg.String()[0]
			if k >= '1' && k <= '9' {
				idx := int(k - '1')
				if idx >= 0 && idx < len(m.inlinePermissionReq.Options) {
					m.inlinePermissionSelected = idx
					m.updateInlinePermissionUI()
					return true, nil
				}
			}
		}
	}
	return false, nil
}

func (m *AppModel) openConfirm(req confirm.Request) {
	if m.confirmView == nil {
		m.prevView = m.activeView
	}
	m.clearPrediction()
	m.confirmView = confirm.New(m.styles, m.state.Language, m.diffHighlightTheme(), req)
	m.confirmView.SetSize(m.width, m.height)
	m.activeView = "confirm"
	m.shell.BlurInput()
}

// tryHandleBubbleActionAt 尝试处理指定坐标处的消息点击。
// 命中可点击消息文本时弹出操作选择框；未命中返回 nil。
func (m *AppModel) tryHandleBubbleActionAt(x, y int) tea.Cmd {
	if m.actionPopup != nil {
		return nil
	}
	ox, oy := m.shell.ContentOrigin()
	if x < ox || y < oy {
		return nil
	}
	ly := y - oy
	if ly < 0 || ly >= m.shell.ContentHeight() {
		return nil
	}
	line := m.shell.ContentYOffset() + ly
	for _, h := range m.actionHits {
		if line < h.y || line >= h.y+h.lines {
			continue
		}
		m.openActionPopup(h)
		return nil
	}
	return nil
}

// openActionPopup 根据命中区构造操作选择弹框。
func (m *AppModel) openActionPopup(h bubbleActionHit) {
	items := make([]confirm.ActionItem, 0, len(h.actions))
	for _, kind := range h.actions {
		items = append(items, confirm.ActionItem{
			Kind:  kind,
			Label: m.actionLabel(kind),
		})
	}
	m.actionPopup = confirm.NewActionPopup(m.styles, m.state.Language, confirm.ActionRequest{
		Actions: items,
		Payload: h.text,
		Index:   h.idx,
	})
	m.actionPopup.SetSize(m.width, m.height)
	m.shell.BlurInput()
}

// actionLabel 返回动作在弹框中的展示文案。
func (m *AppModel) actionLabel(kind string) string {
	switch strings.TrimSpace(strings.ToLower(kind)) {
	case "copy":
		return i18n.T("op.copy", m.state.Language)
	case "download":
		return i18n.T("op.download", m.state.Language)
	default:
		return kind
	}
}

// handleActionResult 处理操作弹框的结果：执行复制/下载，并关闭弹框。
func (m *AppModel) handleActionResult(msg confirm.ActionResultMsg) tea.Cmd {
	idx := msg.Index
	closePopup := func() {
		m.actionPopup = nil
		if m.activeView == "shell" {
			m.shell.FocusInput()
		}
	}

	switch strings.TrimSpace(strings.ToLower(msg.Kind)) {
	case "cancel":
		closePopup()
		return nil
	case "copy":
		closePopup()
		if err := clipboard.WriteAll(msg.Payload); err != nil {
			m.appendSystem(i18n.T("tool.error.copy_error", m.state.Language, err), "error")
			return func() tea.Msg { return nil }
		}
		if idx >= 0 && idx < len(m.history) {
			m.history[idx].copiedAt = time.Now()
		}
		m.rebuildHistoryContent()
		m.appendSystem(i18n.T("clipboard.copied", m.state.Language), "success")
		return tea.Tick(1600*time.Millisecond, func(time.Time) tea.Msg { return clearCopiedMsg{idx: idx} })
	case "download":
		closePopup()
		return m.handlePlanDownloadAction(idx)
	default:
		closePopup()
		return nil
	}
}

// handlePlanDownloadAction 处理计划文件下载操作
// 尝试打开目录选择器，如果不可用则回退到文本输入确认框
func (m *AppModel) handlePlanDownloadAction(idx int) tea.Cmd {
	if _, ok := m.planDownloadEntry(idx); !ok {
		m.appendSystem(i18n.T("plan.download.unavailable", m.state.Language), "warning")
		return func() tea.Msg { return nil }
	}
	dir, err := choosePlanDownloadDirectory(i18n.T("plan.download.chooser.title", m.state.Language))
	switch {
	case err == nil:
		path, saveErr := m.savePlanHistoryEntryToDir(idx, dir)
		if saveErr != nil {
			m.appendSystem(saveErr.Error(), "error")
		} else {
			m.appendSystem(fmt.Sprintf(i18n.T("plan.download.saved", m.state.Language), path), "success")
		}
	case filedialog.IsCanceled(err):
		return func() tea.Msg { return nil }
	case filedialog.IsUnavailable(err):
		// 目录选择器不可用，回退到文本输入
		m.pendingPlanDownload = &planDownloadRequest{HistoryIndex: idx}
		m.openConfirm(confirm.Request{
			Kind:      "plan_download_path",
			Title:     i18n.T("plan.download.fallback.title", m.state.Language),
			Question:  i18n.T("plan.download.fallback.question", m.state.Language),
			Options:   []string{i18n.T("op.save", m.state.Language)},
			AllowText: true,
			TextHint:  i18n.T("plan.download.fallback.hint", m.state.Language),
		})
	default:
		m.appendSystem(fmt.Sprintf(i18n.T("plan.download.failed", m.state.Language), err), "error")
	}
	return func() tea.Msg { return nil }
}

// planDownloadEntry 获取指定索引的计划下载条目
func (m *AppModel) planDownloadEntry(idx int) (historyEntry, bool) {
	if idx < 0 || idx >= len(m.history) {
		return historyEntry{}, false
	}
	entry := m.history[idx]
	if !strings.EqualFold(strings.TrimSpace(entry.executionMode), "plan") {
		return historyEntry{}, false
	}
	if strings.TrimSpace(entry.rawMarkdown) == "" {
		return historyEntry{}, false
	}
	return entry, true
}

// savePlanHistoryEntryToDir 将计划文件保存到指定目录
func (m *AppModel) savePlanHistoryEntryToDir(idx int, rawDir string) (string, error) {
	entry, ok := m.planDownloadEntry(idx)
	if !ok {
		return "", fmt.Errorf("%s", i18n.T("plan.download.unavailable", m.state.Language))
	}
	dir, err := resolveWorkspaceInputPath(rawDir, m.state.Language)
	if err != nil {
		return "", err
	}
	fi, err := os.Stat(dir)
	if err != nil || fi == nil || !fi.IsDir() {
		return "", fmt.Errorf(i18n.T("plan.download.not_directory", m.state.Language), dir)
	}
	path := filepath.Join(dir, m.nextPlanDownloadFileName(entry.timestamp))
	path = uniqueAvailablePath(path)
	if err := writePlanDownloadFile(path, []byte(entry.rawMarkdown), 0o644); err != nil {
		return "", fmt.Errorf(i18n.T("plan.download.failed", m.state.Language), err)
	}
	return path, nil
}

// nextPlanDownloadFileName 生成计划下载文件名，格式：plan-{sessionID}-{timestamp}.md
func (m *AppModel) nextPlanDownloadFileName(ts time.Time) string {
	stamp := ts
	if stamp.IsZero() {
		stamp = planDownloadNow()
	}
	name := "plan"
	if m != nil && m.adapter != nil {
		if sessionID, err := m.adapter.CurrentSessionID(context.Background()); err == nil {
			if cleaned := sanitizePlanFileNameSegment(sessionID); cleaned != "" {
				name += "-" + cleaned
			}
		}
	}
	return fmt.Sprintf("%s-%s.md", name, stamp.Format("20060102-150405"))
}

// sanitizePlanFileNameSegment 清理文件名中的非法字符
func sanitizePlanFileNameSegment(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	var b strings.Builder
	lastDash := false
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case r == '-' || r == '_' || r == '.':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

// uniqueAvailablePath 确保文件路径唯一，如果已存在则添加数字后缀
func uniqueAvailablePath(path string) string {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return path
	}
	dir := filepath.Dir(path)
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(filepath.Base(path), ext)
	for i := 2; ; i++ {
		candidate := filepath.Join(dir, fmt.Sprintf("%s-%d%s", base, i, ext))
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
}

// handlePromptRequestMsg 处理 PromptRequestMsg（Update 分支提取）。
// 原行为：permission 分支早退（return m, nil），其它分支早退（return m, nil）。
func (m *AppModel) handlePromptRequestMsg(msg PromptRequestMsg) (tea.Model, tea.Cmd) {
	req := confirm.Request{
		ID:        msg.ID,
		Kind:      strings.TrimSpace(msg.Kind),
		Title:     strings.TrimSpace(msg.Title),
		Question:  strings.TrimSpace(msg.Question),
		Options:   msg.Options,
		Diff:      msg.Diff,
		DiffPath:  msg.DiffPath,
		AllowText: msg.AllowText,
		TextHint:  msg.TextHint,
	}
	if req.Kind == "" {
		req.Kind = "permission"
	}
	if len(req.Options) == 0 {
		if req.Kind == "permission" {
			req.Options = []string{"accept", "acceptForSession", "decline", "cancel"}
		} else {
			req.Options = []string{"OK"}
		}
	}
	if req.Kind == "permission" {
		m.showInlinePermission(req)
		return m, nil
	}
	if m.confirmView == nil {
		m.prevView = m.activeView
	}
	m.confirmView = confirm.New(m.styles, m.state.Language, m.diffHighlightTheme(), req)
	m.confirmView.SetSize(m.width, m.height)
	m.activeView = "confirm"
	m.shell.BlurInput()
	return m, nil
}

// handleConfirmResultMsg 处理 confirm.ResultMsg（Update 分支提取）。
// 原行为：每个子分支均早退。
func (m *AppModel) handleConfirmResultMsg(msg confirm.ResultMsg) (tea.Model, tea.Cmd) {
	if msg.Kind == "permission" {
		m.clearInlinePermission()
		if msg.ID != "" {
			if err := m.adapter.RespondPrompt(context.Background(), msg.ID, msg.Kind, adapter.PromptResponse{
				Decision:    msg.Decision,
				Option:      msg.Option,
				OptionIndex: msg.OptionIndex,
				Text:        msg.Text,
			}); err != nil {
				m.appendSystem(fmt.Sprintf(i18n.T("toast.approval_failed", m.state.Language), err), "error")
			}
		}
		// 审批响应后 turn 恢复执行（工具重跑或继续生成），
		// 保持“处理中”指示器直到 turn.completed，避免审批后 spinner 一闪没。
		m.state.Processing = true
		m.shell.SetProcessing(true)
		if m.activeView == "shell" {
			m.shell.FocusInput()
		}
		// Re-arm the status animation tick: the prior turn's tick loop may
		// have stopped while waiting for approval, so without this the
		// spinner stays frozen even though processing resumed. The tick
		// loop self-reschedules only while busy.
		return m, m.shell.StatusTick()
	}
	if strings.HasPrefix(msg.Kind, "bg_kill:") {
		id, _ := strings.CutPrefix(msg.Kind, "bg_kill:")
		id = strings.TrimSpace(id)
		if msg.Decision == "confirm" && id != "" {
			if err := m.adapter.KillTask(context.Background(), id); err != nil {
				m.appendSystem(fmt.Sprintf(i18n.T("toast.stop_task_failed", m.state.Language), err), "error")
			} else {
				m.appendSystem(fmt.Sprintf(i18n.T("toast.task_stopped", m.state.Language), id), "warning")
			}
		}
		m.confirmView = nil
		if m.prevView != "" {
			m.activeView = m.prevView
			m.prevView = ""
		} else {
			m.activeView = "shell"
		}
		m.shell.FocusInput()
		return m, nil
	}
	if msg.Kind == browserTakeoverKind {
		return m.handleConfirmResultBrowserTakeover(msg)
	}
	if msg.Kind == "workspace_trust" {
		return m.handleConfirmResultWorkspaceTrust(msg)
	}
	if msg.Kind == "workspace_add" {
		return m.handleConfirmResultWorkspaceAdd(msg)
	}
	if msg.Kind == "plan_download_path" {
		return m.handleConfirmResultPlanDownloadPath(msg)
	}
	if msg.ID != "" {
		if err := m.adapter.RespondPrompt(context.Background(), msg.ID, msg.Kind, adapter.PromptResponse{
			Decision:    msg.Decision,
			Option:      msg.Option,
			OptionIndex: msg.OptionIndex,
			Text:        msg.Text,
		}); err != nil {
			m.appendSystem(fmt.Sprintf(i18n.T("toast.approval_failed", m.state.Language), err), "error")
		}
	}
	// 审批响应后 turn 恢复执行，保持“处理中”直到 turn.completed。
	m.state.Processing = true
	m.shell.SetProcessing(true)
	m.confirmView = nil
	if m.prevView != "" {
		m.activeView = m.prevView
		m.prevView = ""
	} else {
		m.activeView = "shell"
	}
	m.shell.FocusInput()
	// Re-arm the status animation tick after approval (see comment above).
	return m, m.shell.StatusTick()
}

// handleConfirmResultWorkspaceTrust 处理 workspace_trust 类型的确认结果。
func (m *AppModel) handleConfirmResultWorkspaceTrust(msg confirm.ResultMsg) (tea.Model, tea.Cmd) {
	path := strings.TrimSpace(m.trustPendingPath)
	action := strings.TrimSpace(m.trustPendingAction)
	if msg.Decision == "cancel" || path == "" {
		return m, tea.Quit
	}
	if msg.OptionIndex != 0 {
		return m, tea.Quit
	}
	if err := m.addTrustedWorkspace(path); err != nil {
		m.appendSystem(fmt.Sprintf(i18n.T("toast.trust_failed", m.state.Language), err), "error")
		return m, nil
	}
	if err := m.adapter.TrustWorkspace(context.Background(), path); err != nil {
		m.appendSystem(fmt.Sprintf(i18n.T("toast.trust_saved_sync_failed", m.state.Language), err), "warning")
	}
	m.trustPendingPath = ""
	m.trustPendingAction = ""
	m.confirmView = nil
	if m.prevView != "" {
		m.activeView = m.prevView
		m.prevView = ""
	} else {
		m.activeView = "shell"
	}
	switch action {
	case "init":
		rememberKnownWorkspace(path, true)
		_ = m.adapter.StartContextEngine(context.Background(), path)
		_, _ = m.adapter.Settings(context.Background())
		m.refreshWorkspacePanel()
		m.refreshRulesPanel()
		m.refreshLSPPanel()
		// 工作区刚被信任：现在消费 --continue/--resume 指定的会话
		// （Init 里的不信任分支延迟到这里）。
		m.resumeStartupSession()
	case "switch":
		cmd := m.switchWorkspaceTrusted(path)
		if m.activeView == "shell" {
			m.shell.FocusInput()
		}
		return m, cmd
	}
	if m.activeView == "shell" {
		m.shell.FocusInput()
	}
	return m, nil
}

// handleConfirmResultWorkspaceAdd 处理 workspace_add 类型的确认结果。
func (m *AppModel) handleConfirmResultWorkspaceAdd(msg confirm.ResultMsg) (tea.Model, tea.Cmd) {
	if msg.Decision != "confirm" {
		m.confirmView = nil
		if m.prevView != "" {
			m.activeView = m.prevView
			m.prevView = ""
		} else {
			m.activeView = "shell"
		}
		if m.activeView == "shell" {
			m.shell.FocusInput()
		}
		return m, nil
	}
	raw := strings.TrimSpace(msg.Text)
	p, err := resolveWorkspaceInputPath(raw, m.state.Language)
	if err != nil {
		m.appendSystem(err.Error(), "warning")
		return m, nil
	}
	fi, err := os.Stat(p)
	if err != nil || fi == nil || !fi.IsDir() {
		m.appendSystem(i18n.T("path_not_dir", m.state.Language)+p, "warning")
		return m, nil
	}
	if err := m.adapter.AddWorkspace(context.Background(), p); err != nil {
		m.appendSystem(err.Error(), "error")
		return m, nil
	}
	m.refreshWorkspacePanel()
	m.appendSystem(i18n.T("workspace.added", m.state.Language)+p, "success")
	m.confirmView = nil
	if m.prevView != "" {
		m.activeView = m.prevView
		m.prevView = ""
	} else {
		m.activeView = "shell"
	}
	if m.activeView == "shell" {
		m.shell.FocusInput()
	}
	return m, nil
}

// handleConfirmResultPlanDownloadPath 处理 plan_download_path 类型的确认结果。
func (m *AppModel) handleConfirmResultPlanDownloadPath(msg confirm.ResultMsg) (tea.Model, tea.Cmd) {
	req := m.pendingPlanDownload
	m.pendingPlanDownload = nil
	if msg.Decision != "confirm" || req == nil {
		m.confirmView = nil
		if m.prevView != "" {
			m.activeView = m.prevView
			m.prevView = ""
		} else {
			m.activeView = "shell"
		}
		if m.activeView == "shell" {
			m.shell.FocusInput()
		}
		return m, nil
	}
	dir := strings.TrimSpace(msg.Text)
	path, err := m.savePlanHistoryEntryToDir(req.HistoryIndex, dir)
	if err != nil {
		m.appendSystem(err.Error(), "error")
	} else {
		m.appendSystem(fmt.Sprintf(i18n.T("plan.download.saved", m.state.Language), path), "success")
	}
	m.confirmView = nil
	if m.prevView != "" {
		m.activeView = m.prevView
		m.prevView = ""
	} else {
		m.activeView = "shell"
	}
	if m.activeView == "shell" {
		m.shell.FocusInput()
	}
	return m, nil
}

// handleActionResultMsg 处理 confirm.ActionResultMsg（Update 分支提取）。
// 原行为：早退。
func (m *AppModel) handleActionResultMsg(msg confirm.ActionResultMsg) (tea.Model, tea.Cmd) {
	return m, m.handleActionResult(msg)
}

// handleTaskKillRequestMsg 处理 panels.TaskKillRequestMsg（Update 分支提取）。
// 原行为：有 id 时早退，否则 fall-through。
func (m *AppModel) handleTaskKillRequestMsg(msg panels.TaskKillRequestMsg) (tea.Model, tea.Cmd) {
	id := strings.TrimSpace(msg.ID)
	if id == "" {
		return m, m.finalizeUpdate(nil)
	}
	if m.confirmView == nil {
		m.prevView = m.activeView
	}
	req := confirm.Request{
		Kind:     "bg_kill:" + id,
		Title:    i18n.T("tasks.kill.title", m.state.Language),
		Question: fmt.Sprintf(i18n.T("tasks.kill.question", m.state.Language), id),
		Options:  []string{"OK"},
	}
	m.confirmView = confirm.New(m.styles, m.state.Language, m.diffHighlightTheme(), req)
	m.confirmView.SetSize(m.width, m.height)
	m.activeView = "confirm"
	m.shell.BlurInput()
	return m, nil
}
