package ui

// app_slash.go — 斜杠命令（/command）的分发与若干内置命令。
//
// 本文件包含：
//   - handleSlashCommand：命令分发（先查 slashCommandHandler 映射，再查别名）
//   - slashCommandHandler：命令名 → 处理函数的映射表
//   - initEOSMD：/init 生成 EOS.md 项目引导文件
//   - tryInvokeSkillSlash：把 /<skill> 形式当作 skill 调用交给内核
//   - handleHiddenLegalSlash：/_legal 输出版权/许可/版本信息
//
// 其他大量 /command 的具体实现（/model /plan /git ...）位于 slash_runtime.go。
// 代码原位于 app.go，仅做物理拆分，不改行为。

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/eosaios/eos/internal/config"
	"github.com/eosaios/eos/internal/i18n"
	"github.com/eosaios/eos/internal/ui/features/slash"
	"github.com/eosaios/eos/internal/ui/panels"
	"github.com/eosaios/eos/internal/version"

	tea "charm.land/bubbletea/v2"
)

// handleSlashCommand 处理斜杠命令
func (m *AppModel) handleSlashCommand(cmd string, args []string) tea.Cmd {
	handler, ok := slashCommandHandler(m)[cmd]
	if !ok {
		// 别名查找：commands.go 的 Aliases 字段
		for _, c := range slash.Commands {
			for _, alias := range c.Aliases {
				if alias == cmd && c.Name != cmd {
					handler, ok = slashCommandHandler(m)[c.Name]
					break
				}
			}
			if ok {
				break
			}
		}
	}
	if ok {
		return handler(args)
	}
	m.appendSystem(fmt.Sprintf("Unknown command: %s", cmd), "warning")
	return nil
}

