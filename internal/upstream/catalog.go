package upstream

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Catalog 两个站点的模型目录（各占一格，不能合成一份）。
//
// 对外模型 id 是 `{provider}/{model}`（客户端点名用它）；建任务要的是 UUID，
// 因此条目里必须保留 upstream_id 与 cli_name，展示字段不能替代它们。
type Catalog struct {
	mu      sync.RWMutex
	data    map[Region]*catalogState
	dataDir string
}

type catalogState struct {
	Models    []Model `json:"models"`
	FetchedAt int64   `json:"fetched_at"`
}

// Item 对外暴露的模型条目。
type Item struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Region        Region `json:"region"`
	Provider      string `json:"provider"`
	Model         string `json:"model"`
	InterfaceType string `json:"interface_type"`
	CLI           string `json:"cli"`
	ContextLimit  int64  `json:"context_limit,omitempty"`
	OutputLimit   int64  `json:"output_limit,omitempty"`
	IsFree        bool   `json:"is_free"`
	IsDefault     bool   `json:"is_default"`
	AccessLevel   string `json:"access_level,omitempty"`
	Thinking      bool   `json:"thinking,omitempty"`
}

// Candidate 一次模型解析结果：该模型在某个站点上的建任务参数。
type Candidate struct {
	Region Region
	Target Target
}

func NewCatalog(dataDir string) *Catalog {
	c := &Catalog{data: map[Region]*catalogState{}, dataDir: dataDir}
	c.loadFromDisk()
	return c
}

func (c *Catalog) path(r Region) string {
	return filepath.Join(c.dataDir, "models-"+string(r)+".json")
}

func (c *Catalog) loadFromDisk() {
	for _, r := range []Region{CN, INTL} {
		raw, err := os.ReadFile(c.path(r))
		if err != nil {
			continue
		}
		var st catalogState
		if json.Unmarshal(raw, &st) == nil && len(st.Models) > 0 {
			c.data[r] = &st
		}
	}
}

// Refresh 拉一个站点的目录并落盘。返回条目数。
func (c *Catalog) Refresh(ctx context.Context, client *Client, cookie string) (int, error) {
	models, err := client.Models(ctx, cookie)
	if err != nil {
		return 0, err
	}
	kept := models[:0]
	for _, m := range models {
		if strings.TrimSpace(m.ID) != "" && strings.TrimSpace(m.Model) != "" {
			kept = append(kept, m)
		}
	}
	if len(kept) == 0 {
		return 0, &Error{Kind: KindUpstream, Msg: "上游模型目录为空（保留现有清单）"}
	}
	st := &catalogState{Models: kept, FetchedAt: time.Now().Unix()}
	raw, _ := json.MarshalIndent(st, "", "  ")
	_ = os.MkdirAll(c.dataDir, 0o700)
	_ = os.WriteFile(c.path(client.Region), raw, 0o600)

	c.mu.Lock()
	c.data[client.Region] = st
	c.mu.Unlock()
	return len(kept), nil
}

// Fresh 目录是否还在 TTL 内。
func (c *Catalog) Fresh(r Region, ttl time.Duration) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	st, ok := c.data[r]
	return ok && len(st.Models) > 0 && time.Since(time.Unix(st.FetchedAt, 0)) < ttl
}

// FetchedAt 最近一次刷新时刻（unix 秒）。
func (c *Catalog) FetchedAt(r Region) int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if st, ok := c.data[r]; ok {
		return st.FetchedAt
	}
	return 0
}

// List 两个站点的全部可见模型（同名不同站点各占一条，用 region 区分）。
func (c *Catalog) List() []Item {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]Item, 0, 64)
	for _, r := range []Region{CN, INTL} {
		st, ok := c.data[r]
		if !ok {
			continue
		}
		for _, m := range st.Models {
			out = append(out, itemOf(r, m))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ID != out[j].ID {
			return out[i].ID < out[j].ID
		}
		return out[i].Region < out[j].Region
	})
	return out
}

func itemOf(r Region, m Model) Item {
	name := m.DisplayName
	if name == "" {
		name = m.Model
	}
	return Item{
		ID:            ModelID(m),
		Name:          name,
		Region:        r,
		Provider:      m.Provider,
		Model:         m.Model,
		InterfaceType: m.InterfaceType,
		CLI:           CliNameFor(m.InterfaceType),
		ContextLimit:  m.ContextLimit,
		OutputLimit:   m.OutputLimit,
		IsFree:        m.IsFree,
		IsDefault:     m.IsDefault,
		AccessLevel:   m.AccessLevel,
		Thinking:      m.Thinking,
	}
}

// ModelID 对外模型 id：`{provider}/{model}`（provider 为空时退化成 model）。
func ModelID(m Model) string {
	if m.Provider == "" {
		return m.Model
	}
	return m.Provider + "/" + m.Model
}

// Resolve 客户端模型名 → 各站点的建任务参数（国内版优先，确定性）。
//
// 匹配顺序：目录 id → 上游模型名 → 展示名 → UUID。全部大小写不敏感。
// 找不到就返回空，由调用方报可读错误 —— 不静默换成别的模型。
func (c *Catalog) Resolve(name string) []Candidate {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	var out []Candidate
	for _, r := range []Region{CN, INTL} {
		st, ok := c.data[r]
		if !ok {
			continue
		}
		for _, m := range st.Models {
			if eqAny(name, ModelID(m), m.Model, m.DisplayName, m.ID) {
				out = append(out, Candidate{Region: r, Target: Target{
					ModelID: m.ID, CLI: CliNameFor(m.InterfaceType), Model: m.Model,
				}})
				break
			}
		}
	}
	return out
}

func eqAny(name string, candidates ...string) bool {
	for _, c := range candidates {
		if c != "" && strings.EqualFold(name, c) {
			return true
		}
	}
	return false
}
