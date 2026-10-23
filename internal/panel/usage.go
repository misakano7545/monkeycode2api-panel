// usage.go 用量与逐请求记录（只读展示，数据源是 usage.Ledger）。
package panel

import (
	"net/http"
	"strconv"
)

// usageSummary 按天聚合（days 默认 7，上限 90）。
func (p *Panel) usageSummary(w http.ResponseWriter, r *http.Request) {
	days := 7
	if v := r.URL.Query().Get("days"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 90 {
			days = n
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"days": p.cfg.Ledger.ByDay(days)})
}

// requestLogs 最近请求（limit 默认 60，上限 200）。
func (p *Panel) requestLogs(w http.ResponseWriter, r *http.Request) {
	limit := 60
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"records": p.cfg.Ledger.Recent(limit)})
}
