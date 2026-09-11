package webbridge

import (
	"fmt"
	"strings"
	"time"
)

func cloneAutomationRuns(items []AutomationRunCard) []AutomationRunCard {
	out := make([]AutomationRunCard, len(items))
	copy(out, items)
	return nonNilSlice(out)
}

func previewFromMessages(messages []ChatMessage) string {
	// 倒序找最后一条有内容的消息：assistant 从 items 的 agent_message 取正文，
	// user 直接取 content（user 没有 items）。
	for index := len(messages) - 1; index >= 0; index-- {
		msg := messages[index]
		for i := len(msg.Items) - 1; i >= 0; i-- {
			item := msg.Items[i]
			if item.Kind == "agent_message" || item.Kind == "plan" || item.Kind == "status" {
				if text := strings.TrimSpace(item.Text); text != "" {
					return text
				}
			}
		}
		if content := strings.TrimSpace(msg.Content); content != "" {
			return content
		}
	}
	return "等待会话内容"
}

func autoSessionTitle(session *sessionState, input string) string {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return fallbackText(session.Title, "新对话")
	}
	if summarized := summarizeSessionTitleInput(trimmed); summarized != "" {
		return summarized
	}
	return fallbackText(session.Title, "新对话")
}

func isAutoSessionPlaceholderTitle(title string) bool {
	trimmed := strings.TrimSpace(title)
	return trimmed == "" || trimmed == "新对话" || strings.EqualFold(trimmed, "New Chat")
}

func summarizeSessionTitleInput(input string) string {
	normalized := strings.NewReplacer("\r", " ", "\n", " ", "\t", " ").Replace(strings.TrimSpace(input))
	normalized = strings.Join(strings.Fields(normalized), " ")
	if normalized == "" {
		return ""
	}
	clause := firstSessionTitleClause(normalized)
	clause = simplifySessionTitleClause(clause)
	if clause == "" {
		clause = simplifySessionTitleClause(normalized)
	}
	action := detectSessionTitleAction(normalized)
	if action != "" && !strings.HasPrefix(clause, action) {
		object := trimSessionTitleActionPrefix(clause)
		if object != "" {
			clause = action + object
		}
	}
	// 标题本体保存完整摘要（64 字护栏只为防超长粘贴）。侧栏静态展示的省略号
	// 由前端 CSS 截断；悬浮提示与标题跑马灯都依赖这里的完整文本。
	return clampSessionTitle(clause, 64)
}

func firstSessionTitleClause(input string) string {
	for _, separator := range []string{"，", ",", "。", "；", ";", "！", "!", "？", "?", "然后", "并且", "同时", "顺便"} {
		if idx := strings.Index(input, separator); idx >= 0 {
			input = input[:idx]
			break
		}
	}
	return strings.TrimSpace(input)
}

func simplifySessionTitleClause(clause string) string {
	clause = strings.TrimSpace(clause)
	if clause == "" {
		return ""
	}
	for _, prefix := range []string{
		"请帮我", "请你", "麻烦你", "麻烦", "帮我", "帮忙", "你先", "你再", "你", "我想让你", "我想请你",
		"可以帮我", "能不能帮我", "先帮我", "先", "把", "将",
	} {
		for strings.HasPrefix(clause, prefix) {
			clause = strings.TrimSpace(strings.TrimPrefix(clause, prefix))
		}
	}
	replacer := strings.NewReplacer(
		"分析一下", "分析",
		"测试一下", "测试",
		"优化一下", "优化",
		"修一下", "修复",
		"修一修", "修复",
		"改一下", "调整",
		"看一下", "查看",
		"看一眼", "查看",
		"创建一个单独的文件夹", "创建文件夹",
		"创建一个文件夹", "创建文件夹",
		"新建一个文件夹", "创建文件夹",
		"根目录下", "根目录",
		"在根目录", "根目录",
		"软件使用上有哪些问题", "软件问题",
		"使用上有哪些问题", "问题",
		"有哪些问题", "问题",
		"输出报告", "写报告",
		"分析报告", "报告",
		"菜单里面", "菜单",
		"设置的菜单", "设置菜单",
		"工作区里面", "工作区",
		"当前目录的情况", "当前目录",
	)
	clause = replacer.Replace(clause)
	for _, token := range []string{
		"一下", "现在", "当前", "这个", "那个", "里面", "里边", "一下子", "帮忙", "给我", "帮我",
		"还会", "应该", "正常", "怎么", "有个", "有些", "看着", "不太合理", "比例不太合理", "的问题",
		"问题是", "是否", "是不是", "一下看看", "比例",
	} {
		clause = strings.ReplaceAll(clause, token, "")
	}
	clause = strings.ReplaceAll(clause, "的", "")
	clause = strings.ReplaceAll(clause, "了", "")
	clause = strings.Trim(clause, " ，,。；;：:!！?？")
	return strings.Join(strings.Fields(clause), "")
}

