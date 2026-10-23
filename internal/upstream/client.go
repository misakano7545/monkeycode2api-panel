package upstream

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/misakano7545/monkeycode2api-panel/internal/account"
)

// 错误种类：账号池据此决定「换号 / 短冷却 / 长冷却 / 停用」。
const (
	KindAuth     = "auth"      // session 失效 → 停用，要求重新粘贴
	KindBusy     = "busy"      // 该账号已有运行中的任务（10811）→ 短冷却换号
	KindQuota    = "quota"     // 额度不足 / 需升级 → 长冷却
	KindRate     = "rate"      // 429 → 短冷却
	KindNotFound = "not_found" // 404 模型 / 镜像不存在
	KindUpstream = "upstream"  // 其余上游故障 → 短冷却
)

type Error struct {
	Kind   string
	Status int
	Code   int
	Msg    string
}

func (e *Error) Error() string { return e.Msg }

// Is 便于调用方按种类判断。
func Is(err error, kind string) bool {
	ue, ok := err.(*Error)
	return ok && ue.Kind == kind
}

// Client 一个地区的上游客户端。
type Client struct {
	Region Region
	HTTP   *http.Client
	UA     string
}

func New(region Region, ua string, timeout time.Duration) *Client {
	return &Client{Region: region, UA: ua, HTTP: &http.Client{Timeout: timeout}}
}

// API 响应外壳：code 非 0 即业务失败。
type apiResp struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Msg     string          `json:"msg"`
	Data    json.RawMessage `json:"data"`
}

func (c *Client) url(path string) string { return c.Region.Origin() + path }

// doJSON 带 cookie 调上游。cookie 为空时不带（验证码等公开端点）。
// cookie 是完整的 `name=value`（名字由账号记录决定，见 account.CookieHeader）。
func (c *Client) doJSON(ctx context.Context, cookie, method, path string, body any, out any) error {
	var rd io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.url(path), rd)
	if err != nil {
		return err
	}
	ua := c.UA
	if ua == "" {
		ua = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36"
	}
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Origin", c.Region.Origin())
	req.Header.Set("Referer", c.Region.Origin()+"/")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return &Error{Kind: KindUpstream, Msg: fmt.Sprintf("上游请求失败：%v", err)}
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return &Error{Kind: KindUpstream, Msg: fmt.Sprintf("上游响应读取失败：%v", err)}
	}

	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return &Error{Kind: KindAuth, Status: resp.StatusCode, Msg: "上游登录态失效（HTTP " + fmt.Sprint(resp.StatusCode) + "）"}
	case resp.StatusCode == http.StatusTooManyRequests:
		return &Error{Kind: KindRate, Status: resp.StatusCode, Msg: "上游限流（HTTP 429）"}
	case resp.StatusCode == http.StatusNotFound:
		return &Error{Kind: KindNotFound, Status: resp.StatusCode, Msg: "上游 404：" + truncate(string(raw), 160)}
	case resp.StatusCode >= 500:
		return &Error{Kind: KindUpstream, Status: resp.StatusCode, Msg: "上游 " + fmt.Sprint(resp.StatusCode) + "：" + truncate(string(raw), 160)}
	case resp.StatusCode >= 400:
		return &Error{Kind: KindUpstream, Status: resp.StatusCode, Msg: "上游 HTTP " + fmt.Sprint(resp.StatusCode) + "：" + truncate(string(raw), 160)}
	}

	var top map[string]json.RawMessage
	if json.Unmarshal(raw, &top) == nil {
		if _, has := top["code"]; !has {
			// 裸对象（验证码等）：直接解给 out
			if out != nil {
				return json.Unmarshal(raw, out)
			}
			return nil
		}
	}
	var ar apiResp
	if err := json.Unmarshal(raw, &ar); err != nil {
		if out != nil {
			return json.Unmarshal(raw, out)
		}
		return nil
	}
	if ar.Code != 0 {
		msg := ar.Message
		if msg == "" {
			msg = ar.Msg
		}
		return classifyCode(ar.Code, msg)
	}
	if out != nil && len(ar.Data) > 0 {
		return json.Unmarshal(ar.Data, out)
	}
	return nil
}

// classifyCode 业务码 → 错误种类（码表见 README「协议」）。
func classifyCode(code int, msg string) *Error {
	if msg == "" {
		msg = fmt.Sprintf("上游业务码 %d", code)
	} else {
		msg = fmt.Sprintf("上游业务码 %d：%s", code, msg)
	}
	switch code {
	case 40100, 401:
		return &Error{Kind: KindAuth, Code: code, Msg: msg}
	case 40002, 40003, 40004:
		return &Error{Kind: KindAuth, Code: code, Msg: msg + "（账号密码错 / 被封禁 / 未激活）"}
	case 10811:
		return &Error{Kind: KindBusy, Code: code, Msg: msg + "（该账号已有运行中的任务）"}
	case 4002, 4001:
		return &Error{Kind: KindQuota, Code: code, Msg: msg}
	case 50000:
		return &Error{Kind: KindUpstream, Code: code, Msg: msg}
	}
	return &Error{Kind: KindUpstream, Code: code, Msg: msg}
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// User 当前登录用户。
type User struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	Nickname          string `json:"nickname"`
	Email             string `json:"email"`
	SubscriptionLevel string `json:"subscription_level"`
}

