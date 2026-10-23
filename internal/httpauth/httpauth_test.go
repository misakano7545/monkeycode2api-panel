package httpauth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func req(authz, apiKey string) *http.Request {
	r := httptest.NewRequest("GET", "/", nil)
	if authz != "" {
		r.Header.Set("Authorization", authz)
	}
	if apiKey != "" {
		r.Header.Set("x-api-key", apiKey)
	}
	return r
}

func TestVerifyBearer(t *testing.T) {
	cases := []struct {
		name  string
		key   string
		authz string
		want  bool
	}{
		{"空 key 放行（未启用鉴权）", "", "", true},
		{"空 key 也放行任意头", "", "Bearer whatever", true},
		{"正确 key", "sk-abc123", "Bearer sk-abc123", true},
		{"错误 key", "sk-abc123", "Bearer sk-wrong", false},
		{"缺 Authorization 头", "sk-abc123", "", false},
		{"缺 Bearer 前缀", "sk-abc123", "sk-abc123", false},
		{"前缀大小写不符（规范要求精确）", "sk-abc123", "bearer sk-abc123", false},
		{"多余空格", "sk-abc123", "Bearer  sk-abc123", false},
		{"前缀相同但内容短", "sk-abc123", "Bearer sk-abc12", false},
		{"前缀相同但内容长", "sk-abc123", "Bearer sk-abc1234", false},
		{"key 恰好是前缀", "sk-abc", "Bearer sk-abcdef", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := VerifyBearer(req(c.authz, ""), c.key); got != c.want {
				t.Fatalf("VerifyBearer(authz=%q, key=%q) = %v, want %v", c.authz, c.key, got, c.want)
			}
		})
	}
}

func TestPromoteAPIKey(t *testing.T) {
	// x-api-key 提升成 Bearer 后可被同一套比较放行。
	r := req("", "sk-abc123")
	PromoteAPIKey(r)
	if !VerifyBearer(r, "sk-abc123") {
		t.Fatal("x-api-key 提升后应通过校验")
	}
	// 已有 Authorization 时不覆盖（Bearer 优先）。
	r = req("Bearer sk-bearer", "sk-apikey")
	PromoteAPIKey(r)
	if !VerifyBearer(r, "sk-bearer") {
		t.Fatal("已有 Authorization 不应被 x-api-key 覆盖")
	}
}
