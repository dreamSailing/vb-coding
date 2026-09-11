# eos-cli 真正死代码探测报告

> 探测时间：2026-09-11  
> 范围：`github.com/eosaios/eos`（eos-cli）  
> 方法：`golang.org/x/tools/cmd/deadcode` 全仓静态可达性分析 + import 扫描 + 新旧路径对照 + 架构守卫/注释/git 历史交叉验证

---

## 1. 判定口径（重要）

本报告**只收「真正死代码」**，即：

| 收录 | 不收 |
|------|------|
| 功能已有**新实现**，旧实现残留且**生产路径零调用** | 有实现但尚未接入（预留/未来功能） |
| 同一职责存在双轨 API，旧轨已被调用方迁走 | 通用工具库从未接线（grab-bag / 超前抽象） |
| 架构迁移后的遗留包（注释/守卫测试明确 retired） | **测试专用**构造器/fake（deadcode 默认不把 test 当 root） |
| 显式注释「兼容入口 / 已退役」但调用方已消失 | 平台 build-tag 真实路径（如 Windows clipboard DIB） |

`deadcode` 原始结果约 **568 个不可达函数**。其中大量来自 `internal/pkg/{cache,lazy,monitor,utils}` 等**从未接线的工具库**，按口径**不计入**真正死代码主清单（见 §5）。

全仓非测试 Go 约 **75.8k 行**；下文按「替换关系」聚类。

---

## 2. 主清单：真正死代码（按置信度）

### A. 架构迁移残留（置信度：高）

旧 Go/Eino 内核已整体删除（`internal/bridge` / `internal/runtime` / `internal/tools` / `pkg/core`），引擎收敛为 **Rust sidecar + stdio JSON-RPC**。下列包/函数是迁移后未清掉的旧轨。

| # | 位置 | 规模 | 零引用证据 | 新实现（替代方） |
|---|------|------|------------|------------------|
| A1 | `internal/lsp/**`（client/detector/manager/diagnostics/safe） | **~1684 行 / 62 函数** | 除自身外 **0 个生产 import**；仅反向依赖死掉的 `internal/pkg/events` | Rust 内核 LSP：`internal/webbridge/bridge_runtime_rpc_lsp.go`、`pkg/coreapi/lsp_projection.go`（诊断投影仍活） |
| A2 | `internal/search/search.go` | **~390 行 / 16 函数** | 0 import | Rust 内核 tools（file/dir search） |
| A3 | `internal/pkg/plugins/**`（discovery/external/mcp/plugin） | **~597 行 / 31 函数** | 0 生产 import | `internal/webbridge/bridge_plugins.go` + `bridge_runtime_rpc_plugins.go`（`PluginInstall/List/Remove/Search`、`setPluginEnabledRPC`） |
| A4 | `internal/pkg/events/bus.go` | **~120 行 / 6 函数** | 唯一 import 来自死掉的 `internal/lsp` | 内核 JSON-RPC 事件流（`runtime_rpc_events` / turn event stream） |
| A5 | `internal/hooks/**` | **~73 行 / 1 函数** | 0 import | 内核/插件侧 hooks（webbridge plugins 能力） |
| A6 | `internal/notify/notify.go` | **~55 行 / 3 函数** | 0 import | webbridge 降级通知 `bridge_degraded_notify.go`、TUI 系统消息 |

**架构侧证据**：`internal/architecture/import_boundary_test.go` 明确写「旧 Go/Eino 内核已整体删除」；`engineprovider/provider.go:17` 注明「legacy/parity 已退役，不再有 Go 内核回退路径」。

---

### B. 配置 CRUD 双轨（置信度：高）

`internal/config.Load/Save` 仍被广泛使用（语言、开关、工作区等），但**模型 / MCP / Skills / 插件开关** 的 Go 侧 CRUD 整段已无生产调用方。

