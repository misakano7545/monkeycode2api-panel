package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/misakano7545/monkeycode2api-panel/internal/upstream"
	"github.com/misakano7545/monkeycode2api-panel/internal/usage"
)

var jsonUnmarshal = json.Unmarshal

const maxBodyBytes = 16 << 20

func readBody(r *http.Request) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes))
	if err != nil {
		return nil, badf("读取请求体失败：%v", err)
	}
	return b, nil
}

func newID(prefix string) string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return prefix + hex.EncodeToString(b)
}

// sseWriter 统一的 SSE 输出（OpenAI 用 data-only，Anthropic/Responses 带 event 名）。
type sseWriter struct {
	w http.ResponseWriter
	f http.Flusher
}

func newSSE(w http.ResponseWriter) (*sseWriter, bool) {
	f, ok := w.(http.Flusher)
	if !ok {
		return nil, false
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream; charset=utf-8")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	return &sseWriter{w: w, f: f}, true
}

func (s *sseWriter) data(v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(s.w, "data: %s\n\n", raw); err != nil {
		return err
	}
	s.f.Flush()
	return nil
}

func (s *sseWriter) raw(line string) error {
	if _, err := io.WriteString(s.w, line); err != nil {
		return err
	}
	s.f.Flush()
	return nil
}

func (s *sseWriter) event(name string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", name, raw); err != nil {
		return err
	}
	s.f.Flush()
	return nil
}

// record 写一条请求记录（面板与 /panel/api/usage 读它）。
func (s *Server) record(protocol, model, account, region string, status int, chars int,
	in, out int64, started time.Time, errMsg string) {
	s.Ledger.Add(usage.Record{
		Time: time.Now().Unix(), Protocol: protocol, Model: model,
		Account: account, Region: region, Status: status, Chars: chars,
		Input: in, Output: out, Ms: time.Since(started).Milliseconds(), Error: errMsg,
	})
}

// ─── OpenAI: POST /v1/chat/completions ──────────────────────

type chatReq struct {
	Model    string `json:"model"`
	Messages []struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"messages"`
	Stream        bool `json:"stream"`
	StreamOptions *struct {
		IncludeUsage bool `json:"include_usage"`
	} `json:"stream_options"`
}

func (s *Server) chatCompletions(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		failOpenAI(w, err)
		return
	}
	var req chatReq
	if err := json.Unmarshal(body, &req); err != nil {
		failOpenAI(w, badf("请求体不是合法 JSON：%v", err))
		return
	}
	if req.Model == "" {
		failOpenAI(w, badf("缺少 model"))
		return
	}
	msgs := make([]upstream.Msg, 0, len(req.Messages))
	for _, m := range req.Messages {
		msgs = append(msgs, upstream.Msg{Role: m.Role, Text: contentText(m.Content)})
	}
	started := time.Now()
	chars := estimateChars(msgs)

	sess, err := s.start(r.Context(), req.Model, msgs)
	if err != nil {
		code, typ, msg := classify(err)
		writeOpenAIError(w, code, typ, msg)
		s.record("openai", req.Model, "", "", code, chars, 0, 0, started, msg)
		return
	}
	defer sess.Close()
	includeUsage := req.StreamOptions != nil && req.StreamOptions.IncludeUsage
	id := newID("chatcmpl-")

	if !req.Stream {
		var sb []byte
		var u *upstream.Usage
		err := sess.drive(r.Context(), func(ev upstream.Event) error {
			switch ev.Kind {
			case upstream.EvContent:
				sb = append(sb, ev.Text...)
			case upstream.EvUsage:
				u = ev.Usage
			}
			return nil
		})
		if err != nil {
			code, typ, msg := classify(err)
			writeOpenAIError(w, code, typ, msg)
			s.record("openai", req.Model, sess.Account(), string(sess.lease.Region), code, chars, 0, 0, started, msg)
			return
		}
		msg := map[string]any{"role": "assistant", "content": string(sb)}
		writeJSON(w, http.StatusOK, map[string]any{
			"id": id, "object": "chat.completion", "created": started.Unix(),
			"model":   req.Model,
			"choices": []any{map[string]any{"index": 0, "message": msg, "finish_reason": "stop"}},
			"usage":   usageJSON(u),
		})
		s.record("openai", req.Model, sess.Account(), string(sess.lease.Region), 200, chars, inOf(u), outOf(u), started, "")
		return
	}

	sse, ok := newSSE(w)
	if !ok {
		failOpenAI(w, badf("响应流不支持"))
		return
	}
	base := func(delta map[string]any, finish any) map[string]any {
		return map[string]any{
			"id": id, "object": "chat.completion.chunk", "created": started.Unix(),
			"model":   req.Model,
			"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}},
		}
	}
	_ = sse.data(base(map[string]any{"role": "assistant", "content": ""}, nil))

	var u *upstream.Usage
	var outChars int
	err = sess.drive(r.Context(), func(ev upstream.Event) error {
		switch ev.Kind {
		case upstream.EvContent:
			outChars += len(ev.Text)
			return sse.data(base(map[string]any{"content": ev.Text}, nil))
		case upstream.EvReasoning:
			return sse.data(base(map[string]any{"reasoning_content": ev.Text}, nil))
		case upstream.EvUsage:
			u = ev.Usage
		}
		return nil
	})
	status, errMsg := 200, ""
	if err != nil {
		status, errMsg = 502, err.Error()
		_ = sse.data(map[string]any{
			"error": map[string]any{"message": errMsg, "type": "upstream_error"},
		})
	}
	_ = sse.data(base(map[string]any{}, "stop"))
	if includeUsage {
		_ = sse.data(map[string]any{
			"id": id, "object": "chat.completion.chunk", "created": started.Unix(),
			"model": req.Model, "choices": []any{}, "usage": usageJSON(u),
		})
	}
	_ = sse.raw("data: [DONE]\n\n")
	s.record("openai", req.Model, sess.Account(), string(sess.lease.Region), status, chars, inOf(u), outOf(u), started, errMsg)
}

func usageJSON(u *upstream.Usage) map[string]any {
	in, out, total := int64(0), int64(0), int64(0)
	if u != nil {
		in, out, total = u.Input, u.Output, u.Total
	}
	return map[string]any{"prompt_tokens": in, "completion_tokens": out, "total_tokens": total}
}

func inOf(u *upstream.Usage) int64 {
	if u == nil {
		return 0
	}
	return u.Input
}

func outOf(u *upstream.Usage) int64 {
	if u == nil {
		return 0
	}
	return u.Output
}
