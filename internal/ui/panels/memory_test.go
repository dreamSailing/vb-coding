package panels

// Copyright (c) 2026 EOSAIOS
// SPDX-License-Identifier: EOS-NCL-1.1
// 本文件基于 EOS 非商用许可证 v1.1 发布，详见 LICENSE。
// 商业使用请联系版权人获得商业授权。

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/eosaios/eos/internal/ui/styles"
)

func newTestMemoryPanel() *MemoryPanel {
	return NewMemoryPanel(styles.NewStyles(styles.DefaultDarkTheme()), "zh")
}

func TestMemoryPanelShowsSummaryAndHandbookTabs(t *testing.T) {
	panel := newTestMemoryPanel()
	panel.SetSize(80, 24)
	panel.SetData([]MemoryDoc{
		{Scope: "memory_summary.md", Path: "C:/home/.eos/memories/memory_summary.md", Content: "user prefers tabs", Exists: true},
		{Scope: "MEMORY.md", Path: "C:/home/.eos/memories/MEMORY.md", Content: "# handbook", Exists: true},
	}, nil)

	view := panel.View()
	if !strings.Contains(view, "memory_summary.md") || !strings.Contains(view, "MEMORY.md") {
		t.Fatalf("view missing doc tabs:\n%s", view)
	}
	if !strings.Contains(view, "user prefers tabs") {
		t.Fatalf("view missing summary content:\n%s", view)
	}

	updated, _ := panel.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	view = updated.(*MemoryPanel).View()
	if !strings.Contains(view, "# handbook") {
		t.Fatalf("view missing handbook content after tab switch:\n%s", view)
	}
}

func TestMemoryPanelMissingDocShowsEmptyState(t *testing.T) {
	panel := newTestMemoryPanel()
	panel.SetSize(80, 24)
	panel.SetData(nil, nil)

	view := panel.View()
	if !strings.Contains(view, "记忆尚未生成") {
		t.Fatalf("view missing empty-state hint:\n%s", view)
	}
}

func TestMemoryPanelComposeNoteEmitsSaveMsg(t *testing.T) {
	panel := newTestMemoryPanel()
	panel.SetSize(80, 24)

	updated, _ := panel.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	panel = updated.(*MemoryPanel)
	if !panel.IsEditing() {
		t.Fatal("expected composing mode after 'a'")
	}

	panel.editor.SetValue("remember prefer tabs")
	updated, cmd := panel.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	panel = updated.(*MemoryPanel)
	if panel.IsEditing() {
		t.Fatal("expected composing mode to exit after ctrl+s")
	}
	if cmd == nil {
		t.Fatal("expected save cmd")
	}
	msg := cmd()
	save, ok := msg.(MemorySaveMsg)
	if !ok {
		t.Fatalf("cmd() = %T, want MemorySaveMsg", msg)
	}
	if save.Content != "remember prefer tabs" {
		t.Fatalf("save content = %q", save.Content)
	}
}

func TestMemoryPanelRefreshKeyEmitsRefreshMsg(t *testing.T) {
	panel := newTestMemoryPanel()
	_, cmd := panel.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	if cmd == nil {
		t.Fatal("expected refresh cmd")
	}
	if _, ok := cmd().(MemoryRefreshMsg); !ok {
		t.Fatalf("cmd() = %T, want MemoryRefreshMsg", cmd())
	}
}

func TestMemoryPanelProjectScopeSwitch(t *testing.T) {
	panel := newTestMemoryPanel()
	panel.SetSize(80, 24)
	panel.SetData(
		[]MemoryDoc{
			{Scope: "memory_summary.md", Path: "/h/.eos/memories/memory_summary.md", Content: "global facts", Exists: true},
			{Scope: "MEMORY.md", Path: "/h/.eos/memories/MEMORY.md", Content: "# global handbook", Exists: true},
		},
		&MemoryProject{
			Key:  "abc",
			Root: "/work/eos",
			Name: "eos",
			Docs: [2]MemoryDoc{
				{Scope: "memory_summary.md", Path: "/h/.eos/memories/projects/abc/memory_summary.md", Content: "project facts", Exists: true},
				{Scope: "MEMORY.md", Path: "/h/.eos/memories/projects/abc/MEMORY.md", Content: "# project handbook", Exists: true},
			},
		},
	)

	// 默认全局：显示全局内容 + 项目作用域入口。
	view := panel.View()
	if !strings.Contains(view, "global facts") {
		t.Fatalf("view should show global scope by default:\n%s", view)
	}
	if !strings.Contains(view, "eos") {
		t.Fatalf("view should list project scope entry:\n%s", view)
	}

	// 按 2 切到项目分区。
	updated, _ := panel.Update(tea.KeyPressMsg{Code: '2', Text: "2"})
	p := updated.(*MemoryPanel)
	view = p.View()
	if !strings.Contains(view, "project facts") {
		t.Fatalf("view should show project content after switching:\n%s", view)
	}
	// Tab 在项目分区的两个文档间切换。
	updated, _ = p.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if view := updated.(*MemoryPanel).View(); !strings.Contains(view, "# project handbook") {
		t.Fatalf("tab should cycle project docs:\n%s", view)
	}

	// 笔记保存携带当前作用域。
	p.composing = true
	p.editor.SetValue("note")
	updated, cmd := p.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	save, ok := cmd().(MemorySaveMsg)
	if !ok || save.Scope != "project" || save.Content != "note" {
		t.Fatalf("save msg should carry project scope, got %+v ok=%v", save, ok)
	}
	_ = updated

	// 按 1 切回全局。
	updated, _ = p.Update(tea.KeyPressMsg{Code: '1', Text: "1"})
	if view := updated.(*MemoryPanel).View(); !strings.Contains(view, "global facts") {
		t.Fatalf("view should return to global scope:\n%s", view)
	}
}
