// Package httpauth 网关与面板共用的 Bearer 鉴权原语。
//
// 单独成包的原因：server（/v1/*、/status）与 panel（/panel/api/*）两处鉴权
// 必须完全同口径，各写一份既容易漂移，又都带计时侧信道。统一到这里后口径只有
// 一份，且天然常量时间比较。
package httpauth

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"strings"
)

// bearerPrefix 认证方案前缀（大小写敏感，与 HTTP 规范及既有实现一致）。
const bearerPrefix = "Bearer "

// VerifyBearer 校验请求头是否携带正确的 Bearer 密钥。
//
// key 为空表示「未启用鉴权」，恒返回 true（调用方据此放行）。
// 比较用 SHA-256 摘要 + subtle.ConstantTimeCompare：常量时间、长度差异被摘要
// 吸收、摘要不可逆。
func VerifyBearer(r *http.Request, key string) bool {
	if key == "" {
		return true
	}
	authz := r.Header.Get("Authorization")
	if !strings.HasPrefix(authz, bearerPrefix) {
		// 缺头/方案不对：仍走一次摘要比较，保持耗时形状一致。
		subtle.ConstantTimeCompare(digest(""), digest(key))
		return false
	}
	return subtle.ConstantTimeCompare(digest(authz[len(bearerPrefix):]), digest(key)) == 1
}

// PromoteAPIKey 把 Anthropic 官方 SDK 的 x-api-key 提升成 Authorization，
// 好让 Claude 客户端与 Bearer 走同一套比较。
func PromoteAPIKey(r *http.Request) {
	if r.Header.Get("Authorization") == "" {
		if k := r.Header.Get("x-api-key"); k != "" {
			r.Header.Set("Authorization", bearerPrefix+k)
		}
	}
}

// digest 返回 s 的 SHA-256（定长 32 字节，供常量时间比较）。
func digest(s string) []byte {
	sum := sha256.Sum256([]byte(s))
	return sum[:]
}
