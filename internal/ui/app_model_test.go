package ui

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/eosaios/eos/internal/config"
	"github.com/eosaios/eos/internal/pkg/filedialog"
	"github.com/eosaios/eos/internal/pkg/settings"
	"github.com/eosaios/eos/internal/state"
	"github.com/eosaios/eos/internal/ui/components/messages"
	"github.com/eosaios/eos/internal/ui/features/slash"
	"github.com/eosaios/eos/internal/ui/views/setup"
	"github.com/eosaios/eos/internal/version"
	"github.com/eosaios/eos/pkg/coreapi"

	tea "charm.land/bubbletea/v2"
)

var ansiPatternAppTest = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSIAppTest(s string) string {
	return ansiPatternAppTest.ReplaceAllString(s, "")
}

func setTestHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	volume := filepath.VolumeName(home)
	if volume != "" {
		t.Setenv("HOMEDRIVE", volume)
		t.Setenv("HOMEPATH", strings.TrimPrefix(home, volume))
	}
	return home
}

func newTestAppModel(t *testing.T) *AppModel {
	t.Helper()
	engine := newTestEngine()
	engine.models = []coreapi.ModelConfig{{
		Name:    "default-model",
		APIBase: "https://example.com/v1",
		Model:   "demo-model",
	}}
	engine.activeModel = "default-model"
	return NewAppModelFromCoreEngine(engine)
}

func newEmptyTestAppModel(t *testing.T) *AppModel {
	t.Helper()
	return NewAppModelFromCoreEngine(newTestEngine())
}

func sendAppKey(t *testing.T, app *AppModel, msg tea.KeyMsg) *AppModel {
	t.Helper()
	next, _ := app.Update(msg)
	updated, ok := next.(*AppModel)
	if !ok {
		t.Fatalf("expected *AppModel, got %T", next)
	}
	return updated
}

func TestInitialSetupKeepsWelcomeAfterFirstModelAdded(t *testing.T) {
	setTestHome(t)
	t.Setenv("EOS_API_BASE", "")
	t.Setenv("EOS_API_KEY", "")
	t.Setenv("EOS_MODEL", "")

	app := newEmptyTestAppModel(t)
	if app.activeView != "setup" {
		t.Fatalf("expected setup view on first launch, got %q", app.activeView)
	}
	if !app.initialSetupFlow {
		t.Fatalf("expected initial setup flow to be enabled")
	}

	app.handleModelFormComplete(setup.ModelFormCompleteMsg{
		Config: setup.SetupConfig{
			Name:    "first-model",
			APIBase: "https://example.com/v1",
			APIKey:  "secret",
			Model:   "demo-model",
		},
	})

	if app.activeView != "shell" {
		t.Fatalf("expected shell view after setup, got %q", app.activeView)
	}
	if len(app.history) != 0 {
		t.Fatalf("expected initial setup success to keep history empty, got %d entries", len(app.history))
	}

	view := stripANSIAppTest(app.shell.View())
	if strings.Contains(view, "Added and switched to model: first-model") {
		t.Fatalf("expected welcome screen without success history message, got %q", view)
	}
	// Redesigned welcome card shows subtitle + app version, not model/API info.
	if !strings.Contains(view, "AI 领航，助你破浪前行") {
		t.Fatalf("expected welcome card subtitle to remain visible, got %q", view)
	}
	if !strings.Contains(view, "EOS "+version.AppVersion) {
		t.Fatalf("expected welcome card to show app version %q, got %q", version.AppVersion, view)
	}
}

