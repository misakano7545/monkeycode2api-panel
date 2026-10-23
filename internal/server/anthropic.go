package server

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/misakano7545/monkeycode2api-panel/internal/upstream"
)

// Claude Messages API（/v1/messages）。
//
// 上游没有 system/messages 之分，system 抽成任务的 system_prompt 字段，其余按
// 角色拼成一条 prompt；下行把 ACP 正文事件翻成 Anthropic 的 SSE 事件序列。
type messagesReq struct {
	Model     string          `json:"model"`
	MaxTokens int             `json:"max_tokens"`
	System    json.RawMessage `json:"system"`
	Messages  []struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"messages"`
	Stream bool `json:"stream"`
}

// countTokens Claude Code 会先探这个端点；上游没有对应能力，按字符数估一个值。
// shortcut: 粗估（3 字符 ≈ 1 token），只用于客户端预算判断，不参与计费。
func (s *Server) countTokens(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		failAnthropic(w, err)
		return
	}
	var req messagesReq
	if err := json.Unmarshal(body, &req); err != nil {
		failAnthropic(w, badf("请求体不是合法 JSON：%v", err))
		return
	}
	chars := len([]rune(contentText(req.System)))
	for _, m := range req.Messages {
		chars += len([]rune(contentText(m.Content)))
	}
	writeJSON(w, http.StatusOK, map[string]any{"input_tokens": chars/3 + 1})
}

func (s *Server) messages(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		failAnthropic(w, err)
		return
	}
	var req messagesReq
	if err := json.Unmarshal(body, &req); err != nil {
		failAnthropic(w, badf("请求体不是合法 JSON：%v", err))
		return
	}
	if req.Model == "" {
		failAnthropic(w, badf("缺少 model"))
		return
	}
	msgs := make([]upstream.Msg, 0, len(req.Messages)+1)
	if sys := contentText(req.System); sys != "" {
		msgs = append(msgs, upstream.Msg{Role: "system", Text: sys})
	}
	for _, m := range req.Messages {
		msgs = append(msgs, upstream.Msg{Role: m.Role, Text: contentText(m.Content)})
	}
	started := time.Now()
	chars := estimateChars(msgs)

	sess, err := s.start(r.Context(), req.Model, msgs)
	if err != nil {
		code, typ, msg := classify(err)
		writeAnthropicError(w, code, typ, msg)
		s.record("claude", req.Model, "", "", code, chars, 0, 0, started, msg)
		return
	}
	defer sess.Close()
	id := newID("msg_")

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
			writeAnthropicError(w, code, typ, msg)
			s.record("claude", req.Model, sess.Account(), string(sess.lease.Region), code, chars, 0, 0, started, msg)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"id": id, "type": "message", "role": "assistant", "model": req.Model,
			"content":     []any{map[string]any{"type": "text", "text": string(text)}},
			"stop_reason": "end_turn", "stop_sequence": nil,
			"usage": map[string]any{"input_tokens": inOf(u), "output_tokens": outOf(u)},
		})
		s.record("claude", req.Model, sess.Account(), string(sess.lease.Region), 200, chars, inOf(u), outOf(u), started, "")
		return
	}

	sse, ok := newSSE(w)
	if !ok {
		failAnthropic(w, badf("响应流不支持"))
		return
	}
	_ = sse.event("message_start", map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id": id, "type": "message", "role": "assistant", "model": req.Model,
			"content": []any{}, "stop_reason": nil, "stop_sequence": nil,
			"usage": map[string]any{"input_tokens": 0, "output_tokens": 0},
		},
	})
	_ = sse.event("content_block_start", map[string]any{
		"type": "content_block_start", "index": 0,
		"content_block": map[string]any{"type": "text", "text": ""},
	})

	var u *upstream.Usage
	status, errMsg := 200, ""
	err = sess.drive(r.Context(), func(ev upstream.Event) error {
		switch ev.Kind {
		case upstream.EvContent:
			return sse.event("content_block_delta", map[string]any{
				"type": "content_block_delta", "index": 0,
				"delta": map[string]any{"type": "text_delta", "text": ev.Text},
			})
		case upstream.EvUsage:
			u = ev.Usage
		}
		return nil
	})
	if err != nil {
		status, errMsg = 502, err.Error()
		_ = sse.event("error", map[string]any{
			"type": "error", "error": map[string]any{"type": "api_error", "message": errMsg},
		})
	}
	_ = sse.event("content_block_stop", map[string]any{"type": "content_block_stop", "index": 0})
	// usage 只有上游收尾时才给，message_delta 是唯一能带真实数的地方。
	_ = sse.event("message_delta", map[string]any{
		"type":  "message_delta",
		"delta": map[string]any{"stop_reason": "end_turn", "stop_sequence": nil},
		"usage": map[string]any{"input_tokens": inOf(u), "output_tokens": outOf(u)},
	})
	_ = sse.event("message_stop", map[string]any{"type": "message_stop"})
	s.record("claude", req.Model, sess.Account(), string(sess.lease.Region), status, chars, inOf(u), outOf(u), started, errMsg)
}
