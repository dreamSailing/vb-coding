package webbridge

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/eosaios/eos/internal/webbridge/adapter"
	"github.com/eosaios/eos/pkg/coreapi"
)

// 真实事件回放：2026-08-08 sess_132 的 turn.request_user_input（Plan 模式计划问题）。
// 现场症状：状态行「等待回答计划问题…」显示了，但计划问题面板没渲染。
// 本测试验证 Go 侧 handler → s.prompts → 投影 attach → JSON 序列化全链路。
func TestRequestUserInputEventProducesRenderablePrompt(t *testing.T) {
	s := &BridgeService{prompts: map[string]*promptState{}}
	session := &sessionState{
		ID: "sess_132",
		Messages: []ChatMessage{
			{ID: "assistant-1", Role: "assistant", State: "streaming"},
		},
	}
	// payload 字段名对齐内核 RequestUserInputEvent 的 JSON 序列化（snake_case）。
	payload := map[string]any{
		"call_id": "call_019fe12fc1917f609e0d9e03",
		"turn_id": "turn-1786160604670690200",
		"questions": []any{
			map[string]any{
				"id":       "audit_dir_and_scope",
				"header":   "目录与覆盖范围",
				"question": "你说的『根目录』是指哪个根目录？",
				"options": []any{
					map[string]any{"label": "新建独立目录，只做本次", "description": "在 desktop-ux-audit-v2 新建"},
					map[string]any{"label": "复用 tools-validation 续写", "description": "写到已有目录"},
				},
			},
			map[string]any{
				"id":       "test_target",
				"header":   "测试对象",
				"question": "测试对象是什么？",
				"options": []any{
					map[string]any{"label": "验证 EOS 工具流程", "description": "跑工具"},
				},
			},
		},
		"auto_resolution_ms": float64(60000),
	}
	frame := conversationEventFrame{
		session:            session,
		sessionID:          "sess_132",
		assistantMessageID: "assistant-1",
		kind:               "turn.request_user_input",
		event: adapter.Event{
			EventType: "turn.request_user_input",
			RequestID: "call_019fe12fc1917f609e0d9e03",
			SessionID: "sess_132",
			TurnID:    "turn-1786160604670690200",
			Payload:   payload,
		},
	}

	result := s.handleConversationEventLocked(frame)
	if !result.emitBootstrap {
		t.Fatalf("emitBootstrap = false, want true（计划问题事件必须立即全量同步）")
	}

	// 1. prompt 必须已写入 s.prompts，key = call_id。
	prompt, ok := s.prompts["call_019fe12fc1917f609e0d9e03"]
	if !ok {
		t.Fatalf("prompt not found in s.prompts, keys: %v", promptKeys(s.prompts))
	}
	if prompt.Kind != "request_user_input" {
		t.Fatalf("prompt.Kind = %q, want request_user_input", prompt.Kind)
	}
	if prompt.Status != "pending" {
		t.Fatalf("prompt.Status = %q, want pending", prompt.Status)
	}
	if prompt.AssistantMessageID != "assistant-1" {
		t.Fatalf("prompt.AssistantMessageID = %q, want assistant-1", prompt.AssistantMessageID)
	}
	if prompt.SessionID != "sess_132" {
		t.Fatalf("prompt.SessionID = %q, want sess_132", prompt.SessionID)
	}
	if len(prompt.Questions) != 2 {
		t.Fatalf("len(prompt.Questions) = %d, want 2", len(prompt.Questions))
	}
	if prompt.Questions[0].Header != "目录与覆盖范围" || len(prompt.Questions[0].Options) != 2 {
		t.Fatalf("question[0] parsed wrong: %+v", prompt.Questions[0])
	}

	// 2. 状态行 item 必须已追加到消息。
	msg := findSessionMessageByID(session, "assistant-1")
	if msg == nil {
		t.Fatal("assistant message not found")
	}
	foundStatus := false
	for _, item := range msg.Items {
		if item.Kind == "status" && strings.Contains(item.Text, "等待回答计划问题") {
			foundStatus = true
		}
	}
	if !foundStatus {
		t.Fatalf("status item 等待回答计划问题… not found in message items: %+v", msg.Items)
	}

	// 3. JSON 序列化字段名必须与前端 PromptCard 类型匹配（camelCase）。
	data, err := json.Marshal(prompt.PromptCard)
	if err != nil {
		t.Fatalf("marshal prompt: %v", err)
	}
	var wire map[string]any
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatalf("unmarshal wire: %v", err)
	}
	for _, key := range []string{"id", "kind", "status", "sessionId", "assistantMessageId", "questions", "callId"} {
		if _, present := wire[key]; !present {
			t.Fatalf("wire prompt missing key %q: %s", key, data)
		}
	}
	questionsWire, ok := wire["questions"].([]any)
	if !ok || len(questionsWire) != 2 {
		t.Fatalf("wire questions = %v", wire["questions"])
	}
	q0 := questionsWire[0].(map[string]any)
	if q0["header"] != "目录与覆盖范围" {
		t.Fatalf("wire question[0].header = %v（序列化字段名必须是 camelCase/小写，前端按此读取）", q0["header"])
	}
	opts, ok := q0["options"].([]any)
	if !ok || len(opts) != 2 {
		t.Fatalf("wire question[0].options = %v", q0["options"])
	}
	if opts[0].(map[string]any)["label"] != "新建独立目录，只做本次" {
		t.Fatalf("wire option label = %v", opts[0])
	}
}

