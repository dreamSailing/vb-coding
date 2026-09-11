package webbridge

// Conversation 域 DTO：会话卡片、聊天消息、运行期事件、附件、变更集、回滚快照、提示卡、任务卡。
// JSON 字段语义与前端契约一致，仅做类型归属拆分。

type TaskCard struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Status    string `json:"status"`
	CanKill   bool   `json:"canKill"`
	Detail    string `json:"detail"`
	Source    string `json:"source"`
	UpdatedAt string `json:"updatedAt"`
}

type SessionCard struct {
	ID             string `json:"id"`
	Title          string `json:"title"`
	Preview        string `json:"preview"`
	WorkspacePath  string `json:"workspacePath"`
	UpdatedAt      string `json:"updatedAt"`
	Running        bool   `json:"running"`
	Persisted      bool   `json:"persisted"`
	NeedsAttention bool   `json:"needsAttention"`
	MessageCount   int    `json:"messageCount"`
	PendingPrompts int    `json:"pendingPrompts"`
	Archived       bool   `json:"archived"`
	Active         bool   `json:"active"`
}

type ChatMessage struct {
	ID                    string            `json:"id"`
	Role                  string            `json:"role"`
	Content               string            `json:"content"`
	State                 string            `json:"state"`
	CreatedAt             string            `json:"createdAt"`
	UpdatedAt             string            `json:"updatedAt"`
	Attachments           []AttachmentRef   `json:"attachments,omitempty"`
	RuntimeEvents         []RuntimeEvent    `json:"runtimeEvents,omitempty"`
	RuntimeSummary        string            `json:"runtimeSummary"`
	ImplementationResult  string            `json:"implementationResult,omitempty"`
	VerificationResult    string            `json:"verificationResult,omitempty"`
	VerificationVerdict   string            `json:"verificationVerdict,omitempty"`
	VerificationSummary   string            `json:"verificationSummary,omitempty"`
	VerificationCovered   []string          `json:"verificationCoveredChecks,omitempty"`
	VerificationOpenRisks []string          `json:"verificationOpenRisks,omitempty"`
	VerificationEvidence  []string          `json:"verificationEvidence,omitempty"`
	Prompts               []PromptCard      `json:"prompts,omitempty"`
	ChangeSet             *MessageChangeSet `json:"changeSet,omitempty"`
	IsPlaceholder         bool              `json:"isPlaceholder,omitempty"`
	// Items 是结构化的 ThreadItem 列表（对齐 Codex 模型）。
	// assistant 消息的思考/正文/工具调用各是独立 item，按 ID 累积，一旦显示不被覆盖。
	// 历史会话重载时按 metadata.turn_id 合并多条 SessionMessage 重建此列表。
	// user 消息不用 items，Content 是 user 的唯一载体。
	Items    []ThreadItem  `json:"items,omitempty"`
	Rollback *TurnRollback `json:"-"`
	// turnID 是该 assistant 消息所属 turn 的 ID（user 消息留空）。
	// 持久化时按 turn_id 把 Items 展开成多条 SessionMessage，重载时按 turn_id
	// 合并回单条 ChatMessage（见 bridge_message_codec.go）。不暴露给前端。
	turnID string `json:"-"`
}

// ThreadItem 是会话内一个离散输出项（对齐 Codex ThreadItem + 内核 TurnItem）。
// 每个项有独立 ID 和类型，壳层按 ID 累积 delta，不会互相覆盖。
type ThreadItem struct {
	ID         string          `json:"id"`
	Kind       string          `json:"kind"`                 // agent_message | reasoning | tool_call | plan | status
	Text       string          `json:"text,omitempty"`       // agent_message/plan/status 的文本
	Reasoning  string          `json:"reasoning,omitempty"`  // reasoning 的完整思考内容（累积拼接）
	ToolName   string          `json:"toolName,omitempty"`   // tool_call 的工具名
	ToolArgs   string          `json:"toolArgs,omitempty"`   // tool_call 的参数 JSON
	ToolResult *ItemToolResult `json:"toolResult,omitempty"` // tool_call 的执行结果
	Category   string          `json:"category,omitempty"`   // tool_call 的分类（command/shell/file/mcp）
	Level      string          `json:"level,omitempty"`      // status 的级别（info | warning | error）
	Status     string          `json:"status,omitempty"`     // streaming | completed | failed
	// Approval 承载审批/计划问题挂起态（对齐 codex：审批等待项 = 命令执行项，同一 item id）。
	// 非 nil 表示该 ToolCall item 正在等待用户决策；resolved 后翻转 State 并保留作为历史记录。
	// 单一数据源：废弃了独立的 s.prompts 卡片轨 + status item 双轨制，审批浮层从此字段投影。
	Approval *ItemApprovalState `json:"approval,omitempty"`
}

