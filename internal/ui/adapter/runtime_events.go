package adapter

// Copyright (c) 2026 EOSAIOS
// SPDX-License-Identifier: EOS-NCL-1.1
// 本文件基于 EOS 非商用许可证 v1.1 发布，详见 LICENSE。
// 商业使用请联系版权人获得商业授权。

// bridge 边界说明: 本文件已脱离 bridge 依赖。
// 旧版本的 normalizeRuntimeEvent(bridge.Event) 在 Go legacy 引擎废弃后被删除。
// 事件归一化由 core_client.go 中的 runtimeEventFromEnvelope 负责，本文件只保留
// RuntimeEvent / PromptResponse 两个数据类型。

type RuntimeEvent struct {
	Type    string
	RID     string
	Content string
	Data    map[string]any
}

type PromptResponse struct {
	Decision    string
	Option      string
	OptionIndex int
	Text        string
}
