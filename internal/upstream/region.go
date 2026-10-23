// Package upstream 是 MonkeyCode 上游客户端：REST（status/models/wallet/tasks）
// 与任务流 WebSocket（ACP 事件）。
//
// 上游是 Agent 任务平台，不是 chat API：一次对话 = POST /api/v1/users/tasks 建
// 任务 + WS /api/v1/users/tasks/stream 拉事件。协议结论来自实测与逆向（见 README
// 「协议」一节）：帧是 {type,kind,data,timestamp}，data 是 base64 或 JSON 串。
package upstream

import "fmt"

// Region 站点：同一套协议的两个站点。
type Region string

const (
	CN   Region = "cn"
	INTL Region = "intl"
)

func (r Region) Origin() string {
	if r == INTL {
		return "https://monkeycode-ai.net"
	}
	return "https://monkeycode-ai.com"
}

func (r Region) Label() string {
	if r == INTL {
		return "国际版"
	}
	return "国内版"
}

func ParseRegion(s string) (Region, error) {
	switch s {
	case "cn", "com", "":
		return CN, nil
	case "intl", "net":
		return INTL, nil
	}
	return "", fmt.Errorf("未知地区 %q（只支持 cn / intl）", s)
}

// 上游端点与常量（实测，改动请谨慎）。
const (
	EpStatus  = "/api/v1/users/status"
	EpModels  = "/api/v1/users/models"
	EpWallet  = "/api/v1/users/wallet"
	EpCheckin = "/api/v1/users/wallet/checkin"
	EpTasks   = "/api/v1/users/tasks"
	EpStream  = "/api/v1/users/tasks/stream"

	// 账号密码登录（模拟网页登录，不需要浏览器）。
	EpPasswordLogin = "/api/v1/users/password-login"
	EpServerConfig  = "/api/v1/server/config"

	// SessionCookie 普通用户会话 cookie 名（团队管理员是另一套，不接）。
	SessionCookie = "monkeycode_ai_session"

	// PublicHost 公共托管主机占位符。
	PublicHost = "public_host"
)

// CliNameFor interface_type → 容器里跑的 coding agent。
//
//	openai_chat → opencode，openai_responses → codex，anthropic → claude
func CliNameFor(interfaceType string) string {
	switch interfaceType {
	case "openai_responses":
		return "codex"
	case "anthropic":
		return "claude"
	}
	return "opencode"
}
