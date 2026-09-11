package ai

// Copyright (c) 2026 EOSAIOS
// SPDX-License-Identifier: EOS-NCL-1.1
// 本文件基于 EOS 非商用许可证 v1.1 发布，详见 LICENSE。
// 商业使用请联系版权人获得商业授权。

// 模型能力的权威来源是 Rust 内核 model_catalog.rs（运行时通过 model/catalog
// 推送，见 rust_catalog.go::ApplyCoreModelCatalog）。本文件只保留：
//   - ThinkingCapability 枚举（ModelCatalogEntry.ThinkingCap / config 字段在用）
//   - ModelInfo 能力视图（catalog entry 的投影，供能力查询 API 使用）
// 能力查询由内核侧承担；自定义模型（不在 catalog 中）的思考能力由
// detection.go::DetectThinkingCapability 按模型名启发式推断。

// ThinkingCapability 定义模型支持思考的能力等级
type ThinkingCapability int

const (
	ThinkingNone ThinkingCapability = iota
	ThinkingLow
	ThinkingMedium
	ThinkingHigh
)

// String 返回思考能力的字符串表示
func (tc ThinkingCapability) String() string {
	switch tc {
	case ThinkingLow:
		return "low"
	case ThinkingMedium:
		return "medium"
	case ThinkingHigh:
		return "high"
	default:
		return "none"
	}
}

// ModelInfo 是模型能力元数据的只读视图，由 catalog entry 投影而来
// （见 model_catalog.go::CatalogEntryToModelInfo）。不再有本地写死的模型表。
type ModelInfo struct {
	Name                    string             // 模型名称
	Aliases                 []string           // 别名列表
	Thinking                ThinkingCapability // 思考能力等级
	SupportsReasoningEffort bool               // 是否支持 ReasoningEffort 参数
	Provider                string             // 提供商 (openai, anthropic, etc.)
}

