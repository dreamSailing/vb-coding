// Copyright (c) 2026 EOSAIOS
// SPDX-License-Identifier: EOS-NCL-1.1
// 本文件基于 EOS 非商用许可证 v1.1 发布，详见 LICENSE。
// 商业使用请联系版权人获得商业授权。

package ui

import (
	"context"
	"encoding/json"
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/eosaios/eos/pkg/coreapi"
)

// handleScreenshotSlash /screenshot [窗口标题]
// 经内核 screenshot_capture 工具截图，产出 PNG 路径并挂为下一轮消息的
// 图片附件（与粘贴图片同通道 pendingImagePaths）；空参取焦点/最顶层
// 非 EOS 窗口（「上一个打开的软件」语义）。
func (m *AppModel) handleScreenshotSlash(args []string) tea.Cmd {
	m.clearPrediction()
	m.shell.ClearInput()
	if m == nil || m.adapter == nil {
		m.appendSystem("screenshot: core client 不可用", "error")
		return nil
	}

	windowHint := ""
	if len(args) > 0 {
		windowHint = args[0]
	}
	toolArgs := map[string]any{}
	if windowHint != "" {
		toolArgs["window"] = windowHint
	}
	rawArgs, _ := json.Marshal(toolArgs)

	result, err := m.adapter.ExecuteCoreTool(context.Background(), coreapi.ToolRequest{
		Name: "screenshot_capture",
		Args: rawArgs,
	})
	if err != nil {
		m.appendSystem(fmt.Sprintf("截图失败: %v", err), "error")
		return nil
	}
	if result.Status != "ok" {
		m.appendSystem(fmt.Sprintf("截图失败: %s", result.Error), "error")
		return nil
	}

	var out struct {
		Path   string `json:"path"`
		Width  int    `json:"width"`
		Height int    `json:"height"`
	}
	if err := json.Unmarshal(result.Output, &out); err != nil || out.Path == "" {
		m.appendSystem("截图结果缺少文件路径", "error")
		return nil
	}

	m.pendingImagePaths = append(m.pendingImagePaths, out.Path)
	m.appendSystem(fmt.Sprintf(
		"已截图 %dx%d 并挂为附件，发送消息时随图一起提交：%s",
		out.Width, out.Height, out.Path,
	), "info")
	return nil
}
