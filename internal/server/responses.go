package server

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/misakano7545/monkeycode2api-panel/internal/upstream"
)

// Codex Responses API（/v1/responses）。
//
// Codex CLI 走这条协议：input 是字符串或消息数组，流式要按事件序列下发
// （created → item.added → text.delta → text.done → item.done → completed）。
type responsesReq struct {
	Model        string            `json:"model"`
	Input        json.RawMessage   `json:"input"`
	Instructions string            `json:"instructions"`
	Stream       bool              `json:"stream"`
	Tools        []json.RawMessage `json:"tools"` // 只数个数：上游没有工具通道，声明了也不下发
}

func (s *Server) responses(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		failOpenAI(w, err)
		return
	}
	var req responsesReq
	if err := json.Unmarshal(body, &req); err != nil {
		failOpenAI(w, badf("请求体不是合法 JSON：%v", err))
		return
	}
	if req.Model == "" {
		failOpenAI(w, badf("缺少 model"))
		return
	}
	msgs, err := responsesMessages(req.Input, req.Instructions)
	if err != nil {
		failOpenAI(w, err)
		return
	}
	started := time.Now()
	chars := estimateChars(msgs)

	sess, err := s.start(r.Context(), req.Model, msgs)
	if err != nil {
		code, typ, msg := classify(err)
		writeOpenAIError(w, code, typ, msg)
		s.record("codex", req.Model, "", "", code, chars, 0, 0, len(req.Tools), 0, started, msg)
		return
	}
	defer sess.Close()
	id := newID("resp_")
	msgID := newID("msg_")

	if !req.Stream {
		var text []byte
		var u *upstream.Usage
		err := sess.drive(r.Context(), func(ev upstream.Event) error {
			switch ev.Kind {
			case upstream.EvContent:
				text = append(text, ev.Text...)
			case upstream.EvUsage:
				u = ev.Usage
			}
			return nil
		})
		if err != nil {
			code, typ, msg := classify(err)
			writeOpenAIError(w, code, typ, msg)
			s.record("codex", req.Model, sess.Account(), string(sess.lease.Region), code, chars, 0, 0, len(req.Tools), 0, started, msg)
			return
		}
		writeJSON(w, http.StatusOK, responseObject(id, msgID, req.Model, started.Unix(), string(text), u))
		s.record("codex", req.Model, sess.Account(), string(sess.lease.Region), 200, chars, inOf(u), outOf(u), len(req.Tools), 0, started, "")
		return
	}

	sse, ok := newSSE(w)
	if !ok {
		failOpenAI(w, badf("响应流不支持"))
		return
	}
	seq := 0
	emit := func(name string, payload map[string]any) error {
		payload["type"] = name
		payload["sequence_number"] = seq
		seq++
		return sse.event(name, payload)
	}
	empty := map[string]any{
		"id": id, "object": "response", "created_at": started.Unix(),
		"status": "in_progress", "model": req.Model, "output": []any{},
	}
	_ = emit("response.created", map[string]any{"response": empty})
	_ = emit("response.in_progress", map[string]any{"response": empty})
	_ = emit("response.output_item.added", map[string]any{
		"output_index": 0,
		"item": map[string]any{
			"id": msgID, "type": "message", "status": "in_progress", "role": "assistant", "content": []any{},
		},
	})
	_ = emit("response.content_part.added", map[string]any{
		"item_id": msgID, "output_index": 0, "content_index": 0,
		"part": map[string]any{"type": "output_text", "text": "", "annotations": []any{}},
	})

	var sb []byte
	var u *upstream.Usage
	status, errMsg := 200, ""
	err = sess.drive(r.Context(), func(ev upstream.Event) error {
		switch ev.Kind {
		case upstream.EvContent:
			sb = append(sb, ev.Text...)
			return emit("response.output_text.delta", map[string]any{
				"item_id": msgID, "output_index": 0, "content_index": 0, "delta": ev.Text,
			})
		case upstream.EvUsage:
			u = ev.Usage
		}
		return nil
	})
	if err != nil {
		status, errMsg = 502, err.Error()
		_ = emit("response.failed", map[string]any{
			"response": map[string]any{
				"id": id, "object": "response", "status": "failed", "model": req.Model,
				"error": map[string]any{"code": "upstream_error", "message": errMsg},
			},
		})
	}

	text := string(sb)
	if err == nil {
		_ = emit("response.output_text.done", map[string]any{
			"item_id": msgID, "output_index": 0, "content_index": 0, "text": text,
		})
		_ = emit("response.content_part.done", map[string]any{
			"item_id": msgID, "output_index": 0, "content_index": 0,
			"part": map[string]any{"type": "output_text", "text": text, "annotations": []any{}},
		})
		_ = emit("response.output_item.done", map[string]any{
			"output_index": 0,
			"item": map[string]any{
				"id": msgID, "type": "message", "status": "completed", "role": "assistant",
				"content": []any{map[string]any{"type": "output_text", "text": text, "annotations": []any{}}},
			},
		})
		_ = emit("response.completed", map[string]any{
			"response": responseObject(id, msgID, req.Model, started.Unix(), text, u),
		})
	}
	s.record("codex", req.Model, sess.Account(), string(sess.lease.Region), status, chars, inOf(u), outOf(u), len(req.Tools), 0, started, errMsg)
}

// responseObject 非流式与 response.completed 共用的响应体。
func responseObject(id, msgID, model string, created int64, text string, u *upstream.Usage) map[string]any {
	in, out, total := int64(0), int64(0), int64(0)
	if u != nil {
		in, out, total = u.Input, u.Output, u.Total
	}
	return map[string]any{
		"id": id, "object": "response", "created_at": created,
		"status": "completed", "model": model,
		"output": []any{map[string]any{
			"id": msgID, "type": "message", "status": "completed", "role": "assistant",
			"content": []any{map[string]any{"type": "output_text", "text": text, "annotations": []any{}}},
		}},
		"usage": map[string]any{
			"input_tokens": in, "output_tokens": out, "total_tokens": total,
		},
	}
}

// responsesMessages 把 Responses 的 input 归一成消息列表。
func responsesMessages(input json.RawMessage, instructions string) ([]upstream.Msg, error) {
	msgs := make([]upstream.Msg, 0, 8)
	if instructions != "" {
		msgs = append(msgs, upstream.Msg{Role: "system", Text: instructions})
	}
	if len(input) == 0 {
		return msgs, badf("缺少 input")
	}
	var s string
	if err := json.Unmarshal(input, &s); err == nil {
		return append(msgs, upstream.Msg{Role: "user", Text: s}), nil
	}
	var items []map[string]any
	if err := json.Unmarshal(input, &items); err != nil {
		return nil, badf("input 既不是字符串也不是数组")
	}
	for _, it := range items {
		typ, _ := it["type"].(string)
		role, _ := it["role"].(string)
		switch typ {
		case "", "message":
			raw, _ := json.Marshal(it["content"])
			msgs = append(msgs, upstream.Msg{Role: orDefault(role, "user"), Text: contentText(raw)})
		case "function_call_output":
			if out, ok := it["output"].(string); ok {
				msgs = append(msgs, upstream.Msg{Role: "tool", Text: out})
			}
		case "function_call":
			name, _ := it["name"].(string)
			args, _ := it["arguments"].(string)
			msgs = append(msgs, upstream.Msg{Role: "assistant", Text: "[tool_call] " + name + " " + args})
		}
	}
	return msgs, nil
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
