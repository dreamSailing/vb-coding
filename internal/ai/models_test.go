package ai

// Copyright (c) 2026 EOSAIOS
// SPDX-License-Identifier: EOS-NCL-1.1
// 本文件基于 EOS 非商用许可证 v1.1 发布，详见 LICENSE。
// 商业使用请联系版权人获得商业授权。

import (
	"testing"
)

// 本文件曾经测试一张本地写死的 builtinModels 表，该表已删除——模型能力的唯一
// 权威来源是 Rust 内核推送的目录（见 rust_catalog_test.go 对该路径的覆盖）。
// 这里只保留对 ThinkingCapability 枚举（纯函数）的测试，避免为静态值写测试。

func TestThinkingCapabilityString(t *testing.T) {
	tests := []struct {
		capability ThinkingCapability
		want       string
	}{
		{ThinkingNone, "none"},
		{ThinkingLow, "low"},
		{ThinkingMedium, "medium"},
		{ThinkingHigh, "high"},
		{ThinkingCapability(99), "none"}, // Unknown values default to "none"
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := tt.capability.String()
			if got != tt.want {
				t.Errorf("ThinkingCapability(%d).String() = %q, want %q", tt.capability, got, tt.want)
			}
		})
	}
}
