package ui

// app_versions.go — 应用版本检查与更新提示。
//
// checkForUpdates 在后台调用 update.CheckLatest；若发现新版本则
// 通过 VersionCheckMsg 回到 Update，由 handleVersionCheck 把信息
// 写到 shell 的状态栏。
//
// VersionCheckMsg 类型本身定义在 msg.go，本文件只承载检查与处理函数。
//
// 代码原位于 app.go，仅做物理拆分，不改行为。

import (
	"context"

	tea "charm.land/bubbletea/v2"
	"github.com/eosaios/eos/internal/config"
	"github.com/eosaios/eos/internal/update"
)

// checkForUpdates checks for updates in the background
func (m *AppModel) checkForUpdates() tea.Cmd {
	return func() tea.Msg {
		// 更新代理开关（~/.eos.json update_proxy_*，config set update_proxy）
		// 与命令行 update 同源；地址非法时直接跳过本次检查（配置错误不应
		// 阻塞 TUI 启动，静默不提示，由用户主动 config get 时发现）。
		cfg, _ := config.Load()
		client, err := update.NewHTTPClient(config.EffectiveUpdateProxyURL(&cfg))
		if err != nil {
			return nil
		}
		result, err := update.CheckLatestWithClient(context.Background(), client)
		if err != nil || result == nil {
			return nil
		}
		return VersionCheckMsg{Result: result}
	}
}

func (m *AppModel) handleVersionCheck(msg VersionCheckMsg) {
	if msg.Result != nil && msg.Result.HasUpdate {
		m.shell.SetUpdateInfo(msg.Result)
	}
}
