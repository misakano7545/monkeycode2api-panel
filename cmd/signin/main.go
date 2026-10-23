// signin 批量「额度到账检查 + 每日签到」。
//
// 用法：./signin.sh
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/misakano7545/monkeycode2api-panel/internal/account"
	"github.com/misakano7545/monkeycode2api-panel/internal/config"
	"github.com/misakano7545/monkeycode2api-panel/internal/upstream"
)

func main() {
	cfgPath := flag.String("config", "config.json", "配置文件路径")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("配置错误：%v", err)
	}
	store, err := account.Open(cfg.AuthDir)
	if err != nil {
		log.Fatalf("打开账号目录失败：%v", err)
	}
	list := store.List()
	if len(list) == 0 {
		log.Fatal("还没有账号，先跑 ./login.sh")
	}

	for _, a := range list {
		r, _ := upstream.ParseRegion(a.Region)
		client := upstream.New(r, cfg.Upstream.UserAgent, cfg.APITimeout())
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)

		name := a.Name
		if name == "" {
			name = a.ID
		}
		w, err := client.Wallet(ctx, a.CookieHeader())
		if err != nil {
			fmt.Printf("%-24s ❌ 额度查询失败：%v\n", name, err)
			cancel()
			continue
		}
		status := "未到账"
		if w.DailyTokenLimit > 0 {
			status = "已到账"
		}
		fmt.Printf("%-24s 额度 %s/%s（%s）", name,
			human(w.DailyTokenBalance), human(w.DailyTokenLimit), status)

		checked, err := client.CheckinStatus(ctx, a.CookieHeader())
		if err != nil {
			fmt.Printf("，签到状态查询失败：%v\n", err)
			cancel()
			continue
		}
		if checked {
			fmt.Println("，今天已签到")
			_ = store.Update(a.ID, func(x *account.Account) { x.Wallet = w; x.LastCheckin = time.Now().Format("2006-01-02") })
			cancel()
			continue
		}
		if err := client.Checkin(ctx, a.CookieHeader()); err != nil {
			fmt.Printf("，签到失败：%v\n", err)
			cancel()
			continue
		}
		fmt.Println("，签到成功（+100 积分）")
		_ = store.Update(a.ID, func(x *account.Account) { x.Wallet = w; x.LastCheckin = time.Now().Format("2006-01-02") })
		cancel()
	}
}

func human(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	}
	return fmt.Sprint(n)
}