func TestSlashHintsNavigationKeepsPanelAndInsertsCanonicalCommand(t *testing.T) {
	setTestHome(t)
	t.Setenv("EOS_API_BASE", "https://example.com/v1")
	t.Setenv("EOS_API_KEY", "secret")
	t.Setenv("EOS_MODEL", "demo-model")

	testCases := []struct {
		name      string
		command   string
		acceptKey tea.KeyPressMsg
		wantInput string
	}{
		{
			name:      "enter accepts canonical command",
			command:   "/lang",
			acceptKey: tea.KeyPressMsg{Code: tea.KeyEnter},
			wantInput: "/lang ",
		},
		{
			name:      "tab accepts canonical command",
			command:   "/workspace",
			acceptKey: tea.KeyPressMsg{Code: tea.KeyTab},
			wantInput: "/workspace ",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			app := newTestAppModel(t)
			if app.activeView != "shell" {
				t.Fatalf("expected shell view, got %q", app.activeView)
			}

			app = sendAppKey(t, app, tea.KeyPressMsg{Code: '/', Text: "/"})
			if got := app.shell.GetInputValue(); got != "/" {
				t.Fatalf("expected input to contain slash trigger, got %q", got)
			}
			if !app.shell.IsHintsVisible() {
				t.Fatalf("expected slash hints to be visible after typing slash")
			}

			downs := findVisibleSlashCommandIndex(t, tc.command)
			for i := 0; i < downs; i++ {
				app = sendAppKey(t, app, tea.KeyPressMsg{Code: tea.KeyDown})
				if !app.shell.IsHintsVisible() {
					t.Fatalf("expected slash hints to remain visible after down navigation %d", i+1)
				}
			}

			app = sendAppKey(t, app, tc.acceptKey)
			if app.shell.IsHintsVisible() {
				t.Fatalf("expected slash hints to hide after accepting a command")
			}
			if got := app.shell.GetInputValue(); got != tc.wantInput {
				t.Fatalf("expected accepted command %q, got %q", tc.wantInput, got)
			}
		})
	}
}

func findVisibleSlashCommandIndex(t *testing.T, name string) int {
	t.Helper()
	items := slash.VisibleCommands()
	for idx, item := range items {
		if item.Name == name {
			return idx
		}
	}
	t.Fatalf("visible slash command %q not found", name)
	return -1
}

func TestHandlePlanStyleSlashShowsCurrentAndSavesWorkspaceSetting(t *testing.T) {
	setTestHome(t)
	workspace := t.TempDir()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(wd)
	})
	if err := os.Chdir(workspace); err != nil {
		t.Fatalf("chdir workspace: %v", err)
	}
	app := newTestAppModel(t)

	app.handlePlanStyleSlash(nil)
	if len(app.history) == 0 || !strings.Contains(app.history[len(app.history)-1].content, "concise") {
		t.Fatalf("expected current plan style message to mention concise, history=%+v", app.history)
	}

	app.handlePlanStyleSlash([]string{"detailed"})
	ctx := context.Background()
	if got, err := app.adapter.Settings(ctx); err != nil {
		t.Fatalf("Settings() error = %v", err)
	} else if got.PlanPromptStyle != "detailed" {
		t.Fatalf("Settings.PlanPromptStyle=%q, want detailed", got.PlanPromptStyle)
	}

	raw, err := os.ReadFile(filepath.Join(workspace, ".eos", "settings.json"))
	if err != nil {
		t.Fatalf("read workspace settings: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal workspace settings: %v", err)
	}
	if got := doc["plan_prompt_style"]; got != "detailed" {
		t.Fatalf("plan_prompt_style=%v, want detailed", got)
	}
}

func TestHandlePermissionsSlashSupportsAccessAndApprovalModes(t *testing.T) {
	setTestHome(t)
	app := newTestAppModel(t)

	app.handlePermissionsSlash([]string{"access", "read-only"})
	snap, err := app.adapter.PermissionSnapshot(context.Background())
	if err != nil {
		t.Fatalf("PermissionSnapshot() error = %v", err)
	}
	if got := snap.AccessMode; got != "read-only" {
		t.Fatalf("accessMode=%q, want read-only", got)
	}
	// ApplyAccessMode 会同步 mode 快照（read-only）并把审批轴复位 on-request。
	if got := snap.SandboxMode; got != "read-only" {
		t.Fatalf("sandboxMode=%q, want read-only (snapshot synced)", got)
	}
	if got := snap.ApprovalMode; got != "on-request" {
		t.Fatalf("approvalMode=%q, want on-request (reset on non-danger switch)", got)
	}

	app.handlePermissionsSlash([]string{"approval", "never"})
	snap, err = app.adapter.PermissionSnapshot(context.Background())
	if err != nil {
		t.Fatalf("PermissionSnapshot() error = %v", err)
	}
	if got := snap.ApprovalMode; got != "never" {
		t.Fatalf("approvalMode=%q, want never", got)
	}

	last := app.history[len(app.history)-1].content
	if !strings.Contains(last, "审批模式") || !strings.Contains(last, "never") {
		t.Fatalf("expected permissions output to include approval mode, got %q", last)
	}
}