| # | 符号（`internal/config/config.go` 等） | 零引用 | 新实现 |
|---|------------------------------------------|--------|--------|
| B1 | `AddModel` / `UpdateModel` / `DeleteModel` / `SetActive` / `FindModelByName` / `NormalizeModelRole` / `ModelRoleValue` / `ModelEnabled` / `SupportsCapability` / `ResolveCapabilityModel` / `ClearCapabilityModelRefsForModel` / `InferDefaultModel` | 生产代码 0 调用 | 内核 `model/save`：`adapter.SaveModel` → `CoreSaveModelRPC`；GUI 侧 `CapabilityService.UpsertModel/SaveModel/DeleteModel/ActivateModel/SelectCurrentModel` |
| B2 | `AddMCPServer` / `UpdateMCPServer` / `DeleteMCPServer` / `GetEnabledMCPServers` / `ToggleMCPServer` | 0 调用 | `CapabilityService.UpsertMCP/DeleteMCP/SetMCPEnabled/ImportMCPJSON` |
| B3 | `AddSkillsDir` / `UpdateSkillsDir` / `DeleteSkillsDir` / `GetEnabledSkillsDirs` / `ToggleSkillsDir` / `IsSkillDisabled` / `SetSkillDisabled` | 0 调用 | `CapabilityService.SetSkillEnabled/ReloadSkills*` |
| B4 | `PluginEnabled` / `SetPluginEnabled` | 生产 0 调用（仅死掉的 plugins 包引用 `PluginEnabled`） | webbridge `setPluginEnabledRPC` |

合计约 **27 个死函数**（`config.go` 单文件 1053 行中的大段 CRUD）。

---

### C. 更新检查/应用：旧无 client API（置信度：高）

生产路径（CLI `eos update`、TUI 版本检查、webbridge）统一先构造带代理的 `*http.Client`，再走 `*WithClient`。

| # | 旧 API（死） | 新 API（活） | 生产调用点 |
|---|--------------|--------------|------------|
| C1 | `update.CheckLatest` | `CheckLatestWithClient` | `internal/cli/update.go:38`、`internal/ui/app_versions.go:32` |
| C2 | `update.CheckLatestWithProxy` | `NewHTTPClient` + `CheckLatestWithClient`（注释仍写「代理开关入口」但已迁走） | 同上 |
| C3 | `update.CheckLatestFor` | 仅被死掉的 `CheckLatest` 调用 | — |
| C4 | `update.Apply` | `ApplyWithClient` | `internal/cli/update.go:66` |

---

### D. Print 流式双 API（置信度：高）

| # | 旧 | 新 | 证据 |
|---|----|----|------|
| D1 | `cli.RunPrintModeStream`（`internal/cli/print.go:124`） | `RunPrintMode` 内 `stream-json` 分支 → `runStreamJSONTurn` | root/exec 的 `-p/--print` 与 `--output stream-json` 都进 `RunPrintMode`；`RunPrintModeStream` 生产 0 调用 |

---

### E. 权限模式兼容壳（置信度：高）

| # | 符号 | 证据 | 替代 |
|---|------|------|------|
| E1 | `modes.SandboxModeFromAccessMode`（`internal/modes/modes.go:203`） | 注释自承「保留为兼容入口：沙箱轴与访问轴共用词表后即为恒等映射」；**0 调用** | 直接 `NormalizeAccessMode` / `NormalizeSandboxMode`（同一词表） |

---

### F. i18n 死 re-export（置信度：高）

| # | 符号 | 证据 | 替代 |
|---|------|------|------|
| F1 | `pkg/i18n.T`（12 行） | 0 生产 import；仅转发 `internal/i18n.T` | CLI/TUI 用 `internal/i18n`；GUI 用 `internal/webbridge/i18n`（第三套） |

---

### G. TUI 旧 feature 模块（置信度：高）

| # | 包 | 规模 | 0 import | 替代 |
|---|----|------|----------|------|
| G1 | `internal/ui/features/history` | ~175 行 / 9 函数 | 是 | 会话历史在内核；TUI 输入历史未走此包 |
| G2 | `internal/ui/features/plan` | ~191 行 / 7 函数 | 是 | `internal/ui/views/confirm`（Request/Model，活） |
| G3 | `internal/ui/features/thinking` | ~145 行 / 13 函数 | 是 | `internal/ui/components/messages` 的 `ThinkingMessage` 路径 |
| G4 | `internal/pkg/history` | ~103 行 / 4 函数 | 是 | 同上，无调用方 |

---

### H. AI Go 目录/能力 API 残片（置信度：中高，包内部分仍活）

`internal/ai` 包**仍被 setup 向导 / model resolve 使用**（`GetAllProviders`、`GetModelsByProvider`、`GetCatalogContextWindow`、`SupportsVisionFromCatalog`、`ApplyCoreModelCatalog` 等），但下列旧 API 已被 **Rust catalog 投影**取代且零调用：

