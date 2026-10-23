package upstream

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"sync"
)

// Cap.js PoW 验证码（签到用）。流程已对线上端点实测：
//
//  1. POST /api/v1/public/captcha/challenge → {challenge:{c,s,d}, token}
//  2. 对 c 个子挑战求 nonce：sha256(salt_i+nonce) 的 hex 前缀 == target_i
//     salt_i = prng(token+i, s)，target_i = prng(token+i+"d", d)
//  3. POST /api/v1/public/captcha/redeem {token, solutions} → captcha_token
const (
	EpCaptchaChallenge = "/api/v1/public/captcha/challenge"
	EpCaptchaRedeem    = "/api/v1/public/captcha/redeem"
)

const fnvOffset32 = 2166136261

func fnv1a(s string) uint32 {
	h := uint32(fnvOffset32)
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h = (h + (h << 1) + (h << 4) + (h << 7) + (h << 8) + (h << 24)) & 0xFFFFFFFF
	}
	return h
}

// prng 必须与 cap.js widget 的 PRNG 逐位一致（FNV-1a 播种 + xorshift32）。
func prng(seed string, length int) string {
	state := fnv1a(seed)
	var sb strings.Builder
	for sb.Len() < length {
		state ^= (state << 13) & 0xFFFFFFFF
		state ^= state >> 17
		state ^= (state << 5) & 0xFFFFFFFF
		state &= 0xFFFFFFFF
		sb.WriteString(fmt.Sprintf("%08x", state))
	}
	return sb.String()[:length]
}

// maxNoncePerSub 单个子挑战的 nonce 搜索上限（d=3 时远用不到）。
const maxNoncePerSub = 1 << 26

func solveSubChallenge(token string, idx, sLen, dLen int) (int, error) {
	salt := prng(token+fmt.Sprint(idx), sLen)
	target := prng(token+fmt.Sprint(idx)+"d", dLen)
	for n := 0; n < maxNoncePerSub; n++ {
		sum := sha256.Sum256([]byte(salt + fmt.Sprint(n)))
		if hex.EncodeToString(sum[:])[:dLen] == target {
			return n, nil
		}
	}
	return 0, fmt.Errorf("子挑战 %d 在 %d 次内未解出（d=%d）", idx, maxNoncePerSub, dLen)
}

// SolveCaptcha 完成验证码流程，返回 captcha_token。
func (c *Client) SolveCaptcha(ctx context.Context) (string, error) {
	var ch struct {
		Challenge struct {
			C int `json:"c"`
			S int `json:"s"`
			D int `json:"d"`
		} `json:"challenge"`
		Token string `json:"token"`
	}
	if err := c.doJSON(ctx, "", http.MethodPost, EpCaptchaChallenge, nil, &ch); err != nil {
		return "", fmt.Errorf("获取验证码挑战失败：%w", err)
	}
	cfg := ch.Challenge
	if cfg.C <= 0 || cfg.S <= 0 || cfg.D <= 0 {
		return "", fmt.Errorf("验证码挑战配置无效：c=%d s=%d d=%d", cfg.C, cfg.S, cfg.D)
	}

	solutions := make([]int, cfg.C)
	sem := make(chan struct{}, 16)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error
	for i := 1; i <= cfg.C; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}
			n, err := solveSubChallenge(ch.Token, idx, cfg.S, cfg.D)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				return
			}
			solutions[idx-1] = n
		}(i)
	}
	wg.Wait()
	if firstErr != nil {
		return "", firstErr
	}

	var rd struct {
		Success bool   `json:"success"`
		Token   string `json:"token"`
	}
	if err := c.doJSON(ctx, "", http.MethodPost, EpCaptchaRedeem, map[string]any{
		"token": ch.Token, "solutions": solutions,
	}, &rd); err != nil {
		return "", fmt.Errorf("兑换验证码失败：%w", err)
	}
	if !rd.Success || rd.Token == "" {
		return "", fmt.Errorf("验证码兑换未通过")
	}
	return rd.Token, nil
}
