// config.go 配置页：读取（密钥脱敏）与保存（原子写盘 + 列出需重启的字段）。
//
// 本服务不支持热加载：main 在启动时把配置灌进 pool/scheduler/client，运行期改
// 文件不会生效。所以保存只做"校验 + 落盘 + 如实告诉用户哪些字段要重启"，不假装
// 已经生效——谎报热生效会让人以为改了限额/冷却却还在用旧值。
package panel

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"

	"github.com/misakano7545/monkeycode2api-panel/internal/config"
)

// maskKey 密钥脱敏：只留后 4 位，前端原样提交回来时按"未改动"处理。
func maskKey(k string) string {
	if k == "" {
		return ""
	}
	if len(k) <= 4 {
		return "****"
	}
	return "****" + k[len(k)-4:]
}

// getConfig 返回当前配置文件内容（api_key 脱敏）。
func (p *Panel) getConfig(w http.ResponseWriter, r *http.Request) {
	if p.cfg.ConfigPath == "" {
		writeErr(w, http.StatusNotImplemented, "未配置 config.json 路径")
		return
	}
	raw, err := os.ReadFile(p.cfg.ConfigPath)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "读配置失败："+err.Error())
		return
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		writeErr(w, http.StatusInternalServerError, "配置不是合法 JSON："+err.Error())
		return
	}
	masked := false
	if k, ok := m["api_key"].(string); ok && k != "" {
		m["api_key"] = maskKey(k)
		masked = true
	}
	writeJSON(w, http.StatusOK, map[string]any{"config": m, "api_key_masked": masked})
}

// saveConfig 校验并落盘（保留用户手写的未知键），返回需重启生效的字段名。
func (p *Panel) saveConfig(w http.ResponseWriter, r *http.Request) {
	if p.cfg.ConfigPath == "" {
		writeErr(w, http.StatusNotImplemented, "未配置 config.json 路径")
		return
	}
	raw, err := readBody(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	// 先按强类型校验：结构/类型不对就不落盘，避免把服务写成起不来的样子。
	var parsed config.Config
	if err := json.Unmarshal(raw, &parsed); err != nil {
		writeErr(w, http.StatusBadRequest, "配置校验失败："+err.Error())
		return
	}
	var incoming map[string]any
	if err := json.Unmarshal(raw, &incoming); err != nil {
		writeErr(w, http.StatusBadRequest, "配置不是合法 JSON："+err.Error())
		return
	}

	old := map[string]any{}
	if b, err := os.ReadFile(p.cfg.ConfigPath); err == nil {
		_ = json.Unmarshal(b, &old)
	}
	// 脱敏回传的密钥视为"未改动"。
	if k, ok := incoming["api_key"].(string); ok && len(k) > 4 && k[:4] == "****" {
		incoming["api_key"] = old["api_key"]
	}
	merged := map[string]any{}
	for k, v := range old {
		merged[k] = v
	}
	for k, v := range incoming {
		merged[k] = v
	}

	out, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "序列化失败："+err.Error())
		return
	}
	tmp := p.cfg.ConfigPath + ".tmp"
	if err := os.WriteFile(tmp, append(out, '\n'), 0o600); err != nil {
		writeErr(w, http.StatusInternalServerError, "写配置失败："+err.Error())
		return
	}
	if err := os.Rename(tmp, filepath.Clean(p.cfg.ConfigPath)); err != nil {
		writeErr(w, http.StatusInternalServerError, "替换配置失败："+err.Error())
		return
	}

	okJSON(w, "配置已保存", map[string]any{"restart_required": changedKeys(old, incoming)})
}

// changedKeys 值发生变化的顶层键（本服务全部需要重启生效）。
func changedKeys(old, incoming map[string]any) []string {
	var out []string
	for k, v := range incoming {
		prev, _ := json.Marshal(old[k])
		next, _ := json.Marshal(v)
		if string(prev) != string(next) {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	if out == nil {
		out = []string{}
	}
	return out
}
