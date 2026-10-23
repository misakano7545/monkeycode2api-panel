// util.go 面板内部小工具（请求体读取、数字/字符串兜底）。
package panel

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// maxBodyBytes 请求体上限（导入账号 JSON 也用这个；正常配置远小于它）。
const maxBodyBytes = 8 << 20

func readBody(r *http.Request) ([]byte, error) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes+1))
	if err != nil {
		return nil, errors.New("读取请求体失败：" + err.Error())
	}
	if len(raw) > maxBodyBytes {
		return nil, errors.New("请求体过大")
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return nil, errors.New("请求体为空")
	}
	return raw, nil
}

func itoa(n int) string { return strconv.Itoa(n) }

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
