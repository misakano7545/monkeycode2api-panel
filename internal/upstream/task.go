package upstream

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// Target 建任务要用的目录字段（客户端模型名反查出来的）。
type Target struct {
	ModelID string // 上游模型 UUID
	CLI     string // opencode | codex | claude
	Model   string // 上游模型名（日志用）
}

// Prompt 上游提示词：正文 + 可选的 system（单独字段，不进 content）。
type Prompt struct {
	Content string
	System  string
}

// Msg 一条对话消息（三种协议的输入都归一到这里）。
type Msg struct {
	Role string
	Text string
}

// BuildPrompt 把对话消息压成一条 prompt。
//
// 上游没有 messages 概念，只有一条 agent 任务描述；system 抽出来单独走
// system_prompt 字段，其余按 [User]/[Assistant] 前缀拼接。
func BuildPrompt(messages []Msg) (Prompt, error) {
	var system string
	parts := make([]string, 0, len(messages))
	for _, m := range messages {
		text := strings.TrimSpace(m.Text)
		if text == "" {
			continue
		}
		switch m.Role {
		case "system", "developer":
			if system == "" {
				system = text
			}
		case "user":
			parts = append(parts, "[User]\n"+text)
		case "assistant":
			parts = append(parts, "[Assistant]\n"+text)
		case "tool", "function":
			parts = append(parts, "[Tool result]\n"+text)
		default:
			parts = append(parts, text)
		}
	}
	content := strings.Join(parts, "\n\n")
	if strings.TrimSpace(content) == "" {
		return Prompt{}, fmt.Errorf("messages 里没有可发送的文本内容")
	}
	return Prompt{Content: content, System: system}, nil
}

// CreateTaskBody 建任务请求体（与网页端同字段）。
//
// resource 用网页端的默认值（2 核 / 8G / 2 小时）；life 与任务流总超时对齐。
func CreateTaskBody(t Target, p Prompt, imageID string, lifeSeconds int) map[string]any {
	body := map[string]any{
		"content":  p.Content,
		"cli_name": t.CLI,
		"model_id": t.ModelID,
		"image_id": imageID,
		"host_id":  PublicHost,
		"resource": map[string]any{
			"core":   2,
			"memory": 8 << 30,
			"life":   lifeSeconds,
		},
	}
	if s := strings.TrimSpace(p.System); s != "" {
		body["system_prompt"] = s
	}
	return body
}

// UserInputData WS 起始消息的 data：网页端是 base64(JSON{content: base64(text), attachments})。
// 上游后端按「存储格式 → 新格式 → 纯文本」三级解析，因此这里照网页端双层编码。
func UserInputData(prompt string) string {
	inner := map[string]any{
		"content":     base64.StdEncoding.EncodeToString([]byte(prompt)),
		"attachments": []any{},
	}
	raw, _ := json.Marshal(inner)
	return base64.StdEncoding.EncodeToString(raw)
}

// StreamURL 任务流地址（mode=new：本请求新建任务轮次）。
func StreamURL(r Region, taskID string) string {
	return r.wsURL(EpStream + "?id=" + taskID + "&mode=new")
}

// WSHeaders 网页端握手同款头。
func WSHeaders(r Region, cookie, ua string) http.Header {
	h := http.Header{}
	h.Set("User-Agent", ua)
	h.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	h.Set("Cache-Control", "no-cache")
	h.Set("Pragma", "no-cache")
	h.Set("Origin", r.Origin())
	h.Set("Cookie", cookie)
	return h
}