// Status 校验登录态（200 + code 0 即已登录）。
func (c *Client) Status(ctx context.Context, cookie string) (*User, error) {
	var data json.RawMessage
	if err := c.doJSON(ctx, cookie, http.MethodGet, EpStatus, nil, &data); err != nil {
		return nil, err
	}
	// user 可能落在 data.user，也可能是 data 本身 —— 两种都认。
	var top map[string]json.RawMessage
	if json.Unmarshal(data, &top) == nil {
		if u, ok := top["user"]; ok {
			data = u
		}
	}
	var u User
	if err := json.Unmarshal(data, &u); err != nil {
		return &User{}, nil
	}
	return &u, nil
}

// Model 平台模型目录条目。
type Model struct {
	ID            string `json:"id"` // 模型 UUID（建任务的 model_id）
	Provider      string `json:"provider"`
	Model         string `json:"model"` // 上游模型名
	DisplayName   string `json:"display_name"`
	InterfaceType string `json:"interface_type"` // openai_chat|openai_responses|anthropic
	ContextLimit  int64  `json:"context_limit"`
	OutputLimit   int64  `json:"output_limit"`
	IsFree        bool   `json:"is_free"`
	IsDefault     bool   `json:"is_default"`
	AccessLevel   string `json:"access_level"`
	Thinking      bool   `json:"thinking_enabled"`
}

// Models 拉平台模型目录（{code,data:{models:[...]}} 或 {models:[...]}）。
func (c *Client) Models(ctx context.Context, cookie string) ([]Model, error) {
	var data struct {
		Models []Model `json:"models"`
	}
	if err := c.doJSON(ctx, cookie, http.MethodGet, EpModels, nil, &data); err != nil {
		return nil, err
	}
	return data.Models, nil
}

// Wallet 拉钱包 / 每日额度（到账检查与选号都基于它）。
func (c *Client) Wallet(ctx context.Context, cookie string) (*account.Wallet, error) {
	var w account.Wallet
	if err := c.doJSON(ctx, cookie, http.MethodGet, EpWallet, nil, &w); err != nil {
		return nil, err
	}
	w.FetchedAt = time.Now().Unix()
	return &w, nil
}

// CheckinStatus 今日是否已签到。
func (c *Client) CheckinStatus(ctx context.Context, cookie string) (bool, error) {
	var st struct {
		CheckedIn bool `json:"checked_in"`
	}
	if err := c.doJSON(ctx, cookie, http.MethodGet, EpCheckin, nil, &st); err != nil {
		return false, err
	}
	return st.CheckedIn, nil
}

// Checkin 执行签到（自动求解 Cap.js PoW 验证码）。已签到的判断由调用方做。
func (c *Client) Checkin(ctx context.Context, cookie string) error {
	token, err := c.SolveCaptcha(ctx)
	if err != nil {
		return fmt.Errorf("求解验证码失败：%w", err)
	}
	var out map[string]any
	return c.doJSON(ctx, cookie, http.MethodPost, EpCheckin, map[string]string{"captcha_token": token}, &out)
}

// DiscoverImageID 从历史任务的 task.image.id 里取镜像 UUID（best-effort）。
//
// 新账号一个任务都没有时取不到 —— 此时账号仍可添加，由面板提示手动填。
func (c *Client) DiscoverImageID(ctx context.Context, cookie string) string {
	var data struct {
		Tasks []struct {
			Image *struct {
				ID string `json:"id"`
			} `json:"image"`
		} `json:"tasks"`
	}
	if err := c.doJSON(ctx, cookie, http.MethodGet, EpTasks+"?page=1&size=5", nil, &data); err != nil {
		return ""
	}
	for _, t := range data.Tasks {
		if t.Image != nil && strings.TrimSpace(t.Image.ID) != "" {
			return strings.TrimSpace(t.Image.ID)
		}
	}
	return ""
}

// CreateTask 建任务，返回 task id。
//
// 建任务必填：content、model_id（UUID）、image_id、host_id、cli_name。缺
// image_id 或 model_id 传名字上游回 400 参数错误。
func (c *Client) CreateTask(ctx context.Context, cookie string, body any) (string, error) {
	var out struct {
		ID     string `json:"id"`
		TaskID string `json:"task_id"`
	}
	if err := c.doJSON(ctx, cookie, http.MethodPost, EpTasks, body, &out); err != nil {
		return "", err
	}
	id := strings.TrimSpace(out.ID)
	if id == "" {
		id = strings.TrimSpace(out.TaskID)
	}
	if id == "" {
		return "", &Error{Kind: KindUpstream, Msg: "建任务响应里没有 task id（上游响应形态变化？）"}
	}
	return id, nil
}