| # | 死符号簇 | 替代 |
|---|----------|------|
| H1 | `model_catalog.go`：`Search` / `FilterByTags` / `GetRecommended` / `GetAllModels` / `SearchModels` / `FilterModelsByTags` / `GetRecommendedModels` / `CatalogEntryToModelInfo` / `GetBuiltinModelInfo` / `BuiltinSupportsThinking` / `BuiltinSupportsReasoningEffort` / `BuiltinGetThinkingCapability` | `ApplyCoreModelCatalog` + 从 core 投影读目录 |
| H2 | `models.go`：`ParseThinkingCapability` / `SupportsThinking` / `SupportsReasoningEffort` / `GetThinkingCapability` | 内核 catalog 字段 |
| H3 | `detection.go`：`ShouldEnableThinkingForModel` / `GetReasoningEffortLevel` / `GetModelCapabilitySummary` | 同上 |
| H4 | `providers.go`：`GetByID` / `DetectProvider` / `extractDomain` / `GetProviderByID` / `DetectProviderByBase` / `GetCodePlanModelNames` / `IsCodePlanModel` / `ParseProviderType` | rust catalog / `model/save` 白名单 |
| H5 | `context_window_provider.go`：`SetContextWindowOverride` / `PrimeContextWindowFromProvider` / `fetchContextWindowOpenAICompatible` / `findAnyInt` | `GetCatalogContextWindow`（向导仍用） |
| H6 | `rust_catalog.go`：`AllowCustomProviderFromCatalog` / `AllowCustomModelFromCatalog` | 向导前置校验已改走其他白名单路径（零调用） |
| H7 | `provider_resolver.go`：`SupportsToolsFromCatalog` | 内核能力 |
| H8 | `types.go`：`Capability.String` / `ParseCapability` | 内核类型 |

约 **30+ 函数**。

---

### I. 协议层：WS 传输 + 旧 payload/校验（置信度：中高）

| # | 位置 | 规模 | 替代 |
|---|------|------|------|
| I1 | `pkg/protocol/jsonrpc/ws.go` + `ws_client.go` | ~336 行 / 25 函数 | sidecar/stdin **stdio stream**（`stream.go`/`stream_client.go` 仍活）；`serve.go` 仅注释提及 `ServeWS` |
| I2 | `pkg/protocol/payloads.go` + `validate.go` | ~309 行 / 14 函数 | 事件/DTO 走 `pkg/coreapi` 类型与内核侧校验；生产 0 调用 payload helpers / `ValidateEnvelope` |

---

### J. 其他双轨/薄壳（置信度：中）

| # | 位置 | 说明 | 替代 |
|---|------|------|------|
| J1 | `pkg/sandbox.DetectBackend` / `DetectBackendForOS` | Go 侧探测后端，0 调用 | `Sandbox.BackendStatus` RPC（sidecar engine 实现） |
| J2 | `internal/ui/startup.go` `StartInteractiveTUI` | 薄包装 `StartInteractiveTUIWithOptions` | CLI `root.go` 直接调 WithOptions |
| J3 | `internal/webbridge.NewBridgeService` | 薄包装 `NewBridgeServiceWithOptions` | `server.go:60` 用 Options |
| J4 | `internal/ui/render.Formatter.*` + `HighlightCodeANSI`（`formatter.go` 大部 / `highlight.go` 部分） | 格式化已内联到 lipgloss/TUI 组件 | 同包 `MarkdownRenderer` / `HighlightDiffANSI` / `DefaultChromaTheme` 仍活 |
| J5 | `internal/ui/adapter/runtime_events.go` 中 `ensurePayloadText` / `ensureToolName` / `ensureToolResult` / `ensureApprovalPayload` / `ensureInquiryPayload` / `ensureProtocolPayload` / `cloneDataMap` / `stringValue` | 文件头注明：旧 `normalizeRuntimeEvent(bridge.Event)` 在 Go legacy 引擎废弃后删除，这些 helper 成了孤儿 | `core_client.go` 的 `runtimeEventFromEnvelope` |
| J6 | `main.go` `_ "github.com/eosaios/eos/internal/pkg/utils"` | blank import 仅为 `utils.init()` 装日志 handler；包内大量 helper（http/path/params/retry/token…）无调用 | 日志初始化应迁到显式 `utils.NewLogger` 调用（另题，非「旧实现替换」主清单） |

---

## 3. 规模汇总（真正死代码主清单）

