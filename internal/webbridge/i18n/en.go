package i18n

var enText = map[string]string{
	"error.attachment.path_required":                  "file path is required",
	"error.attachment.preview_workspace_required":     "no workspace is available for preview",
	"error.attachment.preview_text_workspace_only":    "only text files inside the current workspace can be previewed",
	"error.automation.template_title_required":        "template title is required",
	"error.automation.template_prompt_required":       "template prompt is required",
	"error.automation.template_preset_edit_forbidden": "preset templates cannot be edited; create a custom template instead",
	"error.automation.template_id_required":           "template ID is required",
	"error.automation.template_custom_delete_only":    "only custom templates can be deleted; preset templates are read-only",
	"error.automation.template_not_found":             "automation template was not found",
	"error.automation.template_schedule_required":     "this template has no cron expression, so scheduled execution cannot be enabled",
	"error.rules.home_unavailable":                    "user home directory is unavailable",
	"error.memory.note_empty":                         "memory note content is empty",
	"memory.note_saved":                               "Memory note added",
	"error.rules.workspace_path_missing":              "workspace rules save requires a workspace path",
	"error.rules.workspace_path_required":             "workspace rules path is required",
	"error.rules.scope_unknown":                       "unknown rules scope",
	"error.system.path_required":                      "path is required",
	"error.system.relative_path_workspace_required":   "cannot resolve a relative path because there is no active workspace",
	"error.terminal.workspace_path_required":          "workspace path is required",
	"error.system.external_app_unknown":               "unknown external app",
	"error.system.external_app_unavailable":           "this app is not installed on this machine",
	// Approval card UI strings (fallbacks; button labels moved to the frontend
	// i18n — the option set is derived per kind on the frontend side).
	"approval.card.title":                       "Approval required",
	"approval.card.message_default":             "A high-risk operation was detected and needs confirmation.",
	"approval.notification.title":               "Approval requested",
	"approval.notification.message_default":     "Waiting for confirmation",
	"approval.runtime_event.detail":             "An operation requiring user confirmation was detected.",
	"approval.message_status.text":              "Waiting for confirmation…",
	"approval.resolved.allowed":                 "Allowed",
	"approval.resolved.allowed_session":         "Allowed (don't ask again this session)",
	"approval.resolved.denied":                  "Denied",
	"approval.resolved.cancelled":               "Cancelled",
	"approval.resolved.default":                 "Acknowledged",
	"request_user_input.resolved.answered":      "Plan answers submitted",
	"request_user_input.message_status.text":    "Waiting for plan answers…",
	"request_user_input.notification.submitted": "Plan answers submitted",
	// Conversation lifecycle status lines (inline status items in the message stream).
	"conversation.stopped_manual": "Stopped manually",
	"conversation.interrupted":    "Interrupted (app restarted)",

	// eos web 模式服务壳文案。
	"web.server.ready":    "eos web is running at %s (frontend dir: %s, Ctrl+C to quit)",
	"web.server.shutdown": "eos web is shutting down…",
}
