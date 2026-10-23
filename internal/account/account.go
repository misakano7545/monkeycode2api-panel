// Package account 管理 auths/ 下的 MonkeyCode 账号（粘贴式 session，无续期）。
//
// 上游只有一个登录入口能自动化：把浏览器里的 monkeycode_ai_session cookie
// 粘进来（密码登录要 go-cap 验证码、OAuth 要短信）。session 30 天硬限制，
// 过期只能重新粘贴，所以这里没有 refresh。
package account

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Wallet 是 GET /api/v1/users/wallet 的快照（额度到账检查与选号都看它）。
type Wallet struct {
	Balance           int64  `json:"balance"`             // 积分
	DailyTokenBalance int64  `json:"daily_token_balance"` // 今日剩余 token
	DailyTokenLimit   int64  `json:"daily_token_limit"`   // 今日上限（免费=1000 万）
	FetchedAt         int64  `json:"fetched_at"`          // unix 秒
	Error             string `json:"error,omitempty"`
}

// Account 一个 MonkeyCode 账号。
type Account struct {
	ID          string  `json:"id"`
	Region      string  `json:"region"` // cn | intl
	Session     string  `json:"session"`
	CookieName  string  `json:"cookie_name,omitempty"` // 会话 cookie 名（默认 monkeycode_ai_session）
	ImageID     string  `json:"image_id"`              // 建任务必需；登录时 best-effort 从历史任务发现
	UserID      string  `json:"user_id,omitempty"`
	Name        string  `json:"name,omitempty"`
	Email       string  `json:"email,omitempty"`
	Plan        string  `json:"plan,omitempty"`
	AddedAt     int64   `json:"added_at"`
	ExpiresAt   int64   `json:"expires_at"` // 估算（AddedAt+30d），上游无续期
	Disabled    bool    `json:"disabled,omitempty"`
	Wallet      *Wallet `json:"wallet,omitempty"`
	LastCheckin string  `json:"last_checkin,omitempty"` // 最近一次签到成功的日期 YYYY-MM-DD
}

// DefaultCookieName 普通用户会话 cookie。团队管理员是 monkeycode_ai_team_session，
// 粘贴时能识别出来并原样带上（见 NormalizeSession）。
const DefaultCookieName = "monkeycode_ai_session"

// CookieHeader 请求头里的 Cookie 值。
func (a *Account) CookieHeader() string {
	name := a.CookieName
	if name == "" {
		name = DefaultCookieName
	}
	return name + "=" + a.Session
}

const SessionTTL = 30 * 24 * time.Hour

// Expiring 是否进入临期窗口（到期前 24h）。
func (a *Account) Expiring() bool {
	return a.ExpiresAt > 0 && a.ExpiresAt-time.Now().Unix() < int64(24*time.Hour/time.Second)
}

// Ready 建任务的两个必要条件：session + image_id。
func (a *Account) Ready() bool {
	return strings.TrimSpace(a.Session) != "" && strings.TrimSpace(a.ImageID) != ""
}

// NormalizeSession 把用户从 DevTools 复制来的各种形态剥成 (cookie 名, 值)。
//
// 上游是 HttpOnly cookie，网页 JS 看不到名字，因此粘贴形态很杂：裸值、整行
// Cookie、`name=value;`、带引号。名字能识别出来就照用（团队号是另一个名字），
// 识别不出就用默认名。
func NormalizeSession(raw string) (name, value string) {
	s := strings.TrimSpace(raw)
	// DevTools「复制为 cURL」整段粘贴：先抠出 cookie 头，再走下面的统一解析。
	// shortcut: 只认 -H 'cookie: …'（Chrome/Firefox 都是这个形态），-b '…' 不支持。
	if i := strings.Index(strings.ToLower(s), "cookie:"); i > 0 {
		s = s[i+len("cookie:"):]
		if j := strings.IndexAny(s, "'\"\n"); j >= 0 {
			s = s[:j]
		}
		s = strings.TrimSpace(s)
	}
	if rest, ok := strings.CutPrefix(s, "Cookie:"); ok {
		s = strings.TrimSpace(rest)
	} else if rest, ok := strings.CutPrefix(s, "cookie:"); ok {
		s = strings.TrimSpace(rest)
	}
	// 只取第一个 cookie 对（后面通常还有 path / HttpOnly 之类的属性）
	if head, _, ok := strings.Cut(s, ";"); ok {
		s = head
	}
	s = strings.TrimSpace(s)
	if n, v, ok := strings.Cut(s, "="); ok {
		n = strings.TrimSpace(strings.Trim(n, `"'`))
		v = strings.TrimSpace(v)
		if n != "" && !strings.ContainsAny(n, " 	") {
			return n, strings.Join(strings.Fields(v), "")
		}
	}
	v := strings.Trim(strings.TrimSpace(s), `"'`)
	return DefaultCookieName, strings.Join(strings.Fields(v), "")
}