func TestPromptRequestPermissionStaysInShellWithInlineOverlay(t *testing.T) {
	setTestHome(t)
	t.Setenv("EOS_API_BASE", "https://example.com/v1")
	t.Setenv("EOS_API_KEY", "secret")
	t.Setenv("EOS_MODEL", "demo-model")

	app := newTestAppModel(t)
	next, _ := app.Update(PromptRequestMsg{
		ID:       "perm-1",
		Kind:     "permission",
		Title:    "授权",
		Question: "是否允许执行危险操作？",
		Options:  []string{"accept", "acceptForSession", "decline", "cancel"},
	})
	updated := next.(*AppModel)

	if updated.activeView != "shell" {
		t.Fatalf("activeView=%q, want shell", updated.activeView)
	}
	if updated.confirmView != nil {
		t.Fatalf("expected permission request to avoid full confirm view")
	}
	if updated.inlinePermissionReq == nil {
		t.Fatalf("expected inline permission state to be stored")
	}
	view := stripANSIAppTest(updated.shell.View())
	if !strings.Contains(view, "是否允许执行危险操作？") || !strings.Contains(view, "是，继续执行") {
		t.Fatalf("expected shell to render inline permission overlay, got %q", view)
	}
}

func TestPromptRequestNonPermissionStillUsesConfirmView(t *testing.T) {
	setTestHome(t)
	t.Setenv("EOS_API_BASE", "https://example.com/v1")
	t.Setenv("EOS_API_KEY", "secret")
	t.Setenv("EOS_MODEL", "demo-model")

	app := newTestAppModel(t)
	next, _ := app.Update(PromptRequestMsg{
		ID:       "ws-1",
		Kind:     "workspace_trust",
		Title:    "信任工作区",
		Question: "是否信任当前工作区？",
		Options:  []string{"信任并继续", "退出"},
	})
	updated := next.(*AppModel)

	if updated.activeView != "confirm" {
		t.Fatalf("activeView=%q, want confirm", updated.activeView)
	}
	if updated.confirmView == nil {
		t.Fatalf("expected non-permission prompt to use confirm view")
	}
	if updated.inlinePermissionReq != nil {
		t.Fatalf("did not expect inline permission state for non-permission prompt")
	}
}

func TestWorkspaceTrustUsesWorkspaceLocalMarker(t *testing.T) {
	setTestHome(t)
	workspace := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	app := newTestAppModel(t)

	if app.isWorkspaceTrusted(workspace) {
		t.Fatal("workspace should not be trusted before local marker exists")
	}
	if err := app.addTrustedWorkspace(workspace); err != nil {
		t.Fatalf("addTrustedWorkspace() error = %v", err)
	}
	if !app.isWorkspaceTrusted(workspace) {
		t.Fatal("workspace should be trusted after local marker is written")
	}
	if _, err := os.Stat(config.WorkspaceTrustPath(workspace)); err != nil {
		t.Fatalf("expected workspace local trust marker to exist: %v", err)
	}
}

func TestWorkspaceTrustKeepsLegacyGlobalCompatibility(t *testing.T) {
	setTestHome(t)
	workspace := filepath.Join(t.TempDir(), "legacy-repo")
	cfg, cfgPath := config.Load()
	cfg.TrustedWorkspaces = []string{config.NormalizeWorkspacePath(workspace)}
	if err := config.Save(cfg, cfgPath); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	app := newTestAppModel(t)
	if !app.isWorkspaceTrusted(workspace) {
		t.Fatal("workspace should remain trusted from legacy global config")
	}
}

func TestHandleStatusSlashShowsAccessAndApprovalModes(t *testing.T) {
	setTestHome(t)
	app := newTestAppModel(t)
	ctx := context.Background()
	if err := app.adapter.SetAccessMode(ctx, "danger-full-access"); err != nil {
		t.Fatalf("SetAccessMode() error = %v", err)
	}
	if err := app.adapter.SetApprovalMode(ctx, "never"); err != nil {
		t.Fatalf("SetApprovalMode() error = %v", err)
	}

	app.handleStatusSlash()
	last := app.history[len(app.history)-1].content
	for _, part := range []string{"访问模式", "danger-full-access", "审批模式", "never", "沙箱模式", "danger-full-access"} {
		if !strings.Contains(last, part) {
			t.Fatalf("expected status output to contain %q, got %q", part, last)
		}
	}
}

