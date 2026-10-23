package upstream

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestBuildPrompt(t *testing.T) {
	p, err := BuildPrompt([]Msg{
		{Role: "system", Text: "你是助手"},
		{Role: "user", Text: "你好"},
		{Role: "assistant", Text: "在"},
		{Role: "user", Text: "继续"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.System != "你是助手" {
		t.Fatalf("system = %q", p.System)
	}
	want := "[User]\n你好\n\n[Assistant]\n在\n\n[User]\n继续"
	if p.Content != want {
		t.Fatalf("content = %q, want %q", p.Content, want)
	}
	if _, err := BuildPrompt([]Msg{{Role: "system", Text: "只有 system"}}); err == nil {
		t.Fatal("没有正文时应报错")
	}
}

func TestCreateTaskBodyRequiresImageAndUUID(t *testing.T) {
	body := CreateTaskBody(Target{ModelID: "uuid-1", CLI: "codex", Model: "gpt-5.4"},
		Prompt{Content: "hi", System: "sys"}, "img-1", 7200)
	for _, key := range []string{"content", "cli_name", "model_id", "image_id", "host_id", "resource"} {
		if _, ok := body[key]; !ok {
			t.Fatalf("缺字段 %s", key)
		}
	}
	if body["system_prompt"] != "sys" {
		t.Fatalf("system_prompt 未透传：%v", body["system_prompt"])
	}
	if body["host_id"] != PublicHost {
		t.Fatalf("host_id = %v", body["host_id"])
	}
}

func TestUserInputDataIsDoubleBase64(t *testing.T) {
	raw, err := base64.StdEncoding.DecodeString(UserInputData("你好"))
	if err != nil {
		t.Fatalf("外层不是 base64：%v", err)
	}
	var inner struct {
		Content     string `json:"content"`
		Attachments []any  `json:"attachments"`
	}
	if err := json.Unmarshal(raw, &inner); err != nil {
		t.Fatalf("内层不是 JSON：%v", err)
	}
	text, err := base64.StdEncoding.DecodeString(inner.Content)
	if err != nil || string(text) != "你好" {
		t.Fatalf("content 解码失败：%q %v", text, err)
	}
}

// 两个参考实现对 ACP 字段名说法不一致：网页端是 sessionUpdate + content.text，
// 参考实现是 type + text。两种都必须认，否则一半事件会被丢掉。
func TestACPBothShapes(t *testing.T) {
	web := map[string]any{
		"sessionUpdate": "agent_message_chunk",
		"content":       map[string]any{"type": "text", "text": "网页端形态"},
	}
	ref := map[string]any{"type": "agent_message_chunk", "text": "参考实现形态"}
	for name, acp := range map[string]map[string]any{"web": web, "ref": ref} {
		ev, ok := acpEvents(acp)
		if !ok || ev.Kind != EvContent || ev.Text == "" {
			t.Fatalf("%s 形态解析失败：%+v", name, ev)
		}
	}
	thought, ok := acpEvents(map[string]any{"sessionUpdate": "agent_thought_chunk",
		"content": map[string]any{"type": "text", "text": "想"}})
	if !ok || thought.Kind != EvReasoning {
		t.Fatalf("思考块解析失败：%+v", thought)
	}
	if _, ok := acpEvents(map[string]any{"sessionUpdate": "plan"}); ok {
		t.Fatal("plan 不应产出下游事件")
	}
}

func TestDecodePayloadBase64AndJSON(t *testing.T) {
	inner := map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": "x"}}
	raw, _ := json.Marshal(inner)
	b64, _ := json.Marshal(base64.StdEncoding.EncodeToString(raw))
	plain, _ := json.Marshal(string(raw))
	for name, payload := range map[string]json.RawMessage{"b64": b64, "json": plain} {
		if _, ok := decodePayload(payload); !ok {
			t.Fatalf("%s 形态解码失败", name)
		}
	}
}

func TestUsageOf(t *testing.T) {
	u := usageOf(map[string]any{"usage": map[string]any{
		"input_tokens": float64(10), "output_tokens": float64(5),
	}})
	if u == nil || u.Input != 10 || u.Output != 5 || u.Total != 15 {
		t.Fatalf("usage 解析失败：%+v", u)
	}
	if usageOf(map[string]any{}) != nil {
		t.Fatal("没有 usage 时应返回 nil（不杜撰 token）")
	}
}

func TestCliNameFor(t *testing.T) {
	cases := map[string]string{
		"openai_chat": "opencode", "openai_responses": "codex", "anthropic": "claude", "": "opencode",
	}
	for in, want := range cases {
		if got := CliNameFor(in); got != want {
			t.Fatalf("CliNameFor(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestModelID(t *testing.T) {
	if got := ModelID(Model{Provider: "openai", Model: "gpt-5.4"}); got != "openai/gpt-5.4" {
		t.Fatalf("ModelID = %q", got)
	}
	if got := ModelID(Model{Model: "kimi-k2.5"}); got != "kimi-k2.5" {
		t.Fatalf("无 provider 时 ModelID = %q", got)
	}
	if !strings.Contains(ModelID(Model{Provider: "a", Model: "b"}), "/") {
		t.Fatal("provider 存在时应带斜杠")
	}
}

// 未知登录方式在打网络之前就拒（面板传错值时不会去捅上游）。
func TestLoginURLRejectsUnknownMethod(t *testing.T) {
	c := &Client{Region: CN, HTTP: &http.Client{Timeout: time.Second}}
	if _, err := c.LoginURL(context.Background(), "wechat", ""); err == nil {
		t.Fatal("未知登录方式应当报错")
	}
}
