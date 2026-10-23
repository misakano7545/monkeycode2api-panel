package panel

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/misakano7545/monkeycode2api-panel/internal/account"
	"github.com/misakano7545/monkeycode2api-panel/internal/config"
	"github.com/misakano7545/monkeycode2api-panel/internal/pool"
	"github.com/misakano7545/monkeycode2api-panel/internal/upstream"
	"github.com/misakano7545/monkeycode2api-panel/internal/usage"
)

func newPanel(t *testing.T, apiKey string) *Panel {
	t.Helper()
	dir := t.TempDir()
	store, err := account.Open(dir + "/auths")
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := usage.Open(dir + "/data")
	if err != nil {
		t.Fatal(err)
	}
	return New(Config{
		Store: store, Pool: pool.New(store, config.Default()),
		Catalog: upstream.NewCatalog(dir + "/data"), Ledger: ledger,
		Logs: NewRing(10), APIKey: apiKey, Version: "test",
	})
}

// 面板页面必须匿名可加载（HTML 里没有秘密），且脚本走同源外链 + 内容指纹。
func TestIndexServesEmbeddedAssets(t *testing.T) {
	p := newPanel(t, "sk-test")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, httptest.NewRequest("GET", "/panel/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{`src="theme.js?v=`, `src="app.js?v=`, "MonkeyCode2API"} {
		if !strings.Contains(body, want) {
			t.Fatalf("页面缺少 %q", want)
		}
	}
	// 指纹必须已注入（否则会退回无缓存兜底，失去跨版本隔离）。
	if strings.Contains(body, `src="app.js"`) {
		t.Fatal("app.js 未带内容指纹")
	}
	// 页面不得内联脚本/事件处理器（CSP script-src 'self' 会直接拦掉）。
	if strings.Contains(body, "onclick=") || strings.Contains(body, "<script>") {
		t.Fatal("页面含内联脚本或内联事件处理器，会被 CSP 拦掉")
	}
	if got := rec.Header().Get("Content-Security-Policy"); !strings.Contains(got, "script-src 'self'") {
		t.Fatalf("CSP = %q", got)
	}
	if rec.Header().Get("X-Frame-Options") != "DENY" {
		t.Fatal("缺少防点击劫持头")
	}
}

// API 与网关同一把 key：无 Bearer 一律 401，错误体是 {ok:false,error}。
func TestAPIRequiresBearer(t *testing.T) {
	p := newPanel(t, "sk-test")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, httptest.NewRequest("GET", "/panel/api/overview", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("无密钥 status = %d, want 401", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"ok":false`) {
		t.Fatalf("错误体 = %s", rec.Body.String())
	}
	req := httptest.NewRequest("GET", "/panel/api/overview", nil)
	req.Header.Set("Authorization", "Bearer sk-test")
	rec = httptest.NewRecorder()
	p.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("带密钥 status = %d, body=%s", rec.Code, rec.Body.String())
	}
	// 空 key = 不鉴权（本机/私网模式）。
	p2 := newPanel(t, "")
	rec = httptest.NewRecorder()
	p2.ServeHTTP(rec, httptest.NewRequest("GET", "/panel/api/overview", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("未启用鉴权时 status = %d, want 200", rec.Code)
	}
}

// 密码登录入口：缺字段/坏地区必须在打上游之前就拒掉（不打网络、不耗验证码）。
func TestLoginValidatesBeforeUpstream(t *testing.T) {
	p := newPanel(t, "sk-test")
	for _, tc := range []struct{ name, body, want string }{
		{"缺密码", `{"region":"cn","email":"a@b.c"}`, "请填账号和密码"},
		{"坏地区", `{"region":"mars","email":"a@b.c","password":"x"}`, "未知地区"},
		{"坏 JSON", `{`, "不是合法 JSON"},
	} {
		req := httptest.NewRequest("POST", "/panel/api/accounts/login", strings.NewReader(tc.body))
		req.Header.Set("Authorization", "Bearer sk-test")
		rec := httptest.NewRecorder()
		p.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), tc.want) {
			t.Fatalf("%s: status=%d body=%s", tc.name, rec.Code, rec.Body.String())
		}
	}
}

// 日志频道归类：对话请求 / 任务动作 / 系统，各走各的容量。
func TestRingClassify(t *testing.T) {
	r := NewRing(2)
	r.Add(classify("POST /v1/chat/completions 120ms"), ChChat)
	r.Add(classify("[scheduler] a 签到成功"), ChTask)
	r.Add(classify("monkeycode2api-panel 监听 :7887"), ChSys)
	r.Add(classify("GET /v2/chat/busy 3ms"), ChChat)
	r.Add(classify("[scheduler] b 签到失败"), ChTask)
	r.Add(classify("HTTP 服务退出"), ChSys)

	got := map[string]int{}
	for _, e := range r.Snapshot() {
		got[e.Ch]++
	}
	// 每频道容量 2：三个频道各写 2 条，正好都留住。
	if got[ChChat] != 2 || got[ChTask] != 2 || got[ChSys] != 2 {
		t.Fatalf("频道分布 = %v", got)
	}
	if n := len(r.Snapshot()); n != 6 {
		t.Fatalf("快照条数 = %d, want 6", n)
	}
}
