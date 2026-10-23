// ring.go 固定容量的结构化日志环形缓冲（并发安全，实现 io.Writer）。
//
// main 把 log 包输出经 MultiWriter 镜像进来，面板 /panel/api/logs 读快照。
// 每行入环时按内容归类频道（chat=对话请求 / task=任务与签到动作 / sys=系统），
// 面板日志视图按频道筛选——对话流量大时任务结果不被冲掉。
package panel

import (
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// 日志频道。
const (
	ChChat = "chat"
	ChTask = "task"
	ChSys  = "sys"
)

// LogEntry 单条日志（时间戳取写入时刻）。
type LogEntry struct {
	TS   time.Time `json:"ts"`
	Ch   string    `json:"ch"`
	Text string    `json:"text"`
}

// taskHints 任务/额度动作日志的关键词（scheduler 与 panel 的既有口径）。
var taskHints = []string{
	"[scheduler]", "签到", "checkin", "额度", "wallet", "已停用", "建任务",
	"panel:",
}

// Ring 每频道独立限额的日志环。
type Ring struct {
	mu   sync.Mutex
	cap  int
	buf  map[string][]LogEntry
	last time.Time
}

// NewRing 构建每频道容量为 capacity 的日志环（非正值回退 500）。
func NewRing(capacity int) *Ring {
	if capacity <= 0 {
		capacity = 500
	}
	return &Ring{cap: capacity, buf: map[string][]LogEntry{}}
}

// classify 按行内容判定频道。
func classify(line string) string {
	if strings.Contains(line, "/v1/") || strings.Contains(line, "/v2/") {
		return ChChat
	}
	for _, h := range taskHints {
		if strings.Contains(line, h) {
			return ChTask
		}
	}
	return ChSys
}

// tsPrefixRe log 包默认 flags（日期 时间）产生的行首时间戳。
// 入环时剥掉：面板自己渲染时刻，留着会变成「09:41:53 2026/10/23 09:41:53 …」。
var tsPrefixRe = regexp.MustCompile(`^\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2}(\.\d+)? `)

// Write 实现 io.Writer（log 包一行一次调用，尾部换行剥掉）。
func (r *Ring) Write(p []byte) (int, error) {
	for _, line := range strings.Split(strings.TrimRight(string(p), "\n"), "\n") {
		line = tsPrefixRe.ReplaceAllString(line, "")
		if strings.TrimSpace(line) == "" {
			continue
		}
		r.Add(classify(line), line)
	}
	return len(p), nil
}

// Add 写入一行到指定频道，超出容量丢最旧的。
func (r *Ring) Add(ch, text string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	r.last = now
	b := append(r.buf[ch], LogEntry{TS: now, Ch: ch, Text: text})
	if len(b) > r.cap {
		b = b[len(b)-r.cap:]
	}
	r.buf[ch] = b
}

// Snapshot 返回全部频道按时间升序合并后的快照（只读副本）。
func (r *Ring) Snapshot() []LogEntry {
	r.mu.Lock()
	out := make([]LogEntry, 0, len(r.buf)*r.cap/4)
	for _, b := range r.buf {
		out = append(out, b...)
	}
	r.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].TS.Before(out[j].TS) })
	return out
}

// Last 最近一次写入时刻（面板显示「x 秒前」）。
func (r *Ring) Last() time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.last
}
