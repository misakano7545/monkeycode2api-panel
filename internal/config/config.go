// Package config 读 config.json 并用 MC2A_* 环境变量覆盖。
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Cooldown struct {
	HardQuota    string `json:"hard_quota"`   // 额度耗尽：长冷却，等隔日刷新
	SoftRate     string `json:"soft_rate"`    // 429 / 账号忙：短冷却
	ErrCooldown  string `json:"err_cooldown"` // 连续错误达阈值
	ErrThreshold int    `json:"err_threshold"`
}

type Schedule struct {
	CheckinEnabled  bool  `json:"checkin_enabled"`
	CheckinHours    []int `json:"checkin_hours"`    // 每日签到时刻（已签自动跳过）
	QuotaCheckHour  int   `json:"quota_check_hour"` // 每日额度到账检查
	QuotaRefreshMin int   `json:"quota_refresh_minutes"`
}

type Upstream struct {
	TimeoutSeconds     int    `json:"timeout_seconds"`      // 普通 API 调用
	IdleTimeoutSeconds int    `json:"idle_timeout_seconds"` // 任务流无下行消息即断线
	TaskTimeoutSeconds int    `json:"task_timeout_seconds"` // 与 resource.life 对齐
	UserAgent          string `json:"user_agent"`
}

type Config struct {
	Listen   string   `json:"listen"`
	APIKey   string   `json:"api_key"`
	AuthDir  string   `json:"auth_dir"`
	DataDir  string   `json:"data_dir"`
	Cooldown Cooldown `json:"cooldown"`
	Schedule Schedule `json:"schedule"`
	Upstream Upstream `json:"upstream"`
	Panel    struct {
		Enabled bool `json:"enabled"`
	} `json:"panel"`
}

func Default() *Config {
	c := &Config{
		Listen:  ":7887",
		AuthDir: "./auths",
		DataDir: "./data",
		Cooldown: Cooldown{
			HardQuota: "12h", SoftRate: "90s", ErrCooldown: "10m", ErrThreshold: 5,
		},
		Schedule: Schedule{
			CheckinEnabled: true, CheckinHours: []int{9, 21},
			QuotaCheckHour: 0, QuotaRefreshMin: 30,
		},
		Upstream: Upstream{
			TimeoutSeconds: 30, IdleTimeoutSeconds: 300, TaskTimeoutSeconds: 7200,
			UserAgent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
		},
	}
	c.Panel.Enabled = true
	return c
}

// Load 读配置文件（不存在则用默认值）后叠加环境变量。
func Load(path string) (*Config, error) {
	c := Default()
	if b, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(b, c); err != nil {
			return nil, fmt.Errorf("解析 %s: %w", path, err)
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	c.applyEnv()
	if c.APIKey == "" {
		return nil, fmt.Errorf("缺少 api_key：请在 config.json 设置或导出 MC2A_API_KEY")
	}
	return c, nil
}

func (c *Config) applyEnv() {
	str := func(key string, dst *string) {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			*dst = v
		}
	}
	num := func(key string, dst *int) {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				*dst = n
			}
		}
	}
	str("MC2A_LISTEN", &c.Listen)
	str("MC2A_API_KEY", &c.APIKey)
	str("MC2A_AUTH_DIR", &c.AuthDir)
	str("MC2A_DATA_DIR", &c.DataDir)
	str("MC2A_SOFT_RATE", &c.Cooldown.SoftRate)
	str("MC2A_HARD_QUOTA", &c.Cooldown.HardQuota)
	num("MC2A_ERR_THRESHOLD", &c.Cooldown.ErrThreshold)
	num("MC2A_QUOTA_CHECK_HOUR", &c.Schedule.QuotaCheckHour)
	num("MC2A_TASK_TIMEOUT", &c.Upstream.TaskTimeoutSeconds)
	num("MC2A_IDLE_TIMEOUT", &c.Upstream.IdleTimeoutSeconds)
	if v := strings.TrimSpace(os.Getenv("MC2A_CHECKIN_HOURS")); v != "" {
		hours := []int{}
		for _, part := range strings.Split(v, ",") {
			if n, err := strconv.Atoi(strings.TrimSpace(part)); err == nil {
				hours = append(hours, n)
			}
		}
		if len(hours) > 0 {
			c.Schedule.CheckinHours = hours
		}
	}
	if v := strings.TrimSpace(os.Getenv("MC2A_CHECKIN_ENABLED")); v != "" {
		c.Schedule.CheckinEnabled = v != "0" && !strings.EqualFold(v, "false")
	}
}

func (c *Config) Dur(s string, fallback time.Duration) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return fallback
	}
	return d
}

func (c *Config) HardQuota() time.Duration   { return c.Dur(c.Cooldown.HardQuota, 12*time.Hour) }
func (c *Config) SoftRate() time.Duration    { return c.Dur(c.Cooldown.SoftRate, 90*time.Second) }
func (c *Config) ErrCooldown() time.Duration { return c.Dur(c.Cooldown.ErrCooldown, 10*time.Minute) }
func (c *Config) TaskTimeout() time.Duration {
	return time.Duration(max(c.Upstream.TaskTimeoutSeconds, 60)) * time.Second
}
func (c *Config) IdleTimeout() time.Duration {
	return time.Duration(max(c.Upstream.IdleTimeoutSeconds, 30)) * time.Second
}
func (c *Config) APITimeout() time.Duration {
	return time.Duration(max(c.Upstream.TimeoutSeconds, 5)) * time.Second
}
