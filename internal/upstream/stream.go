package upstream

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"
)

// Stream 建连 → 起始交互 → 消费任务流，把事件推给 emit。
//
// 四个出口都会显式收尾（不挂死下游）：task-ended / task-error / 连接断开 /
// 空闲或总时长超时。
func (c *Client) Stream(ctx context.Context, cookie, taskID, prompt string, idle, total time.Duration, emit func(Event) error) error {
	ws, err := dialWS(ctx, StreamURL(c.Region, taskID), WSHeaders(c.Region, cookie, c.UA), 20*time.Second)
	if err != nil {
		return err
	}
	defer ws.Close()

	// mode=new 下客户端必须先发 auto-approve + user-input，上游才开始跑。
	// 不发 auto-approve，任务会停在第一个工具调用等确认。
	if err := ws.WriteText(`{"type":"auto-approve"}`); err != nil {
		return &Error{Kind: KindUpstream, Msg: "任务流发送 auto-approve 失败：" + err.Error()}
	}
	start, _ := json.Marshal(map[string]any{"type": "user-input", "data": UserInputData(prompt)})
	if err := ws.WriteText(string(start)); err != nil {
		return &Error{Kind: KindUpstream, Msg: "任务流发送 user-input 失败：" + err.Error()}
	}

	deadline := time.Now().Add(total)
	var usage *Usage
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return &Error{Kind: KindUpstream, Msg: fmt.Sprintf("任务流超过 %s 总时长上限", total)}
		}
		wait := idle
		if remaining < wait {
			wait = remaining
		}
		ws.SetReadDeadline(time.Now().Add(wait))

		op, payload, err := ws.ReadMessage()
		if err != nil {
			if time.Now().After(deadline) {
				return &Error{Kind: KindUpstream, Msg: fmt.Sprintf("任务流超过 %s 总时长上限", total)}
			}
			if ne, ok := err.(interface{ Timeout() bool }); ok && ne.Timeout() {
				return &Error{Kind: KindUpstream, Msg: fmt.Sprintf("任务流空闲超时（%s 无下行消息）", idle)}
			}
			return &Error{Kind: KindUpstream, Msg: "任务流连接中断（未收到 task-ended）：" + err.Error()}
		}
		if op == opClose {
			return &Error{Kind: KindUpstream, Msg: "任务流被上游关闭（未收到 task-ended）"}
		}

		var f frame
		if json.Unmarshal(payload, &f) != nil {
			continue
		}
		debugFrame(f)
		switch f.Type {
		case "ping":
			_ = ws.WriteText(`{"type":"ping"}`)
		case "task-started":
			// 轮次开始，无需动作
		case "task-running":
			switch f.Kind {
			case "acp_event":
				acp, ok := decodePayload(f.Data)
				if !ok {
					continue
				}
				if ev, ok := acpEvents(acp); ok {
					if err := emit(ev); err != nil {
						return err
					}
				}
				if u := usageOf(acp); u != nil {
					usage = u
				}
			case "acp_ask_user_question":
				if reply := questionReply(f.Data); reply != "" {
					_ = ws.WriteText(reply)
				}
			}
		case "task-ended":
			if p, ok := decodePayload(f.Data); ok {
				if u := usageOf(p); u != nil {
					usage = u
				}
			}
			if usage != nil {
				_ = emit(Event{Kind: EvUsage, Usage: usage})
			}
			return nil
		case "task-error":
			msg := "任务执行出错（上游未给原因）"
			if p, ok := decodePayload(f.Data); ok {
				for _, key := range []string{"error", "message", "msg"} {
					if s := str(p, key); s != "" {
						msg = s
						break
					}
				}
			}
			return &Error{Kind: KindUpstream, Msg: msg}
		}
	}
}

// debugFrame MC2A_DEBUG=1 时打印帧类型（排障 / 校准用，不打正文）。
func debugFrame(f frame) {
	if os.Getenv("MC2A_DEBUG") == "" {
		return
	}
	detail := ""
	if p, ok := decodePayload(f.Data); ok {
		if n := str(p, "sessionUpdate"); n != "" {
			detail = " acp=" + n
		} else if n := str(p, "type"); n != "" {
			detail = " acp=" + n
		}
	}
	log.Printf("[ws] type=%s kind=%s%s", f.Type, f.Kind, detail)
}
