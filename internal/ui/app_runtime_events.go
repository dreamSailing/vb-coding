package ui

// app_runtime_events.go — 内核运行时事件的接收与转发。
//
// listenEvents 阻塞监听 sidecar 内核的事件流，事件到达后以 RuntimeEvent
// 形式回到 Update；handleRuntimeEvent 把 RuntimeEvent 转换为 UI 消息并
// 再次投递到 Update，同时重新武装事件监听以保持事件泵持续运转。
//
// 代码原位于 app.go，仅做物理拆分，不改行为。

import (
	"github.com/eosaios/eos/internal/ui/adapter"

	tea "charm.land/bubbletea/v2"
)

// listenEvents 监听运行时事件
func (m *AppModel) listenEvents() tea.Cmd {
	return func() tea.Msg {
		event := <-m.adapter.Events()
		return event
	}
}

// handleRuntimeEvent 处理 runtime event 消息
func (m *AppModel) handleRuntimeEvent(e adapter.RuntimeEvent) (tea.Model, tea.Cmd) {
	uiMsg := ConvertEvent(e)
	if uiMsg != nil {
		_, cmd := m.Update(uiMsg)
		// Always re-arm the event listener so the pump keeps draining events
		// even when the inner Update returned a nil cmd (e.g. ItemDeltaMsg).
		return m, tea.Batch(cmd, m.listenEvents())
	}
	return m, m.listenEvents()
}
