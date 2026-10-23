// Package scheduler 每日额度到账检查与自动签到。
package scheduler

import (
	"context"
	"log"
	"time"

	"github.com/misakano7545/monkeycode2api-panel/internal/account"
	"github.com/misakano7545/monkeycode2api-panel/internal/config"
	"github.com/misakano7545/monkeycode2api-panel/internal/upstream"
)

type Scheduler struct {
	cfg   *config.Config
	store *account.Store
}

func New(cfg *config.Config, store *account.Store) *Scheduler {
	return &Scheduler{cfg: cfg, store: store}
}

// Run 每分钟醒一次，按配置的时刻干活（进程内去重，重启不会重复签到）。
func (s *Scheduler) Run(ctx context.Context) {
	lastQuota := ""
	lastCheckin := ""
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	// 启动时先拉一次额度，面板立刻有数据。
	s.refreshWallets(ctx)
	lastQuota = time.Now().Format("2006-01-02 15")

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			slot := now.Format("2006-01-02 15")
			if now.Hour() == s.cfg.Schedule.QuotaCheckHour && slot != lastQuota {
				s.refreshWallets(ctx)
				lastQuota = slot
			} else if s.cfg.Schedule.QuotaRefreshMin > 0 && now.Minute()%s.cfg.Schedule.QuotaRefreshMin == 0 {
				s.refreshWallets(ctx)
			}
			if s.cfg.Schedule.CheckinEnabled && containsHour(s.cfg.Schedule.CheckinHours, now.Hour()) && now.Minute() == 5 {
				day := now.Format("2006-01-02")
				if day != lastCheckin {
					s.checkinAll(ctx)
					lastCheckin = day
				}
			}
		}
	}
}

func containsHour(hours []int, h int) bool {
	for _, x := range hours {
		if x == h {
			return true
		}
	}
	return false
}

// refreshWallets 逐个账号拉钱包，写回快照。
func (s *Scheduler) refreshWallets(ctx context.Context) {
	for _, a := range s.store.List() {
		if a.Disabled || a.Session == "" {
			continue
		}
		r, err := upstream.ParseRegion(a.Region)
		if err != nil {
			continue
		}
		client := upstream.New(r, s.cfg.Upstream.UserAgent, s.cfg.APITimeout())
		w, err := client.Wallet(ctx, a.CookieHeader())
		if err != nil {
			if upstream.Is(err, upstream.KindAuth) {
				log.Printf("[scheduler] %s 登录态失效，已停用：%v", a.ID, err)
				_ = s.store.Update(a.ID, func(x *account.Account) { x.Disabled = true })
				continue
			}
			log.Printf("[scheduler] %s 拉额度失败：%v", a.ID, err)
			continue
		}
		_ = s.store.Update(a.ID, func(x *account.Account) { x.Wallet = w })
	}
}

// checkinAll 尽力签到（已签自动跳过，不会重复拿积分）。
func (s *Scheduler) checkinAll(ctx context.Context) {
	for _, a := range s.store.List() {
		if a.Disabled || a.Session == "" {
			continue
		}
		r, err := upstream.ParseRegion(a.Region)
		if err != nil {
			continue
		}
		client := upstream.New(r, s.cfg.Upstream.UserAgent, s.cfg.APITimeout())
		checked, err := client.CheckinStatus(ctx, a.CookieHeader())
		if err != nil {
			log.Printf("[scheduler] %s 查签到状态失败：%v", a.ID, err)
			continue
		}
		if checked {
			_ = s.store.Update(a.ID, func(x *account.Account) { x.LastCheckin = time.Now().Format("2006-01-02") })
			continue
		}
		if err := client.Checkin(ctx, a.CookieHeader()); err != nil {
			log.Printf("[scheduler] %s 签到失败：%v", a.ID, err)
			continue
		}
		log.Printf("[scheduler] %s 签到成功", a.ID)
		_ = s.store.Update(a.ID, func(x *account.Account) { x.LastCheckin = time.Now().Format("2006-01-02") })
	}
}