func detectSessionTitleAction(input string) string {
	for _, candidate := range []struct {
		needle string
		action string
	}{
		{"修复", "修复"},
		{"修一下", "修复"},
		{"修一修", "修复"},
		{"修", "修复"},
		{"排查", "排查"},
		{"分析", "分析"},
		{"测试", "测试"},
		{"优化", "优化"},
		{"重构", "重构"},
		{"整理", "整理"},
		{"创建", "创建"},
		{"新建", "创建"},
		{"编写", "编写"},
		{"实现", "实现"},
		{"设计", "设计"},
		{"提炼", "提炼"},
		{"同步", "同步"},
		{"调整", "调整"},
		{"恢复", "恢复"},
		{"删除", "删除"},
		{"命名", "命名"},
		{"总结", "总结"},
		{"记录", "记录"},
		{"查看", "查看"},
	} {
		if strings.Contains(input, candidate.needle) {
			return candidate.action
		}
	}
	return ""
}

func trimSessionTitleActionPrefix(clause string) string {
	for _, action := range []string{
		"修复", "排查", "分析", "测试", "优化", "重构", "整理", "创建", "编写", "实现", "设计",
		"提炼", "同步", "调整", "恢复", "删除", "命名", "总结", "记录", "查看",
	} {
		clause = strings.TrimPrefix(clause, action)
	}
	return strings.TrimSpace(clause)
}

func clampSessionTitle(title string, maxRunes int) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return ""
	}
	runes := []rune(title)
	if len(runes) <= maxRunes {
		return title
	}
	return string(runes[:maxRunes]) + "…"
}

func newID(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

func compactPaths(paths []string) []string {
	out := make([]string, 0, len(paths))
	seen := map[string]struct{}{}
	for _, item := range paths {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func compactStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, item := range values {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		out = append(out, item)
	}
	return out
}

func nonNilSlice[T any](items []T) []T {
	if items == nil {
		return []T{}
	}
	return items
}

func formatTimeRFC3339(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Format(time.RFC3339)
}

func cloneMessages(messages []ChatMessage) []ChatMessage {
	out := make([]ChatMessage, len(messages))
	copy(out, messages)
	for index := range out {
		out[index].Attachments = nonNilSlice(append([]AttachmentRef(nil), out[index].Attachments...))
		out[index].RuntimeEvents = nonNilSlice(append([]RuntimeEvent(nil), out[index].RuntimeEvents...))
		out[index].Prompts = nonNilSlice(append([]PromptCard(nil), out[index].Prompts...))
		if out[index].ChangeSet != nil {
			clone := *out[index].ChangeSet
			clone.Files = nonNilSlice(append([]ChangedFile(nil), out[index].ChangeSet.Files...))
			out[index].ChangeSet = &clone
		}
		if out[index].Rollback != nil {
			clone := *out[index].Rollback
			clone.Files = nonNilSlice(append([]RollbackFileSnapshot(nil), out[index].Rollback.Files...))
			out[index].Rollback = &clone
		}
		if strings.TrimSpace(out[index].UpdatedAt) == "" {
			out[index].UpdatedAt = out[index].CreatedAt
		}
		if strings.TrimSpace(out[index].RuntimeSummary) == "" {
			out[index].RuntimeSummary = runtimeSummaryForMessage(out[index])
		}
	}
	return nonNilSlice(out)
}

func cloneSessionState(session *sessionState) *sessionState {
	if session == nil {
		return nil
	}
	return &sessionState{
		ID:             session.ID,
		Title:          session.Title,
		WorkspacePath:  session.WorkspacePath,
		Messages:       cloneMessages(session.Messages),
		Running:        session.Running,
		Persisted:      session.Persisted,
		NeedsAttention: session.NeedsAttention,
		UpdatedAt:      session.UpdatedAt,
	}
}

func cloneNotifications(items []NotificationItem) []NotificationItem {
	out := make([]NotificationItem, len(items))
	copy(out, items)
	return nonNilSlice(out)
}
