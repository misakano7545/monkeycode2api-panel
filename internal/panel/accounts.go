// accounts.go 账号增删与单号运维：粘贴 session 添加、批量导入、签到、刷新额度、
// 停用/启用、解冷却、移除，以及"全部签到/全部刷新额度"。
//
// 所有动作都落到 account.Store / pool 的既有入口，不新增池语义：面板只是把
// 上游结果与本地状态对齐，重启后 auths/ 目录仍是唯一事实来源。
package panel

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/misakano7545/monkeycode2api-panel/internal/account"
	"github.com/misakano7545/monkeycode2api-panel/internal/upstream"
)

// client 按账号地区取上游客户端；未注入时返回 nil（调用方 501）。
func (p *Panel) client(region upstream.Region) *upstream.Client {
	if p.cfg.Client == nil {
		return nil
	}
	return p.cfg.Client(region)
}

// regionOf 账号地区（脏值回落国内版）。
func regionOf(a account.Account) upstream.Region {
	r, err := upstream.ParseRegion(a.Region)
	if err != nil {
		return upstream.CN
	}
	return r
}

// disableOnAuth 上游判定登录态失效时把账号停用，避免继续消耗请求。
// 只对 KindAuth 生效：网络抖动/上游 5xx 不该把好账号停掉。
func (p *Panel) disableOnAuth(id string, err error) bool {
	if !upstream.Is(err, upstream.KindAuth) {
		return false
	}
	_ = p.cfg.Store.Update(id, func(x *account.Account) { x.Disabled = true })
	return true
}

type addReq struct {
	Region  string `json:"region"`
	Session string `json:"session"`
	ImageID string `json:"image_id"`
}

// addSession 粘贴 session 添加账号（校验登录态 + 自动发现 image_id）。
func (p *Panel) addSession(w http.ResponseWriter, r *http.Request) {
	var req addReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求体不是合法 JSON")
		return
	}
	region, err := upstream.ParseRegion(req.Region)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	client := p.client(region)
	if client == nil {
		writeErr(w, http.StatusNotImplemented, "上游客户端未装配")
		return
	}
	cookieName, session := account.NormalizeSession(req.Session)
	if session == "" {
		writeErr(w, http.StatusBadRequest, "请粘贴 MonkeyCode 的 session cookie")
		return
	}
	cookie := cookieName + "=" + session
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()

	user, err := client.Status(ctx, cookie)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "登录态校验失败："+err.Error()+"（确认 session 有效、站点选对：国内 .com / 国际 .net）")
		return
	}
	imageID := strings.TrimSpace(req.ImageID)
	if imageID == "" {
		imageID = client.DiscoverImageID(ctx, cookie)
	}
	wallet, _ := client.Wallet(ctx, cookie)

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
	if err := p.cfg.Store.Put(a); err != nil {
		writeErr(w, http.StatusInternalServerError, "保存账号失败："+err.Error())
		return
	}
	okJSON(w, "已添加 "+a.ID, map[string]any{"account": a, "warning": imageWarn(imageID)})
}

// imageWarn 缺 image_id 的提示（上游建任务必填，新账号常拿不到）。
func imageWarn(imageID string) string {
	if imageID == "" {
		return "没发现 image_id（账号还没有历史任务）：请先在官网跑一个任务，或手动填入镜像 UUID，否则无法建任务"
	}
	return ""
}

