package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/misakano7545/monkeycode2api-panel/internal/account"
	"github.com/misakano7545/monkeycode2api-panel/internal/config"
	"github.com/misakano7545/monkeycode2api-panel/internal/panel"
	"github.com/misakano7545/monkeycode2api-panel/internal/pool"
	"github.com/misakano7545/monkeycode2api-panel/internal/scheduler"
	"github.com/misakano7545/monkeycode2api-panel/internal/server"
	"github.com/misakano7545/monkeycode2api-panel/internal/upstream"
	"github.com/misakano7545/monkeycode2api-panel/internal/usage"
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
		log.Fatalf("打开账号目录 %s 失败：%v", cfg.AuthDir, err)
	}
	ledger, err := usage.Open(cfg.DataDir)
	if err != nil {
		log.Fatalf("打开数据目录 %s 失败：%v", cfg.DataDir, err)
	}
	catalog := upstream.NewCatalog(cfg.DataDir)
	p := pool.New(store, cfg)
	srv := server.New(cfg, store, catalog, p, ledger)

	if cfg.Panel.Enabled {
		srv.Mount("/panel/", panel.Handler(panel.Deps{
			Cfg: cfg, Store: store, Catalog: catalog, Pool: p, Ledger: ledger,
			RefreshCatalog: srv.RefreshCatalog,
		}))
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go scheduler.New(cfg, store).Run(ctx)
	go func() {
		// 冷启动拉一次模型目录（没有账号就跳过，等面板添加后再拉）。
		time.Sleep(2 * time.Second)
		srv.RefreshCatalog(ctx, false)
	}()

	httpSrv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           logRequests(srv.Handler()),
		ReadHeaderTimeout: 20 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shutCtx)
	}()

	log.Printf("monkeycode2api-panel 监听 %s（账号 %d 个，面板 %v）", cfg.Listen, store.Len(), cfg.Panel.Enabled)
	if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("HTTP 服务退出：%v", err)
	}
}

// logRequests 只记一行摘要（不打正文、不打 key）。
func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		if r.URL.Path == "/healthz" || r.URL.Path == "/panel/" {
			return
		}
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
	})
}