// slashCommandHandler 构建命令名→处理函数的映射表。
// 新增命令只需在此 map 加一行，不再需要改 switch。
func slashCommandHandler(m *AppModel) map[string]func(args []string) tea.Cmd {
	return map[string]func(args []string) tea.Cmd{
		"/help": func(_ []string) tea.Cmd {
			m.clearPrediction()
			m.activeView = "help"
			if m.helpView != nil {
				m.helpView.ResetScroll()
			}
			return nil
		},
		"/clear": func(_ []string) tea.Cmd {
			m.shell.ClearContent()
			m.shell.ClearInput()
			m.shell.ClearLive()
			m.history = m.history[:0]
			m.actionHits = nil
			return nil
		},
		"/exit":           func(_ []string) tea.Cmd { return tea.Quit },
		"/init":           func(_ []string) tea.Cmd { m.shell.ClearInput(); return m.initEOSMD() },
		"/init-verifiers": m.handleInitVerifiersSlash,
		"/history": func(_ []string) tea.Cmd {
			m.clearPrediction()
			m.activeView = "panel"
			m.activePanel = "versions"
			m.shell.ClearInput()
			m.refreshVersionsPanel()
			return nil
		},
		"/model": m.handleModelSlash,
		"/mcp": func(_ []string) tea.Cmd {
			m.clearPrediction()
			m.activeView = "panel"
			m.activePanel = "mcp"
			m.shell.ClearInput()
			m.refreshMCPPanel()
			return nil
		},
		"/context": func(_ []string) tea.Cmd { m.openContextPanel(); return nil },
		"/memory": func(args []string) tea.Cmd {
			m.openMemoryPanel()
			if len(args) > 0 && strings.EqualFold(strings.TrimSpace(args[0]), "project") {
				if panel, ok := m.panels["memory"].(*panels.MemoryPanel); ok && panel != nil {
					panel.SelectProjectScope()
				}
			}
			return nil
		},
		"/cost": func(_ []string) tea.Cmd {
			m.clearPrediction()
			m.activeView = "panel"
			m.activePanel = "cost"
			m.shell.ClearInput()
			m.refreshCostPanel()
			return nil
		},
		"/tasks": func(_ []string) tea.Cmd {
			m.clearPrediction()
			m.activeView = "panel"
			m.activePanel = "tasks"
			m.shell.ClearInput()
			if panel, ok := m.panels["tasks"].(*panels.TasksPanel); ok && panel != nil {
				panel.ResetView()
				return panel.Init()
			}
			return nil
		},
		"/commit":     func(_ []string) tea.Cmd { return m.dispatchGitCommitRequest() },
		"/workspace":  m.handleWorkspaceSlash,
		"/goal":       m.handleGoalSlash,
		"/config":     func(_ []string) tea.Cmd { m.openSettingsPanel(); return nil },
		"/screenshot": m.handleScreenshotSlash,
		"/feedback":   m.handleFeedbackSlash,
		"/plugin":     func(args []string) tea.Cmd { return m.handlePluginSlash(args...) },
		"/lsp": func(_ []string) tea.Cmd {
			m.clearPrediction()
			m.activeView = "panel"
			m.activePanel = "lsp"
			m.shell.ClearInput()
			m.refreshLSPPanel()
			return nil
		},
		"/rules": func(_ []string) tea.Cmd {
			m.clearPrediction()
			m.activeView = "panel"
			m.activePanel = "rules"
			m.shell.ClearInput()
			m.refreshRulesPanel()
			return nil
		},
		"/lang": func(args []string) tea.Cmd {
			if len(args) > 0 {
				m.state.Language = args[0]
				if cfg, path := config.Load(); path != "" {
					cfg.Language = args[0]
					if err := config.Save(cfg, path); err != nil {
						m.appendSystem(fmt.Sprintf("Failed to save language config: %v", err), "error")
					}
				}
				m.appendSystem(fmt.Sprintf("Language changed to: %s", args[0]), "success")
				return func() tea.Msg { return panels.LanguageChangeMsg{Language: args[0]} }
			}
			return nil
		},
		"/compact": func(_ []string) tea.Cmd {
			if message, err := m.adapter.CompactContext(context.Background()); err != nil {
				m.appendSystem(fmt.Sprintf("%s: %v", m.localize("上下文压缩失败", "Context compact failed"), err), "error")
			} else if strings.TrimSpace(message) != "" {
				m.appendSystem(message, "success")
			} else {
				m.appendSystem(i18n.T("context.compacted", m.state.Language), "success")
			}
			m.refreshContextPanel()
			return nil
		},
		"/session":        m.handleSessionSlash,
		"/resume":         m.handleResumeSlash,
		"/permissions":    m.handlePermissionsSlash,
		"/skills":         m.handleSkillsSlash,
		"/reload-plugins": func(_ []string) tea.Cmd { return m.handleReloadPluginsSlash() },
		"/doctor":         func(_ []string) tea.Cmd { return m.handleDoctorSlash() },
		"/diff":           m.handleDiffSlash,
		"/review":         m.handleReviewSlash,
		"/verify":         m.handleVerifySlash,
		"/plan":           m.handlePlanSlash,
		"/plan-style":     m.handlePlanStyleSlash,
		"/git":            m.handleGitSlash,
		"/remote":         m.handleRemoteSlash,
		"/status":         func(_ []string) tea.Cmd { return m.handleStatusSlash() },
		"/fast":           func(_ []string) tea.Cmd { return m.handleFastSlash() },
		"/export":         m.handleExportSlash,
		"/theme":          m.handleThemeSlash,
		"/stats":          func(_ []string) tea.Cmd { return m.handleStatsSlash() },
		"/rename":         m.handleRenameSlash,
		"/share":          func(_ []string) tea.Cmd { return m.handleShareSlash() },
		"/_legal":         func(_ []string) tea.Cmd { return m.handleHiddenLegalSlash() },
	}
}