// ItemApprovalState 是挂在 ToolCall item 上的审批/问询挂起态。
// 对齐 codex 的 CommandExecution lifecycle：同一个 call_id 的 item 用 approval.state
// 表达 pending→approved/denied/executing/done 的流转，单一数据源、单一 delta 同步通道。
type ItemApprovalState struct {
	// ApprovalID 是内核 pending 表的 key（approval_{n}），approval/respond RPC 必须用它。
	// 注意：它 ≠ item.ID（item.ID = call.id）。内核 respond 只认 approval_id。
	ApprovalID string `json:"approvalId"`
	// Kind 区分审批类型："approval"（命令审批）或 "request_user_input"（计划问题）。
	Kind string `json:"kind"`
	// State 审批状态机：pending | approved | denied | cancelled | answered | failed。
	State string `json:"state"`
	// Title 审批卡片标题（壳层 i18n）。
	Title string `json:"title,omitempty"`
	// Message 审批卡片正文（来自内核 risk preview reason 或问题文本）。
	Message string `json:"message,omitempty"`
	// 审批卡按钮不在此携带：选项集是 kind 的纯函数（approval → accept/
	// acceptForSession/decline 三枚 token），由前端按 kind 派生并本地化，
	// 决策回传 token（= coreapi.ApprovalDecision wire 值）。壳层不落盘、
	// 不透传展示文案（AGENTS.md §3：渲染归前端，裁决归内核）。
	// RiskLevel 内核风险分类（low/medium/high），来自 tool.approval_required 内联 preview。
	RiskLevel string `json:"riskLevel,omitempty"`
	// Reason 内核给出的风险原因（preview.reason）。
	Reason string `json:"reason,omitempty"`
	// Diff / DiffPath 变更预览（审批时展示）。
	Diff     string `json:"diff,omitempty"`
	DiffPath string `json:"diffPath,omitempty"`
	// Questions 仅 request_user_input 使用：结构化多选问题列表。
	Questions []bridgeRequestUserInputQuestion `json:"questions,omitempty"`
	// ResolvedAt 解决时间戳（resolved 后填入，用于历史展示）。
	ResolvedAt string `json:"resolvedAt,omitempty"`
}

// ItemToolResult 是 ThreadItem.ToolCall 的执行结果（从内核 ToolResult payload 提取）。
type ItemToolResult struct {
	Status     string `json:"status,omitempty"`     // success | error | denied | ...
	Output     string `json:"output,omitempty"`     // 工具输出（格式化后的可读文本）
	Error      string `json:"error,omitempty"`      // 错误信息
	DurationMS int64  `json:"durationMs,omitempty"` // 执行耗时
}

// ConversationDeltaPayload 是流式增量事件的轻量 payload（对齐 codex 的 item/agentMessage/delta）。
// 只带单条消息的单个 item 增量，前端据此 patch 单条消息，无需全量 loadBootstrap。
// 零 RPC 往返——emit 是纯 Wails EventProcessor 调用，不查 core 状态。
type ConversationDeltaPayload struct {
	SessionID string `json:"sessionId"`
	MessageID string `json:"messageId"` // assistantMessageID
	ItemID    string `json:"itemId"`
	Kind      string `json:"kind"`      // agent_message | reasoning | tool_call | plan
	DeltaType string `json:"deltaType"` // reasoning | tool_args | text
	Delta     string `json:"delta"`     // 增量文本（item_started/completed 时可为空，靠 item 字段）
	Status    string `json:"status"`    // streaming | completed | failed
	// Item 用于 item_started/item_completed 时携带完整 ThreadItem 状态（delta 为空）。
	// 前端按 itemID upsert：存在则替换、不存在则追加。
	Item *ThreadItem `json:"item,omitempty"`
}

// ContextBreakdown 是当前上下文各构成部分的 token 份额：内核按本步真实请求
// 的部件（消息/工具定义/技能目录/系统提示词）估算占比后缩放到真实 prompt
// tokens，各类之和 ≈ lastPromptTokens。前端按「类别值/总和」算每项百分比。
type ContextBreakdown struct {
	Messages     int64 `json:"messages"`
	SystemTools  int64 `json:"systemTools"`
	McpTools     int64 `json:"mcpTools"`
	Skills       int64 `json:"skills"`
	SystemPrompt int64 `json:"systemPrompt"`
	Other        int64 `json:"other"`
}

// TurnUsagePayload 是内核 turn.token_usage 事件的轻量转发 payload：
// 当前 turn 已消耗 token 的累计快照（每步模型响应后更新一次）。
// 前端据此实时刷新上下文用量浮层，不等 turn 结束的全量快照。
type TurnUsagePayload struct {
	SessionID        string `json:"sessionId"`
	MessageID        string `json:"messageId"` // assistantMessageID
	TurnID           string `json:"turnId"`
	PromptTokens     int64  `json:"promptTokens"`
	CompletionTokens int64  `json:"completionTokens"`
	TotalTokens      int64  `json:"totalTokens"`
	// LastPromptTokens 是最近一步模型返回的真实 prompt tokens ≈ 当前上下文规模。
	// PromptTokens 是 turn 内各步累加（计费口径），多轮 ReAct 会远超窗口，
	// 拿它当上下文占用会把占用环顶满到数倍窗口（对齐 codex last_token_usage）。
	LastPromptTokens int64 `json:"lastPromptTokens"`
	// ContextBreakdown 是上下文构成占比（旧内核无此字段时为 nil，浮层隐藏占比区）。
	ContextBreakdown *ContextBreakdown `json:"contextBreakdown,omitempty"`
}