func TestPredictionUpdateMsgShowsPredictionForMatchingDraft(t *testing.T) {
	setTestHome(t)
	t.Setenv("EOS_API_BASE", "https://example.com/v1")
	t.Setenv("EOS_API_KEY", "secret")
	t.Setenv("EOS_MODEL", "demo-model")

	app := newTestAppModel(t)
	app.predictionEnabled = true
	app.predictionSeq = 1
	app.shell.SetInputValue("我")

	next, _ := app.Update(PredictionUpdateMsg{Seq: 1, Draft: "我", Text: "我们继续拆一下这个方案"})
	updated := next.(*AppModel)
	if !updated.shell.HasPrediction() {
		t.Fatalf("expected prediction to be visible")
	}
	if got := updated.shell.GetInputValue(); got != "我" {
		t.Fatalf("input=%q, want 我", got)
	}

	updated = sendAppKey(t, updated, tea.KeyPressMsg{Code: '们', Text: "们"})
	if !updated.shell.HasPrediction() {
		t.Fatalf("expected prediction to remain when typed input still matches prefix")
	}
	if got := updated.shell.GetInputValue(); got != "我们" {
		t.Fatalf("input=%q, want 我们", got)
	}
}

func TestPredictionUpdateMsgIgnoresStaleDraft(t *testing.T) {
	setTestHome(t)
	t.Setenv("EOS_API_BASE", "https://example.com/v1")
	t.Setenv("EOS_API_KEY", "secret")
	t.Setenv("EOS_MODEL", "demo-model")

	app := newTestAppModel(t)
	app.predictionEnabled = true
	app.predictionSeq = 1
	app.shell.SetInputValue("我")

	next, _ := app.Update(PredictionUpdateMsg{Seq: 1, Draft: "他", Text: "他们继续拆一下这个方案"})
	updated := next.(*AppModel)
	if updated.shell.HasPrediction() {
		t.Fatalf("expected stale draft prediction to be ignored")
	}
	if got := updated.shell.GetInputValue(); got != "我" {
		t.Fatalf("input=%q, want 我", got)
	}
}

func TestSchedulePredictionCreatesDebouncedRequest(t *testing.T) {
	setTestHome(t)
	t.Setenv("EOS_API_BASE", "https://example.com/v1")
	t.Setenv("EOS_API_KEY", "secret")
	t.Setenv("EOS_MODEL", "demo-model")

	app := newTestAppModel(t)
	app.predictionEnabled = true

	cmd := app.schedulePrediction("我想")
	if cmd == nil {
		t.Fatalf("expected debounced prediction cmd")
	}
	msg := cmd()
	debounce, ok := msg.(predictionDebounceMsg)
	if !ok {
		t.Fatalf("msg=%T, want predictionDebounceMsg", msg)
	}
	if debounce.Draft != "我想" {
		t.Fatalf("draft=%q, want 我想", debounce.Draft)
	}
}

func TestHandleSettingsSavePersistsGlobalPredictionFlag(t *testing.T) {
	home := setTestHome(t)
	workspace := t.TempDir()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(wd)
	})
	if err := os.Chdir(workspace); err != nil {
		t.Fatalf("chdir workspace: %v", err)
	}

	app := newTestAppModel(t)
	app.predictionEnabled = true
	app.predictionText = "继续帮我拆一下这个方案"
	app.shell.SetPrediction(app.predictionText)

	disabled := false
	memoryOff := false
	app.handleSettingsSave(&settings.Settings{Language: "zh"}, &disabled, &memoryOff, "")

	if app.predictionEnabled {
		t.Fatalf("expected prediction to be disabled")
	}
	if app.memoryInjectionEnabled {
		t.Fatalf("expected memory injection to be disabled")
	}
	if app.shell.HasPrediction() {
		t.Fatalf("expected visible prediction to clear after disabling")
	}

	raw, err := os.ReadFile(filepath.Join(home, ".eos.json"))
	if err != nil {
		t.Fatalf("read global config: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal global config: %v", err)
	}
	if got := doc["next_message_prediction_enabled"]; got != false {
		t.Fatalf("next_message_prediction_enabled=%v, want false", got)
	}
}

