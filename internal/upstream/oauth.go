// oauth.go 官方登录（百智云 / Google / GitHub）的授权页地址构建。
//
// 只能做「前半段」：服务端把授权 URL 生成好，用户在自己浏览器里登录。
// 后半段（自动取回凭证）做不到，原因是上游结构决定的，不是没写：
//
//	redirect_uri 写死成 https://<站点>/api/v1/users/{baizhi|oauth/<p>}/callback，
//	  改不了（实测 /users/login?redirect=… 的 302 里 redirect_uri 不变）；
//	回调是服务端消费的：GET 那个 callback 会立刻用掉 code 并 Set-Cookie 到
//	  用户浏览器，302 去 /console/tasks，地址栏里不会留下 code；
//	没有设备码 / 轮询端点。WorkBuddy 能轮询是因为腾讯有
//	  /v2/plugin/auth/token?state=…（设备流），MonkeyCode 没有对应物。
//
// 所以 OAuth 的凭证只能由用户在浏览器里「落地」，我们只负责把 URL 拼对、
// 把哪种方式在哪个站点可用说清楚，剩下的粘贴交给 NormalizeSession。
package upstream

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// 登录方式（面板按钮的取值）。
const (
	MethodBaizhi = "baizhi"
	MethodGoogle = "google"
	MethodGithub = "github"
)

// EpLogin /api/v1/users/login：302 到百智云授权页。
const EpLogin = "/api/v1/users/login"

// LoginURL 返回指定登录方式的授权页地址。
//
//	baizhi  → GET /users/login 的 302 Location（INTL 给的是相对地址，这里补成绝对）
//	google / github → GET /users/oauth/<p>/login 的 data.auth_url
//
// 站点不支持该方式时（国内版 Google/GitHub 实测 10626「暂未启用」）如实返回错误。
func (c *Client) LoginURL(ctx context.Context, method, redirectURL string) (string, error) {
	if redirectURL == "" {
		redirectURL = "/console/tasks"
	}
	switch method {
	case MethodBaizhi:
		return c.baizhiLoginURL(ctx, redirectURL)
	case MethodGoogle, MethodGithub:
		return c.oauthLoginURL(ctx, method, redirectURL)
	}
	return "", fmt.Errorf("未知登录方式 %q（只支持 baizhi / google / github）", method)
}

func (c *Client) baizhiLoginURL(ctx context.Context, redirectURL string) (string, error) {
	path := EpLogin + "?redirect=" + url.QueryEscape(redirectURL) + "&inviter_id="
	req, err := c.newReq(ctx, "", http.MethodGet, path, nil)
	if err != nil {
		return "", err
	}
	// 不跟 302：Location 就是授权页。
	hc := *c.HTTP
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := hc.Do(req)
	if err != nil {
		return "", &Error{Kind: KindUpstream, Msg: "上游请求失败：" + err.Error()}
	}
	defer resp.Body.Close()
	loc := resp.Header.Get("Location")
	if resp.StatusCode/100 != 3 || loc == "" {
		return "", &Error{Kind: KindUpstream, Status: resp.StatusCode, Msg: "百智云登录没有返回授权地址（HTTP " + fmt.Sprint(resp.StatusCode) + "）"}
	}
	base, err := url.Parse(c.Region.Origin())
	if err != nil {
		return "", err
	}
	ref, err := url.Parse(loc)
	if err != nil {
		return "", err
	}
	// INTL 实测给相对路径 /oauth/authorize?client_id=（client_id 为空，百智云只在国内版配好）。
	return base.ResolveReference(ref).String(), nil
}

func (c *Client) oauthLoginURL(ctx context.Context, method, redirectURL string) (string, error) {
	var out struct {
		AuthURL string `json:"auth_url"`
	}
	path := "/api/v1/users/oauth/" + method + "/login?redirect_url=" + url.QueryEscape(redirectURL)
	if err := c.doJSON(ctx, "", http.MethodGet, path, nil, &out); err != nil {
		return "", err
	}
	if strings.TrimSpace(out.AuthURL) == "" {
		return "", &Error{Kind: KindUpstream, Msg: method + " 登录没有返回 auth_url"}
	}
	return out.AuthURL, nil
}
