package webbridge

// Copyright (c) 2026 EOSAIOS
// SPDX-License-Identifier: EOS-NCL-1.1
// 本文件基于 EOS 非商用许可证 v1.1 发布，详见 LICENSE。
// 商业使用请联系版权人获得商业授权。

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const (
	defaultLanguage           = "zh"
	defaultTheme              = "system"
	defaultDiffTheme          = "github"
	defaultSandboxMode        = "workspace-write"
	defaultMaxInjectKB        = 32
	defaultWatchDebounceMs    = 500
	defaultPollIntervalSec    = 5
	workspaceSettingsFileName = "settings.json"
)

type GUISettingsDefaults struct {
	Language string
	Theme    string
}

type GUIWorkspaceSettings struct {
	Theme                string
	DiffTheme            string
	SandboxMode          string
	AutoContext          bool
	DesktopNotifications bool
	// GitCommitReminder 是「git 提交提醒」开关（默认开）：turn 结束且工作区
	// 有未提交/未推送变更时提示，点击直派 AI 提交推送。
	GitCommitReminder bool
	// GitCommitMarker 是「提交信息附带 eos 署名」开关（默认开）：提交时在
	// commit message 尾行追加 "🤖 Generated with eos"。
	GitCommitMarker bool
	// StayInTray 是「驻留系统托盘」开关（默认开）：关闭窗口隐藏到系统托盘
	// 常驻（如微信），托盘菜单退出才是真退出。
	StayInTray bool
	// UseMemory 是桌面壳的请求级记忆注入开关（默认开），turn/start 时随
	// use_memory 下发；注入最终裁决在内核（与全局 [memories].use_memories 求与）。
	UseMemory       bool
	MaxInjectKB     int
	WatchDebounceMs int
	PollIntervalSec int
	PlanPromptStyle string
	// PromptTimeoutSecs 是询问（审批/问询）等待超时秒数（0 = 一直等待）。
	// 超时后内核自动响应：审批拒绝、问询选 (Recommended) 项。
	PromptTimeoutSecs int
	Trusted           bool
	TrustedAt         string
}

type GUISettingsSnapshot struct {
	Language              string
	LogDir                string
	GlobalConfigPath      string
	WorkspaceSettingsPath string
	Workspace             GUIWorkspaceSettings
	// UpdateProxyEnabled/UpdateProxyURL 是更新代理开关（全局配置）。
	UpdateProxyEnabled bool
	UpdateProxyURL     string
}

type GUISettingsSaveInput struct {
	Language             string
	LogDir               string
	Theme                string
	DiffTheme            string
	SandboxMode          string
	AutoContext          bool
	DesktopNotifications bool
	GitCommitReminder    bool
	GitCommitMarker      bool
	StayInTray           bool
	UseMemory            bool
	MaxInjectKB          int
	WatchDebounceMs      int
	PollIntervalSec      int
	PlanPromptStyle      string
	PromptTimeoutSecs    int
	// UpdateProxyEnabled/UpdateProxyURL 是更新（检查+下载）代理开关，
	// 写在全局配置 ~/.eos.json（与 CLI 共用 update_proxy_* 字段）。
	UpdateProxyEnabled bool
	UpdateProxyURL     string
}

func ResolveWorkspaceSettingsPath(activeWorkspace string) string {
	trimmed := strings.TrimSpace(activeWorkspace)
	if trimmed != "" {
		return filepath.Join(trimmed, ".eos", workspaceSettingsFileName)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".eos", workspaceSettingsFileName)
	}
	return filepath.Join(home, ".eos", workspaceSettingsFileName)
}

func LoadGUISettings(globalConfigPath, activeWorkspace string, defaults GUISettingsDefaults) (GUISettingsSnapshot, error) {
	globalConfigPath = resolveGlobalConfigPath(globalConfigPath)
	workspaceSettingsPath := ResolveWorkspaceSettingsPath(activeWorkspace)
	defaults = normalizeGUISettingsDefaults(defaults)

	globalDoc, err := loadJSONDocument(globalConfigPath)
	if err != nil {
		return GUISettingsSnapshot{}, err
	}
	workspaceDoc, err := loadJSONDocument(workspaceSettingsPath)
	if err != nil {
		return GUISettingsSnapshot{}, err
	}

	return GUISettingsSnapshot{
		Language:              loadGUILanguage(globalDoc, defaults.Language),
		LogDir:                resolveLogDir(loadJSONString(globalDoc, "log_dir", "")),
		GlobalConfigPath:      globalConfigPath,
		WorkspaceSettingsPath: workspaceSettingsPath,
		Workspace:             decodeWorkspaceSettings(workspaceDoc, defaults),
		UpdateProxyEnabled:    loadJSONBool(globalDoc, updateProxyEnabledKey, false),
		UpdateProxyURL:        loadJSONString(globalDoc, updateProxyURLKey, ""),
	}, nil
}