// importAccounts 批量导入账号 JSON（单个对象或数组，字段与 auths/*.json 一致）。
// 不做上游校验：导入的往往是别处导出的既有凭证，校验交给"刷新额度"按钮。
func (p *Panel) importAccounts(w http.ResponseWriter, r *http.Request) {
	raw, err := readBody(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	var list []account.Account
	trimmed := strings.TrimSpace(string(raw))
	if strings.HasPrefix(trimmed, "[") {
		if err := json.Unmarshal(raw, &list); err != nil {
			writeErr(w, http.StatusBadRequest, "JSON 数组解析失败："+err.Error())
			return
		}
	} else {
		var one account.Account
		if err := json.Unmarshal(raw, &one); err != nil {
			writeErr(w, http.StatusBadRequest, "JSON 解析失败："+err.Error())
			return
		}
		list = append(list, one)
	}
	var added, failed int
	var firstErr string
	for i := range list {
		a := list[i]
		name, session := account.NormalizeSession(a.Session)
		if session == "" {
			failed++
			continue
		}
		a.Session, a.CookieName = session, firstNonEmpty(a.CookieName, name)
		if _, err := upstream.ParseRegion(a.Region); err != nil {
			a.Region = string(upstream.CN)
		}
		if err := p.cfg.Store.Put(&a); err != nil {
			failed++
			if firstErr == "" {
				firstErr = err.Error()
			}
			continue
		}
		added++
	}
	if added == 0 {
		writeErr(w, http.StatusBadRequest, "没有可导入的账号（session 为空或落盘失败）："+firstErr)
		return
	}
	okJSON(w, "已导入 "+itoa(added)+" 个账号", map[string]any{"added": added, "failed": failed})
}

// accountCheckin 单号签到（已签自动跳过）。
func (p *Panel) accountCheckin(w http.ResponseWriter, r *http.Request) {
	a, client, ok := p.accountAndClient(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	checked, err := client.CheckinStatus(ctx, a.CookieHeader())
	if err != nil {
		if p.disableOnAuth(a.ID, err) {
			writeErr(w, http.StatusUnauthorized, "登录态已失效，账号已停用："+err.Error())
			return
		}
		writeErr(w, http.StatusBadGateway, "查签到状态失败："+err.Error())
		return
	}
	if checked {
		okJSON(w, "今天已签到", nil)
		return
	}
	if err := client.Checkin(ctx, a.CookieHeader()); err != nil {
		if p.disableOnAuth(a.ID, err) {
			writeErr(w, http.StatusUnauthorized, "登录态已失效，账号已停用："+err.Error())
			return
		}
		writeErr(w, http.StatusBadGateway, "签到失败："+err.Error())
		return
	}
	today := time.Now().Format("2006-01-02")
	_ = p.cfg.Store.Update(a.ID, func(x *account.Account) { x.LastCheckin = today })
	okJSON(w, "签到成功（+100 积分）", nil)
}

// accountBalance 单号刷新额度（积分 + 今日 token）。
func (p *Panel) accountBalance(w http.ResponseWriter, r *http.Request) {
	a, client, ok := p.accountAndClient(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	wallet, err := client.Wallet(ctx, a.CookieHeader())
	if err != nil {
		_ = p.cfg.Store.Update(a.ID, func(x *account.Account) {
			x.Wallet = &account.Wallet{Error: err.Error(), FetchedAt: time.Now().Unix()}
		})
		if p.disableOnAuth(a.ID, err) {
			writeErr(w, http.StatusUnauthorized, "登录态已失效，账号已停用："+err.Error())
			return
		}
		writeErr(w, http.StatusBadGateway, "拉额度失败："+err.Error())
		return
	}
	_ = p.cfg.Store.Update(a.ID, func(x *account.Account) { x.Wallet = wallet })
	okJSON(w, "额度已刷新", map[string]any{"wallet": wallet})
}

// accountDisable 手动停用（不再被池选中）。
func (p *Panel) accountDisable(w http.ResponseWriter, r *http.Request) {
	if !p.mustGet(w, r) {
		return
	}
	_ = p.cfg.Store.Update(r.PathValue("uid"), func(x *account.Account) { x.Disabled = true })
	okJSON(w, "已停用", nil)
}

// accountRevive 手动启用。
func (p *Panel) accountRevive(w http.ResponseWriter, r *http.Request) {
	if !p.mustGet(w, r) {
		return
	}
	_ = p.cfg.Store.Update(r.PathValue("uid"), func(x *account.Account) { x.Disabled = false })
	okJSON(w, "已启用", nil)
}

// accountCooldownClear 解除冷却（不改账号状态）。
func (p *Panel) accountCooldownClear(w http.ResponseWriter, r *http.Request) {
	if !p.mustGet(w, r) {
		return
	}
	p.cfg.Pool.ClearCooldown(r.PathValue("uid"))
	okJSON(w, "已解除冷却", nil)
}

// accountRemove 移除账号（auths/<id>.json 一并删除）。
func (p *Panel) accountRemove(w http.ResponseWriter, r *http.Request) {
	if !p.mustGet(w, r) {
		return
	}
	if err := p.cfg.Store.Delete(r.PathValue("uid")); err != nil {
		writeErr(w, http.StatusInternalServerError, "删除失败："+err.Error())
		return
	}
	okJSON(w, "已删除", nil)
}

// checkinAll 全部签到（逐号串行，避免同时打上游验证码接口）。
func (p *Panel) checkinAll(w http.ResponseWriter, r *http.Request) {
	if p.cfg.Client == nil {
		writeErr(w, http.StatusNotImplemented, "上游客户端未装配")
		return
	}
	var okN, failed, skipped int
	var firstErr string
	for _, a := range p.cfg.Store.List() {
		if a.Disabled {
			skipped++
			continue
		}
		client := p.client(regionOf(a))
		ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
		checked, err := client.CheckinStatus(ctx, a.CookieHeader())
		if err == nil && checked {
			cancel()
			skipped++
			continue
		}
		if err == nil {
			err = client.Checkin(ctx, a.CookieHeader())
		}
		cancel()
		if err != nil {
			failed++
			if firstErr == "" {
				firstErr = a.ID + "：" + err.Error()
			}
			p.disableOnAuth(a.ID, err)
			continue
		}
		today := time.Now().Format("2006-01-02")
		_ = p.cfg.Store.Update(a.ID, func(x *account.Account) { x.LastCheckin = today })
		okN++
	}
	msg := "签到完成：成功 " + itoa(okN) + " / 已签 " + itoa(skipped) + " / 失败 " + itoa(failed)
	okJSON(w, msg, map[string]any{"ok_count": okN, "ok": okN, "skipped": skipped, "failed": failed, "first_error": firstErr})
}

// balanceAll 全部刷新额度。
func (p *Panel) balanceAll(w http.ResponseWriter, r *http.Request) {
	if p.cfg.Client == nil {
		writeErr(w, http.StatusNotImplemented, "上游客户端未装配")
		return
	}
	var okN, failed int
	var firstErr string
	for _, a := range p.cfg.Store.List() {
		client := p.client(regionOf(a))
		ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
		wallet, err := client.Wallet(ctx, a.CookieHeader())
		cancel()
		if err != nil {
			failed++
			if firstErr == "" {
				firstErr = a.ID + "：" + err.Error()
			}
			_ = p.cfg.Store.Update(a.ID, func(x *account.Account) {
				x.Wallet = &account.Wallet{Error: err.Error(), FetchedAt: time.Now().Unix()}
			})
			p.disableOnAuth(a.ID, err)
			continue
		}
		_ = p.cfg.Store.Update(a.ID, func(x *account.Account) { x.Wallet = wallet })
		okN++
	}
	okJSON(w, "额度已刷新 "+itoa(okN)+" 个账号", map[string]any{"ok": okN, "failed": failed, "first_error": firstErr})
}

// accountAndClient 取账号与对应上游客户端；失败时已写响应。
func (p *Panel) accountAndClient(w http.ResponseWriter, r *http.Request) (account.Account, *upstream.Client, bool) {
	id := r.PathValue("uid")
	a, ok := p.cfg.Store.Get(id)
	if !ok {
		writeErr(w, http.StatusNotFound, "账号不存在："+id)
		return account.Account{}, nil, false
	}
	client := p.client(regionOf(a))
	if client == nil {
		writeErr(w, http.StatusNotImplemented, "上游客户端未装配")
		return account.Account{}, nil, false
	}
	return a, client, true
}

// mustGet 只做存在性校验（无上游调用的动作共用）。
func (p *Panel) mustGet(w http.ResponseWriter, r *http.Request) bool {
	id := r.PathValue("uid")
	if _, ok := p.cfg.Store.Get(id); !ok {
		writeErr(w, http.StatusNotFound, "账号不存在："+id)
		return false
	}
	return true
}
