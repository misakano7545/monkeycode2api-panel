// Package server 三套协议的入口：OpenAI chat、Claude messages、Codex responses。
//
// 三者共用一条上游链路（建任务 → 任务流），差别只在「请求怎么读、事件怎么写成
// 客户端协议」。因此 run.go 是唯一的执行层，*_adapter 只做编解码。
package server

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/misakano7545/monkeycode2api-panel/internal/account"
	"github.com/misakano7545/monkeycode2api-panel/internal/config"
	"github.com/misakano7545/monkeycode2api-panel/internal/pool"
	"github.com/misakano7545/monkeycode2api-panel/internal/upstream"
	"github.com/misakano7545/monkeycode2api-panel/internal/usage"
)

type Server struct {
	Cfg     *config.Config
	Store   *account.Store
	Catalog *upstream.Catalog
	Pool    *pool.Pool
	Ledger  *usage.Ledger

	StartAt time.Time
	mux     *http.ServeMux

	refreshMu sync.Mutex
}

func New(cfg *config.Config, store *account.Store, catalog *upstream.Catalog, p *pool.Pool, ledger *usage.Ledger) *Server {
	s := &Server{Cfg: cfg, Store: store, Catalog: catalog, Pool: p, Ledger: ledger,
		StartAt: time.Now(), mux: http.NewServeMux()}

	s.mux.HandleFunc("POST /v1/chat/completions", s.withAuth(s.chatCompletions))
	s.mux.HandleFunc("POST /v1/messages", s.withAnthropicAuth(s.messages))
	s.mux.HandleFunc("POST /v1/messages/count_tokens", s.withAnthropicAuth(s.countTokens))
	s.mux.HandleFunc("POST /messages", s.withAnthropicAuth(s.messages))
	s.mux.HandleFunc("POST /v1/responses", s.withAuth(s.responses))
	s.mux.HandleFunc("GET /v1/models", s.withAuth(s.models))
	s.mux.HandleFunc("GET /v1/models/{id...}", s.withAuth(s.models))
	s.mux.HandleFunc("GET /status", s.withAuth(s.status))
	s.mux.HandleFunc("GET /healthz", s.healthz)
	return s
}

func (s *Server) Handler() http.Handler { return s.mux }

// Mount 挂一个额外处理器（面板用）。
func (s *Server) Mount(pattern string, h http.Handler) { s.mux.Handle(pattern, h) }

// RefreshCatalog 拉两个站点的模型目录（面板与冷启动共用）。
func (s *Server) RefreshCatalog(ctx context.Context, force bool) map[string]string {
	return s.refreshCatalog(ctx, force)
}

// Client 按地区造一个上游客户端。
func (s *Server) Client(r upstream.Region) *upstream.Client {
	return upstream.New(r, s.Cfg.Upstream.UserAgent, s.Cfg.APITimeout())
}

// ─── 鉴权 ────────────────────────────────────────────────────

func (s *Server) keyOK(got string) bool {
	return got != "" && subtle.ConstantTimeCompare([]byte(got), []byte(s.Cfg.APIKey)) == 1
}

func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if rest, ok := strings.CutPrefix(h, "Bearer "); ok {
		return strings.TrimSpace(rest)
	}
	return ""
}

func (s *Server) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := bearer(r)
		if key == "" {
			key = r.Header.Get("x-api-key")
		}
		if !s.keyOK(key) {
			writeOpenAIError(w, http.StatusUnauthorized, "invalid_api_key", "API key 无效")
			return
		}
		next(w, r)
	}
}

// withAnthropicAuth Claude 客户端用 x-api-key，也接受 Bearer。
func (s *Server) withAnthropicAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("x-api-key")
		if key == "" {
			key = bearer(r)
		}
		if !s.keyOK(key) {
			writeAnthropicError(w, http.StatusUnauthorized, "authentication_error", "API key 无效")
			return
		}
		next(w, r)
	}
}

// ─── 基础端点 ────────────────────────────────────────────────

func (s *Server) healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "accounts": s.Store.Len()})
}

