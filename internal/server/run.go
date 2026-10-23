package server

import (
	"context"
	"errors"
	"strings"

	"github.com/misakano7545/monkeycode2api-panel/internal/pool"
	"github.com/misakano7545/monkeycode2api-panel/internal/upstream"
)

// maxAttempts 一次请求最多换几次账号（忙 / 冷却 / 失效）。
const maxAttempts = 3

// Session 一次已经建好任务的上游会话。
//
// start 负责「挑号 + 建任务」，drive 负责「拉任务流」。拆开是因为建任务失败时
// 还没给客户端写过任何字节，可以返回带状态码的 HTTP 错误；一旦开始流式输出，
// 失败只能进流内。
type Session struct {
	s      *Server
	lease  *pool.Lease
	client *upstream.Client
	TaskID string
	Prompt upstream.Prompt
	Target upstream.Target
	Model  string
}

// start 解析模型、挑账号、建任务（失败自动换号重试）。
func (s *Server) start(ctx context.Context, model string, msgs []upstream.Msg) (*Session, error) {
	prompt, err := upstream.BuildPrompt(msgs)
	if err != nil {
		return nil, &badRequest{msg: err.Error()}
	}

	cands := s.Catalog.Resolve(model)
	if len(cands) == 0 {
		// 冷启动：目录还没拉过，先拉一次再解析。
		if len(s.Catalog.List()) == 0 {
			s.refreshCatalog(ctx, false)
			cands = s.Catalog.Resolve(model)
		}
		if len(cands) == 0 {
			return nil, &badRequest{msg: "未知模型 " + model + "：先用 GET /v1/models 看可用清单（或到面板刷新模型）"}
		}
	}

	regions := make([]upstream.Region, 0, len(cands))
	byRegion := map[upstream.Region]upstream.Target{}
	for _, c := range cands {
		regions = append(regions, c.Region)
		byRegion[c.Region] = c.Target
	}

	tried := map[string]bool{}
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		lease, err := s.Pool.Acquire(regions, tried)
		if err != nil {
			if lastErr != nil {
				return nil, lastErr
			}
			return nil, err
		}
		target := byRegion[lease.Region]
		client := s.Client(lease.Region)
		body := upstream.CreateTaskBody(target, prompt, lease.Account.ImageID,
			int(s.Cfg.TaskTimeout().Seconds()))

		taskID, err := client.CreateTask(ctx, lease.Account.CookieHeader(), body)
		if err != nil {
			tried[lease.Account.ID] = true
			s.Pool.Fail(lease.Account, err)
			lease.Release()
			lastErr = err
			// 账号忙 / 额度 / 登录态：换号值得重试；参数错误重试也没用。
			if !retryable(err) {
				return nil, err
			}
			continue
		}
		return &Session{s: s, lease: lease, client: client, TaskID: taskID,
			Prompt: prompt, Target: target, Model: model}, nil
	}
	return nil, lastErr
}

func retryable(err error) bool {
	var ue *upstream.Error
	if !errors.As(err, &ue) {
		return false
	}
	switch ue.Kind {
	case upstream.KindBusy, upstream.KindAuth, upstream.KindQuota, upstream.KindRate, upstream.KindUpstream:
		return true
	}
	return false
}

// drive 拉任务流；emit 出错（客户端断开）立即停止并关连接。
func (ss *Session) drive(ctx context.Context, emit func(upstream.Event) error) error {
	err := ss.client.Stream(ctx, ss.lease.Account.CookieHeader(), ss.TaskID, ss.Prompt.Content,
		ss.s.Cfg.IdleTimeout(), ss.s.Cfg.TaskTimeout(), emit)
	if err != nil {
		// 客户端主动断开不算账号的错，别计入冷却。
		if ctx.Err() == nil {
			ss.s.Pool.Fail(ss.lease.Account, err)
		}
		return err
	}
	ss.s.Pool.OK(ss.lease.Account.ID)
	return nil
}

// Close 归还账号（幂等）。
func (ss *Session) Close() { ss.lease.Release() }

// Account 本次使用的账号（记账 / 日志）。
func (ss *Session) Account() string { return ss.lease.Account.ID }

// ─── 输入归一化 ──────────────────────────────────────────────

// contentText 取一段 content 的文本：字符串，或 [{type:text|input_text, text}]。
//
// 图片 / 文件附件不转发：上游只收文本提示词，转发不了的部件静默丢弃（与
// 「多带一张图导致整个请求 400」相比，丢附件更可用）。
func contentText(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := jsonUnmarshal(raw, &s); err == nil {
		return s
	}
	var parts []map[string]any
	if err := jsonUnmarshal(raw, &parts); err != nil {
		return ""
	}
	var sb strings.Builder
	for _, p := range parts {
		typ, _ := p["type"].(string)
		if typ != "" && typ != "text" && typ != "input_text" && typ != "output_text" {
			continue
		}
		if t, ok := p["text"].(string); ok {
			sb.WriteString(t)
		}
	}
	return sb.String()
}

// estimateChars 粗略估算正文长度（面板统计用，不参与计费）。
func estimateChars(msgs []upstream.Msg) int {
	n := 0
	for _, m := range msgs {
		n += len([]rune(m.Text))
	}
	return n
}