func TestThinkingStatusIsNotRenderedAsFoldStateWhenIdle(t *testing.T) {
	setTestHome(t)
	t.Setenv("EOS_API_BASE", "https://example.com/v1")
	t.Setenv("EOS_API_KEY", "secret")
	t.Setenv("EOS_MODEL", "demo-model")
	state.SetThinking(true)
	t.Cleanup(func() { state.SetThinking(true) })

	app := newTestAppModel(t)
	view := stripANSIAppTest(app.shell.View())
	if strings.Contains(view, "思考:折叠") || strings.Contains(view, "Thinking:collapsed") {
		t.Fatalf("idle status should not expose thinking fold state, got %q", view)
	}
	if !strings.Contains(view, "思考:开") {
		t.Fatalf("expected idle status to show thinking switch state, got %q", view)
	}
}

func TestThinkingLiveBlockTogglesInContentAndClearsWhenAnswerStarts(t *testing.T) {
	setTestHome(t)
	t.Setenv("EOS_API_BASE", "https://example.com/v1")
	t.Setenv("EOS_API_KEY", "secret")
	t.Setenv("EOS_MODEL", "demo-model")
	state.SetThinking(true)
	t.Cleanup(func() { state.SetThinking(true) })

	app := newTestAppModel(t)
	app.state.Processing = true
	app.shell.SetProcessing(true)
	app.currentAIStartTime = time.Now()

	next, _ := app.Update(ThinkingMsg{Content: "第一步分析\n第二步推理"})
	updated := next.(*AppModel)
	view := stripANSIAppTest(updated.shell.View())
	if !strings.Contains(view, "Thinking") || !strings.Contains(view, "Alt+H") {
		t.Fatalf("expected current thinking to render in content live block, got %q", view)
	}
	if !strings.Contains(view, "第二步推理") {
		t.Fatalf("expected collapsed thinking summary to be visible, got %q", view)
	}
	if strings.Contains(view, "思考:折叠") {
		t.Fatalf("status bar should not render thinking fold state, got %q", view)
	}

	historyLen := len(updated.history)
	updated = sendAppKey(t, updated, tea.KeyPressMsg{Code: 'h', Mod: tea.ModAlt})
	view = stripANSIAppTest(updated.shell.View())
	if !strings.Contains(view, "第一步分析") {
		t.Fatalf("expected Alt+H to expand current thinking content, got %q", view)
	}
	if len(updated.history) != historyLen {
		t.Fatalf("Alt+H should not append a history message, got %d want %d", len(updated.history), historyLen)
	}

	updated.handleAIResponse(AIResponseMsg{Type: "delta", Content: "最终回答"})
	view = stripANSIAppTest(updated.shell.View())
	if strings.Contains(view, "第一步分析") || strings.Contains(view, "第二步推理") || strings.Contains(view, "Thinking") {
		t.Fatalf("thinking live block should clear once assistant output starts, got %q", view)
	}
	if !strings.Contains(view, "最终回答") {
		t.Fatalf("expected assistant live output after thinking clears, got %q", view)
	}
}

func TestRenderHistoryEntryShowsDownloadActionOnlyForPlanMessages(t *testing.T) {
	setTestHome(t)
	app := newTestAppModel(t)

	// 文本流布局下不再渲染内联按钮；动作列表由 bubbleActionsForEntry 决定，
	// 点击消息文本时弹出操作弹框。这里断言动作集合的正确性。
	hasAction := func(actions []messages.BubbleAction, kind string) bool {
		for _, a := range actions {
			if a.Kind == kind {
				return true
			}
		}
		return false
	}

	planActions := app.bubbleActionsForEntry(historyEntry{
		kind:          "ai",
		content:       "## 执行计划",
		rawMarkdown:   "## 执行计划",
		executionMode: "plan",
		timestamp:     time.Now(),
	})
	if !hasAction(planActions, "copy") {
		t.Fatalf("expected copy action for plan entry, got %v", planActions)
	}
	if !hasAction(planActions, "download") {
		t.Fatalf("expected download action for plan entry, got %v", planActions)
	}

	autoActions := app.bubbleActionsForEntry(historyEntry{
		kind:          "ai",
		content:       "普通回复",
		rawMarkdown:   "普通回复",
		executionMode: "auto",
		timestamp:     time.Now(),
	})
	if !hasAction(autoActions, "copy") {
		t.Fatalf("expected copy action for auto entry, got %v", autoActions)
	}
	if hasAction(autoActions, "download") {
		t.Fatalf("did not expect download action for auto entry, got %v", autoActions)
	}

	userActions := app.bubbleActionsForEntry(historyEntry{
		kind:      "user",
		content:   "帮我写一个排序函数",
		timestamp: time.Now(),
	})
	if !hasAction(userActions, "copy") {
		t.Fatalf("expected copy action for user entry, got %v", userActions)
	}
	if hasAction(userActions, "download") {
		t.Fatalf("did not expect download action for user entry, got %v", userActions)
	}
}

