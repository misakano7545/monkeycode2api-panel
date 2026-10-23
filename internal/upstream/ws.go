package upstream

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// 极简 RFC6455 客户端（零第三方依赖）。只用文本帧，够任务流用。
const (
	opText   = 0x1
	opBinary = 0x2
	opClose  = 0x8
	opPing   = 0x9
	opPong   = 0xA
)

const wsGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

const maxFrameBytes = 16 << 20

type wsConn struct {
	conn net.Conn
	br   *bufio.Reader
}

// dialWS 建 WebSocket 连接：urlStr 必须是 ws:// 或 wss://。
func dialWS(ctx context.Context, urlStr string, headers http.Header, timeout time.Duration) (*wsConn, error) {
	u, err := url.Parse(urlStr)
	if err != nil {
		return nil, err
	}
	useTLS := u.Scheme == "wss"
	port := u.Port()
	if port == "" {
		if useTLS {
			port = "443"
		} else {
			port = "80"
		}
	}
	addr := net.JoinHostPort(u.Hostname(), port)

	dialer := net.Dialer{Timeout: timeout}
	var conn net.Conn
	if useTLS {
		td := &tls.Dialer{NetDialer: &dialer, Config: &tls.Config{ServerName: u.Hostname()}}
		conn, err = td.DialContext(ctx, "tcp", addr)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return nil, err
	}
	// 握手整体受超时约束；成功后再清掉，交给读循环自己设 deadline。
	_ = conn.SetDeadline(time.Now().Add(timeout))

	keyBytes := make([]byte, 16)
	if _, err := rand.Read(keyBytes); err != nil {
		conn.Close()
		return nil, err
	}
	key := base64.StdEncoding.EncodeToString(keyBytes)

	req := &http.Request{
		Method: http.MethodGet,
		URL:    &url.URL{Scheme: "http", Host: u.Host, Path: u.Path, RawQuery: u.RawQuery},
		Host:   u.Host,
		Header: http.Header{
			"Upgrade":               {"websocket"},
			"Connection":            {"Upgrade"},
			"Sec-WebSocket-Key":     {key},
			"Sec-WebSocket-Version": {"13"},
		},
	}
	for name, values := range headers {
		for _, v := range values {
			req.Header.Set(name, v)
		}
	}
	if err := req.Write(conn); err != nil {
		conn.Close()
		return nil, err
	}

	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, req)
	if err != nil {
		conn.Close()
		return nil, err
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		conn.Close()
		return nil, &Error{Kind: KindUpstream, Status: resp.StatusCode,
			Msg: fmt.Sprintf("任务流握手被拒（HTTP %d）", resp.StatusCode)}
	}
	sum := sha1.Sum([]byte(key + wsGUID))
	if got, want := resp.Header.Get("Sec-WebSocket-Accept"), base64.StdEncoding.EncodeToString(sum[:]); got != want {
		conn.Close()
		return nil, errors.New("任务流握手校验失败（Sec-WebSocket-Accept 不匹配）")
	}
	_ = conn.SetDeadline(time.Time{})
	return &wsConn{conn: conn, br: br}, nil
}

// SetReadDeadline 读超时（空闲兜底用）。
func (w *wsConn) SetReadDeadline(t time.Time) { _ = w.conn.SetDeadline(t) }

// ReadMessage 读一条完整消息（自动回 pong、拼接分片）。op 为 opText/opBinary/opClose。
func (w *wsConn) ReadMessage() (byte, []byte, error) {
	var (
		op      byte
		payload []byte
		frag    bool
	)
	for {
		fin, frameOp, data, err := w.readFrame()
		if err != nil {
			return 0, nil, err
		}
		switch frameOp {
		case opPing:
			_ = w.writeFrame(opPong, data)
			continue
		case opPong:
			continue
		case opClose:
			return opClose, data, nil
		}
		if !frag {
			op = frameOp
			payload = data
			frag = true
		} else {
			payload = append(payload, data...)
		}
		if fin {
			return op, payload, nil
		}
		if len(payload) > maxFrameBytes {
			return 0, nil, errors.New("任务流消息过大")
		}
	}
}

func (w *wsConn) readFrame() (fin bool, op byte, payload []byte, err error) {
	head := make([]byte, 2)
	if _, err = io.ReadFull(w.br, head); err != nil {
		return
	}
	fin = head[0]&0x80 != 0
	op = head[0] & 0x0f
	masked := head[1]&0x80 != 0
	length := uint64(head[1] & 0x7f)
	switch length {
	case 126:
		var b [2]byte
		if _, err = io.ReadFull(w.br, b[:]); err != nil {
			return
		}
		length = uint64(binary.BigEndian.Uint16(b[:]))
	case 127:
		var b [8]byte
		if _, err = io.ReadFull(w.br, b[:]); err != nil {
			return
		}
		length = binary.BigEndian.Uint64(b[:])
	}
	if length > maxFrameBytes {
		return fin, op, nil, errors.New("任务流帧过大")
	}
	var mask [4]byte
	if masked {
		if _, err = io.ReadFull(w.br, mask[:]); err != nil {
			return
		}
	}
	payload = make([]byte, length)
	if _, err = io.ReadFull(w.br, payload); err != nil {
		return
	}
	if masked {
		for i := range payload {
			payload[i] ^= mask[i&3]
		}
	}
	return fin, op, payload, nil
}

// WriteText 发一条文本帧（客户端帧必须掩码）。
func (w *wsConn) WriteText(s string) error { return w.writeFrame(opText, []byte(s)) }

func (w *wsConn) writeFrame(op byte, payload []byte) error {
	var head []byte
	n := len(payload)
	switch {
	case n <= 125:
		head = []byte{0x80 | op, 0x80 | byte(n)}
	case n <= 0xFFFF:
		head = []byte{0x80 | op, 0x80 | 126, byte(n >> 8), byte(n)}
	default:
		head = []byte{0x80 | op, 0x80 | 127, 0, 0, 0, 0,
			byte(n >> 24), byte(n >> 16), byte(n >> 8), byte(n)}
	}
	var mask [4]byte
	if _, err := rand.Read(mask[:]); err != nil {
		return err
	}
	masked := make([]byte, len(payload))
	for i, b := range payload {
		masked[i] = b ^ mask[i&3]
	}
	_ = w.conn.SetWriteDeadline(time.Now().Add(20 * time.Second))
	if _, err := w.conn.Write(append(head, mask[:]...)); err != nil {
		return err
	}
	_, err := w.conn.Write(masked)
	return err
}

func (w *wsConn) Close() error {
	_ = w.writeFrame(opClose, []byte{0x03, 0xe8}) // 1000 normal closure
	return w.conn.Close()
}

// wsURL 把站点 origin 转成 wss:// 基址。
func (r Region) wsURL(path string) string {
	base := r.Origin()
	base = strings.Replace(base, "https://", "wss://", 1)
	base = strings.Replace(base, "http://", "ws://", 1)
	return base + path
}