func promptKeys(m map[string]*promptState) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func TestRequestUserInputReasonJSONPrefersFullPayload(t *testing.T) {
	prompt := &promptState{
		PromptCard: PromptCard{
			ID:     "approval_47",
			CallID: "call_019fe12fc1917f609e0d9e03",
			Questions: []bridgeRequestUserInputQuestion{
				{ID: "root_dir", Question: "根目录？"},
				{ID: "target", Question: "测试对象？"},
			},
		},
	}
	raw := `{"answers":{"root_dir":{"answers":["复用 tools-validation 续写"]},"target":{"answers":["验证 EOS 工具流程"]}}}`
	got, err := requestUserInputReasonJSON(prompt, "ignored", raw)
	if err != nil {
		t.Fatalf("requestUserInputReasonJSON() error = %v", err)
	}
	if got != raw {
		t.Fatalf("requestUserInputReasonJSON() = %s, want original JSON", got)
	}
	var back coreapi.RequestUserInputResponse
	if err := json.Unmarshal([]byte(got), &back); err != nil {
		t.Fatalf("response JSON should remain valid: %v", err)
	}
	if len(back.Answers) != 2 || back.Answers["target"].Answers[0] != "验证 EOS 工具流程" {
		t.Fatalf("decoded answers = %+v", back.Answers)
	}
}

func TestRequestUserInputReasonJSONFallsBackToFirstQuestion(t *testing.T) {
	prompt := &promptState{
		PromptCard: PromptCard{
			ID:     "approval_47",
			CallID: "call_019fe12fc1917f609e0d9e03",
			Questions: []bridgeRequestUserInputQuestion{
				{ID: "root_dir", Question: "根目录？"},
				{ID: "target", Question: "测试对象？"},
			},
		},
	}
	got, err := requestUserInputReasonJSON(prompt, "新建独立目录，只做本次", "")
	if err != nil {
		t.Fatalf("requestUserInputReasonJSON() error = %v", err)
	}
	var back coreapi.RequestUserInputResponse
	if err := json.Unmarshal([]byte(got), &back); err != nil {
		t.Fatalf("fallback JSON invalid: %v", err)
	}
	if len(back.Answers) != 1 || back.Answers["root_dir"].Answers[0] != "新建独立目录，只做本次" {
		t.Fatalf("fallback answers = %+v", back.Answers)
	}
}

func TestResolvedStatusTextAndLevelForRequestUserInput(t *testing.T) {
	s := &BridgeService{}
	text, level := s.resolvedStatusTextAndLevel(&promptState{
		PromptCard: PromptCard{ID: "approval_47"},
		Source:     "request-user-input",
	}, "answered")
	if text != "已回答计划问题" || level != "info" {
		t.Fatalf("resolvedStatusTextAndLevel(request_user_input) = (%q, %q), want (已回答计划问题, info)", text, level)
	}
}

func TestResolvedStatusTextAndLevelForDecisionTokens(t *testing.T) {
	s := &BridgeService{}
	cases := map[string]struct {
		text  string
		level string
	}{
		"accept":           {"已允许", "success"},
		"acceptForSession": {"已允许（本次会话不再询问）", "success"},
		"decline":          {"已拒绝", "warning"},
		"cancel":           {"已取消", "warning"},
	}
	for token, want := range cases {
		text, level := s.resolvedStatusTextAndLevel(nil, token)
		if text != want.text || level != want.level {
			t.Fatalf("resolvedStatusTextAndLevel(%q) = (%q, %q), want (%q, %q)", token, text, level, want.text, want.level)
		}
	}
}
