package setup

// Copyright (c) 2026 EOSAIOS
// SPDX-License-Identifier: EOS-NCL-1.1
// 本文件基于 EOS 非商用许可证 v1.1 发布，详见 LICENSE。
// 商业使用请联系版权人获得商业授权。

import (
	"testing"

	"github.com/eosaios/eos/internal/ai"
	"github.com/eosaios/eos/internal/config"
	"github.com/eosaios/eos/internal/ui/styles"
	"github.com/eosaios/eos/pkg/coreapi"

	tea "charm.land/bubbletea/v2"
)

// newPlanConfigWizard 构造处于配置步骤、选中套餐类 preset（两个可选模型）的向导。
func newPlanConfigWizard() *ModelSetupView {
	v := NewModelSetupWizard(styles.NewStyles(styles.DefaultDarkTheme()), "zh")
	model := &ai.ModelCatalogEntry{
		ID:        "test-plan",
		Name:      "Test Plan",
		ModelName: "model-a",
		PlanModels: []coreapi.PlanModel{
			{ModelID: "model-a", Label: "A"},
			{ModelID: "model-b", Label: "B"},
		},
	}
	v.step = ModelSetupStepConfig
	v.selectedModel = model
	v.planIndex = 0
	v.modelReadOnly = false
	v.apiBaseReadOnly = true
	v.inputs[3].SetValue("model-a")
	v.focusInput(0)
	return v
}

// assertTabSequence 逐个按键后校验 focusIndex 轨迹。
func assertTabSequence(t *testing.T, v *ModelSetupView, key tea.KeyPressMsg, want []int) {
	t.Helper()
	for _, w := range want {
		v.Update(key)
		if v.focusIndex != w {
			t.Fatalf("after %s, focusIndex = %d, want %d", key.String(), v.focusIndex, w)
		}
	}
}

// 套餐类配置步骤：Tab 显示名(0)→API Key(2)→模型选择(3)后回绕到显示名(0)，
// Shift+Tab 反向同样回绕。回归：旧实现在模型选择处吞掉 Tab 导致焦点卡死。
func TestPlanConfigFocusCycles(t *testing.T) {
	v := newPlanConfigWizard()
	assertTabSequence(t, v, tea.KeyPressMsg{Code: tea.KeyTab}, []int{2, 3, 0})
	assertTabSequence(t, v, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}, []int{3, 2, 0})
}

// 套餐内模型选择器：←/→ 切换模型并同步到表单值（handleSave 读 inputs[3]）。
func TestPlanPickerCyclesPlanModels(t *testing.T) {
	v := newPlanConfigWizard()
	v.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	v.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if v.focusIndex != 3 {
		t.Fatalf("focusIndex = %d, want 3 (plan picker)", v.focusIndex)
	}

	v.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	if got := v.inputs[3].Value(); got != "model-b" {
		t.Fatalf("after right, model = %q, want model-b", got)
	}
	v.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	if got := v.inputs[3].Value(); got != "model-a" {
		t.Fatalf("after left, model = %q, want model-a", got)
	}
}

// 自定义服务商：四个字段全部参与循环（含 API Base）。
func TestCustomProviderFocusCycles(t *testing.T) {
	v := NewModelSetupWizard(styles.NewStyles(styles.DefaultDarkTheme()), "zh")
	v.step = ModelSetupStepCustom
	v.customProvider = true
	v.focusInput(0)

	assertTabSequence(t, v, tea.KeyPressMsg{Code: tea.KeyTab}, []int{1, 2, 3, 0})
	assertTabSequence(t, v, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}, []int{3, 2, 1, 0})
}

// 自定义模型：API Base 只读，焦点在 显示名(0)→API Key(2)→模型名(3) 循环。
func TestCustomModelFocusSkipsAPIBase(t *testing.T) {
	v := NewModelSetupWizard(styles.NewStyles(styles.DefaultDarkTheme()), "zh")
	v.step = ModelSetupStepConfig
	v.customModel = true
	v.modelReadOnly = false
	v.apiBaseReadOnly = true
	v.focusInput(0)

	assertTabSequence(t, v, tea.KeyPressMsg{Code: tea.KeyTab}, []int{2, 3, 0})
	assertTabSequence(t, v, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}, []int{3, 2, 0})
}

// 普通 preset：模型只读，焦点在 显示名(0)↔API Key(2) 循环。
func TestFixedPresetFocusCycles(t *testing.T) {
	v := NewModelSetupWizard(styles.NewStyles(styles.DefaultDarkTheme()), "zh")
	v.step = ModelSetupStepConfig
	v.selectedModel = &ai.ModelCatalogEntry{ID: "fixed", Name: "Fixed", ModelName: "fixed-model"}
	v.modelReadOnly = true
	v.apiBaseReadOnly = true
	v.focusInput(0)

	assertTabSequence(t, v, tea.KeyPressMsg{Code: tea.KeyTab}, []int{2, 0})
	assertTabSequence(t, v, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}, []int{2, 0})
}

// 编辑模式：显示名只读，焦点在 API Base(1)→API Key(2)→模型名(3) 循环。
func TestEditModeFocusSkipsDisplayName(t *testing.T) {
	v := NewModelSetupWizard(styles.NewStyles(styles.DefaultDarkTheme()), "zh")
	v.LoadForEdit(config.ModelEntry{
		Name:    "demo",
		APIBase: "https://example.com/v1",
		Model:   "demo-model",
	})
	if v.focusIndex != 1 {
		t.Fatalf("focusIndex = %d, want 1 (api base)", v.focusIndex)
	}
	assertTabSequence(t, v, tea.KeyPressMsg{Code: tea.KeyTab}, []int{2, 3, 1})
	assertTabSequence(t, v, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}, []int{3, 2, 1})
}

// 编辑模式保存：固定原始条目名并标记 EditMode。
func TestEditModeSaveUsesOriginalName(t *testing.T) {
	v := NewModelSetupWizard(styles.NewStyles(styles.DefaultDarkTheme()), "zh")
	v.LoadForEdit(config.ModelEntry{
		Name:    "demo",
		APIBase: "https://example.com/v1",
		Model:   "demo-model",
	})
	v.inputs[1].SetValue("https://new.example.com/v1")
	v.inputs[3].SetValue("new-model")
	cmd := v.handleSave()
	if cmd == nil {
		t.Fatal("handleSave() = nil")
	}
	msg := cmd()
	complete, ok := msg.(ModelFormCompleteMsg)
	if !ok {
		t.Fatalf("handleSave -> %T, want ModelFormCompleteMsg", msg)
	}
	if !complete.EditMode {
		t.Fatal("EditMode = false, want true")
	}
	if complete.Config.Name != "demo" {
		t.Fatalf("Name = %q, want demo", complete.Config.Name)
	}
	if complete.Config.APIBase != "https://new.example.com/v1" {
		t.Fatalf("APIBase = %q", complete.Config.APIBase)
	}
	if complete.Config.Model != "new-model" {
		t.Fatalf("Model = %q", complete.Config.Model)
	}
}
