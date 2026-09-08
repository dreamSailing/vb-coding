package input

// Copyright (c) 2026 EOSAIOS
// SPDX-License-Identifier: EOS-NCL-1.1
// 本文件基于 EOS 非商用许可证 v1.1 发布，详见 LICENSE。
// 商业使用请联系版权人获得商业授权。

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func keyRunesMsg(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: r, Text: string(r)}
}

func TestHistoryDraftRestore(t *testing.T) {
	m := New()
	m.SetSize(40, 0)
	m.SetHistory([]string{"prev"})
	m.SetValue("draft")

	m.HistoryUp()
	if got := m.Value(); got != "prev" {
		t.Fatalf("expected prev, got %q", got)
	}

	m.HistoryDown()
	if got := m.Value(); got != "draft" {
		t.Fatalf("expected draft restored, got %q", got)
	}
}

func TestIsMultiLine(t *testing.T) {
	m := New()
	m.SetSize(10, 0)
	m.SetValue("a\nb\nc")
	if !m.IsMultiLine() {
		t.Fatalf("expected multiline for newline text")
	}

	m.Clear()
	m.SetSize(6, 0) // textarea width becomes 4
	m.SetValue("12345")
	if !m.IsMultiLine() {
		t.Fatalf("expected multiline for wrapped text")
	}
}

func TestAcceptPredictionPromotesPlaceholderToValue(t *testing.T) {
	m := New()
	m.SetSize(40, 0)
	m.SetPlaceholder("base")
	m.SetPrediction("下一句预测")

	if !m.HasPrediction() {
		t.Fatalf("expected prediction to exist")
	}
	if !m.AcceptPrediction() {
		t.Fatalf("expected accept prediction to succeed")
	}
	if got := m.Value(); got != "下一句预测" {
		t.Fatalf("value=%q, want 下一句预测", got)
	}
	if m.HasPrediction() {
		t.Fatalf("expected prediction to clear after accept")
	}
}

func TestTypingClearsPrediction(t *testing.T) {
	m := New()
	m.SetSize(40, 0)
	m.SetPrediction("你好吗")

	updated, _ := m.Update(keyRunesMsg('你'))
	if !updated.HasPrediction() {
		t.Fatalf("expected prediction to remain when typed text matches prefix")
	}
	if got := updated.PredictionSuffix(); got != "好吗" {
		t.Fatalf("suffix=%q, want 好吗", got)
	}
	if got := updated.Value(); got != "你" {
		t.Fatalf("value=%q, want 你", got)
	}
}

func TestAcceptPredictionAppendsSuffixForExistingInput(t *testing.T) {
	m := New()
	m.SetSize(40, 0)
	m.SetValue("你")
	m.SetPrediction("你好吗")

	if !m.CanAcceptPrediction() {
		t.Fatalf("expected suffix prediction to be acceptable")
	}
	if got := m.PredictionSuffix(); got != "好吗" {
		t.Fatalf("suffix=%q, want 好吗", got)
	}
	if !m.AcceptPrediction() {
		t.Fatalf("expected accept prediction to append suffix")
	}
	if got := m.Value(); got != "你好吗" {
		t.Fatalf("value=%q, want 你好吗", got)
	}
	if m.HasPrediction() {
		t.Fatalf("expected prediction to clear after accept")
	}
}

func TestTypingClearsPredictionWhenPrefixNoLongerMatches(t *testing.T) {
	m := New()
	m.SetSize(40, 0)
	m.SetPrediction("你好吗")

	updated, _ := m.Update(keyRunesMsg('他'))
	if updated.HasPrediction() {
		t.Fatalf("expected prediction to clear after typing")
	}
	if got := updated.Value(); got != "他" {
		t.Fatalf("value=%q, want 他", got)
	}
}

func TestClearRemovesPrediction(t *testing.T) {
	m := New()
	m.SetSize(40, 0)
	m.SetPrediction("下一句预测")

	m.Clear()
	if m.HasPrediction() {
		t.Fatalf("expected prediction to clear")
	}
}
