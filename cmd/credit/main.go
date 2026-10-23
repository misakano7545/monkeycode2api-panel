// credit 钱包 / 额度日报。
//
// 用法：./credit.sh 或 go run ./cmd/credit -json
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"text/tabwriter"
	"time"

	"github.com/misakano7545/monkeycode2api-panel/internal/account"
	"github.com/misakano7545/monkeycode2api-panel/internal/config"
	"github.com/misakano7545/monkeycode2api-panel/internal/upstream"
)

func main() {
	cfgPath := flag.String("config", "config.json", "配置文件路径")
	asJSON := flag.Bool("json", false, "输出原始 JSON")
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

	type row struct {
		ID      string `json:"id"`
		Region  string `json:"region"`
		Name    string `json:"name"`
		Balance int64  `json:"balance"`
		Used    int64  `json:"daily_used"`
		Limit   int64  `json:"daily_limit"`
		Left    int64  `json:"daily_left"`
		Error   string `json:"error,omitempty"`
	}
	rows := make([]row, 0, len(list))
	for _, a := range list {
		r, _ := upstream.ParseRegion(a.Region)
		client := upstream.New(r, cfg.Upstream.UserAgent, cfg.APITimeout())
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		w, err := client.Wallet(ctx, a.CookieHeader())
		cancel()
		out := row{ID: a.ID, Region: a.Region, Name: a.Name}
		if err != nil {
			out.Error = err.Error()
		} else {
			out.Balance = w.Balance
			out.Limit = w.DailyTokenLimit
			out.Left = w.DailyTokenBalance
			out.Used = w.DailyTokenLimit - w.DailyTokenBalance
			_ = store.Update(a.ID, func(x *account.Account) { x.Wallet = w })
		}
		rows = append(rows, out)
	}

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(rows)
		return
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "账号\t地区\t今日已用\t今日上限\t剩余\t积分\t状态")
	for _, r := range rows {
		status := "到账"
		if r.Error != "" {
			status = "❌ " + r.Error
		} else if r.Limit == 0 {
			status = "未到账"
		}
		name := r.Name
		if name == "" {
			name = r.ID
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%d\t%s\n", name, r.Region,
			human(r.Used), human(r.Limit), human(r.Left), r.Balance, status)
	}
	_ = tw.Flush()
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
