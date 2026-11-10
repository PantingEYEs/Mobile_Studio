package main

// 最小化的 WebSocket 服务端实现（RFC 6455），仅依赖标准库。
// 不引入第三方模块，避免在 Termux 上 go build 时依赖 proxy.golang.org 等境外网络。
//
// 支持：握手、客户端掩码帧、分片消息、ping/pong、close；不支持扩展（如 permessage-deflate）。

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	wsOpContinuation = 0x0
	wsOpText         = 0x1
	wsOpBinary       = 0x2
	wsOpClose        = 0x8
	wsOpPing         = 0x9
	wsOpPong         = 0xA

	wsMaxMessage = 1 << 20 // 单条消息上限 1MB（终端输入远小于此）
	wsGUID       = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
)

type wsConn struct {
	conn      net.Conn
	br        *bufio.Reader
	writeMu   sync.Mutex
	closeSent bool // RFC 6455 §5.5.1：发送 close 帧后不得再发送任何帧
}

func headerContainsToken(h http.Header, name, token string) bool {
	for _, v := range h.Values(name) {
		for _, part := range strings.Split(v, ",") {
			if strings.EqualFold(strings.TrimSpace(part), token) {
				return true
			}
		}
	}
	return false
}

// wsUpgrade 完成握手并接管底层连接。
func wsUpgrade(w http.ResponseWriter, r *http.Request) (*wsConn, error) {
	if r.Method != http.MethodGet ||
		!headerContainsToken(r.Header, "Connection", "upgrade") ||
		!headerContainsToken(r.Header, "Upgrade", "websocket") {
		http.Error(w, "expected websocket upgrade", http.StatusBadRequest)
		return nil, errors.New("not a websocket handshake")
	}
	if r.Header.Get("Sec-WebSocket-Version") != "13" {
		w.Header().Set("Sec-WebSocket-Version", "13")
		http.Error(w, "unsupported websocket version", http.StatusUpgradeRequired)
		return nil, errors.New("unsupported websocket version")
	}
	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		http.Error(w, "missing Sec-WebSocket-Key", http.StatusBadRequest)
		return nil, errors.New("missing key")
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijacking unsupported", http.StatusInternalServerError)
		return nil, errors.New("hijacking unsupported")
	}
	conn, brw, err := hj.Hijack()
	if err != nil {
		return nil, err
	}

	sum := sha1.Sum([]byte(key + wsGUID))
	accept := base64.StdEncoding.EncodeToString(sum[:])
	resp := "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + accept + "\r\n\r\n"
	conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if _, err := conn.Write([]byte(resp)); err != nil {
		conn.Close()
		return nil, err
	}
	conn.SetWriteDeadline(time.Time{})
	return &wsConn{conn: conn, br: brw.Reader}, nil
}

func (c *wsConn) writeFrame(op byte, payload []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if c.closeSent {
		return net.ErrClosed
	}
	if op == wsOpClose {
		c.closeSent = true
	}
	hdr := make([]byte, 0, 10)
	hdr = append(hdr, 0x80|op) // FIN + opcode
	n := len(payload)
	switch {
	case n < 126:
		hdr = append(hdr, byte(n))
	case n <= 0xFFFF:
		hdr = append(hdr, 126, byte(n>>8), byte(n))
	default:
		hdr = append(hdr, 127)
		hdr = binary.BigEndian.AppendUint64(hdr, uint64(n))
	}
	c.conn.SetWriteDeadline(time.Now().Add(15 * time.Second))
	defer c.conn.SetWriteDeadline(time.Time{})
	if _, err := c.conn.Write(hdr); err != nil {
		return err
	}
	_, err := c.conn.Write(payload)
	return err
}

func (c *wsConn) WriteBinary(b []byte) error { return c.writeFrame(wsOpBinary, b) }
func (c *wsConn) WriteText(s string) error   { return c.writeFrame(wsOpText, []byte(s)) }
func (c *wsConn) Ping() error                { return c.writeFrame(wsOpPing, nil) }

// Close 发送 close 帧（若尚未发送，尽力而为）并关闭连接。
func (c *wsConn) Close(code uint16, reason string) {
	payload := binary.BigEndian.AppendUint16(nil, code)
	payload = append(payload, reason...)
	c.writeFrame(wsOpClose, payload)
	c.conn.Close()
}

// ReadMessage 读取一条完整消息（自动拼接分片、应答 ping）。
// 对端发送 close 时返回 io.EOF。
func (c *wsConn) ReadMessage() (op byte, data []byte, err error) {
	var msgOp byte
	var msg []byte
	for {
		var h [2]byte
		if _, err = io.ReadFull(c.br, h[:]); err != nil {
			return 0, nil, err
		}
		fin := h[0]&0x80 != 0
		if h[0]&0x70 != 0 {
			return 0, nil, errors.New("websocket: reserved bits set")
		}
		frameOp := h[0] & 0x0F
		masked := h[1]&0x80 != 0
		length := uint64(h[1] & 0x7F)
		switch length {
		case 126:
			var ext [2]byte
			if _, err = io.ReadFull(c.br, ext[:]); err != nil {
				return 0, nil, err
			}
			length = uint64(binary.BigEndian.Uint16(ext[:]))
		case 127:
			var ext [8]byte
			if _, err = io.ReadFull(c.br, ext[:]); err != nil {
				return 0, nil, err
			}
			length = binary.BigEndian.Uint64(ext[:])
		}
		if !masked {
			return 0, nil, errors.New("websocket: client frame not masked")
		}
		if length > wsMaxMessage || uint64(len(msg))+length > wsMaxMessage {
			return 0, nil, errors.New("websocket: message too large")
		}
		var mask [4]byte
		if _, err = io.ReadFull(c.br, mask[:]); err != nil {
			return 0, nil, err
		}
		payload := make([]byte, length)
		if _, err = io.ReadFull(c.br, payload); err != nil {
			return 0, nil, err
		}
		for i := range payload {
			payload[i] ^= mask[i%4]
		}

		switch frameOp {
		case wsOpPing:
			if len(payload) > 125 {
				return 0, nil, errors.New("websocket: control frame too large")
			}
			c.writeFrame(wsOpPong, payload)
			continue
		case wsOpPong:
			continue
		case wsOpClose:
			c.writeFrame(wsOpClose, payload) // 回显 close 完成关闭握手
			return 0, nil, io.EOF
		case wsOpText, wsOpBinary:
			if msg != nil {
				return 0, nil, errors.New("websocket: new message before previous finished")
			}
			msgOp = frameOp
			msg = payload
		case wsOpContinuation:
			if msg == nil {
				return 0, nil, errors.New("websocket: unexpected continuation frame")
			}
			msg = append(msg, payload...)
		default:
			return 0, nil, fmt.Errorf("websocket: unknown opcode %d", frameOp)
		}
		if fin {
			return msgOp, msg, nil
		}
	}
}
