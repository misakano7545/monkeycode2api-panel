// Package pool 账号池：按地区 + 剩余额度挑号，一个账号同时只跑一个任务。
//
// 上游硬约束：同一账号已有运行中的任务时建任务返回 10811。因此「占用」是
// 每账号 1 个租约，忙时换号而不是排队（排队会把延迟拖到几分钟）。
package pool

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/misakano7545/monkeycode2api-panel/internal/account"
	"github.com/misakano7545/monkeycode2api-panel/internal/config"
	"github.com/misakano7545/monkeycode2api-panel/internal/upstream"
)

type cool struct {
	until time.Time
	kind  string
}

type Pool struct {
	store *account.Store
	cfg   *config.Config

	mu   sync.Mutex
	busy map[string]int
	cool map[string]cool
	errs map[string]int
}

func New(store *account.Store, cfg *config.Config) *Pool {
	return &Pool{store: store, cfg: cfg,
		busy: map[string]int{}, cool: map[string]cool{}, errs: map[string]int{}}
}

// Lease 一个被占用的账号。
type Lease struct {
	Account account.Account
	Region  upstream.Region
	p       *Pool
	freed   bool
}

// Release 归还账号（幂等）。
func (l *Lease) Release() {
	if l == nil || l.freed {
		return
	}
	l.freed = true
	l.p.mu.Lock()
	if l.p.busy[l.Account.ID] > 0 {
		l.p.busy[l.Account.ID]--
	}
	l.p.mu.Unlock()
}

// ErrNoAccount 没有可用账号（全忙 / 全冷却 / 全停用）。
type ErrNoAccount struct {
	Regions []upstream.Region
	Reason  string
}

func (e *ErrNoAccount) Error() string {
	if e.Reason != "" {
		return e.Reason
	}
	return "没有可用账号（全部忙 / 冷却中 / 未配置 image_id）"
}

// Acquire 在给定地区里挑一个可用账号并占用。
//
// 排序：剩余每日额度多的优先（额度未知的排在有数据之后），同档按加入时间。
func (p *Pool) Acquire(regions []upstream.Region, exclude map[string]bool) (*Lease, error) {
	allowed := map[upstream.Region]bool{}
	for _, r := range regions {
		allowed[r] = true
	}
	now := time.Now()

	type cand struct {
		acct account.Account
		left int64
	}
	var cands []cand
	reason := ""

	for _, a := range p.store.List() {
		r, err := upstream.ParseRegion(a.Region)
		if err != nil || !allowed[r] {
			continue
		}
		if exclude[a.ID] {
			continue
		}
		if a.Disabled {
			reason = "账号已停用"
			continue
		}
		if !a.Ready() {
			reason = "账号缺少 image_id（在面板补填，或先在官网跑一个任务让网关自动发现）"
			continue
		}
		p.mu.Lock()
		busy := p.busy[a.ID] > 0
		c, cooling := p.cool[a.ID]
		p.mu.Unlock()
		if busy {
			reason = "账号都在跑任务"
			continue
		}
		if cooling && now.Before(c.until) {
			reason = fmt.Sprintf("账号冷却中（%s，剩 %s）", c.kind, c.until.Sub(now).Round(time.Second))
			continue
		}
		if a.Wallet != nil && a.Wallet.DailyTokenLimit > 0 && a.Wallet.DailyTokenBalance <= 0 {
			reason = "账号今日额度已用尽"
			continue
		}
		var left int64 = -1
		if a.Wallet != nil {
			left = a.Wallet.DailyTokenBalance
		}
		cands = append(cands, cand{acct: a, left: left})
	}
	if len(cands) == 0 {
		return nil, &ErrNoAccount{Regions: regions, Reason: reason}
	}
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].left != cands[j].left {
			return cands[i].left > cands[j].left
		}
		return cands[i].acct.AddedAt < cands[j].acct.AddedAt
	})
	chosen := cands[0].acct
	p.mu.Lock()
	p.busy[chosen.ID]++
	p.mu.Unlock()
	r, _ := upstream.ParseRegion(chosen.Region)
	return &Lease{Account: chosen, Region: r, p: p}, nil
}

// Fail 按错误种类给账号打冷却 / 停用（调用方在 Release 前后都可调）。
func (p *Pool) Fail(a account.Account, err error) {
	ue, ok := err.(*upstream.Error)
	if !ok {
		p.bumpErr(a.ID)
		return
	}
	switch ue.Kind {
	case upstream.KindAuth:
		_ = p.store.Update(a.ID, func(x *account.Account) { x.Disabled = true })
	case upstream.KindBusy, upstream.KindRate:
		p.setCool(a.ID, "短冷却", p.cfg.SoftRate())
	case upstream.KindQuota:
		p.setCool(a.ID, "额度不足", p.cfg.HardQuota())
	case upstream.KindUpstream, upstream.KindNotFound:
		p.bumpErr(a.ID)
	}
}

// OK 成功一次：清掉错误计数。
func (p *Pool) OK(id string) {
	p.mu.Lock()
	delete(p.errs, id)
	p.mu.Unlock()
}

func (p *Pool) bumpErr(id string) {
	p.mu.Lock()
	p.errs[id]++
	n := p.errs[id]
	p.mu.Unlock()
	if n >= max(p.cfg.Cooldown.ErrThreshold, 1) {
		p.setCool(id, "连续错误", p.cfg.ErrCooldown())
		p.mu.Lock()
		delete(p.errs, id)
		p.mu.Unlock()
	}
}

func (p *Pool) setCool(id, kind string, d time.Duration) {
	p.mu.Lock()
	p.cool[id] = cool{until: time.Now().Add(d), kind: kind}
	p.mu.Unlock()
}

// Snapshot 面板展示用：每个账号的忙 / 冷却状态。
type Status struct {
	Busy     bool   `json:"busy"`
	Cooldown string `json:"cooldown,omitempty"` // 剩余时长，空=不在冷却
	CoolKind string `json:"cool_kind,omitempty"`
}

func (p *Pool) Snapshot() map[string]Status {
	now := time.Now()
	p.mu.Lock()
	defer p.mu.Unlock()
	out := map[string]Status{}
	for id, n := range p.busy {
		out[id] = Status{Busy: n > 0}
	}
	for id, c := range p.cool {
		if now.Before(c.until) {
			st := out[id]
			st.Cooldown = c.until.Sub(now).Round(time.Second).String()
			st.CoolKind = c.kind
			out[id] = st
		}
	}
	return out
}

// ClearCooldown 面板「解除冷却」用。
func (p *Pool) ClearCooldown(id string) {
	p.mu.Lock()
	delete(p.cool, id)
	delete(p.errs, id)
	p.mu.Unlock()
}
