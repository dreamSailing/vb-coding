package shell

// Copyright (c) 2026 EOSAIOS
// SPDX-License-Identifier: EOS-NCL-1.1
// 本文件基于 EOS 非商用许可证 v1.1 发布，详见 LICENSE。
// 商业使用请联系版权人获得商业授权。

import (
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/eosaios/eos/internal/ui/styles"
)

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSIForTest(s string) string {
	return ansiPattern.ReplaceAllString(s, "")
}

func TestRenderStatusBarShowsExecutionMode(t *testing.T) {
	s := styles.NewStyles(styles.GetTheme("dark"))
	model := New(100, 30, s, "zh")
	model.SetContextVisible(true)
	model.SetExecutionMode("plan")

	out := stripANSIForTest(model.renderStatusBar())
	if !strings.Contains(out, "执行:plan") {
		t.Fatalf("expected status bar to include execution mode, got %q", out)
	}
}

func TestRenderStatusBarShowsThinkingActiveWhenRunning(t *testing.T) {
	s := styles.NewStyles(styles.GetTheme("dark"))
	model := New(100, 30, s, "zh")
	model.SetProcessing(true)
	model.statusAnim = 2

	out := stripANSIForTest(model.renderStatusBar())
	if !strings.Contains(out, "思考中...") {
		t.Fatalf("expected running status bar to include animated thinking text, got %q", out)
	}
	if strings.Contains(out, "思考:开") || strings.Contains(out, "思考:关") {
		t.Fatalf("expected running status bar to hide static thinking toggle, got %q", out)
	}
}

func TestViewRendersPromptOverlayAboveStatusBar(t *testing.T) {
	s := styles.NewStyles(styles.GetTheme("dark"))
	model := New(100, 30, s, "zh")
	model.SetContent("历史消息")
	model.SetPromptOverlay("授权块")

	out := stripANSIForTest(model.View())
	overlayIdx := strings.Index(out, "授权块")
	statusIdx := strings.Index(out, "[ AI ]")
	if overlayIdx < 0 {
		t.Fatalf("expected prompt overlay to render, got %q", out)
	}
	if statusIdx < 0 {
		t.Fatalf("expected status bar to render, got %q", out)
	}
	if overlayIdx > statusIdx {
		t.Fatalf("expected prompt overlay above status bar, got %q", out)
	}
}

// TestProcessingShowsAnimatedSpinnerInContent verifies that while the turn is
// in flight but no content has streamed yet, the conversation area shows an
// animated "thinking" spinner so the user knows it isn't frozen.
func TestProcessingShowsAnimatedSpinnerInContent(t *testing.T) {
	s := styles.NewStyles(styles.GetTheme("dark"))
	model := New(100, 30, s, "zh")
	model.SetContent("历史消息")
	model.SetProcessing(true)
	model.statusAnim = 1

	out := stripANSIForTest(model.renderInlineLive())
	if out == "" {
		t.Fatalf("expected an animated spinner while processing with no live content, got empty")
	}
	// The spinner should carry a braille glyph and the localized thinking label.
	if !strings.ContainsAny(out[:min(len(out), 4)], "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏") {
		t.Fatalf("expected spinner glyph at start of inline live, got %q", out)
	}
}

// TestNoSpinnerWhenIdle verifies the spinner disappears once processing stops.
func TestNoSpinnerWhenIdle(t *testing.T) {
	s := styles.NewStyles(styles.GetTheme("dark"))
	model := New(100, 30, s, "zh")
	model.SetContent("历史消息")
	// Not processing, no live content → no inline live block.
	if got := model.renderInlineLive(); got != "" {
		t.Fatalf("expected no inline live when idle, got %q", got)
	}
}

func TestRightKeyAcceptsPredictionOnlyWhenInputEmpty(t *testing.T) {
	s := styles.NewStyles(styles.GetTheme("dark"))
	model := New(100, 30, s, "zh")
	model.SetPrediction("请继续展开这个方案")

	handled, _ := model.HandleKey(tea.KeyPressMsg{Code: tea.KeyRight})
	if !handled {
		t.Fatalf("expected right key to accept prediction")
	}
	if got := model.GetInputValue(); got != "请继续展开这个方案" {
		t.Fatalf("input=%q, want accepted prediction", got)
	}
	if model.HasPrediction() {
		t.Fatalf("expected prediction to clear after accept")
	}
}

func TestRightKeyAppendsPredictionSuffixForExistingInput(t *testing.T) {
	s := styles.NewStyles(styles.GetTheme("dark"))
	model := New(100, 30, s, "zh")
	model.SetInputValue("请继续")
	model.SetPrediction("请继续展开这个方案")

	handled, _ := model.HandleKey(tea.KeyPressMsg{Code: tea.KeyRight})
	if !handled {
		t.Fatalf("expected right key to accept suffix prediction")
	}
	if got := model.GetInputValue(); got != "请继续展开这个方案" {
		t.Fatalf("input=%q, want accepted prediction", got)
	}
}

