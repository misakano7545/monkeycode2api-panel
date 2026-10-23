// Package panel 内嵌式 Web 管理面板：账号池总览、单号运维（签到/刷新额度/
// 解冷却/停用启用/移除）、粘贴式 session 添加账号（免重启热加载进池）、
// 模型目录与用量观测、运行日志环形缓冲。
//
// 设计约束：
//   - 前端 go:embed（index.html + theme.js + app.js），无任何外部构建依赖，
//     与二进制同体部署；
//   - 鉴权复用网关 api_key（Bearer），与 /v1/* 同一口径；api_key 为空 = 不鉴权
//     （仅本机/私网使用）。面板 HTML 本身无秘密，可匿名加载，密钥只发给 /panel/api/*；
//   - 不改写既有池语义：所有运维操作落到 pool/account 已有入口，添加账号走
//     上游校验 + account.Store.Put，重启后与 auths/ 目录天然对齐。
package panel

import (
	"encoding/json"
	"net/http"
	"sort"
	"time"

	"github.com/misakano7545/monkeycode2api-panel/internal/account"
	"github.com/misakano7545/monkeycode2api-panel/internal/httpauth"
	"github.com/misakano7545/monkeycode2api-panel/internal/pool"
	"github.com/misakano7545/monkeycode2api-panel/internal/upstream"
	"github.com/misakano7545/monkeycode2api-panel/internal/usage"
)

// Config 面板依赖（main 装配注入）。
type Config struct {
	Store   *account.Store
	Pool    *pool.Pool
	Catalog *upstream.Catalog
	Ledger  *usage.Ledger
	Logs    *Ring

	// Client 按地区取上游客户端（额度/签到/模型刷新用）。nil = 相关接口 501。
	Client func(upstream.Region) *upstream.Client

	APIKey  string // 空 = 不鉴权（与主服务同语义）
	Version string // 面板版本号（展示用）

	// ConfigPath config.json 路径（配置页读写用）；空 = 配置页返回 501。
	ConfigPath string
}

// Panel 管理面板 handler。挂载方式：外层 mux Handle("/panel/", panel)，
// 本 mux 的 pattern 均带 /panel 前缀（外层不做前缀剥离）。
type Panel struct {
	cfg     Config
	mux     *http.ServeMux
	started time.Time
	logs    *Ring
}

// New 构建面板 handler 并注册路由。
func New(cfg Config) *Panel {
	p := &Panel{cfg: cfg, mux: http.NewServeMux(), started: time.Now(), logs: cfg.Logs}
	if p.logs == nil {
		p.logs = NewRing(500)
	}
	p.mux.HandleFunc("GET /panel/{$}", p.index)
	p.mux.HandleFunc("GET /panel/theme.js", p.themeScript)
	p.mux.HandleFunc("GET /panel/app.js", p.appScript)
	p.mux.HandleFunc("GET /panel/api/overview", p.withAuth(p.overview))
	p.mux.HandleFunc("GET /panel/api/logs", p.withAuth(p.logsHandler))
	p.mux.HandleFunc("GET /panel/api/models", p.withAuth(p.models))
	p.mux.HandleFunc("POST /panel/api/models/refresh", p.withAuth(p.modelsRefresh))
	p.mux.HandleFunc("GET /panel/api/usage", p.withAuth(p.usageSummary))
	p.mux.HandleFunc("GET /panel/api/request_logs", p.withAuth(p.requestLogs))
	p.mux.HandleFunc("GET /panel/api/config", p.withAuth(p.getConfig))
	p.mux.HandleFunc("POST /panel/api/config", p.withAuth(p.saveConfig))
	p.mux.HandleFunc("POST /panel/api/accounts/add_session", p.withAuth(p.addSession))
	p.mux.HandleFunc("POST /panel/api/accounts/login", p.withAuth(p.loginAccount))
	p.mux.HandleFunc("POST /panel/api/accounts/oauth/start", p.withAuth(p.oauthStart))
	p.mux.HandleFunc("POST /panel/api/accounts/import", p.withAuth(p.importAccounts))
	p.mux.HandleFunc("POST /panel/api/accounts/{uid}/checkin", p.withAuth(p.accountCheckin))
	p.mux.HandleFunc("POST /panel/api/accounts/{uid}/balance", p.withAuth(p.accountBalance))
	p.mux.HandleFunc("POST /panel/api/accounts/{uid}/disable", p.withAuth(p.accountDisable))
	p.mux.HandleFunc("POST /panel/api/accounts/{uid}/revive", p.withAuth(p.accountRevive))
	p.mux.HandleFunc("POST /panel/api/accounts/{uid}/cooldown_clear", p.withAuth(p.accountCooldownClear))
	p.mux.HandleFunc("POST /panel/api/accounts/{uid}/remove", p.withAuth(p.accountRemove))
	p.mux.HandleFunc("POST /panel/api/checkin_all", p.withAuth(p.checkinAll))
	p.mux.HandleFunc("POST /panel/api/balance_all", p.withAuth(p.balanceAll))
	return p
}

