package ai

// Copyright (c) 2026 EOSAIOS
// SPDX-License-Identifier: EOS-NCL-1.1
// 本文件基于 EOS 非商用许可证 v1.1 发布，详见 LICENSE。
// 商业使用请联系版权人获得商业授权。

import (
	"strings"

	"github.com/eosaios/eos/pkg/coreapi"
)

// ModelCatalogEntry 模型目录条目
type ModelCatalogEntry struct {
	ID                      string             // 唯一标识
	Name                    string             // 显示名称
	Provider                ProviderType       // 服务商类型
	ModelName               string             // API 调用时的模型名称
	APIType                 APIType            // API 类型
	Plan                    string             // 内核 preset 的 plan 值（开放字符串：api/coding/token/agent）
	PlanFormat              string             // 内核 preset 的请求格式：openai_chat/openai_responses/anthropic
	PlanModels              []coreapi.PlanModel // 套餐类 preset 内可选的模型（非空 = 套餐类，模型可从中选择）
	ContextWindow           int                // 上下文窗口大小
	ThinkingCap             ThinkingCapability // 思考能力等级
	SupportsVision          bool               // 是否支持视觉
	SupportsImageGeneration bool               // 是否支持图片生成
	SupportsVideoGeneration bool               // 是否支持视频生成
	SupportsSpeechSynthesis bool               // 是否支持语音合成
	SupportsTools           bool               // 是否支持工具调用
	SupportsReasoningEffort bool               // 是否支持 ReasoningEffort 参数
	Tags                    []string           // 标签（推荐、免费、推理等）
	Description             string             // 描述
}

type ModelCatalog struct {
	models     map[string]*ModelCatalogEntry
	byProvider map[ProviderType][]*ModelCatalogEntry
	entries    []*ModelCatalogEntry
}

// NewModelCatalog 创建模型目录
func NewModelCatalog() *ModelCatalog {
	return &ModelCatalog{
		models:     make(map[string]*ModelCatalogEntry),
		byProvider: make(map[ProviderType][]*ModelCatalogEntry),
		entries:    make([]*ModelCatalogEntry, 0),
	}
}

// Get 根据 ID 获取模型
func (mc *ModelCatalog) Get(id string) *ModelCatalogEntry {
	return mc.models[strings.ToLower(strings.TrimSpace(id))]
}

// GetByProvider 获取指定服务商的所有模型
func (mc *ModelCatalog) GetByProvider(provider ProviderType) []*ModelCatalogEntry {
	return mc.byProvider[provider]
}

// GetAll 获取所有模型
func (mc *ModelCatalog) GetAll() []*ModelCatalogEntry {
	result := make([]*ModelCatalogEntry, 0, len(mc.entries))
	result = append(result, mc.entries...)
	return result
}

// globalCatalog 全局模型目录
var globalCatalog = NewModelCatalog()

func (mc *ModelCatalog) replaceAll(entries []*ModelCatalogEntry) {
	mc.models = make(map[string]*ModelCatalogEntry, len(entries))
	mc.byProvider = make(map[ProviderType][]*ModelCatalogEntry)
	mc.entries = make([]*ModelCatalogEntry, 0, len(entries))
	for _, entry := range entries {
		if entry == nil {
			continue
		}
		mc.models[strings.ToLower(strings.TrimSpace(entry.ID))] = entry
		mc.byProvider[entry.Provider] = append(mc.byProvider[entry.Provider], entry)
		mc.entries = append(mc.entries, entry)
	}
}

// GetModelEntry 根据 ID 获取模型（使用全局目录）
func GetModelEntry(id string) *ModelCatalogEntry {
	return globalCatalog.Get(id)
}

// GetAllModels 获取所有模型（使用全局目录）
func GetAllModels() []*ModelCatalogEntry {
	return globalCatalog.GetAll()
}

// GetModelsByProvider 获取指定服务商的所有模型（使用全局目录）
func GetModelsByProvider(provider ProviderType) []*ModelCatalogEntry {
	return globalCatalog.GetByProvider(provider)
}