func TestTabKeyAcceptsPredictionWhenHintsHidden(t *testing.T) {
	s := styles.NewStyles(styles.GetTheme("dark"))
	model := New(100, 30, s, "zh")
	model.SetInputValue("请继续")
	model.SetPrediction("请继续展开这个方案")

	handled, _ := model.HandleKey(tea.KeyPressMsg{Code: tea.KeyTab})
	if !handled {
		t.Fatalf("expected tab key to accept suffix prediction")
	}
	if got := model.GetInputValue(); got != "请继续展开这个方案" {
		t.Fatalf("input=%q, want accepted prediction", got)
	}
}

// TestEnterAcceptsSlashHintAndRestoresInputBoxPosition is a regression test
// for the bug where accepting a slash hint via Enter hid the hints panel
// but left the input box lifted (content area stayed shrunk) because the
// layout was not recomputed.
func TestEnterAcceptsSlashHintAndRestoresInputBoxPosition(t *testing.T) {
	s := styles.NewStyles(styles.GetTheme("dark"))
	model := New(100, 30, s, "zh")
	baseline := model.contentH

	// Typing "/" surfaces the slash command hints, which lifts the input box.
	model.SetInputValue("/")
	model.ShowSlashHints("")
	if !model.IsHintsVisible() {
		t.Fatalf("expected slash hints to be visible after ShowSlashHints")
	}
	if model.contentH >= baseline {
		t.Fatalf("expected content height to shrink while hints visible: got %d, baseline %d", model.contentH, baseline)
	}

	// Accepting the hint via Enter must hide the hints AND restore the layout.
	handled, _ := model.HandleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !handled {
		t.Fatalf("expected Enter to be handled while hints visible")
	}
	if model.IsHintsVisible() {
		t.Fatalf("expected hints to be hidden after Enter")
	}
	if model.contentH != baseline {
		t.Fatalf("expected content height to return to baseline %d after accepting hint, got %d", baseline, model.contentH)
	}
}

// TestEscDismissesSlashHintAndRestoresInputBoxPosition covers the Esc path,
// which must likewise recompute the layout so the input box drops back.
func TestEscDismissesSlashHintAndRestoresInputBoxPosition(t *testing.T) {
	s := styles.NewStyles(styles.GetTheme("dark"))
	model := New(100, 30, s, "zh")
	baseline := model.contentH

	model.SetInputValue("/")
	model.ShowSlashHints("")
	if !model.IsHintsVisible() {
		t.Fatalf("expected slash hints to be visible")
	}

	handled, _ := model.HandleKey(tea.KeyPressMsg{Code: tea.KeyEsc})
	if !handled {
		t.Fatalf("expected Esc to be handled while hints visible")
	}
	if model.IsHintsVisible() {
		t.Fatalf("expected hints to be hidden after Esc")
	}
	if model.contentH != baseline {
		t.Fatalf("expected content height to return to baseline %d after Esc, got %d", baseline, model.contentH)
	}
}

func TestRenderStatusBarShowsGitBranchWhenSet(t *testing.T) {
	s := styles.NewStyles(styles.GetTheme("dark"))
	model := New(100, 30, s, "zh")
	model.SetGitSummary("feat/memory", 0, 0)
	bar := stripANSIForTest(model.renderStatusBar())
	if !strings.Contains(bar, "⎇ feat/memory") {
		t.Fatalf("status bar should contain branch item, got: %s", bar)
	}
	model.SetGitSummary("", 0, 0)
	bar = stripANSIForTest(model.renderStatusBar())
	if strings.Contains(bar, "⎇") {
		t.Fatalf("branch item should be hidden when branch is empty, got: %s", bar)
	}
}

func TestRenderStatusBarShowsGitDirtyAndAheadMarkers(t *testing.T) {
	s := styles.NewStyles(styles.GetTheme("dark"))
	model := New(100, 30, s, "zh")
	model.SetGitSummary("main", 3, 2)
	bar := stripANSIForTest(model.renderStatusBar())
	if !strings.Contains(bar, "⎇ main ●3 ↑2") {
		t.Fatalf("status bar should contain dirty/ahead markers, got: %s", bar)
	}
	// 干净且同步：只有分支，无标记。
	model.SetGitSummary("main", 0, 0)
	bar = stripANSIForTest(model.renderStatusBar())
	if strings.Contains(bar, "●") || strings.Contains(bar, "↑") {
		t.Fatalf("clean synced repo should not show markers, got: %s", bar)
	}
	// 仅未推送：只有 ↑ 标记。
	model.SetGitSummary("main", 0, 4)
	bar = stripANSIForTest(model.renderStatusBar())
	if !strings.Contains(bar, "↑4") || strings.Contains(bar, "●") {
		t.Fatalf("ahead-only repo should show ↑4 only, got: %s", bar)
	}
}