func TestHandleItemDeltaReasoningStreamsIntoLiveThinkingBlock(t *testing.T) {
	setTestHome(t)
	state.SetThinking(true)
	t.Cleanup(func() { state.SetThinking(true) })

	app := newTestAppModel(t)
	app.state.Processing = true
	app.shell.SetProcessing(true)
	app.currentAIStartTime = time.Now()

	// A reasoning delta must stream into the live thinking block (mirroring the
	// legacy ThinkingMsg path) instead of being silently dropped.
	app.handleItemDelta(ItemDeltaMsg{ItemID: "rs_1", DeltaType: "reasoning", Delta: "第一步分析\n第二步推理"})

	if app.thinkingLive.String() != "第一步分析\n第二步推理" {
		t.Fatalf("thinkingLive=%q, want streamed reasoning delta", app.thinkingLive.String())
	}
	if !app.state.Thinking {
		t.Fatalf("state.Thinking should be true while reasoning streams")
	}
	view := stripANSIAppTest(app.shell.View())
	if !strings.Contains(view, "Thinking") {
		t.Fatalf("expected live thinking block to render, got %q", view)
	}
	if !strings.Contains(view, "第二步推理") {
		t.Fatalf("expected collapsed thinking summary (last non-empty line) to be visible, got %q", view)
	}
}

func TestHandleItemCompletedReasoningArchivesDimSummaryEntry(t *testing.T) {
	setTestHome(t)
	state.SetThinking(true)
	t.Cleanup(func() { state.SetThinking(true) })

	app := newTestAppModel(t)
	app.state.Processing = true
	app.currentAIStartTime = time.Now()
	app.startReasoningItem("rs_1")
	app.handleItemDelta(ItemDeltaMsg{ItemID: "rs_1", DeltaType: "reasoning", Delta: "前导内容\n最终结论"})

	historyBefore := len(app.history)
	app.handleItemCompleted(ItemCompletedMsg{
		ItemID:    "rs_1",
		ItemType:  "reasoning",
		Reasoning: "前导内容\n最终结论",
	})

	if len(app.history) != historyBefore+1 {
		t.Fatalf("expected one reasoning history entry, got history len %d want %d", len(app.history), historyBefore+1)
	}
	entry := app.history[len(app.history)-1]
	if entry.kind != "reasoning" {
		t.Fatalf("archived kind=%q, want reasoning", entry.kind)
	}
	if entry.content != "前导内容\n最终结论" {
		t.Fatalf("archived content=%q, want full reasoning text", entry.content)
	}
	// Live thinking state is cleared after archiving.
	if app.thinkingLive.Len() != 0 {
		t.Fatalf("thinkingLive should be cleared after completion, got %q", app.thinkingLive.String())
	}
	if app.state.Thinking {
		t.Fatalf("state.Thinking should be false after reasoning completes")
	}
}

func TestRenderHistoryEntryReasoningRendersCollapsedSummary(t *testing.T) {
	setTestHome(t)
	app := newTestAppModel(t)

	rendered := app.renderHistoryEntry(historyEntry{
		kind:      "reasoning",
		content:   "第一段推理\n最终结论摘要",
		duration:  2 * time.Second,
		timestamp: time.Now(),
	})
	plain := stripANSIAppTest(rendered)
	if !strings.Contains(plain, "Thinking") {
		t.Fatalf("expected reasoning header in history render, got %q", plain)
	}
	// Collapsed state shows only the last non-empty line as a dim summary.
	if !strings.Contains(plain, "最终结论摘要") {
		t.Fatalf("expected collapsed summary (last line) in history render, got %q", plain)
	}
	if strings.Contains(plain, "第一段推理") {
		t.Fatalf("collapsed reasoning must not show full content, got %q", plain)
	}
}