type RuntimeEvent struct {
	ID         string `json:"id"`
	Type       string `json:"type"`
	Title      string `json:"title"`
	Detail     string `json:"detail"`
	Status     string `json:"status"`
	Timestamp  string `json:"timestamp"`
	DurationMS int64  `json:"durationMs"`
}

type AttachmentRef struct {
	Name          string `json:"name"`
	Path          string `json:"path"`
	MIME          string `json:"mime,omitempty"`
	Kind          string `json:"kind,omitempty"`
	WorkspacePath string `json:"workspacePath,omitempty"`
	OriginalPath  string `json:"originalPath,omitempty"`
	Copied        bool   `json:"copied,omitempty"`
}

type AttachmentPreview struct {
	Name string `json:"name"`
	Path string `json:"path"`
	MIME string `json:"mime"`
	// URL 指向资产服务器上的附件图片路由（AttachmentImageRoutePath），
	// 前端 <img src> 直接加载。不再回传 base64——大图会把 WebView2 消息
	// 通道打爆（响应被静默丢弃，前端永远 pending）。
	URL string `json:"url"`
}

type WorkspaceFilePreview struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	Language  string `json:"language"`
	Content   string `json:"content"`
	Line      int    `json:"line,omitempty"`
	Truncated bool   `json:"truncated"`
}

type ChangedFile struct {
	Path      string `json:"path"`
	Status    string `json:"status"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Diff      string `json:"diff"`
	Truncated bool   `json:"truncated"`
}

type MessageChangeSet struct {
	ID            string        `json:"id"`
	WorkspacePath string        `json:"workspacePath"`
	CreatedAt     string        `json:"createdAt"`
	Summary       string        `json:"summary"`
	Additions     int           `json:"additions"`
	Deletions     int           `json:"deletions"`
	Truncated     bool          `json:"truncated"`
	Files         []ChangedFile `json:"files"`
}

type TurnRollback struct {
	UserMessageID      string                 `json:"userMessageId"`
	AssistantMessageID string                 `json:"assistantMessageId"`
	WorkspacePath      string                 `json:"workspacePath"`
	CreatedAt          string                 `json:"createdAt"`
	Unsupported        bool                   `json:"unsupported"`
	UnsupportedReason  string                 `json:"unsupportedReason,omitempty"`
	Files              []RollbackFileSnapshot `json:"files"`
}

type RollbackFileSnapshot struct {
	Path              string `json:"path"`
	ExistedBefore     bool   `json:"existedBefore"`
	ContentBase64     string `json:"contentBase64,omitempty"`
	ContentHash       string `json:"contentHash,omitempty"`
	PostHash          string `json:"postHash,omitempty"`
	UnsupportedReason string `json:"unsupportedReason,omitempty"`
}

type PromptCard struct {
	ID                 string `json:"id"`
	Kind               string `json:"kind"`
	Title              string `json:"title"`
	Message            string `json:"message"`
	AllowText          bool   `json:"allowText"`
	SessionID          string   `json:"sessionId"`
	AssistantMessageID string   `json:"assistantMessageId"`
	WorkspacePath      string   `json:"workspacePath"`
	DiffPath           string   `json:"diffPath,omitempty"`
	Diff               string   `json:"diff,omitempty"`
	Status             string   `json:"status,omitempty"`
	ResolvedAt         string   `json:"resolvedAt,omitempty"`
	CreatedAt          string   `json:"createdAt"`
	// RiskLevel carries the kernel-side risk classification (low/medium/high)
	// from tool.approval_required event's inline ApprovalPreviewResponse. The
	// shell renders this for styling (color/icon) but does not decide it —
	// AGENTS.md §3: shells render, kernel decides.
	RiskLevel string `json:"riskLevel,omitempty"`
	// Questions carries structured request_user_input questions (Plan mode).
	// When non-empty, the card renders as a multi-choice prompt instead of the
	// flat Options list. Mirrors core's RequestUserInputQuestion.
	Questions []bridgeRequestUserInputQuestion `json:"questions,omitempty"`
	// CallID is the request_user_input call identifier used to resolve via
	// approval/respond (it doubles as the approval_id in core's pending store).
	CallID string `json:"callId,omitempty"`
}

// bridgeRequestUserInputQuestion mirrors core's RequestUserInputQuestion,
// used by the request_user_input tool (Plan mode only).
type bridgeRequestUserInputQuestion struct {
	ID       string                         `json:"id"`
	Header   string                         `json:"header"`
	Question string                         `json:"question"`
	Options  []bridgeRequestUserInputOption `json:"options,omitempty"`
}

type bridgeRequestUserInputOption struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}
