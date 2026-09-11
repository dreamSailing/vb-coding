package i18n

var zhText = map[string]string{
	"error.attachment.path_required":                  "文件路径为空",
	"error.attachment.preview_workspace_required":     "当前没有可预览的工作区",
	"error.attachment.preview_text_workspace_only":    "只能预览当前工作区内的文本文件",
	"error.automation.template_title_required":        "模板标题不能为空",
	"error.automation.template_prompt_required":       "模板提示词不能为空",
	"error.automation.template_preset_edit_forbidden": "预设模板不能编辑，请新建自定义模板",
	"error.automation.template_id_required":           "模板 ID 不能为空",
	"error.automation.template_custom_delete_only":    "只能删除自定义模板；预设模板不可删除",
	"error.automation.template_not_found":             "自动化模板不存在",
	"error.automation.template_schedule_required":     "该模板没有 cron 表达式，无法启用定时调度",
	"error.rules.home_unavailable":                    "用户目录不可用",
	"error.memory.note_empty":                         "记忆笔记内容为空",
	"memory.note_saved":                               "记忆笔记已添加",
	"error.rules.workspace_path_missing":              "工作区规则保存缺少工作区路径",
	"error.rules.workspace_path_required":             "工作区规则路径不能为空",
	"error.rules.scope_unknown":                       "未知规则作用域",
	"error.system.path_required":                      "路径不能为空",
	"error.system.relative_path_workspace_required":   "无法解析相对路径：当前没有活动工作区",
	"error.terminal.workspace_path_required":          "工作区路径不能为空",
	"error.system.external_app_unknown":               "未知的外部应用",
	"error.system.external_app_unavailable":           "该应用未在本机安装",
	// Approval card UI strings. The kernel (tool.approval_required event) now
	// inlines an ApprovalPreviewResponse with a risk reason; these are only the
	// fallbacks when the kernel provided no reason. 按钮文案已迁至前端 i18n
	// （选项集由前端按 kind 派生，见 ItemApprovalState 注释）。
	"approval.card.title":                       "审批确认",
	"approval.card.message_default":             "检测到高风险操作，需要先确认。",
	"approval.notification.title":               "出现审批请求",
	"approval.notification.message_default":     "等待确认",
	"approval.runtime_event.detail":             "检测到需要用户确认的操作。",
	"approval.message_status.text":              "等待确认…",
	"approval.resolved.allowed":                 "已允许",
	"approval.resolved.allowed_session":         "已允许（本次会话不再询问）",
	"approval.resolved.denied":                  "已拒绝",
	"approval.resolved.cancelled":               "已取消",
	"approval.resolved.default":                 "已确认",
	"request_user_input.resolved.answered":      "已回答计划问题",
	"request_user_input.message_status.text":    "等待回答计划问题…",
	"request_user_input.notification.submitted": "计划问题已提交",
	// 会话生命周期状态行（消息流内联 status item 文案）。
	"conversation.stopped_manual": "已手动停止",
	"conversation.interrupted":    "已中断（应用已重启）",

	// eos web 模式服务壳文案。
	"web.server.ready":    "eos web 已启动：%s（前端目录：%s，Ctrl+C 退出）",
	"web.server.shutdown": "eos web 正在关闭……",
}