func TestHandlePlanDownloadActionFallsBackToManualPath(t *testing.T) {
	setTestHome(t)
	app := newTestAppModel(t)
	app.history = []historyEntry{{
		kind:          "ai",
		content:       "## 执行计划",
		rawMarkdown:   "## 执行计划",
		executionMode: "plan",
		timestamp:     time.Now(),
	}}

	origChooser := choosePlanDownloadDirectory
	choosePlanDownloadDirectory = func(string) (string, error) {
		return "", filedialog.ErrUnavailable
	}
	t.Cleanup(func() {
		choosePlanDownloadDirectory = origChooser
	})

	cmd := app.handlePlanDownloadAction(0)
	if cmd == nil {
		t.Fatalf("expected non-nil command to mark the mouse action as handled")
	}
	if app.confirmView == nil {
		t.Fatalf("expected fallback confirm view to open")
	}
	if app.pendingPlanDownload == nil || app.pendingPlanDownload.HistoryIndex != 0 {
		t.Fatalf("expected pending plan download state to be recorded, got %+v", app.pendingPlanDownload)
	}
	if app.activeView != "confirm" {
		t.Fatalf("activeView=%q, want confirm", app.activeView)
	}
}

func TestSavePlanHistoryEntryToDirWritesMarkdownAndDeduplicatesName(t *testing.T) {
	setTestHome(t)
	app := newTestAppModel(t)
	dir := t.TempDir()
	ts := time.Date(2026, 5, 9, 12, 30, 0, 0, time.UTC)
	app.history = []historyEntry{{
		kind:          "ai",
		content:       "## 执行计划",
		rawMarkdown:   "# Plan\n\n- step 1\n",
		executionMode: "plan",
		timestamp:     ts,
	}}

	first, err := app.savePlanHistoryEntryToDir(0, dir)
	if err != nil {
		t.Fatalf("first save error: %v", err)
	}
	second, err := app.savePlanHistoryEntryToDir(0, dir)
	if err != nil {
		t.Fatalf("second save error: %v", err)
	}
	if first == second {
		t.Fatalf("expected unique file path on second save, got %q", first)
	}
	raw1, err := os.ReadFile(first)
	if err != nil {
		t.Fatalf("read first file: %v", err)
	}
	raw2, err := os.ReadFile(second)
	if err != nil {
		t.Fatalf("read second file: %v", err)
	}
	if string(raw1) != "# Plan\n\n- step 1\n" || string(raw2) != "# Plan\n\n- step 1\n" {
		t.Fatalf("saved markdown mismatch: %q / %q", string(raw1), string(raw2))
	}
}