// ServeHTTP 实现 http.Handler（安全头对页面与 API 统一生效）。
func (p *Panel) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	setSecurityHeaders(w)
	p.mux.ServeHTTP(w, r)
}

// Logs 日志环（main 把 log 输出镜像进来用）。
func (p *Panel) Logs() *Ring { return p.logs }

// withAuth 面板统一鉴权：与 /v1/* 同一口径（Bearer，常量时间比较）。
func (p *Panel) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !httpauth.VerifyBearer(r, p.cfg.APIKey) {
			writeErr(w, http.StatusUnauthorized, "invalid_api_key")
			return
		}
		next(w, r)
	}
}

// accountView 面板账号行（只读投影，不暴露 session 原文）。
type accountView struct {
	ID                string `json:"id"`
	Region            string `json:"region"`
	Name              string `json:"name,omitempty"`
	Email             string `json:"email,omitempty"`
	Plan              string `json:"plan,omitempty"`
	UserID            string `json:"user_id,omitempty"`
	Disabled          bool   `json:"disabled"`
	Busy              bool   `json:"busy"`
	Cooldown          string `json:"cooldown,omitempty"`
	CoolKind          string `json:"cool_kind,omitempty"`
	ImageID           string `json:"image_id,omitempty"`
	Expiring          bool   `json:"expiring"`
	SessionExpiresAt  int64  `json:"session_expires_at,omitempty"`
	DailyTokenBalance int64  `json:"daily_token_balance"`
	DailyTokenLimit   int64  `json:"daily_token_limit"`
	Balance           int64  `json:"balance"`
	WalletError       string `json:"wallet_error,omitempty"`
	LastCheckin       string `json:"last_checkin,omitempty"`
	AddedAt           int64  `json:"added_at"`
}

// overview 账号池总览（面板首屏唯一数据源，5 秒轮询）。
func (p *Panel) overview(w http.ResponseWriter, r *http.Request) {
	list := p.cfg.Store.List()
	stat := p.cfg.Pool.Snapshot()
	views := make([]accountView, 0, len(list))
	var healthy, cooling, disabled int
	var tokens, credits int64
	for _, a := range list {
		st := stat[a.ID]
		v := accountView{
			ID: a.ID, Region: a.Region, Name: a.Name, Email: a.Email, Plan: a.Plan,
			UserID: a.UserID, Disabled: a.Disabled, Busy: st.Busy,
			Cooldown: st.Cooldown, CoolKind: st.CoolKind, ImageID: a.ImageID,
			Expiring: a.Expiring(), SessionExpiresAt: a.ExpiresAt,
			LastCheckin: a.LastCheckin, AddedAt: a.AddedAt,
		}
		if a.Wallet != nil {
			v.DailyTokenBalance = a.Wallet.DailyTokenBalance
			v.DailyTokenLimit = a.Wallet.DailyTokenLimit
			v.Balance = a.Wallet.Balance
			v.WalletError = a.Wallet.Error
		}
		switch {
		case a.Disabled:
			disabled++
		case st.Cooldown != "":
			cooling++
		case !st.Busy && a.Ready():
			healthy++
		}
		if !a.Disabled {
			tokens += v.DailyTokenBalance
			credits += v.Balance
		}
		views = append(views, v)
	}
	sort.Slice(views, func(i, j int) bool { return views[i].ID < views[j].ID })
	writeJSON(w, http.StatusOK, map[string]any{
		"version":          p.cfg.Version,
		"uptime_sec":       int(time.Since(p.started).Seconds()),
		"auth_required":    p.cfg.APIKey != "",
		"total":            len(views),
		"healthy":          healthy,
		"cooling":          cooling,
		"disabled":         disabled,
		"in_flight_full":   healthy == 0 && len(views) > 0,
		"tokens_remaining": tokens,
		"credits":          credits,
		"accounts":         views,
	})
}

// logsHandler 返回日志环形缓冲快照（时间升序，含频道标记 chat/task/sys）。
func (p *Panel) logsHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"entries": p.logs.Snapshot()})
}

// writeJSON 面板统一 JSON 出口。
func writeJSON(w http.ResponseWriter, status int, v any) {
	raw, _ := json.Marshal(v)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(raw)
}

// writeErr 面板统一错误出口（前端按 d.error 展示）。
func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"ok": false, "error": msg})
}

// okJSON 成功出口（带一句给用户看的中文说明）。
func okJSON(w http.ResponseWriter, msg string, extra map[string]any) {
	out := map[string]any{"ok": true, "message": msg}
	for k, v := range extra {
		out[k] = v
	}
	writeJSON(w, http.StatusOK, out)
}
