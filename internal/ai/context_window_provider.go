package ai

// Copyright (c) 2026 EOSAIOS
// SPDX-License-Identifier: EOS-NCL-1.1
// 本文件基于 EOS 非商用许可证 v1.1 发布，详见 LICENSE。
// 商业使用请联系版权人获得商业授权。

import (
	"strings"
	"sync"
)

var contextWindowOverrides sync.Map

func getContextWindowOverride(model string) (int, bool) {
	m := strings.ToLower(strings.TrimSpace(model))
	if m == "" {
		return 0, false
	}
	if v, ok := contextWindowOverrides.Load(m); ok {
		if n, ok2 := v.(int); ok2 && n > 0 {
			return n, true
		}
	}
	return 0, false
}