// loadGUILanguage 读取桌面端独立的 GUI 语言字段 gui_language。
//
// 桌面端和 CLI 各自维护自己的界面语言，互不覆盖：
//   - 桌面端读/写 gui_language（本函数 + saveGUILanguage）。
//   - CLI 读/写 language 字段（见 eos-cli/internal/config）。
//
// 旧配置只有 language 字段时，首次启动会 fallback 到它，让历史设置不丢失；
// 一旦用户在桌面端改过语言，gui_language 就会落地，后续与 CLI 完全独立。
func loadGUILanguage(doc map[string]json.RawMessage, fallback string) string {
	gui := strings.TrimSpace(loadJSONString(doc, "gui_language", ""))
	if gui != "" {
		return gui
	}
	// 旧配置迁移：没有 gui_language 时，沿用 CLI 的 language 字段作为初始值。
	// 注意：这里只「读」，不会把 language 的值拷到 gui_language，避免在用户
	// 还没明确操作过桌面端设置前就分裂两个壳层的语言。
	return strings.TrimSpace(loadJSONString(doc, "language", fallback))
}

func SaveGUISettings(globalConfigPath, activeWorkspace string, input GUISettingsSaveInput, defaults GUISettingsDefaults) (GUISettingsSnapshot, error) {
	globalConfigPath = resolveGlobalConfigPath(globalConfigPath)
	workspaceSettingsPath := ResolveWorkspaceSettingsPath(activeWorkspace)
	defaults = normalizeGUISettingsDefaults(defaults)
	input = normalizeGUISettingsInput(input, defaults)

	globalDoc, err := loadJSONDocument(globalConfigPath)
	if err != nil {
		return GUISettingsSnapshot{}, err
	}
	workspaceDoc, err := loadJSONDocument(workspaceSettingsPath)
	if err != nil {
		return GUISettingsSnapshot{}, err
	}

	// 桌面端语言独立存 gui_language，不触碰 CLI 用的 language 字段，
	// 保证两个壳层各自维护界面语言、互不覆盖（见 loadGUILanguage 注释）。
	if err := setJSONValue(globalDoc, "gui_language", input.Language); err != nil {
		return GUISettingsSnapshot{}, err
	}
	if err := setJSONValue(globalDoc, "log_dir", resolveLogDir(input.LogDir)); err != nil {
		return GUISettingsSnapshot{}, err
	}
	if err := setJSONValue(globalDoc, updateProxyEnabledKey, input.UpdateProxyEnabled); err != nil {
		return GUISettingsSnapshot{}, err
	}
	if err := setJSONValueOrDeleteEmptyString(globalDoc, updateProxyURLKey, input.UpdateProxyURL); err != nil {
		return GUISettingsSnapshot{}, err
	}
	if err := writeJSONDocument(globalConfigPath, globalDoc); err != nil {
		return GUISettingsSnapshot{}, err
	}

	if err := setJSONValue(workspaceDoc, "theme", input.Theme); err != nil {
		return GUISettingsSnapshot{}, err
	}
	if err := setJSONValue(workspaceDoc, "diff_theme", input.DiffTheme); err != nil {
		return GUISettingsSnapshot{}, err
	}
	if err := setJSONValue(workspaceDoc, "sandbox_mode", input.SandboxMode); err != nil {
		return GUISettingsSnapshot{}, err
	}
	if err := setJSONValue(workspaceDoc, "auto_context", input.AutoContext); err != nil {
		return GUISettingsSnapshot{}, err
	}
	if err := setJSONValue(workspaceDoc, "desktop_notifications", input.DesktopNotifications); err != nil {
		return GUISettingsSnapshot{}, err
	}
	if err := setJSONValue(workspaceDoc, "git_commit_reminder", input.GitCommitReminder); err != nil {
		return GUISettingsSnapshot{}, err
	}
	if err := setJSONValue(workspaceDoc, "git_commit_marker", input.GitCommitMarker); err != nil {
		return GUISettingsSnapshot{}, err
	}
	if err := setJSONValue(workspaceDoc, "stay_in_tray", input.StayInTray); err != nil {
		return GUISettingsSnapshot{}, err
	}
	if err := setJSONValue(workspaceDoc, "use_memory", input.UseMemory); err != nil {
		return GUISettingsSnapshot{}, err
	}
	if err := setJSONValue(workspaceDoc, "max_inject_kb", input.MaxInjectKB); err != nil {
		return GUISettingsSnapshot{}, err
	}
	if err := setJSONValue(workspaceDoc, "watch_debounce_ms", input.WatchDebounceMs); err != nil {
		return GUISettingsSnapshot{}, err
	}
	if err := setJSONValue(workspaceDoc, "poll_interval_sec", input.PollIntervalSec); err != nil {
		return GUISettingsSnapshot{}, err
	}
	if err := setJSONValueOrDeleteEmptyString(workspaceDoc, "plan_prompt_style", input.PlanPromptStyle); err != nil {
		return GUISettingsSnapshot{}, err
	}
	if err := setJSONValue(workspaceDoc, "prompt_timeout_secs", input.PromptTimeoutSecs); err != nil {
		return GUISettingsSnapshot{}, err
	}
	if err := writeJSONDocument(workspaceSettingsPath, workspaceDoc); err != nil {
		return GUISettingsSnapshot{}, err
	}

	return LoadGUISettings(globalConfigPath, activeWorkspace, defaults)
}

