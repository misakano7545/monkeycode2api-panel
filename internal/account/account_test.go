package account

import "testing"

// 用户从 DevTools 复制来的形态很杂：裸值 / 整行 Cookie / 带引号 / 带属性。
// 剥不干净就会把一整行当 cookie 值发给上游，稳定 401。
func TestNormalizeSession(t *testing.T) {
	type want struct{ name, value string }
	cases := map[string]want{
		"abc123":                       {DefaultCookieName, "abc123"},
		"  abc123  ":                   {DefaultCookieName, "abc123"},
		"monkeycode_ai_session=abc123": {"monkeycode_ai_session", "abc123"},
		"Cookie: monkeycode_ai_session=abc123; other=1":  {"monkeycode_ai_session", "abc123"},
		"cookie: monkeycode_ai_session=abc123":           {"monkeycode_ai_session", "abc123"},
		`"abc123"`:                                       {DefaultCookieName, "abc123"},
		"abc 123":                                        {DefaultCookieName, "abc123"},
		"monkeycode_ai_session=abc123; path=/; HttpOnly": {DefaultCookieName, "abc123"},
		"monkeycode_ai_team_session=team-tok; Path=/":    {"monkeycode_ai_team_session", "team-tok"},
		"nebula_session=n1":                              {"nebula_session", "n1"},
		// DevTools「复制为 cURL」整段（Chrome 形）：抠 cookie 头，别把整条命令当 cookie 值。
		`curl 'https://monkeycode-ai.com/api/v1/users/status' -H 'cookie: monkeycode_ai_session=abc123; _ga=GA1.1' -H 'accept: application/json'`: {"monkeycode_ai_session", "abc123"},
		`curl "https://monkeycode-ai.net/x" -H "Cookie: nebula_session=n9; a=b"`:                                                                  {"nebula_session", "n9"},
		"": {DefaultCookieName, ""},
	}
	for in, w := range cases {
		name, value := NormalizeSession(in)
		if name != w.name || value != w.value {
			t.Fatalf("NormalizeSession(%q) = (%q,%q), want (%q,%q)", in, name, value, w.name, w.value)
		}
	}
}

func TestCookieHeaderUsesStoredName(t *testing.T) {
	a := &Account{Session: "v"}
	if got := a.CookieHeader(); got != "monkeycode_ai_session=v" {
		t.Fatalf("默认 cookie 头 = %q", got)
	}
	a.CookieName = "monkeycode_ai_team_session"
	if got := a.CookieHeader(); got != "monkeycode_ai_team_session=v" {
		t.Fatalf("自定义 cookie 头 = %q", got)
	}
}

func TestAccountIDRegionPrefix(t *testing.T) {
	if got := AccountID("cn", "u1"); got != "monkeycode-u1" {
		t.Fatalf("cn id = %q", got)
	}
	if got := AccountID("intl", "u1"); got != "monkeycode-intl-u1" {
		t.Fatalf("intl id = %q", got)
	}
}

func TestReadyNeedsImage(t *testing.T) {
	a := &Account{Session: "s"}
	if a.Ready() {
		t.Fatal("缺 image_id 时不应 ready")
	}
	a.ImageID = "img"
	if !a.Ready() {
		t.Fatal("两个条件齐了应 ready")
	}
}
