package panels

// Copyright (c) 2026 EOSAIOS
// SPDX-License-Identifier: EOS-NCL-1.1
// 本文件基于 EOS 非商用许可证 v1.1 发布，详见 LICENSE。
// 商业使用请联系版权人获得商业授权。

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eosaios/eos/internal/pkg/settings"
	"github.com/eosaios/eos/internal/ui/styles"

	tea "charm.land/bubbletea/v2"
)

func TestSettingsPanelAlwaysShowsDefaultPlanPromptStyle(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".eos", "settings.json")
	panel := NewSettingsPanel(styles.NewStyles(styles.DefaultDarkTheme()), settings.NewManager(path), "zh")

	panel.LoadSettings()

	for _, row := range panel.table.Rows() {
		if row[0] == "计划详略" {
			if row[1] != "concise" {
				t.Fatalf("计划详略 row=%q, want concise", row[1])
			}
			return
		}
	}
	t.Fatal("expected 计划详略 row")
}

func TestSettingsPanelShowsConfiguredPlanPromptStyle(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".eos", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir settings dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(`{"plan_prompt_style":"detailed"}`), 0o644); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	panel := NewSettingsPanel(styles.NewStyles(styles.DefaultDarkTheme()), settings.NewManager(path), "zh")

	panel.LoadSettings()

	for _, row := range panel.table.Rows() {
		if row[0] == "计划详略" {
			if row[1] != "detailed" {
				t.Fatalf("计划详略 row=%q, want detailed", row[1])
			}
			return
		}
	}
	t.Fatal("expected 计划详略 row")
}

func TestSettingsPanelHidesDevelopmentOnlyRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".eos", "settings.json")
	panel := NewSettingsPanel(styles.NewStyles(styles.DefaultDarkTheme()), settings.NewManager(path), "zh")

	panel.LoadSettings()

	hiddenRows := map[string]bool{
		"NextMessagePrediction(Global)": true,
		"PlanBubbleColor":               true,
		"LogDir":                        true,
		"WatchDebounceMs":               true,
		"PollIntervalSec":               true,
	}
	for _, row := range panel.table.Rows() {
		if hiddenRows[row[0]] {
			t.Fatalf("development-only row %q should not be visible", row[0])
		}
	}
}

func keyRunes(s string) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: rune(s[0]), Text: s}
}

// selectRow 把表格光标移到稳定字段 ID 对应的行
func selectRow(t *testing.T, panel *SettingsPanel, key string) {
	t.Helper()
	for i, rowKey := range panel.rowKeys {
		if rowKey == key {
			for panel.table.Cursor() != i {
				panel.Update(keyRunes("j"))
			}
			return
		}
	}
	t.Fatalf("row %q not found", key)
}

func TestSettingsPanelLanguageEditUsesChoiceMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".eos", "settings.json")
	panel := NewSettingsPanel(styles.NewStyles(styles.DefaultDarkTheme()), settings.NewManager(path), "zh")
	panel.LoadSettings()
	selectRow(t, panel, "Language")

	panel.Update(keyRunes("e"))
	if !panel.editMode || panel.editChoices == nil {
		t.Fatalf("语言应进入选择模式: editMode=%v choices=%v", panel.editMode, panel.editChoices)
	}
	if panel.editChoiceIndex != 0 {
		t.Fatalf("当前值 zh 应选中第 0 项，got %d", panel.editChoiceIndex)
	}

	panel.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	if panel.editChoiceIndex != 1 {
		t.Fatalf("right 应切到第 1 项，got %d", panel.editChoiceIndex)
	}
	panel.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if panel.editMode {
		t.Fatal("enter 后应退出编辑模式")
	}
	if panel.settings.Language != "en" {
		t.Fatalf("Language=%q, want en", panel.settings.Language)
	}
}

func TestSettingsPanelLanguageChoiceWrapsAround(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".eos", "settings.json")
	panel := NewSettingsPanel(styles.NewStyles(styles.DefaultDarkTheme()), settings.NewManager(path), "zh")
	panel.LoadSettings()
	selectRow(t, panel, "Language")

	panel.Update(keyRunes("e"))
	panel.Update(tea.KeyPressMsg{Code: tea.KeyLeft}) // zh → 回绕到 en
	if panel.editChoiceIndex != 1 {
		t.Fatalf("left 应从 zh 回绕到 en（第 1 项），got %d", panel.editChoiceIndex)
	}
	panel.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if panel.settings.Language != "en" {
		t.Fatalf("Language=%q, want en", panel.settings.Language)
	}
}

func TestSettingsPanelThemeEditUsesChoiceMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".eos", "settings.json")
	panel := NewSettingsPanel(styles.NewStyles(styles.DefaultDarkTheme()), settings.NewManager(path), "zh")
	panel.LoadSettings()
	selectRow(t, panel, "Theme")

	panel.Update(keyRunes("e"))
	if panel.editChoices == nil || len(panel.editChoices) != 3 {
		t.Fatalf("主题应有 3 个候选，got %v", panel.editChoices)
	}
	panel.Update(tea.KeyPressMsg{Code: tea.KeyRight}) // dark → light
	panel.Update(tea.KeyPressMsg{Code: tea.KeyRight}) // light → high-contrast
	panel.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if panel.settings.Theme != "high-contrast" {
		t.Fatalf("Theme=%q, want high-contrast", panel.settings.Theme)
	}
}

func TestSettingsPanelFreeTextFieldStillUsesInput(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".eos", "settings.json")
	panel := NewSettingsPanel(styles.NewStyles(styles.DefaultDarkTheme()), settings.NewManager(path), "zh")
	panel.LoadSettings()
	selectRow(t, panel, "MaxInjectKB")

	panel.Update(keyRunes("e"))
	if panel.editChoices != nil {
		t.Fatal("数值字段应走文本输入，不该有候选集")
	}
	panel.Update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl}) // 清空原值
	panel.Update(keyRunes("9"))
	panel.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if panel.settings.MaxInjectKB != 9 {
		t.Fatalf("MaxInjectKB=%d, want 9", panel.settings.MaxInjectKB)
	}
}