func normalizeGUISettingsDefaults(defaults GUISettingsDefaults) GUISettingsDefaults {
	defaults.Language = strings.TrimSpace(defaults.Language)
	if defaults.Language == "" {
		defaults.Language = defaultLanguage
	}
	defaults.Theme = strings.TrimSpace(defaults.Theme)
	if defaults.Theme == "" {
		defaults.Theme = defaultTheme
	}
	return defaults
}

func normalizeGUISettingsInput(input GUISettingsSaveInput, defaults GUISettingsDefaults) GUISettingsSaveInput {
	input.Language = strings.TrimSpace(input.Language)
	if input.Language == "" {
		input.Language = defaults.Language
	}
	input.LogDir = resolveLogDir(input.LogDir)
	input.Theme = strings.TrimSpace(input.Theme)
	if input.Theme == "" {
		input.Theme = defaults.Theme
	}
	// diff_theme 由前端 select 提供合法预设；这里只兜底空值，非法值由
	// 前端渲染层回退默认（见 workbench-code-highlight 的 applyHighlightTheme）。
	input.DiffTheme = strings.TrimSpace(input.DiffTheme)
	if input.DiffTheme == "" {
		input.DiffTheme = defaultDiffTheme
	}
	input.SandboxMode = NormalizeSandboxMode(input.SandboxMode)
	if input.MaxInjectKB <= 0 {
		input.MaxInjectKB = defaultMaxInjectKB
	}
	if input.WatchDebounceMs <= 0 {
		input.WatchDebounceMs = defaultWatchDebounceMs
	}
	if input.PollIntervalSec <= 0 {
		input.PollIntervalSec = defaultPollIntervalSec
	}
	input.PlanPromptStyle = strings.TrimSpace(input.PlanPromptStyle)
	return input
}

