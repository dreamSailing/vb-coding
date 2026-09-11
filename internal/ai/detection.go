package ai

// Copyright (c) 2026 EOSAIOS
// SPDX-License-Identifier: EOS-NCL-1.1
// 本文件基于 EOS 非商用许可证 v1.1 发布，详见 LICENSE。
// 商业使用请联系版权人获得商业授权。

import (
	"strings"

)

// DetectThinkingCapability 尝试从模型名称检测思考能力
// 作为字典的后备方案，当模型不在字典中时使用
func DetectThinkingCapability(modelName string) ThinkingCapability {
	name := strings.ToLower(strings.TrimSpace(modelName))

	// OpenAI o1 系列
	if strings.Contains(name, "o1") {
		if strings.Contains(name, "mini") {
			return ThinkingMedium
		}
		if strings.Contains(name, "preview") {
			return ThinkingMedium
		}
		return ThinkingHigh
	}

	// DeepSeek Reasoning 系列
	if strings.Contains(name, "deepseek") && (strings.Contains(name, "r1") || strings.Contains(name, "reasoning") || strings.Contains(name, "reasoner")) {
		return ThinkingHigh
	}

	// Kimi 系列
	if strings.Contains(name, "kimi") && (strings.Contains(name, "k2.5") || strings.Contains(name, "k2-5") || strings.Contains(name, "thinking")) {
		return ThinkingMedium
	}

	// GLM 系列
	if strings.Contains(name, "glm") && (strings.Contains(name, "4.7") || strings.Contains(name, "4.6") || strings.Contains(name, "thinking")) {
		return ThinkingMedium
	}

	// Qwen Reasoning 系列
	if strings.Contains(name, "qwen") && strings.Contains(name, "thinking") {
		return ThinkingMedium
	}
	if strings.Contains(name, "qwen") && (strings.Contains(name, "reasoning") || strings.Contains(name, "qwq")) {
		return ThinkingHigh
	}

	// Doubao / Ark 系列
	if strings.Contains(name, "doubao") || strings.Contains(name, "ark") {
		if strings.Contains(name, "thinking") || strings.Contains(name, "seed") {
			return ThinkingMedium
		}
		if strings.Contains(name, "code") {
			return ThinkingLow
		}
	}

	// 未来可以添加更多提供商的模式
	// 例如: Gemini Think, Meta COT 等

	return ThinkingNone
}

