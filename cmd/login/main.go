// login 粘贴式登录：校验 session → 自动发现 image_id → 落盘 auths/。
//
// 用法：./login.sh 或 go run ./cmd/login -region cn
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/misakano7545/monkeycode2api-panel/internal/account"
	"github.com/misakano7545/monkeycode2api-panel/internal/config"
	"github.com/misakano7545/monkeycode2api-panel/internal/upstream"
)

func main() {
	cfgPath := flag.String("config", "config.json", "配置文件路径")
	regionFlag := flag.String("region", "cn", "站点：cn（.com）/ intl（.net）")
	sessionFlag := flag.String("session", "", "session cookie（不给则交互粘贴）")
	imageFlag := flag.String("image", "", "image_id（可空，自动从历史任务发现）")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("配置错误：%v", err)
	}
	region, err := upstream.ParseRegion(*regionFlag)
	if err != nil {
		log.Fatalf("%v", err)
	}

	raw := *sessionFlag
	if raw == "" {
		fmt.Printf("站点：%s（%s）\n", region.Label(), region.Origin())
		fmt.Println("浏览器登录后，DevTools → Application → Cookies 复制 monkeycode_ai_session 的值：")
		fmt.Print("session> ")
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		raw = strings.TrimSpace(line)
	}
	cookieName, session := account.NormalizeSession(raw)
	if session == "" {
		log.Fatal("没有拿到 session")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	client := upstream.New(region, cfg.Upstream.UserAgent, cfg.APITimeout())

	cookie := cookieName + "=" + session
	user, err := client.Status(ctx, cookie)
	if err != nil {
		log.Fatalf("登录态校验失败：%v（确认 session 有效、站点选对）", err)
	}
	imageID := strings.TrimSpace(*imageFlag)
	if imageID == "" {
		imageID = client.DiscoverImageID(ctx, cookie)
	}
	wallet, _ := client.Wallet(ctx, cookie)

	store, err := account.Open(cfg.AuthDir)
	if err != nil {
		log.Fatalf("打开账号目录失败：%v", err)
	}
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
	if err := store.Put(a); err != nil {
		log.Fatalf("保存账号失败：%v", err)
	}

	fmt.Printf("✅ 已保存 %s\n", a.ID)
	if a.Name != "" {
		fmt.Printf("   用户：%s（%s）\n", a.Name, a.Email)
	}
	if wallet != nil {
		fmt.Printf("   额度：%d / %d，积分 %d\n", wallet.DailyTokenBalance, wallet.DailyTokenLimit, wallet.Balance)
	}
	if imageID == "" {
		fmt.Println("⚠️  没发现 image_id：先在官网跑一个任务，或用 -image 手动填入镜像 UUID，否则无法建任务")
	} else {
		fmt.Printf("   image_id：%s\n", imageID)
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