func decodeWorkspaceSettings(doc map[string]json.RawMessage, defaults GUISettingsDefaults) GUIWorkspaceSettings {
	settings := GUIWorkspaceSettings{
		Theme:                defaults.Theme,
		DiffTheme:            defaultDiffTheme,
		SandboxMode:          defaultSandboxMode,
		AutoContext:          true,
		DesktopNotifications: true,
		GitCommitReminder:    true,
		GitCommitMarker:      true,
		StayInTray:           true,
		// 记忆注入默认开：对齐内核 use_memory.unwrap_or(true) 缺省语义，
		// 旧配置（无 use_memory key）升级后行为不变。
		UseMemory:       true,
		MaxInjectKB:     defaultMaxInjectKB,
		WatchDebounceMs: defaultWatchDebounceMs,
		PollIntervalSec: defaultPollIntervalSec,
	}
	settings.Theme = loadJSONString(doc, "theme", settings.Theme)
	settings.DiffTheme = loadJSONString(doc, "diff_theme", settings.DiffTheme)
	settings.SandboxMode = NormalizeSandboxMode(loadJSONString(doc, "sandbox_mode", settings.SandboxMode))
	settings.AutoContext = loadJSONBool(doc, "auto_context", settings.AutoContext)
	settings.DesktopNotifications = loadJSONBool(doc, "desktop_notifications", settings.DesktopNotifications)
	settings.GitCommitReminder = loadJSONBool(doc, "git_commit_reminder", settings.GitCommitReminder)
	settings.GitCommitMarker = loadJSONBool(doc, "git_commit_marker", settings.GitCommitMarker)
	settings.StayInTray = loadJSONBool(doc, "stay_in_tray", settings.StayInTray)
	settings.UseMemory = loadJSONBool(doc, "use_memory", settings.UseMemory)
	settings.MaxInjectKB = loadJSONInt(doc, "max_inject_kb", settings.MaxInjectKB)
	settings.WatchDebounceMs = loadJSONInt(doc, "watch_debounce_ms", settings.WatchDebounceMs)
	settings.PollIntervalSec = loadJSONInt(doc, "poll_interval_sec", settings.PollIntervalSec)
	settings.PlanPromptStyle = loadJSONString(doc, "plan_prompt_style", "")
	settings.PromptTimeoutSecs = loadJSONInt(doc, "prompt_timeout_secs", 0)
	settings.Trusted = loadJSONBool(doc, "trusted", false)
	settings.TrustedAt = loadJSONString(doc, "trusted_at", "")
	return settings
}

func resolveGlobalConfigPath(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed != "" {
		return trimmed
	}
	return configPath()
}

func loadJSONDocument(path string) (map[string]json.RawMessage, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]json.RawMessage{}, nil
	}
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return map[string]json.RawMessage{}, nil
	}
	doc := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	return doc, nil
}

func writeJSONDocument(path string, doc map[string]json.RawMessage) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	body, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	return os.WriteFile(path, body, 0o644)
}

func setJSONValue(doc map[string]json.RawMessage, key string, value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	doc[key] = encoded
	return nil
}

func setJSONValueOrDeleteEmptyString(doc map[string]json.RawMessage, key, value string) error {
	if strings.TrimSpace(value) == "" {
		delete(doc, key)
		return nil
	}
	return setJSONValue(doc, key, value)
}

func loadJSONString(doc map[string]json.RawMessage, key, fallback string) string {
	raw, ok := doc[key]
	if !ok {
		return fallback
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return fallback
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func loadJSONBool(doc map[string]json.RawMessage, key string, fallback bool) bool {
	raw, ok := doc[key]
	if !ok {
		return fallback
	}
	var value bool
	if err := json.Unmarshal(raw, &value); err != nil {
		return fallback
	}
	return value
}

func loadJSONInt(doc map[string]json.RawMessage, key string, fallback int) int {
	raw, ok := doc[key]
	if !ok {
		return fallback
	}
	var value int
	if err := json.Unmarshal(raw, &value); err != nil || value <= 0 {
		return fallback
	}
	return value
}

// NormalizeSandboxMode maps user-facing sandbox mode aliases to the canonical
// wire value. The canonical vocabulary is the kernel SandboxMode kebab-case
// enum ("read-only" / "workspace-write" / "danger-full-access", 对标 Codex
// sandbox_mode)；历史 GUI 值（workspace / full_access）与中文别名只作为读取侧
// 别名。Shared by the desktop bridge layer (package main) and the settings
// store to avoid duplicate copies.
func NormalizeSandboxMode(mode string) string {
	key := strings.ToLower(strings.TrimSpace(mode))
	key = strings.ReplaceAll(key, "_", "-")
	switch key {
	case "read-only", "readonly", "ro":
		return "read-only"
	case "danger-full-access", "dangerfullaccess", "full-access", "fullaccess", "full", "danger", "allow-all", "完全访问", "完全访问权限":
		return "danger-full-access"
	case "workspace-write", "workspacewrite", "workspace", "ww":
		return "workspace-write"
	default:
		return defaultSandboxMode
	}
}
