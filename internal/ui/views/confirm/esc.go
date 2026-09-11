package confirm

// Copyright (c) 2026 EOSAIOS
// SPDX-License-Identifier: EOS-NCL-1.1
// 本文件基于 EOS 非商用许可证 v1.1 发布，详见 LICENSE。
// 商业使用请联系版权人获得商业授权。

// esc.go 提供 Esc 键决策的归一化纯函数。
//
// 设计目标：消除「esc 按 confirm.Kind 一刀切」的壳层裁决
// （旧逻辑：permission→decline，其它→cancel）。改为基于弹框 options 内容
// 推断 esc 应发的 decision，与 eos-app workbench-approvals-logic.ts 的
// decisionForEsc 同一套硬性不变量（esc 永远是安全 abort，必须发 decision，
// 不能只关 UI 否则内核阻塞在等待 approval 上）。

import "strings"

// escCancelKeywords 是 esc 会匹配的「取消/拒绝」语义 option 关键词（小写）。
// 与 eos-app workbench-approvals-logic.ts 的 cancelKeywords 保持一致。
var escCancelKeywords = []string{
	"cancel", "decline", "deny", "reject", "abort",
	"取消", "拒绝", "驳回",
}

// EscDecision 根据 confirm 弹框的 options 推断 Esc 键应发送的 decision。
//
// 策略（对齐 eos-app decisionForEsc）：
//  1. 遍历 options，返回第一个含取消/拒绝语义关键词的 option 值；OptionIndex 为
//     该 option 的索引。例如 permission 的 options=[accept,acceptForSession,
//     decline,cancel] → 匹配 "decline"（索引 2），内核据此拒绝工具让 agent 续跑。
//  2. options 里没有取消/拒绝类按钮时，回退 decision="cancel"、OptionIndex=-1。
//     依赖 Decision=="cancel" 退出的路径（workspace_trust/bg_kill 等）仍按原逻辑退出。
//
// 返回 (decision, optionIndex)。纯函数，无副作用，不依赖 req.Kind——决策只看
// 用户实际看到的按钮，不由壳层按弹框类型硬编码。
func EscDecision(options []string) (decision string, optionIndex int) {
	for idx, option := range options {
		lower := strings.ToLower(strings.TrimSpace(option))
		if lower == "" {
			continue
		}
		for _, kw := range escCancelKeywords {
			if strings.Contains(lower, kw) {
				return option, idx
			}
		}
	}
	return "cancel", -1
}
