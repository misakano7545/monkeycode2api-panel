// Package panel 面板：账号池、额度、模型、请求日志。
//
// 页面本身不含秘密；数据接口 /panel/api/* 与网关同一把 API key 鉴权
// （X-API-Key 头或 ?key=）。Deps 只传依赖与两个回调，避免和 server 互相 import。
package panel

import (
	"context"
	"crypto/subtle"
	_ "embed"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/misakano7545/monkeycode2api-panel/internal/account"
	"github.com/misakano7545/monkeycode2api-panel/internal/config"
	"github.com/misakano7545/monkeycode2api-panel/internal/pool"
	"github.com/misakano7545/monkeycode2api-panel/internal/upstream"
	"github.com/misakano7545/monkeycode2api-panel/internal/usage"
)

//go:embed index.html
var indexHTML []byte

type Deps struct {
	Cfg     *config.Config
	Store   *account.Store
	Catalog *upstream.Catalog
	Pool    *pool.Pool
	Ledger  *usage.Ledger
	// RefreshCatalog 拉两个站点的模型目录（force=true 绕过 TTL）。
	RefreshCatalog func(ctx context.Context, force bool) map[string]string
}

type panel struct {
	d Deps
}

// Handler 返回挂在 /panel/ 下的处理器。
func Handler(d Deps) http.Handler {
	p := &panel{d: d}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /panel/", p.index)
	mux.HandleFunc("GET /panel/api/accounts", p.guard(p.accounts))
	mux.HandleFunc("POST /panel/api/accounts", p.guard(p.addAccount))
	mux.HandleFunc("DELETE /panel/api/accounts/{id}", p.guard(p.deleteAccount))
	mux.HandleFunc("POST /panel/api/accounts/{id}/{action}", p.guard(p.accountAction))
	mux.HandleFunc("POST /panel/api/models/refresh", p.guard(p.refreshModels))
	mux.HandleFunc("GET /panel/api/usage", p.guard(p.usageSummary))
	mux.HandleFunc("GET /panel/api/logs", p.guard(p.logs))
	return mux
}

func (p *panel) index(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/panel/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(indexHTML)
}

func (p *panel) guard(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("X-API-Key")
		if key == "" {
			key = r.URL.Query().Get("key")
		}
		if key == "" {
			key = bearer(r)
		}
		if key == "" || subtle.ConstantTimeCompare([]byte(key), []byte(p.d.Cfg.APIKey)) != 1 {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "API key 无效"})
			return
		}
		next(w, r)
	}
}

func bearer(r *http.Request) string {
	if rest, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
		return strings.TrimSpace(rest)
	}
	return ""
}

// accounts 账号列表（含池状态）。
func (p *panel) accounts(w http.ResponseWriter, _ *http.Request) {
	snap := p.d.Pool.Snapshot()
	list := p.d.Store.List()
	out := make([]map[string]any, 0, len(list))
	for _, a := range list {
		st := snap[a.ID]
		out = append(out, map[string]any{
			"id": a.ID, "region": a.Region, "name": a.Name, "user_id": a.UserID,
			"email": a.Email, "plan": a.Plan,
			"image_id": a.ImageID, "ready": a.Ready(), "disabled": a.Disabled,
			"busy": st.Busy, "cooldown": st.Cooldown, "cool_kind": st.CoolKind,
			"added_at": a.AddedAt, "expires_at": a.ExpiresAt, "expiring": a.Expiring(),
			"wallet": a.Wallet, "last_checkin": a.LastCheckin,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"accounts": out})
}

type addReq struct {
	Region  string `json:"region"`
	Session string `json:"session"`
	ImageID string `json:"image_id"`
}

// addAccount 粘贴 session 添加账号（校验 + 自动发现 image_id）。
func (p *panel) addAccount(w http.ResponseWriter, r *http.Request) {
	var req addReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "请求体不是合法 JSON"})
		return
	}
	region, err := upstream.ParseRegion(req.Region)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	cookieName, session := account.NormalizeSession(req.Session)
	if session == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "请粘贴 MonkeyCode 的 session cookie"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	client := upstream.New(region, p.d.Cfg.Upstream.UserAgent, p.d.Cfg.APITimeout())

	user, err := client.Status(ctx, cookieName+"="+session)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "登录态校验失败：" + err.Error() + "（确认 session 有效、站点选对：国内 .com / 国际 .net）",
		})
		return
	}
	imageID := strings.TrimSpace(req.ImageID)
	if imageID == "" {
		imageID = client.DiscoverImageID(ctx, cookieName+"="+session)
	}
	wallet, _ := client.Wallet(ctx, cookieName+"="+session)

	a := &account.Account{
		ID:         account.AccountID(string(region), user.ID),
		Region:     string(region),
		Session:    session,
		CookieName: cookieName,
		ImageID:    imageID,
		UserID:     user.ID,
		Name:       firstNonEmpty(user.Name, user.Nickname, user.Email),
		Email:      user.Email,
		Plan:       user.SubscriptionLevel,
		Wallet:     wallet,
	}
	if err := p.d.Store.Put(a); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "保存账号失败：" + err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": a.ID, "image_id": imageID,
		"warning": imageWarn(imageID),
	})
}