// AccountID 账号落盘 id：两地前缀分开，同一个人两地注册不会撞。
func AccountID(region, userID string) string {
	if userID == "" {
		userID = randomHex(6)
	}
	if region == "intl" {
		return "monkeycode-intl-" + userID
	}
	return "monkeycode-" + userID
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Store 账号集合（内存 + 磁盘），并发安全。
type Store struct {
	dir string
	mu  sync.RWMutex
	by  map[string]*Account
}

func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	s := &Store{dir: dir, by: map[string]*Account{}}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var a Account
		if json.Unmarshal(raw, &a) != nil || a.Session == "" {
			continue
		}
		if a.ID == "" {
			a.ID = strings.TrimSuffix(e.Name(), ".json")
		}
		if a.Region == "" {
			a.Region = "cn"
		}
		s.by[a.ID] = &a
	}
	return s, nil
}

// List 返回账号副本（按地区、加入时间排序）。
func (s *Store) List() []Account {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Account, 0, len(s.by))
	for _, a := range s.by {
		cp := *a
		if a.Wallet != nil {
			w := *a.Wallet
			cp.Wallet = &w
		}
		out = append(out, cp)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Region != out[j].Region {
			return out[i].Region < out[j].Region
		}
		return out[i].AddedAt < out[j].AddedAt
	})
	return out
}

func (s *Store) Get(id string) (Account, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	a, ok := s.by[id]
	if !ok {
		return Account{}, false
	}
	cp := *a
	if a.Wallet != nil {
		w := *a.Wallet
		cp.Wallet = &w
	}
	return cp, true
}

// Put 落盘并进内存（同 ID 覆盖）。
func (s *Store) Put(a *Account) error {
	if a.Region == "" {
		a.Region = "cn"
	}
	if a.ID == "" {
		a.ID = AccountID(a.Region, a.UserID)
	}
	if a.AddedAt == 0 {
		a.AddedAt = time.Now().Unix()
	}
	if a.ExpiresAt == 0 {
		a.ExpiresAt = a.AddedAt + int64(SessionTTL/time.Second)
	}
	raw, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(s.dir, a.ID+".json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	s.mu.Lock()
	cp := *a
	s.by[a.ID] = &cp
	s.mu.Unlock()
	return nil
}

// Update 就地改一个账号（不存在返回 false）。
func (s *Store) Update(id string, fn func(*Account)) bool {
	s.mu.Lock()
	a, ok := s.by[id]
	if !ok {
		s.mu.Unlock()
		return false
	}
	cp := *a
	if a.Wallet != nil {
		w := *a.Wallet
		cp.Wallet = &w
	}
	fn(&cp)
	s.mu.Unlock()
	_ = s.Put(&cp)
	return true
}

func (s *Store) Delete(id string) error {
	s.mu.Lock()
	delete(s.by, id)
	s.mu.Unlock()
	err := os.Remove(filepath.Join(s.dir, id+".json"))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (s *Store) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.by)
}

func (a *Account) String() string {
	name := a.Name
	if name == "" {
		name = a.UserID
	}
	if name == "" {
		name = a.ID
	}
	return fmt.Sprintf("%s(%s)", name, a.Region)
}