| 聚类 | 估计可删（非测试） | 置信度 |
|------|-------------------|--------|
| A. Go runtime 残留（lsp/search/plugins/events/hooks/notify） | **~2900 行** | 高 |
| B. config CRUD | ~250–350 行（散落函数） | 高 |
| C. update 旧 API | ~40 行（薄包装） | 高 |
| D. RunPrintModeStream | ~80 行 | 高 |
| E. SandboxModeFromAccessMode | ~5 行 | 高 |
| F. pkg/i18n | 12 行 | 高 |
| G. TUI 旧 features + pkg/history | ~614 行 | 高 |
| H. ai 旧 catalog/能力 API | ~400–600 行（包内部分保留） | 中高 |
| I. WS 传输 + protocol payloads/validate | ~645 行 | 中高 |
| J. 其他双轨/薄壳/孤儿 helper | ~400–500 行 | 中 |
| **合计（保守）** | **约 5.5k–6.5k 行** | — |

> 精确到行需逐文件 PR 级删除并跑测试；上表为静态可达性 + 对照后的区间估计。

---

## 4. 验证方法（可复现）

```bash
# 1) 安装并跑可达性
go install golang.org/x/tools/cmd/deadcode@latest
cd eos-cli && deadcode -json ./... > /tmp/deadcode.json

# 2) 对候选包做「生产 import 为零」复核（排除 _test.go）
rg -n 'github.com/eosaios/eos/internal/lsp"' --type go --glob '!*_test.go'
rg -n 'github.com/eosaios/eos/internal/pkg/plugins' --type go --glob '!*_test.go'
# …同理 search/hooks/notify/events/ui/features

# 3) 对照新路径
rg -n 'SaveModel|model/save|PluginInstall|CheckLatestWithClient|runStreamJSONTurn' --type go --glob '!*_test.go'
```

注意：`deadcode` **不把测试当调用 root**。下列**不算**生产死代码（测试仍在用）：

- `sidecar.NewRemoteEngine`、`sidecar/client.Attach`
- `ui.NewAppModelFromCoreEngine`、`adapter.NewCoreClientAdapterFromEngine`（注释写明「供测试场景」）

---

## 5. 明确排除（不是「旧实现被替换」）

| 包/符号 | 原因 |
|---------|------|
| `internal/pkg/cache`（~823 行） | 超前通用缓存库，从未接线，无「新实现替换」叙事 |
| `internal/pkg/lazy`（~455 行） | 同上 |
| `internal/pkg/monitor`（~416 行） | 同上 |
| `internal/pkg/utils` 大部分 helper | grab-bag；包因 `log.init` 被 blank import，**函数**死≠整包是旧实现 |
| `internal/pkg/clip` `dibToPNG`/`dibToRGBA` | Windows clipboard 路径（`image_fallback_windows.go`），build-tag 真实使用 |
| `internal/document`、`internal/serve` | CLI 子命令仍引用 |
| `internal/ui/render` 的 Markdown/Diff | 仍被 confirm/messages/app_messages 使用 |

---

## 6. 建议清理优先级

1. **P0（整包删除，风险低、收益大）**  
   `internal/lsp`、`internal/search`、`internal/pkg/plugins`、`internal/pkg/events`、`internal/hooks`、`internal/notify`、`internal/pkg/history`、`internal/ui/features/{history,plan,thinking}`、`pkg/i18n`  
   → 删除后跑 `go build ./...` + `go test ./...` + architecture 边界测试。

2. **P1（API 收敛）**  
   - 删 `internal/config` 中模型/MCP/Skills CRUD 死函数（保留 Load/Save 与非模型字段）。  
   - 删 `update` 旧无 client 包装、`RunPrintModeStream`、`StartInteractiveTUI`、`NewBridgeService`、`SandboxModeFromAccessMode`。  
   - 删 `pkg/protocol/jsonrpc` WS 与 `payloads`/`validate` 死段。

3. **P2（包内瘦身）**  
   - `internal/ai` 旧 catalog/detect/thinking API 与 setup 仍用符号拆分。  
   - `ui/render` Formatter 孤儿。  
   - `ui/adapter/runtime_events.go` 事件归一化孤儿 helper。  
   - 把 `main.go` blank import 的日志 init 改为显式调用，再评估 `utils` 子集。

4. **P3（另开议题，本报告不主张直接删）**  
   `internal/pkg/{cache,lazy,monitor}` 与 utils grab-bag：要么接线要么归档，避免继续被当成「可用基础设施」。

---

## 7. 结论

- eos-cli 已完成 **Go 内核 → Rust sidecar** 架构迁移，守卫测试锁住了「旧核心包不回流」。  
- 但迁移后仍残留 **~5.5k–6.5k 行**「有新实现、旧实现零生产调用」的真正死代码，集中在：  
  1）Go LSP/search/plugins/events 栈；  
  2）config 模型/MCP/Skills CRUD；  
  3）update/print/constructors 的旧 API 薄壳；  
  4）TUI 旧 feature 模块；  
  5）ai/protocol/ws 残片。  