func TestSavePlanHistoryEntryToDirRejectsNonPlanMessage(t *testing.T) {
	setTestHome(t)
	app := newTestAppModel(t)
	app.history = []historyEntry{{
		kind:          "ai",
		content:       "普通回复",
		rawMarkdown:   "普通回复",
		executionMode: "auto",
		timestamp:     time.Now(),
	}}

	_, err := app.savePlanHistoryEntryToDir(0, t.TempDir())
	if err == nil {
		t.Fatalf("expected non-plan message to be rejected")
	}
	if !strings.Contains(err.Error(), "计划") && !strings.Contains(strings.ToLower(err.Error()), "downloadable") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHandlePlanDownloadActionReportsChooserFailure(t *testing.T) {
	setTestHome(t)
	app := newTestAppModel(t)
	app.history = []historyEntry{{
		kind:          "ai",
		content:       "## 执行计划",
		rawMarkdown:   "## 执行计划",
		executionMode: "plan",
		timestamp:     time.Now(),
	}}

	origChooser := choosePlanDownloadDirectory
	choosePlanDownloadDirectory = func(string) (string, error) {
		return "", errors.New("boom")
	}
	t.Cleanup(func() {
		choosePlanDownloadDirectory = origChooser
	})

	_ = app.handlePlanDownloadAction(0)
	if len(app.history) == 0 {
		t.Fatalf("expected an error message in history")
	}
	last := app.history[len(app.history)-1]
	if last.kind != "system" || last.level != "error" {
		t.Fatalf("expected system error entry, got %+v", last)
	}
}

func TestStartReasoningItemDoesNotArchivePartialAgentText(t *testing.T) {
	setTestHome(t)
	app := newTestAppModel(t)
	app.state.Processing = true
	app.activeItemID = "msg_1"
	app.aiLive.WriteString("你")

	app.startReasoningItem("rs_1")

	if len(app.history) != 0 {
		t.Fatalf("reasoning start must not archive partial agent text, history len=%d", len(app.history))
	}
	if app.aiLive.String() != "你" {
		t.Fatalf("aiLive=%q, want preserved partial agent text", app.aiLive.String())
	}
}

func TestAgentMessageStreamCommitsSingleHistoryEntry(t *testing.T) {
	setTestHome(t)
	app := newTestAppModel(t)
	app.state.Processing = true
	app.currentAIStartTime = time.Now()

	app.startAgentMessageItem("msg_1")
	app.handleItemDelta(ItemDeltaMsg{ItemID: "msg_1", DeltaType: "text", Delta: "你"})
	app.handleItemDelta(ItemDeltaMsg{ItemID: "msg_1", DeltaType: "text", Delta: "好！"})
	app.handleItemCompleted(ItemCompletedMsg{ItemID: "msg_1", ItemType: "agent_message", Text: "你好！"})

	aiCount := 0
	var lastAI string
	for _, e := range app.history {
		if e.kind == "ai" {
			aiCount++
			lastAI = e.content
		}
	}
	if aiCount != 1 {
		t.Fatalf("want 1 ai history entry, got %d (%v)", aiCount, app.history)
	}
	if lastAI != "你好！" {
		t.Fatalf("archived ai=%q, want 你好！", lastAI)
	}
}

func TestSecondAgentSegmentDoesNotArchiveViaItemStarted(t *testing.T) {
	setTestHome(t)
	app := newTestAppModel(t)
	app.state.Processing = true
	app.currentAIStartTime = time.Now()

	app.startAgentMessageItem("msg_1")
	app.handleItemDelta(ItemDeltaMsg{ItemID: "msg_1", DeltaType: "text", Delta: "第一段"})
	app.handleItemCompleted(ItemCompletedMsg{ItemID: "msg_1", ItemType: "agent_message", Text: "第一段"})

	app.startAgentMessageItem("msg_2")
	app.handleItemDelta(ItemDeltaMsg{ItemID: "msg_2", DeltaType: "text", Delta: "第二段"})
	app.handleItemCompleted(ItemCompletedMsg{ItemID: "msg_2", ItemType: "agent_message", Text: "第二段"})

	aiEntries := make([]string, 0)
	for _, e := range app.history {
		if e.kind == "ai" {
			aiEntries = append(aiEntries, e.content)
		}
	}
	if len(aiEntries) != 2 || aiEntries[0] != "第一段" || aiEntries[1] != "第二段" {
		t.Fatalf("want two distinct ai segments, got %v", aiEntries)
	}
}

func TestLateAgentDeltaAfterCompletionIsIgnored(t *testing.T) {
	setTestHome(t)
	app := newTestAppModel(t)
	app.state.Processing = true
	app.currentAIStartTime = time.Now()

	app.startAgentMessageItem("msg_1")
	app.handleItemDelta(ItemDeltaMsg{ItemID: "msg_1", DeltaType: "text", Delta: "你好！"})
	app.handleItemCompleted(ItemCompletedMsg{ItemID: "msg_1", ItemType: "agent_message", Text: "你好！"})

	before := len(app.history)
	app.handleItemDelta(ItemDeltaMsg{ItemID: "msg_1", DeltaType: "text", Delta: "重复"})
	app.handleInvokeDoneMsg(InvokeDoneMsg{})

	aiCount := 0
	for _, e := range app.history {
		if e.kind == "ai" {
			aiCount++
		}
	}
	if aiCount != 1 {
		t.Fatalf("late delta must not create duplicate ai entry, got %d (history grew %d→%d)", aiCount, before, len(app.history))
	}
}
