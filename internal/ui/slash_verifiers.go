package ui

// Copyright (c) 2026 EOSAIOS
// SPDX-License-Identifier: EOS-NCL-1.1
// 本文件基于 EOS 非商用许可证 v1.1 发布，详见 LICENSE。
// 商业使用请联系版权人获得商业授权。

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
)

type verifierProjectType string

const (
	verifierProjectWeb verifierProjectType = "web"
	verifierProjectCLI verifierProjectType = "cli"
	verifierProjectAPI verifierProjectType = "api"
)

type verifierProjectDetection struct {
	Type     verifierProjectType
	Evidence []string
}

type verifierToolSuggestion struct {
	Tool     string
	ReasonZH string
	ReasonEN string
}

func (m *AppModel) handleInitVerifiersSlash(args []string) tea.Cmd {
	if len(args) > 0 {
		m.appendSystem(
			m.localize("用法: /init-verifiers", "Usage: /init-verifiers"),
			"warning",
		)
		return nil
	}

	root := strings.TrimSpace(m.currentWorkspaceRoot())
	if root == "" {
		m.appendSystem(
			m.localize("未找到可检测的工作区目录。", "No workspace directory available for detection."),
			"error",
		)
		return nil
	}

	detections, err := detectVerifierProjectTypes(root)
	if err != nil {
		m.appendSystem(
			fmt.Sprintf("%s: %v", m.localize("检测验证工具失败", "Failed to detect verifier tools"), err),
			"error",
		)
		return nil
	}

	lines := []string{
		fmt.Sprintf("%s: %s", m.localize("检测目录", "Workspace"), root),
	}

	if len(detections) == 0 {
		lines = append(lines,
			m.localize(
				"未检测到明确的 Web/CLI/API 特征。可按目标流程手动选择 Playwright、Tmux 或 HTTP 验证。",
				"No clear Web/CLI/API markers found. Choose Playwright, Tmux, or HTTP based on your target workflow.",
			),
		)
		m.appendSystem(strings.Join(lines, "\n"), "info")
		return nil
	}

	typeNames := make([]string, 0, len(detections))
	for _, detection := range detections {
		typeNames = append(typeNames, verifierProjectTypeLabel(detection.Type, m.state.Language))
	}
	lines = append(lines,
		fmt.Sprintf("%s: %s", m.localize("项目类型", "Detected project types"), strings.Join(typeNames, ", ")),
		m.localize("检测依据：", "Detection signals:"),
	)
	for _, detection := range detections {
		lines = append(lines,
			fmt.Sprintf("- %s: %s",
				verifierProjectTypeLabel(detection.Type, m.state.Language),
				strings.Join(limitSignals(detection.Evidence, 3), ", "),
			),
		)
	}

	lines = append(lines, m.localize("建议验证工具：", "Suggested verifier tools:"))
	for _, suggestion := range verifierToolSuggestions(detections) {
		lines = append(lines,
			fmt.Sprintf("- %s: %s", suggestion.Tool, localizeSuggestion(suggestion, m.state.Language)),
		)
	}

	m.appendSystem(strings.Join(lines, "\n"), "info")
	return nil
}

