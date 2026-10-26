package server

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/misakano7545/monkeycode2api-panel/internal/usage"
)

// 三种协议的入站 tools 都只数个数；纯聊天保持零值（零值不计入分母）。
func TestToolsDeclaredFromBody(t *testing.T) {
	body := []byte(`{"model":"m","messages":[{"role":"user","content":"hi"}],` +
		`"tools":[{"type":"function"},{"type":"function"}]}`)
	var c chatReq
	if err := json.Unmarshal(body, &c); err != nil || len(c.Tools) != 2 {
		t.Fatalf("openai tools = %d, err %v", len(c.Tools), err)
	}
	var a messagesReq
	if err := json.Unmarshal(body, &a); err != nil || len(a.Tools) != 2 {
		t.Fatalf("claude tools = %d, err %v", len(a.Tools), err)
	}
	var r responsesReq
	if err := json.Unmarshal([]byte(`{"model":"m","input":"hi","tools":[{"type":"function"}]}`), &r); err != nil || len(r.Tools) != 1 {
		t.Fatalf("codex tools = %d, err %v", len(r.Tools), err)
	}
	var plain chatReq
	if err := json.Unmarshal([]byte(`{"model":"m","messages":[]}`), &plain); err != nil || len(plain.Tools) != 0 {
		t.Fatalf("纯聊天不该有 tools：%v, err %v", plain.Tools, err)
	}
}

// 非流式计数认两种组包形态（[]map[string]any 与 []any）。
func TestToolCallsCountsBothShapes(t *testing.T) {
	asMaps := map[string]any{"choices": []any{map[string]any{"message": map[string]any{
		"tool_calls": []map[string]any{{"id": "a"}, {"id": "b"}}}}}}
	asAnys := map[string]any{"choices": []any{map[string]any{"message": map[string]any{
		"tool_calls": []any{map[string]any{"id": "a"}}}}}}
	if n := toolCalls(asMaps); n != 2 {
		t.Fatalf("[]map[string]any = %d", n)
	}
	if n := toolCalls(asAnys); n != 1 {
		t.Fatalf("[]any = %d", n)
	}
	if n := toolCalls(map[string]any{"choices": []any{}}); n != 0 {
		t.Fatalf("空 choices = %d", n)
	}
}

// 失败请求也要留下声明数；零值靠 omitempty 不进归档。
func TestRecordKeepsToolsSurface(t *testing.T) {
	dir := t.TempDir()
	l, err := usage.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{Ledger: l}
	s.record("openai", "m", "", "", 503, 0, 0, 0, 2, 0, time.Now(), "no account")

	got := l.Recent(1)
	if len(got) != 1 || got[0].ToolsDeclared != 2 || got[0].ToolCalls != 0 {
		t.Fatalf("record = %+v", got)
	}
	raw, err := os.ReadFile(dir + "/requests.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	var line map[string]any
	if err := json.Unmarshal(raw, &line); err != nil {
		t.Fatal(err)
	}
	if line["tools_declared"] != float64(2) {
		t.Fatalf("归档缺 tools_declared：%s", raw)
	}
	if _, ok := line["tool_calls"]; ok {
		t.Fatalf("零值不该落盘：%s", raw)
	}
}