func imageWarn(imageID string) string {
	if imageID == "" {
		return "没发现 image_id（账号还没有历史任务）：请先在官网跑一个任务，或手动填入镜像 UUID，否则无法建任务"
	}
	return ""
}

func (p *panel) deleteAccount(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := p.d.Store.Delete(id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// accountAction 单账号动作：wallet / checkin / toggle / cooldown_clear。
func (p *panel) accountAction(w http.ResponseWriter, r *http.Request) {
	id, action := r.PathValue("id"), r.PathValue("action")
	a, ok := p.d.Store.Get(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "账号不存在"})
		return
	}
	region, _ := upstream.ParseRegion(a.Region)
	client := upstream.New(region, p.d.Cfg.Upstream.UserAgent, p.d.Cfg.APITimeout())
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	switch action {
	case "wallet":
		w2, err := client.Wallet(ctx, a.CookieHeader())
		if err != nil {
			_ = p.d.Store.Update(id, func(x *account.Account) { x.Wallet = &account.Wallet{Error: err.Error(), FetchedAt: time.Now().Unix()} })
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
			return
		}
		_ = p.d.Store.Update(id, func(x *account.Account) { x.Wallet = w2 })
		writeJSON(w, http.StatusOK, map[string]any{"wallet": w2})
	case "checkin":
		checked, err := client.CheckinStatus(ctx, a.CookieHeader())
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
			return
		}
		if checked {
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "今天已签到"})
			return
		}
		if err := client.Checkin(ctx, a.CookieHeader()); err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
			return
		}
		_ = p.d.Store.Update(id, func(x *account.Account) { x.LastCheckin = time.Now().Format("2006-01-02") })
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "签到成功（+100 积分）"})
	case "toggle":
		_ = p.d.Store.Update(id, func(x *account.Account) { x.Disabled = !x.Disabled })
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	case "cooldown_clear":
		p.d.Pool.ClearCooldown(id)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	case "image":
		var body struct {
			ImageID string `json:"image_id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		imageID := strings.TrimSpace(body.ImageID)
		_ = p.d.Store.Update(id, func(x *account.Account) { x.ImageID = imageID })
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "image_id": imageID})
	default:
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "未知动作 " + action})
	}
}

func (p *panel) refreshModels(w http.ResponseWriter, r *http.Request) {
	result := p.d.RefreshCatalog(r.Context(), true)
	items := p.d.Catalog.List()
	writeJSON(w, http.StatusOK, map[string]any{"result": result, "count": len(items)})
}

func (p *panel) usageSummary(w http.ResponseWriter, r *http.Request) {
	days := 7
	if v := r.URL.Query().Get("days"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 90 {
			days = n
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"days": p.d.Ledger.ByDay(days)})
}

func (p *panel) logs(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if v := r.URL.Query().Get("limit"); v == "100" || v == "200" {
		limit = 200
	}
	writeJSON(w, http.StatusOK, map[string]any{"records": p.d.Ledger.Recent(limit)})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
