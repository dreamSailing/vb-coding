package ai

// Copyright (c) 2026 EOSAIOS
// SPDX-License-Identifier: EOS-NCL-1.1
// 本文件基于 EOS 非商用许可证 v1.1 发布，详见 LICENSE。
// 商业使用请联系版权人获得商业授权。

import (
	"testing"
)

func TestDetectThinkingCapability(t *testing.T) {
	tests := []struct {
		name  string
		model string
		want  ThinkingCapability
	}{
		{
			name:  "OpenAI o1 prefix",
			model: "o1-custom",
			want:  ThinkingHigh,
		},
		{
			name:  "OpenAI o1-mini prefix",
			model: "o1-mini-v2",
			want:  ThinkingMedium,
		},
		{
			name:  "DeepSeek R1 prefix",
			model: "deepseek-r1-local",
			want:  ThinkingHigh,
		},
		{
			name:  "Qwen thinking prefix",
			model: "qwen-thinking-max",
			want:  ThinkingMedium,
		},
		{
			name:  "Claude sonnet (no thinking)",
			model: "claude-3-5-sonnet",
			want:  ThinkingNone,
		},
		{
			name:  "GPT prefix (no thinking)",
			model: "gpt-4-turbo",
			want:  ThinkingNone,
		},
		{
			name:  "Unknown model",
			model: "unknown-xyz",
			want:  ThinkingNone,
		},
		{
			name:  "Empty string",
			model: "",
			want:  ThinkingNone,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectThinkingCapability(tt.model)
			if got != tt.want {
				t.Errorf("DetectThinkingCapability(%q) = %v, want %v", tt.model, got, tt.want)
			}
		})
	}
}