func (m *AppModel) initEOSMD() tea.Cmd {
	root := ""
	if m != nil && m.adapter != nil {
		root = strings.TrimSpace(m.adapter.ActiveWorkspace(context.Background()))
	}
	if root == "" {
		wd, err := os.Getwd()
		if err != nil {
			m.appendSystem(fmt.Sprintf(i18n.T("toast.init_failed", m.state.Language), err), "error")
			return nil
		}
		root = wd
	}

	dst := filepath.Join(root, "EOS.md")
	existing := ""
	existed := false
	if raw, err := os.ReadFile(dst); err == nil {
		existed = true
		existing = string(raw)
	}

	template := strings.TrimRight(`# EOS.md

This file provides guidance to EOS when working with code in this repository.

## Project Context

- What this project does:
- Target users:
- Key constraints (performance/security/platform):

## How To Work

- When changing behavior, add/adjust tests when possible.
- Prefer minimal, focused diffs over broad refactors.
- Keep user-facing text consistent with UI language (zh/en).

## Build and Development Commands

`+"```bash"+`
go test ./...
go build -o eos
`+"```"+`

## Repository Map

- UI: internal/ui/ (TUI 基于 Bubble Tea)
- Engine: pkg/coreapi/sidecar/ (通过 JSON-RPC 调用 Rust 内核 eos-core)
- CLI: internal/cli/ (cobra 子命令)
- Config: internal/config/

## Coding Style

- Follow existing patterns and naming.
- Avoid introducing new dependencies unless necessary.
- Don’t log secrets/keys.
`, "\n") + "\n"

	mergeEOS := func(old string) string {
		s := strings.TrimSpace(old)
		if s == "" {
			return template
		}
		s = strings.Replace(s, "# CLAUDE.md", "# EOS.md", 1)
		s = strings.Replace(s, "Claude Code (claude.ai/code)", "EOS", 1)
		s = strings.Replace(s, "guidance to Claude Code", "guidance to EOS", 1)
		if !strings.HasPrefix(strings.TrimSpace(s), "# EOS.md") {
			s = "# EOS.md\n\n" + strings.TrimLeft(s, "\n")
		}
		required := []struct {
			heading string
			block   string
		}{
			{"## Project Context", "## Project Context\n\n- What this project does:\n- Target users:\n- Key constraints (performance/security/platform):\n"},
			{"## How To Work", "## How To Work\n\n- When changing behavior, add/adjust tests when possible.\n- Prefer minimal, focused diffs over broad refactors.\n- Keep user-facing text consistent with UI language (zh/en).\n"},
			{"## Build and Development Commands", "## Build and Development Commands\n\n```bash\ngo test ./...\ngo build -o eos\n```\n"},
			{"## Repository Map", "## Repository Map\n\n- UI: internal/ui/\n- Bridge: internal/bridge/\n- Runtime: internal/runtime/\n- Tools: internal/tools/\n"},
			{"## Coding Style", "## Coding Style\n\n- Follow existing patterns and naming.\n- Avoid introducing new dependencies unless necessary.\n- Don’t log secrets/keys.\n"},
		}
		for _, it := range required {
			if strings.Contains(s, "\n"+it.heading+"\n") || strings.HasPrefix(strings.TrimSpace(s), it.heading+"\n") {
				continue
			}
			s = strings.TrimRight(s, "\n") + "\n\n" + strings.TrimRight(it.block, "\n") + "\n"
		}
		return strings.TrimRight(s, "\n") + "\n"
	}

	content := mergeEOS(existing)

	if err := os.WriteFile(dst, []byte(content), 0o644); err != nil {
		m.appendSystem(fmt.Sprintf(i18n.T("toast.eosmd_write_failed", m.state.Language), err), "error")
		return nil
	}
	_ = m.adapter.PinContextDocument(context.Background(), "EOS.md", content, 20000)
	if existed {
		m.appendSystem(i18n.T("eosmd.updated", m.state.Language), "success")
	} else {
		m.appendSystem(i18n.T("eosmd.generated", m.state.Language), "success")
	}
	return nil
}

func (m *AppModel) tryInvokeSkillSlash(skillName string, args []string) bool {
	arguments := strings.TrimSpace(strings.Join(args, " "))
	invoked, err := m.adapter.InvokeSkill(context.Background(), skillName, arguments)
	if err != nil {
		m.appendSystem(fmt.Sprintf(i18n.T("toast.skill_enable_failed", m.state.Language), err), "error")
		return true
	}
	if !invoked {
		return false
	}
	m.appendSystem(i18n.T("skill.enabled", m.state.Language)+skillName, "success")
	return true
}

func (m *AppModel) handleHiddenLegalSlash() tea.Cmd {
	m.appendSystem("Copyright (c) 2026 EOSAIOS", "info")
	m.appendSystem("License: EOS Non-Commercial License v1.1 (EOS-NCL-1.1)", "info")
	m.appendSystem("SPDX-License-Identifier: EOS-NCL-1.1", "info")
	m.appendSystem("Contact: legal@eosaios.com", "info")
	m.appendSystem(fmt.Sprintf("Version: %s | Commit: %s | Build: %s", version.AppVersion, version.BuildCommit, version.BuildDate), "info")
	return nil
}
