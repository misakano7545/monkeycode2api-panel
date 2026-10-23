package upstream

import (
	"encoding/base64"
	"encoding/json"
	"strings"
)

// EventKind 归一化后的事件类型（三种协议适配器共用）。
type EventKind int

const (
	EvContent   EventKind = iota // 正文增量
	EvReasoning                  // 思考增量
	EvTool                       // agent 的工具调用（只记日志，不下发客户端）
	EvUsage                      // 用量结算（Usage 非空，Text 为空）
)

// Usage 上游给的 token 计数（累计值，非零才覆盖）。
type Usage struct {
	Input  int64
	Output int64
	Total  int64
}

func (u *Usage) Seen() bool { return u.Input != 0 || u.Output != 0 || u.Total != 0 }

// Event 一次下游可见的增量。
type Event struct {
	Kind  EventKind
	Text  string
	Tool  string
	Usage *Usage
}

// frame 任务流下行信封：{type, kind?, data?, timestamp?}
type frame struct {
	Type string          `json:"type"`
	Kind string          `json:"kind"`
	Data json.RawMessage `json:"data"`
}

// decodePayload 上游的 data 有两种形态：base64(JSON) 或 JSON 字符串。两种都认。
func decodePayload(raw json.RawMessage) (map[string]any, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return decodeJSONString(s)
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) == nil {
		return m, true
	}
	return nil, false
}

func decodeJSONString(s string) (map[string]any, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, false
	}
	var m map[string]any
	if json.Unmarshal([]byte(s), &m) == nil {
		return m, true
	}
	if b, err := base64.StdEncoding.DecodeString(s); err == nil {
		if json.Unmarshal(b, &m) == nil {
			return m, true
		}
	}
	return nil, false
}

// acpEvents 一条 ACP 事件 → 0..1 个下游事件。
//
// 两个来源对字段名的说法不一致（网页端读 sessionUpdate + content.text，
// 参考实现读 type + text），因此两种都认。
func acpEvents(acp map[string]any) (Event, bool) {
	name := str(acp, "sessionUpdate")
	if name == "" {
		name = str(acp, "type")
	}
	if upd, ok := acp["update"].(map[string]any); ok {
		if n := str(upd, "sessionUpdate"); n != "" {
			name = n
			acp = upd
		}
	}
	switch name {
	case "agent_message_chunk", "message", "text", "delta":
		if t := chunkText(acp); t != "" {
			return Event{Kind: EvContent, Text: t}, true
		}
	case "agent_thought_chunk", "thought", "reasoning":
		if t := chunkText(acp); t != "" {
			return Event{Kind: EvReasoning, Text: t}, true
		}
	case "tool_call", "tool_call_update":
		name := str(acp, "toolName")
		if name == "" {
			name = str(acp, "title")
		}
		if name == "" {
			name = str(acp, "kind")
		}
		return Event{Kind: EvTool, Tool: name}, true
	}
	return Event{}, false
}

// chunkText 取文本块：content.text（网页端）> text（参考实现）> content 字符串。
func chunkText(acp map[string]any) string {
	switch v := acp["content"].(type) {
	case map[string]any:
		if t := str(v, "text"); t != "" {
			return t
		}
		if v["type"] != nil && str(v, "type") != "text" {
			return ""
		}
	case string:
		if v != "" {
			return v
		}
	}
	if t := str(acp, "text"); t != "" {
		return t
	}
	return ""
}

func str(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

// usageOf 从 usage 对象里取三件套（累计值）。
func usageOf(m map[string]any) *Usage {
	src := m
	if u, ok := m["usage"].(map[string]any); ok {
		src = u
	}
	num := func(key string) int64 {
		switch v := src[key].(type) {
		case float64:
			return int64(v)
		case json.Number:
			n, _ := v.Int64()
			return n
		}
		return 0
	}
	u := &Usage{Input: num("input_tokens"), Output: num("output_tokens"), Total: num("total_tokens")}
	if u.Total == 0 {
		u.Total = u.Input + u.Output
	}
	if !u.Seen() {
		return nil
	}
	return u
}

// questionReply Agent 提问 → 自动回复（不自动回，任务会停在提问处）。
func questionReply(raw json.RawMessage) string {
	payload, ok := decodePayload(raw)
	if !ok {
		return ""
	}
	reqID := str(payload, "request_id")
	if reqID == "" {
		reqID = str(payload, "id")
	}
	body, _ := json.Marshal(map[string]any{
		"request_id":   reqID,
		"answers_json": "",
		"cancelled":    false,
	})
	out, _ := json.Marshal(map[string]any{"type": "reply-question", "data": string(body)})
	return string(out)
}