func detectVerifierProjectTypes(root string) ([]verifierProjectDetection, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("not a directory: %s", root)
	}

	detections := make([]verifierProjectDetection, 0, 3)
	if evidence := existingRelativePaths(root,
		"frontend/package.json",
		"frontend/src",
		"vite.config.ts",
		"vite.config.js",
		"vite.config.mjs",
		"next.config.js",
		"next.config.mjs",
		"next.config.ts",
		"nuxt.config.ts",
		"nuxt.config.js",
		"src/main.tsx",
		"src/main.jsx",
		"src/App.tsx",
		"src/App.jsx",
		"public/index.html",
	); len(evidence) > 0 {
		detections = append(detections, verifierProjectDetection{
			Type:     verifierProjectWeb,
			Evidence: evidence,
		})
	}

	if evidence := existingRelativePaths(root,
		"cmd",
		"internal/cli",
		"internal/ui",
		"main.go",
		"main.rs",
		"main.py",
		"main.ts",
		"main.js",
	); len(evidence) > 0 {
		detections = append(detections, verifierProjectDetection{
			Type:     verifierProjectCLI,
			Evidence: evidence,
		})
	}

	if evidence := existingRelativePaths(root,
		"api",
		"internal/api",
		"internal/gateway",
		"internal/serve",
		"server",
		"routes",
		"server.go",
		"routes.go",
		"openapi.yaml",
		"openapi.yml",
		"openapi.json",
		"swagger.yaml",
		"swagger.yml",
		"swagger.json",
	); len(evidence) > 0 {
		detections = append(detections, verifierProjectDetection{
			Type:     verifierProjectAPI,
			Evidence: evidence,
		})
	}

	sort.SliceStable(detections, func(i, j int) bool {
		return verifierProjectTypeOrder(detections[i].Type) < verifierProjectTypeOrder(detections[j].Type)
	})
	return detections, nil
}

func existingRelativePaths(root string, rels ...string) []string {
	out := make([]string, 0, len(rels))
	seen := make(map[string]struct{}, len(rels))
	for _, rel := range rels {
		rel = strings.TrimSpace(rel)
		if rel == "" {
			continue
		}
		if _, ok := seen[rel]; ok {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err == nil {
			out = append(out, rel)
			seen[rel] = struct{}{}
		}
	}
	return out
}

func verifierProjectTypeOrder(kind verifierProjectType) int {
	switch kind {
	case verifierProjectWeb:
		return 0
	case verifierProjectCLI:
		return 1
	case verifierProjectAPI:
		return 2
	default:
		return 99
	}
}

func verifierProjectTypeLabel(kind verifierProjectType, lang string) string {
	switch kind {
	case verifierProjectWeb:
		if strings.EqualFold(lang, "en") {
			return "Web"
		}
		return "Web"
	case verifierProjectCLI:
		if strings.EqualFold(lang, "en") {
			return "CLI"
		}
		return "CLI"
	case verifierProjectAPI:
		if strings.EqualFold(lang, "en") {
			return "API"
		}
		return "API"
	default:
		return strings.ToUpper(string(kind))
	}
}

func verifierToolSuggestions(detections []verifierProjectDetection) []verifierToolSuggestion {
	suggestions := make([]verifierToolSuggestion, 0, len(detections))
	seen := make(map[string]struct{}, len(detections))
	for _, detection := range detections {
		var suggestion verifierToolSuggestion
		switch detection.Type {
		case verifierProjectWeb:
			suggestion = verifierToolSuggestion{
				Tool:     "Playwright",
				ReasonZH: "适合浏览器 UI、页面交互和截图回归验证。",
				ReasonEN: "Best for browser UI flows, page interactions, and screenshot regression checks.",
			}
		case verifierProjectCLI:
			suggestion = verifierToolSuggestion{
				Tool:     "Tmux",
				ReasonZH: "适合命令行交互、长时间运行任务和日志观察。",
				ReasonEN: "Best for terminal interactions, long-running tasks, and live log observation.",
			}
		case verifierProjectAPI:
			suggestion = verifierToolSuggestion{
				Tool:     "HTTP",
				ReasonZH: "适合接口联调、健康检查和请求回归验证。",
				ReasonEN: "Best for API regression, health checks, and request-level verification.",
			}
		default:
			continue
		}
		if _, ok := seen[suggestion.Tool]; ok {
			continue
		}
		seen[suggestion.Tool] = struct{}{}
		suggestions = append(suggestions, suggestion)
	}
	return suggestions
}

func localizeSuggestion(item verifierToolSuggestion, lang string) string {
	if strings.EqualFold(lang, "en") {
		return item.ReasonEN
	}
	return item.ReasonZH
}

func limitSignals(items []string, max int) []string {
	if max <= 0 || len(items) <= max {
		return items
	}
	return items[:max]
}
