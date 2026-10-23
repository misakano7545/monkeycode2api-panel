// models.go 模型目录：读 Catalog 快照 + 手动刷新（按地区拉上游可用模型）。
package panel

import (
	"context"
	"net/http"
	"time"

	"github.com/misakano7545/monkeycode2api-panel/internal/upstream"
)

// models 模型目录快照（两个地区合并，带各自拉取时间）。
func (p *Panel) models(w http.ResponseWriter, r *http.Request) {
	items := p.cfg.Catalog.List()
	out := map[string]any{
		"items": items,
		"cn":    regionMeta(p.cfg.Catalog, upstream.CN),
		"intl":  regionMeta(p.cfg.Catalog, upstream.INTL),
	}
	writeJSON(w, http.StatusOK, out)
}

func regionMeta(c *upstream.Catalog, r upstream.Region) map[string]any {
	n := 0
	for _, it := range c.List() {
		if it.Region == r {
			n++
		}
	}
	return map[string]any{"count": n, "fetched_at": c.FetchedAt(r)}
}

// modelsRefresh 按地区拉一次上游模型目录（取该地区任一可用账号的 cookie）。
//
// 没有可用账号的地区会跳过并在 errors 里说明：模型目录是上游按账号返回的，
// 没有 cookie 拿不到（这里不伪造空目录当成"该地区没有模型"）。
func (p *Panel) modelsRefresh(w http.ResponseWriter, r *http.Request) {
	if p.cfg.Client == nil {
		writeErr(w, http.StatusNotImplemented, "上游客户端未装配")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()

	counts := map[upstream.Region]int{}
	var errs []string
	for _, region := range []upstream.Region{upstream.CN, upstream.INTL} {
		cookie := p.anyCookie(region)
		if cookie == "" {
			errs = append(errs, region.Label()+"：没有可用账号")
			continue
		}
		n, err := p.cfg.Catalog.Refresh(ctx, p.client(region), cookie)
		if err != nil {
			errs = append(errs, region.Label()+"："+err.Error())
			continue
		}
		counts[region] = n
	}
	okJSON(w, "模型清单已刷新", map[string]any{
		"cn": counts[upstream.CN], "intl": counts[upstream.INTL], "errors": errs,
	})
}

// anyCookie 取该地区第一个可用账号的 Cookie 头（不占用租约：只读接口）。
func (p *Panel) anyCookie(region upstream.Region) string {
	for _, a := range p.cfg.Store.List() {
		if a.Region == string(region) && !a.Disabled && a.Session != "" {
			return a.CookieHeader()
		}
	}
	return ""
}