func (s *Server) models(w http.ResponseWriter, _ *http.Request) {
	items := s.Catalog.List()
	data := make([]map[string]any, 0, len(items))
	seen := map[string]bool{}
	for _, it := range items {
		// 两地同名模型只报一次（region 在 status/panel 里看，客户端只认 id）
		if seen[it.ID] {
			continue
		}
		seen[it.ID] = true
		owner := it.Provider
		if owner == "" {
			owner = "monkeycode"
		}
		data = append(data, map[string]any{
			"id":       it.ID,
			"object":   "model",
			"created":  s.Catalog.FetchedAt(it.Region),
			"owned_by": owner,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": data})
}

func (s *Server) status(w http.ResponseWriter, _ *http.Request) {
	accounts := s.Store.List()
	snap := s.Pool.Snapshot()
	out := make([]map[string]any, 0, len(accounts))
	for _, a := range accounts {
		st := snap[a.ID]
		row := map[string]any{
			"id": a.ID, "region": a.Region, "name": a.Name, "user_id": a.UserID,
			"image_id": a.ImageID != "", "disabled": a.Disabled,
			"busy": st.Busy, "cooldown": st.Cooldown, "cool_kind": st.CoolKind,
			"expires_at": a.ExpiresAt, "expiring": a.Expiring(),
			"last_checkin": a.LastCheckin,
		}
		if a.Wallet != nil {
			row["wallet"] = a.Wallet
		}
		out = append(out, row)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"uptime_seconds": int(time.Since(s.StartAt).Seconds()),
		"accounts":       out,
		"models":         len(s.Catalog.List()),
	})
}

// ─── 目录刷新（懒加载 / 面板手动）────────────────────────────

// refreshCatalog 用每个地区的一个可用账号拉一次目录。
func (s *Server) refreshCatalog(ctx context.Context, force bool) map[string]string {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()

	const ttl = 10 * time.Minute
	result := map[string]string{}
	for _, r := range []upstream.Region{upstream.CN, upstream.INTL} {
		if !force && s.Catalog.Fresh(r, ttl) {
			continue
		}
		session, ok := s.sessionFor(r)
		if !ok {
			result[string(r)] = "没有该地区的可用账号"
			continue
		}
		n, err := s.Catalog.Refresh(ctx, s.Client(r), session)
		if err != nil {
			result[string(r)] = err.Error()
			continue
		}
		result[string(r)] = fmt.Sprintf("已刷新 %d 个模型", n)
	}
	return result
}

func (s *Server) sessionFor(r upstream.Region) (string, bool) {
	for _, a := range s.Store.List() {
		if a.Region == string(r) && !a.Disabled && a.Session != "" {
			return a.CookieHeader(), true
		}
	}
	return "", false
}

// ─── 通用工具 ────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeOpenAIError(w http.ResponseWriter, code int, typ, msg string) {
	writeJSON(w, code, map[string]any{
		"error": map[string]any{"message": msg, "type": typ, "code": typ},
	})
}

func writeAnthropicError(w http.ResponseWriter, code int, typ, msg string) {
	writeJSON(w, code, map[string]any{
		"type":  "error",
		"error": map[string]any{"type": typ, "message": msg},
	})
}

// failOpenAI 把上游错误翻成 OpenAI 风格的 HTTP 错误。
func failOpenAI(w http.ResponseWriter, err error) {
	code, typ, msg := classify(err)
	writeOpenAIError(w, code, typ, msg)
}

func failAnthropic(w http.ResponseWriter, err error) {
	code, typ, msg := classify(err)
	if typ == "invalid_request_error" {
		typ = "invalid_request_error"
	}
	writeAnthropicError(w, code, typ, msg)
}

// classify 统一错误 → (HTTP 码, 类型, 文案)。
func classify(err error) (int, string, string) {
	var noAcct *pool.ErrNoAccount
	if errors.As(err, &noAcct) {
		return http.StatusServiceUnavailable, "no_available_account", noAcct.Error()
	}
	var ue *upstream.Error
	if errors.As(err, &ue) {
		switch ue.Kind {
		case upstream.KindAuth:
			return http.StatusUnauthorized, "authentication_error", ue.Msg
		case upstream.KindBusy:
			return http.StatusServiceUnavailable, "upstream_busy", ue.Msg
		case upstream.KindQuota:
			return http.StatusTooManyRequests, "insufficient_quota", ue.Msg
		case upstream.KindRate:
			return http.StatusTooManyRequests, "rate_limit_error", ue.Msg
		case upstream.KindNotFound:
			return http.StatusNotFound, "not_found", ue.Msg
		default:
			return http.StatusBadGateway, "upstream_error", ue.Msg
		}
	}
	var be *badRequest
	if errors.As(err, &be) {
		return http.StatusBadRequest, "invalid_request_error", be.msg
	}
	if errors.Is(err, context.Canceled) {
		return 499, "canceled", "客户端已断开"
	}
	return http.StatusInternalServerError, "internal_error", err.Error()
}

// badRequest 客户端参数错误。
type badRequest struct{ msg string }

func (e *badRequest) Error() string { return e.msg }

func badf(format string, args ...any) error { return &badRequest{msg: fmt.Sprintf(format, args...)} }

func logf(format string, args ...any) { log.Printf(format, args...) }
