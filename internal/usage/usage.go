// Package usage 记录每次请求与每日用量（data/usage.json + data/requests.jsonl）。
package usage

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Record 一次请求的结果。
type Record struct {
	Time     int64  `json:"ts"`
	Protocol string `json:"protocol"` // openai | claude | codex
	Model    string `json:"model"`
	Account  string `json:"account"`
	Region   string `json:"region"`
	Status   int    `json:"status"`
	Chars    int    `json:"chars"`
	Input    int64  `json:"input"`
	Output   int64  `json:"output"`
	Ms       int64  `json:"ms"`
	Error    string `json:"error,omitempty"`
	// 工具面只数个数：声明>0 而调用==0 的一轮，客户端眼里什么都没发生，归档里却与正常收尾一样。
	ToolsDeclared int `json:"tools_declared,omitempty"`
	ToolCalls     int `json:"tool_calls,omitempty"`
}

// Day 一天的汇总。
type Day struct {
	Requests int   `json:"requests"`
	Errors   int   `json:"errors"`
	Chars    int   `json:"chars"`
	Input    int64 `json:"input"`
	Output   int64 `json:"output"`
}

type Ledger struct {
	dir string
	mu  sync.Mutex
}

func Open(dir string) (*Ledger, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &Ledger{dir: dir}, nil
}

func (l *Ledger) logPath() string { return filepath.Join(l.dir, "requests.jsonl") }

// Add 追加一条请求记录。
func (l *Ledger) Add(r Record) {
	if r.Time == 0 {
		r.Time = time.Now().Unix()
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	f, err := os.OpenFile(l.logPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(raw, '\n'))
}

// Recent 最近 n 条记录（倒序）。
func (l *Ledger) Recent(n int) []Record {
	l.mu.Lock()
	defer l.mu.Unlock()
	f, err := os.Open(l.logPath())
	if err != nil {
		return nil
	}
	defer f.Close()

	// 只读文件尾部：按行倒着攒 n 条。
	stat, err := f.Stat()
	if err != nil {
		return nil
	}
	size := stat.Size()
	const chunk = 64 << 10
	var tail []byte
	for off := size; off > 0 && len(tail) < chunk*8; {
		start := off - chunk
		if start < 0 {
			start = 0
		}
		buf := make([]byte, off-start)
		if _, err := f.ReadAt(buf, start); err != nil {
			break
		}
		tail = append(buf, tail...)
		off = start
	}
	lines := strings.Split(strings.TrimRight(string(tail), "\n"), "\n")
	out := make([]Record, 0, n)
	for i := len(lines) - 1; i >= 0 && len(out) < n; i-- {
		if strings.TrimSpace(lines[i]) == "" {
			continue
		}
		var r Record
		if json.Unmarshal([]byte(lines[i]), &r) == nil {
			out = append(out, r)
		}
	}
	return out
}

// ByDay 按天汇总（最近 days 天，含今天）。
func (l *Ledger) ByDay(days int) []struct {
	Date string `json:"date"`
	Day
} {
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := time.Now().AddDate(0, 0, -days+1)
	cutoff = time.Date(cutoff.Year(), cutoff.Month(), cutoff.Day(), 0, 0, 0, 0, cutoff.Location())

	byDay := map[string]*Day{}
	f, err := os.Open(l.logPath())
	if err != nil {
		return nil
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 1<<20)
	for sc.Scan() {
		var r Record
		if json.Unmarshal(sc.Bytes(), &r) != nil {
			continue
		}
		t := time.Unix(r.Time, 0)
		if t.Before(cutoff) {
			continue
		}
		key := t.Format("2006-01-02")
		d := byDay[key]
		if d == nil {
			d = &Day{}
			byDay[key] = d
		}
		d.Requests++
		if r.Error != "" || r.Status >= 400 {
			d.Errors++
		}
		d.Chars += r.Chars
		d.Input += r.Input
		d.Output += r.Output
	}
	keys := make([]string, 0, len(byDay))
	for k := range byDay {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]struct {
		Date string `json:"date"`
		Day
	}, 0, len(keys))
	for _, k := range keys {
		out = append(out, struct {
			Date string `json:"date"`
			Day
		}{Date: k, Day: *byDay[k]})
	}
	return out
}
