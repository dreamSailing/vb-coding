package panels

// Copyright (c) 2026 EOSAIOS
// SPDX-License-Identifier: EOS-NCL-1.1
// 本文件基于 EOS 非商用许可证 v1.1 发布，详见 LICENSE。
// 商业使用请联系版权人获得商业授权。

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/eosaios/eos/internal/config"
	"github.com/eosaios/eos/internal/ui/styles"
)

func newTestModelsPanel() *ModelsPanel {
	return NewModelsPanel(styles.NewStyles(styles.DefaultDarkTheme()), "zh")
}

// runKey 向面板发单个 KeyMsg，返回 cmd 执行出的消息与面板（用于断言）。
func runKey(t *testing.T, p *ModelsPanel, key string) (Panel, tea.Msg) {
	t.Helper()
	p2, cmd := p.Update(tea.KeyPressMsg{Code: rune(key[0]), Text: key})
	p = p2.(*ModelsPanel)
	if cmd == nil {
		return p, nil
	}
	return p, cmd()
}

func TestModelsPanelShortcutKeys(t *testing.T) {
	panel := newTestModelsPanel()
	panel.SetModels([]config.ModelEntry{
		{Name: "MiniMax M3", Source: "user", APIBase: "https://api.minimaxi.com/v1", Model: "MiniMax-M3"},
		{Name: "GLM-5.2", Source: "user", APIBase: "https://ark.cn-beijing.volces.com", Model: "glm-5.2"},
	}, "MiniMax M3")

	// u：使用实际选中的模型
	_, msg := runKey(t, panel, "u")
	if _, ok := msg.(ModelSelectMsg); !ok {
		t.Fatalf("u -> got %T, want ModelSelectMsg", msg)
	}

	// a：添加
	panel = newTestModelsPanel()
	panel.SetModels([]config.ModelEntry{{Name: "MiniMax M3", Model: "MiniMax-M3"}}, "MiniMax M3")
	_, msg = runKey(t, panel, "a")
	if _, ok := msg.(ModelAddMsg); !ok {
		t.Fatalf("a -> got %T, want ModelAddMsg", msg)
	}

	// e：编辑
	panel = newTestModelsPanel()
	panel.SetModels([]config.ModelEntry{{Name: "MiniMax M3", Model: "MiniMax-M3"}}, "MiniMax M3")
	_, msg = runKey(t, panel, "e")
	if editMsg, ok := msg.(ModelEditMsg); !ok {
		t.Fatalf("e -> got %T, want ModelEditMsg", msg)
	} else if editMsg.Name != "MiniMax M3" {
		t.Fatalf("e -> Name = %q, want MiniMax M3", editMsg.Name)
	}

	// d：删除
	panel = newTestModelsPanel()
	panel.SetModels([]config.ModelEntry{{Name: "MiniMax M3", Model: "MiniMax-M3"}}, "MiniMax M3")
	_, msg = runKey(t, panel, "d")
	if _, ok := msg.(ModelDeleteMsg); !ok {
		t.Fatalf("d -> got %T, want ModelDeleteMsg", msg)
	}

	// r：刷新
	panel = newTestModelsPanel()
	panel.SetModels([]config.ModelEntry{{Name: "MiniMax M3", Model: "MiniMax-M3"}}, "MiniMax M3")
	_, msg = runKey(t, panel, "r")
	if _, ok := msg.(ModelRefreshMsg); !ok {
		t.Fatalf("r -> got %T, want ModelRefreshMsg", msg)
	}

	// s：同步（回归：help 声称 S: sync，此前无 case 分支）
	panel = newTestModelsPanel()
	panel.SetModels([]config.ModelEntry{{Name: "MiniMax M3", Model: "MiniMax-M3"}}, "MiniMax M3")
	_, msg = runKey(t, panel, "s")
	if _, ok := msg.(ModelSyncMsg); !ok {
		t.Fatalf("s -> got %T, want ModelSyncMsg", msg)
	}
	if p := panel.GetCurrentAction(); p != "Use" {
		t.Fatalf("actionOps = %q, want first Use with Refresh included", p)
	}
}

// TestModelsPanelActionBarNoEmptyOption 回归：actionOps 的每一项都必须有 i18n 标签。
// 此前 View 的映射缺少 Refresh case，操作栏渲染出空 [] 选项，→ 键会落到空项上。
func TestModelsPanelActionBarNoEmptyOption(t *testing.T) {
	panel := newTestModelsPanel()
	panel.SetModels([]config.ModelEntry{{Name: "MiniMax M3", Model: "MiniMax-M3"}}, "MiniMax M3")

	view := panel.View()
	if strings.Contains(view, "[]") {
		t.Fatalf("action bar renders an empty option:\n%s", view)
	}
	for _, label := range []string{"使用", "套餐模型", "新增", "编辑", "删除", "同步环境变量", "刷新"} {
		if !strings.Contains(view, label) {
			t.Fatalf("action bar missing label %q:\n%s", label, view)
		}
	}

	// → 从最后一项（Refresh）再按一次应直接回到第一项（Use），中间不能有空项。
	for range len(panel.actionOps) {
		if _, cmd := panel.Update(tea.KeyPressMsg{Code: tea.KeyRight}); cmd != nil {
			cmd()
		}
	}
	if got := panel.GetCurrentAction(); got != "Use" {
		t.Fatalf("after wrapping right from last op, action = %q, want Use", got)
	}
}
