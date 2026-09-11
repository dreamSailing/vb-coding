package config

// Copyright (c) 2026 EOSAIOS
// SPDX-License-Identifier: EOS-NCL-1.1
// 本文件基于 EOS 非商用许可证 v1.1 发布，详见 LICENSE。
// 商业使用请联系版权人获得商业授权。

import (
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

type CapabilityModelRefs struct {
	ImageGeneration string `json:"image_generation,omitempty"`
	VideoGeneration string `json:"video_generation,omitempty"`
	SpeechSynthesis string `json:"speech_synthesis,omitempty"`
}

type ModelEntry struct {
	Name                    string `json:"name"`
	APIBase                 string `json:"api_base"`
	APIKey                  string `json:"api_key"`
	Model                   string `json:"model"`
	Source                  string `json:"source,omitempty"`
	Provider                string `json:"provider,omitempty"`                  // 服务商类型 (deepseek, dashscope, etc.)
	PresetID                string `json:"preset_id,omitempty"`                 // 内置 preset ID（套餐内切换模型走 model/save 时需要）
	APIType                 string `json:"api_type,omitempty"`                  // API 类型 (standard, code-plan)
	Role                    string `json:"role,omitempty"`                      // 模型角色: primary/image_generation/video_generation/speech_synthesis
	Enabled                 *bool  `json:"enabled,omitempty"`                   // 是否启用（旧配置留空时默认 true）
	ThinkingEnabled         bool   `json:"thinking_enabled,omitempty"`          // 是否为该模型启用思考
	ThinkingCapability      string `json:"thinking_capability,omitempty"`       // "none", "low", "medium", "high"
	SupportsReasoningEffort bool   `json:"supports_reasoning_effort,omitempty"` // 是否支持 ReasoningEffort 参数
	SupportsVision          bool   `json:"supports_vision,omitempty"`           // 是否支持视觉/多模态输入
	SupportsTools           bool   `json:"supports_tools,omitempty"`            // 是否支持工具调用
	SupportsImageGeneration bool   `json:"supports_image_generation,omitempty"`
	SupportsVideoGeneration bool   `json:"supports_video_generation,omitempty"`
	SupportsSpeechSynthesis bool   `json:"supports_speech_synthesis,omitempty"`
}

// ThinkingConfig 思考模式全局配置
type ThinkingConfig struct {
	Enabled         bool     `json:"enabled"`          // 是否启用思考模式
	AutoDetect      bool     `json:"auto_detect"`      // 是否自动检测模型思考能力
	ReasoningEffort string   `json:"reasoning_effort"` // 推理级别: "low", "medium", "high"
	CustomModels    []string `json:"custom_models"`    // 额外支持思考的自定义模型列表
}

type AgentConfig struct {
	MaxStep              int `json:"max_step,omitempty"`
	InvokeTimeoutSeconds int `json:"invoke_timeout_seconds,omitempty"`
	ToolTimeoutSeconds   int `json:"tool_timeout_seconds,omitempty"`
}

// SkillsDirEntry Skills 目录配置条目
type SkillsDirEntry struct {
	Path    string `json:"path"`    // Skills 目录路径
	Enabled bool   `json:"enabled"` // 是否启用
}

// PluginEntry 插件配置条目
type PluginEntry struct {
	Name    string `json:"name"`    // 插件名称
	Enabled bool   `json:"enabled"` // 是否启用
}

// MCPClientType MCP客户端类型
type MCPClientType string

const (
	MCPTypeStdio          MCPClientType = "stdio"           // 本地命令行工具
	MCPTypeSSE            MCPClientType = "sse"             // 远程SSE服务
	MCPTypeStreamableHTTP MCPClientType = "streamable-http" // Streamable HTTP MCP transport
)

// MCPEntry MCP服务配置条目
type MCPEntry struct {
	Name                 string            `json:"name"`                             // 服务名称（唯一标识）
	Type                 MCPClientType     `json:"type"`                             // 客户端类型: "stdio" 或 "sse"
	Command              string            `json:"command,omitempty"`                // stdio: 执行命令
	Args                 []string          `json:"args,omitempty"`                   // stdio: 命令参数
	Envs                 map[string]string `json:"envs,omitempty"`                   // stdio: 环境变量
	BaseURL              string            `json:"base_url,omitempty"`               // sse: 服务URL
	Enabled              bool              `json:"enabled"`                          // 是否启用
	Auth                 *MCPAuth          `json:"auth,omitempty"`                   // 认证配置
	ApprovalMode         string            `json:"approval_mode,omitempty"`          // 服务默认审批模式覆盖
	ToolApprovalOverride map[string]string `json:"tool_approval_override,omitempty"` // 单工具审批模式覆盖
}

// MCPAuth defines authentication configuration for MCP servers
type MCPAuth struct {
	Type       string            `json:"type"`                  // "bearer", "basic", "api_key"
	Token      string            `json:"token,omitempty"`       // Bearer token or API key value
	Headers    map[string]string `json:"headers,omitempty"`     // Custom headers to inject
	HeadersEnv map[string]string `json:"headers_env,omitempty"` // Header names whose values come from env vars
}

// LSPConfig LSP 配置
type LSPConfig struct {
	Enabled    *bool           `json:"enabled,omitempty"`     // 是否启用 LSP（默认 true）
	AutoDetect *bool           `json:"auto_detect,omitempty"` // 自动检测语言服务器（默认 true）
	Go         LSPServerConfig `json:"go,omitempty"`          // Go 语言配置
	Python     LSPServerConfig `json:"python,omitempty"`      // Python 语言配置
	TypeScript LSPServerConfig `json:"typescript,omitempty"`  // TypeScript 语言配置
}

func (c LSPConfig) EnabledValue() bool {
	if c.Enabled == nil {
		return true
	}
	return *c.Enabled
}

func (c LSPConfig) AutoDetectValue() bool {
	if c.AutoDetect == nil {
		return true
	}
	return *c.AutoDetect
}

// LSPServerConfig LSP 服务器配置
type LSPServerConfig struct {
	Enabled bool     `json:"enabled"`           // 是否启用
	Command string   `json:"command,omitempty"` // 自定义命令（留空使用自动检测）
	Args    []string `json:"args,omitempty"`    // 自定义参数
}

// PermissionsConfig defines tool permissions in config
type PermissionsConfig struct {
	AccessMode   string   `json:"access_mode,omitempty"`
	ApprovalMode string   `json:"approval_mode,omitempty"`
	AllowedTools []string `json:"allowed_tools,omitempty"`
	DeniedTools  []string `json:"denied_tools,omitempty"`
}

// RemotePlatformType 远程仓库平台类型
type RemotePlatformType string

const (
	RemotePlatformGitHub RemotePlatformType = "github"
	RemotePlatformGitee  RemotePlatformType = "gitee"
)

// RemoteOAuthAppConfig 远程平台 OAuth 应用配置
type RemoteOAuthAppConfig struct {
	ClientID     string `json:"client_id,omitempty"`
	ClientSecret string `json:"client_secret,omitempty"`
	RedirectURI  string `json:"redirect_uri,omitempty"`
}

// RemoteProviderConfig 远程平台配置
type RemoteProviderConfig struct {
	OAuth       RemoteOAuthAppConfig `json:"oauth,omitempty"`
	AccessToken string               `json:"access_token,omitempty"` // 可选：预置 token，便于服务端或 CI 场景
	Username    string               `json:"username,omitempty"`     // 可选：与预置 token 搭配使用
}

// RemoteAuthToken 持久化保存的授权令牌
type RemoteAuthToken struct {
	Platform     RemotePlatformType `json:"platform"`
	AccountID    string             `json:"account_id,omitempty"`
	AccountName  string             `json:"account_name,omitempty"`
	Login        string             `json:"login,omitempty"`
	AccessToken  string             `json:"access_token,omitempty"`
	RefreshToken string             `json:"refresh_token,omitempty"`
	TokenType    string             `json:"token_type,omitempty"`
	Scope        string             `json:"scope,omitempty"`
	ExpiryUnix   int64              `json:"expiry_unix,omitempty"`
}

// RemoteRepoEntry 远程仓库缓存信息
type RemoteRepoEntry struct {
	Platform      RemotePlatformType `json:"platform"`
	RepoURL       string             `json:"repo_url"`
	Owner         string             `json:"owner,omitempty"`
	Repo          string             `json:"repo,omitempty"`
	DefaultBranch string             `json:"default_branch,omitempty"`
	LocalPath     string             `json:"local_path,omitempty"`
	LastBranch    string             `json:"last_branch,omitempty"`
	LastUsedUnix  int64              `json:"last_used_unix,omitempty"`
}

type Config struct {
	Models                       []ModelEntry        `json:"models,omitempty"`
	Active                       string              `json:"active_model,omitempty"`
	CapabilityModels             CapabilityModelRefs `json:"capability_models,omitempty"`
	Thinking                     ThinkingConfig      `json:"thinking,omitempty"` // 思考模式配置
	NextMessagePredictionEnabled *bool               `json:"next_message_prediction_enabled,omitempty"`
	// MemoryInjectionEnabled 是 CLI 壳层的请求级记忆注入开关（默认开）。
	// 发 turn 时随 StartTurnRequest.use_memory 下发；注入最终裁决在内核
	//（与全局 [memories].use_memories 求与）。nil = 未设置 = 默认开。
	MemoryInjectionEnabled *bool `json:"memory_injection_enabled,omitempty"`
	// GitCommitReminder 是 CLI 的「git 提交提醒」开关（turn 结束且工作区有
	// 未提交/未推送时系统提示）。nil = 未设置 = 默认开。内核 Settings 不落盘，
	// 该开关的持久化由 CLI config 承担（与 MemoryInjectionEnabled 同款）。
	GitCommitReminder *bool            `json:"git_commit_reminder,omitempty"`
	Agent             AgentConfig      `json:"agent,omitempty"`
	MCP               []MCPEntry       `json:"mcp,omitempty"`                // MCP服务配置
	Skills            []SkillsDirEntry `json:"skills,omitempty"`             // Skills 目录配置
	DisabledSkills    []string         `json:"disabled_skills,omitempty"`    // 被禁用的 skill 名称
	Plugins           []PluginEntry    `json:"plugins,omitempty"`            // 插件启停覆盖配置
	LSP               LSPConfig        `json:"lsp,omitempty"`                // LSP 配置
	KnownWorkspaces   []string         `json:"known_workspaces,omitempty"`   // 已知工作区（绝对路径）
	LastWorkspace     string           `json:"last_workspace,omitempty"`     // 上次前台工作区（绝对路径）
	TrustedWorkspaces []string         `json:"trusted_workspaces,omitempty"` // 已信任的工作区（绝对路径）
	Language          string           `json:"language,omitempty"`           // 语言设置 (zh, en)
	DiffTheme         string           `json:"diff_theme,omitempty"`         // diff/代码块 chroma 高亮主题（monokai 等）
	LogDir            string           `json:"log_dir,omitempty"`            // 全局日志目录
	FastModel         string           `json:"fast_model,omitempty"`         // Fast mode model name
	// UpdateProxyEnabled/UpdateProxyURL 是更新（检查+下载）代理开关：
	// enabled=false（默认关）时更新走直连/环境代理，url 仅在 enabled 时生效。
	UpdateProxyEnabled bool                            `json:"update_proxy_enabled,omitempty"`
	UpdateProxyURL     string                          `json:"update_proxy_url,omitempty"`
	Permissions        *PermissionsConfig              `json:"permissions,omitempty"`      // Tool permissions
	RemoteProviders    map[string]RemoteProviderConfig `json:"remote_providers,omitempty"` // GitHub/Gitee OAuth/Token 配置
	RemoteAuth         map[string]RemoteAuthToken      `json:"remote_auth,omitempty"`      // 已授权账号（按平台）
	RemoteRepos        []RemoteRepoEntry               `json:"remote_repos,omitempty"`     // 最近访问的远程仓库
}

func boolPtr(v bool) *bool {
	return &v
}

func DefaultLogDir() string {
	switch runtime.GOOS {
	case "windows":
		base := os.Getenv("LOCALAPPDATA")
		if strings.TrimSpace(base) == "" {
			base = os.Getenv("APPDATA")
		}
		if strings.TrimSpace(base) == "" {
			base, _ = os.UserHomeDir()
		}
		return filepath.Join(base, "EOS", "logs")
	case "darwin":
		home, _ := os.UserHomeDir()
		return filepath.Join(home, "Library", "Logs", "EOS")
	default:
		base := os.Getenv("XDG_STATE_HOME")
		if strings.TrimSpace(base) == "" {
			home, _ := os.UserHomeDir()
			base = filepath.Join(home, ".local", "state")
		}
		return filepath.Join(base, "eos", "logs")
	}
}

func ConfiguredLogDir() string {
	// Load() 返回 (Config, 路径)，无 error——这里的 _ 丢弃的是未使用的配置文件路径，
	// 不是被吞掉的错误（AGENTS.md L108 针对 `_ = err`）。命名以避免歧义。
	cfg, _ := Load()
	return ResolveLogDir(cfg.LogDir)
}

func ResolveLogDir(value string) string {
	trimmed := strings.TrimSpace(os.ExpandEnv(value))
	if trimmed == "" {
		return DefaultLogDir()
	}
	if strings.HasPrefix(trimmed, "~") {
		home, err := os.UserHomeDir()
		if err == nil && strings.TrimSpace(home) != "" {
			if trimmed == "~" {
				trimmed = home
			} else if strings.HasPrefix(trimmed, "~/") || strings.HasPrefix(trimmed, "~\\") {
				trimmed = filepath.Join(home, trimmed[2:])
			}
		}
	}
	if !filepath.IsAbs(trimmed) {
		if abs, err := filepath.Abs(trimmed); err == nil {
			trimmed = abs
		}
	}
	return filepath.Clean(trimmed)
}

func NextMessagePredictionEnabled(cfg *Config) bool {
	if cfg == nil || cfg.NextMessagePredictionEnabled == nil {
		return true
	}
	return *cfg.NextMessagePredictionEnabled
}

// MemoryInjectionEnabled 返回记忆注入开关；未配置时默认开
// （对齐内核 use_memory.unwrap_or(true) 的缺省语义）。
func MemoryInjectionEnabled(cfg *Config) bool {
	if cfg == nil || cfg.MemoryInjectionEnabled == nil {
		return true
	}
	return *cfg.MemoryInjectionEnabled
}

// GitCommitReminderEnabled 返回 git 提交提醒开关；未配置时默认开
// （与 Settings.git_commit_reminder 缺省语义一致）。
func GitCommitReminderEnabled(cfg *Config) bool {
	if cfg == nil || cfg.GitCommitReminder == nil {
		return true
	}
	return *cfg.GitCommitReminder
}

// DiffHighlightTheme 返回 diff/代码块高亮主题名；空值回默认 monokai。
// 主题合法性由渲染层校验（chroma styles registry），非法名渲染时同样回退。
func DiffHighlightTheme(cfg *Config) string {
	if cfg == nil {
		return ""
	}
	return strings.TrimSpace(cfg.DiffTheme)
}

// EffectiveUpdateProxyURL 返回启用状态下的更新代理地址；
// 开关关闭（默认）时返回空串（空 = 直连/遵循环境 HTTP_PROXY）。
func EffectiveUpdateProxyURL(cfg *Config) string {
	if cfg == nil || !cfg.UpdateProxyEnabled {
		return ""
	}
	return strings.TrimSpace(cfg.UpdateProxyURL)
}

func Path() string {
	home, err := os.UserHomeDir()
	if err != nil {
		slog.Error("config.path.user_home_dir.error",
			"error", err)
		return ".eos.json"
	}
	return filepath.Join(home, ".eos.json")
}

func Load() (Config, string) {
	configMutex.Lock()
	defer configMutex.Unlock()

	p := Path()
	var cfg Config
	b, err := os.ReadFile(p)
	if err != nil {
		slog.Debug("config.load.file_not_found",
			"path", p,
			"error", err)
		return cfg, p
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		slog.Error("config.load.unmarshal.error",
			"path", p,
			"data_size", len(b),
			"error", err)
	}
	if NormalizeWorkspaceState(&cfg) {
		// 持锁回写：必须用 saveLocked（Save 会再次 Lock 同一互斥锁 → 死锁）。
		if err := saveLocked(cfg, p); err != nil {
			slog.Warn("config.load.normalize_workspace_state.save.error", "path", p, "error", err.Error())
		}
	}

	if len(cfg.MCP) == 0 {
		if migrated, ok := tryMigrateLegacyMCPServers(b, &cfg); ok {
			if err := saveLocked(cfg, p); err != nil {
				slog.Warn("config.load.migrate_mcp.save.error", "path", p, "error", err.Error())
			} else {
				slog.Info("config.load.migrate_mcp.save.success", "path", p, "mcp_count", len(migrated))
			}
		}
	}
	slog.Debug("config.load.success",
		"path", p,
		"models_count", len(cfg.Models),
		"active_model", cfg.Active)
	return cfg, p
}

func tryMigrateLegacyMCPServers(b []byte, cfg *Config) ([]MCPEntry, bool) {
	entries, err := ParseLegacyMCPServersJSON(b)
	if err != nil || len(entries) == 0 {
		return nil, false
	}
	cfg.MCP = entries
	return entries, true
}

func ParseLegacyMCPServersJSON(b []byte) ([]MCPEntry, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(b, &top); err != nil {
		return nil, err
	}
	raw, ok := top["mcpServers"]
	if !ok || len(raw) == 0 {
		return nil, errors.New("missing mcpServers")
	}
	return parseLegacyMCPServersRaw(raw)
}

func parseLegacyMCPServersRaw(raw json.RawMessage) ([]MCPEntry, error) {
	var servers map[string]json.RawMessage
	if err := json.Unmarshal(raw, &servers); err != nil {
		return nil, err
	}
	if len(servers) == 0 {
		return nil, nil
	}

	entries := make([]MCPEntry, 0, len(servers))
	for name, body := range servers {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		var obj map[string]any
		_ = json.Unmarshal(body, &obj)

		entry := MCPEntry{
			Name:    name,
			Enabled: true,
		}

		if v, ok := obj["enabled"].(bool); ok {
			entry.Enabled = v
		}
		if v, ok := obj["type"].(string); ok {
			entry.Type = MCPClientType(strings.TrimSpace(v))
		}
		if v, ok := obj["command"].(string); ok {
			entry.Command = strings.TrimSpace(v)
		}
		if v, ok := obj["args"].([]any); ok {
			args := make([]string, 0, len(v))
			for _, a := range v {
				if s, ok := a.(string); ok {
					s = strings.TrimSpace(s)
					if s != "" {
						args = append(args, s)
					}
				}
			}
			entry.Args = args
		}
		if v, ok := obj["base_url"].(string); ok {
			entry.BaseURL = strings.TrimSpace(v)
		}
		if entry.BaseURL == "" {
			if v, ok := obj["url"].(string); ok {
				entry.BaseURL = strings.TrimSpace(v)
			}
		}

		if envs := parseLegacyEnvMap(obj["envs"]); len(envs) > 0 {
			entry.Envs = envs
		} else if env := parseLegacyEnvMap(obj["env"]); len(env) > 0 {
			entry.Envs = env
		}
		if v, ok := obj["approval_mode"].(string); ok {
			entry.ApprovalMode = strings.TrimSpace(v)
		}
		if rawOverrides, ok := obj["tool_approval_override"].(map[string]any); ok {
			overrides := make(map[string]string, len(rawOverrides))
			for toolName, value := range rawOverrides {
				toolName = strings.TrimSpace(toolName)
				text, _ := value.(string)
				text = strings.TrimSpace(text)
				if toolName == "" || text == "" {
					continue
				}
				overrides[toolName] = text
			}
			if len(overrides) > 0 {
				entry.ToolApprovalOverride = overrides
			}
		}

		if entry.Type == "" {
			if entry.BaseURL != "" {
				entry.Type = MCPTypeSSE
			} else {
				entry.Type = MCPTypeStdio
			}
		}

		entries = append(entries, entry)
	}
	return entries, nil
}

func parseLegacyEnvMap(v any) map[string]string {
	m, ok := v.(map[string]any)
	if !ok || len(m) == 0 {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, vv := range m {
		ks := strings.TrimSpace(k)
		if ks == "" {
			continue
		}
		if s, ok := vv.(string); ok {
			out[ks] = s
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// configMutex 保护 ~/.eos.json 的并发读写，防止多个 goroutine 同时 Load→改→Save
// 导致 last-writer-wins 静默丢数据（S5 修复）。
var configMutex sync.Mutex

func Save(cfg Config, p string) error {
	configMutex.Lock()
	defer configMutex.Unlock()
	return saveLocked(cfg, p)
}

// saveLocked 落盘配置（调用方必须已持有 configMutex）。
// Load 的迁移路径在持锁状态下回写，必须走本函数——直接调 Save 会因
// 互斥锁不可重入而死锁。
func saveLocked(cfg Config, p string) error {
	bs, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		slog.Error("config.save.marshal.error",
			"path", p,
			"models_count", len(cfg.Models),
			"active_model", cfg.Active,
			"error", err)
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		slog.Error("config.save.mkdir_all.error",
			"path", p,
			"error", err)
		return err
	}
	// S4: 原子写——先写 .tmp 再 rename，避免写一半崩溃导致 ~/.eos.json 截断损坏。
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, bs, 0600); err != nil {
		slog.Error("config.save.write_tmp.error",
			"path", tmp,
			"data_size", len(bs),
			"error", err)
		return err
	}
	if err := os.Rename(tmp, p); err != nil {
		slog.Error("config.save.rename.error",
			"tmp", tmp,
			"dest", p,
			"error", err)
		// rename 失败时清理 tmp 文件
		_ = os.Remove(tmp)
		return err
	}
	slog.Debug("config.save.success",
		"path", p,
		"models_count", len(cfg.Models),
		"active_model", cfg.Active)
	return nil
}

func ActiveModel(cfg Config) (ModelEntry, bool) {
	for _, m := range cfg.Models {
		if m.Name == cfg.Active {
			return m, true
		}
	}
	return ModelEntry{}, false
}