- 这些与「实现了但还没接入」的 cache/lazy/monitor/utils **不是同一类**，清理时应分开 PR、分层验收。

## 8. 清理执行记录（2026-09-11，本次已完成）

按「只清被替换的旧实现、不清未接线的新机制」口径执行完毕，`go build` / `go vet` / `go test ./...` 全绿。

### 已清理

- **A 组全部**：`internal/lsp`（含 `scripts/download_gopls.go`——唯一用途是给 lsp 放 gopls）、`internal/search`、`internal/pkg/plugins`、`internal/pkg/events`、`internal/hooks`、`internal/notify` 整包删除。
- **B 组全部**：config.go 中模型/MCP/Skills/插件 CRUD 共 26 个死函数 + `ModelRole*`/`Capability*` 孤儿常量 + `CapabilityModelRefs.Get/Set`（唯一调用方是同批死函数）。
- **C 组全部**：`update.CheckLatest` / `CheckLatestWithProxy` / `CheckLatestFor` / `Apply`（私有 `checkLatestFor` 被 `CheckLatestWithClient` 复用，保留）。
- **D/E/F 组全部**：`RunPrintModeStream`、`SandboxModeFromAccessMode`、`pkg/i18n` 整包。
- **G 组**：`internal/ui/features/{history,plan,thinking}`、`internal/pkg/history` 删除（`features/slash` 活，保留）。
- **H 组大部分**：model_catalog 旧检索 API（Search/FilterByTags/GetRecommended 及包级转发）、能力查询链（`CatalogEntryToModelInfo`/`GetBuiltinModelInfo`/`Builtin*`）、models.go 薄转发（`SupportsThinking`/`SupportsReasoningEffort`/`GetThinkingCapability`/`ParseThinkingCapability`）、detection.go 决策函数、providers.go 的 `DetectProvider(DetectProviderByBase)`/`extractDomain`/`GetCodePlanModelNames`/`IsCodePlanModel`/`ParseProviderType`、context_window 的 `SetContextWindowOverride`/`Prime*`/`fetch*`/`findAnyInt`、types.go 的 `Capability` 整套。
- **I 组全部**：jsonrpc `ws.go`/`ws_client.go`/`ws_test.go`、protocol `payloads.go`/`validate.go`。
- **J 组**：`sandbox.DetectBackend(ForOS)`、`StartInteractiveTUI`、`NewBridgeService`、`render/formatter.go` 整文件、`HighlightCodeANSI`、adapter `runtime_events.go` 八个孤儿 helper（保留 `RuntimeEvent`/`PromptResponse` 类型）。

### 验证后保留（与原报告差异）

| 符号 | 原报告 | 保留原因 |
|------|--------|----------|
| `ai.GetAllModels`、`ai.GetProviderByID`、`ai.AllowCustomProviderFromCatalog`、`ai.AllowCustomModelFromCatalog`、`ai.SupportsToolsFromCatalog` | H1/H4/H6/H7 判死 | 均为 Rust 迁移时**新写**的快照机制查询面（读 `globalCatalog`/`globalRegistry`），被活测试 `rust_catalog_test.go` 锁行为，属「未接线的新实现」而非旧残留 |
| `ai.GetByID`、`ai.GetAPIBase`、`ai.DetectThinkingCapability`、`ThinkingCapability` 枚举 | — | 分别被 `GetProviderByID`、setup 向导、`ApplyCoreModelCatalog` 使用 |
| `config.boolPtr` | （连带） | 被活测试使用 |
| `main.go` blank import `internal/pkg/utils` | J6 | 报告自认另题：是日志初始化接线，非旧实现残留 |
| `internal/pkg/{cache,lazy,monitor}`、utils grab-bag | §5 排除 | 从未接线的基础设施，无替换叙事，不在本次口径内 |

### 附带同步

- `internal/architecture/process_boundary_test.go`：从进程执行白名单移除三个已删文件的调用点。
- `pkg/protocol/protocol_test.go`：删除依赖已删 payload/validate 的测试；`NewEvent` 存活测试改用裸 map。
- `pkg/sandbox/policy_test.go`：round-trip 测试改字面量构造；删除纯测已删函数行为的两个测试。
- `internal/ai/{detection,models}_test.go`：删除死函数测试，保留存活函数测试。

---

*本报告只做探测与证据归档，未删除任何生产代码。（§8 除外：2026-09-11 已按上述范围执行清理。）*
