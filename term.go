package main

// Web 终端：在伪终端中运行 Termux 的登录 Shell，经 WebSocket 与前端 xterm.js 双向桥接。
//
// 会话独立于浏览器连接存活（与 Termux 会话一致）：关闭页面或断网后重新打开，
// 服务端会回放最近 512KB 的输出并继续实时推送。

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	termScrollbackLimit = 512 * 1024
	termMaxSessions     = 8
	termClientQueue     = 1024
)

type termMsg struct {
	bin  []byte // 终端输出（二进制帧）
	text string // 控制消息 JSON（文本帧）
}

type termClient struct {
	ch   chan termMsg
	ws   *wsConn
	once sync.Once
}

func (c *termClient) close() {
	c.once.Do(func() { close(c.ch) })
}

type TermSession struct {
	ID      string
	Title   string
	Cwd     string
	Shell   string
	Created time.Time

	cmd  *exec.Cmd
	ptmx *os.File

	mu         sync.Mutex
	scrollback []byte
	clients    map[*termClient]struct{}
	exited     bool
	exitCode   int
	readerDone chan struct{}
}

type TermInfo struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Cwd      string `json:"cwd"`
	Shell    string `json:"shell"`
	Created  int64  `json:"created"`
	Exited   bool   `json:"exited"`
	ExitCode int    `json:"exitCode"`
	Clients  int    `json:"clients"`
}

func (s *TermSession) Info() TermInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	return TermInfo{
		ID: s.ID, Title: s.Title, Cwd: s.Cwd, Shell: s.Shell, Created: s.Created.UnixMilli(),
		Exited: s.exited, ExitCode: s.exitCode, Clients: len(s.clients),
	}
}

// broadcastLocked 向所有客户端推送消息；队列已满的客户端（长期无响应）会被断开。
func (s *TermSession) broadcastLocked(m termMsg) {
	for c := range s.clients {
		select {
		case c.ch <- m:
		default:
			delete(s.clients, c)
			c.close()
		}
	}
}

func (s *TermSession) appendScrollbackLocked(b []byte) {
	s.scrollback = append(s.scrollback, b...)
	if over := len(s.scrollback) - termScrollbackLimit; over > 0 {
		// 尽量从换行处截断，减少回放时出现半截转义序列
		cut := over
		if i := strings.IndexByte(string(s.scrollback[over:min(len(s.scrollback), over+4096)]), '\n'); i >= 0 {
			cut = over + i + 1
		}
		s.scrollback = append([]byte(nil), s.scrollback[cut:]...)
	}
}

func (s *TermSession) readLoop() {
	defer close(s.readerDone)
	buf := make([]byte, 32*1024)
	for {
		n, err := s.ptmx.Read(buf)
		if n > 0 {
			chunk := append([]byte(nil), buf[:n]...)
			s.mu.Lock()
			s.appendScrollbackLocked(chunk)
			s.broadcastLocked(termMsg{bin: chunk})
			s.mu.Unlock()
		}
		if err != nil {
			return // 从设备全部关闭时返回 EIO，或会话被关闭
		}
	}
}

func (s *TermSession) waitLoop() {
	err := s.cmd.Wait()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			code = -1
		}
	}
	// 稍等读取协程把剩余输出推送完，保证 exit 消息位于最后
	select {
	case <-s.readerDone:
	case <-time.After(300 * time.Millisecond):
	}
	s.mu.Lock()
	s.exited = true
	s.exitCode = code
	msg := fmt.Sprintf("\r\n\x1b[90m[进程已退出，返回码 %d]\x1b[0m\r\n", code)
	s.appendScrollbackLocked([]byte(msg))
	s.broadcastLocked(termMsg{bin: []byte(msg)})
	s.broadcastLocked(termMsg{text: fmt.Sprintf(`{"type":"exit","code":%d}`, code)})
	s.mu.Unlock()
}

func (s *TermSession) attach(ws *wsConn) *termClient {
	c := &termClient{ch: make(chan termMsg, termClientQueue), ws: ws}
	s.mu.Lock()
	if len(s.scrollback) > 0 {
		c.ch <- termMsg{bin: append([]byte(nil), s.scrollback...)}
	}
	if s.exited {
		c.ch <- termMsg{text: fmt.Sprintf(`{"type":"exit","code":%d}`, s.exitCode)}
	}
	s.clients[c] = struct{}{}
	s.mu.Unlock()
	return c
}

func (s *TermSession) detach(c *termClient) {
	s.mu.Lock()
	delete(s.clients, c)
	s.mu.Unlock()
	c.close()
}

// terminate 结束整个进程组并释放伪终端。
func (s *TermSession) terminate() {
	if s.cmd.Process != nil {
		pgid := s.cmd.Process.Pid
		syscall.Kill(-pgid, syscall.SIGHUP)
		go func() {
			time.Sleep(800 * time.Millisecond)
			syscall.Kill(-pgid, syscall.SIGKILL)
		}()
	}
	s.ptmx.Close()
	s.mu.Lock()
	s.broadcastLocked(termMsg{text: `{"type":"closed"}`})
	for c := range s.clients {
		delete(s.clients, c)
		c.close()
	}
	s.mu.Unlock()
}

type TermManager struct {
	mu       sync.Mutex
	seq      int
	sessions map[string]*TermSession
}

var terms = &TermManager{sessions: map[string]*TermSession{}}

func (m *TermManager) Get(id string) *TermSession {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessions[id]
}

func (m *TermManager) List() []TermInfo {
	m.mu.Lock()
	list := make([]*TermSession, 0, len(m.sessions))
	for _, s := range m.sessions {
		list = append(list, s)
	}
	m.mu.Unlock()
	sort.Slice(list, func(i, j int) bool { return list[i].Created.Before(list[j].Created) })
	out := make([]TermInfo, 0, len(list))
	for _, s := range list {
		out = append(out, s.Info())
	}
	return out
}

// detectShell 优先使用 $SHELL，其次 bash，最后 sh。
func detectShell() string {
	if sh := os.Getenv("SHELL"); sh != "" {
		if st, err := os.Stat(sh); err == nil && !st.IsDir() {
			return sh
		}
	}
	for _, name := range []string{"bash", "sh"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	return "/system/bin/sh"
}

func termEnv() []string {
	env := make([]string, 0, len(os.Environ())+3)
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "TERM=") || strings.HasPrefix(kv, "COLORTERM=") {
			continue
		}
		env = append(env, kv)
	}
	return append(env, "TERM=xterm-256color", "COLORTERM=truecolor", "MOBILE_STUDIO_TERM=1")
}

func (m *TermManager) Create(cwd string, cols, rows int) (*TermSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.sessions) >= termMaxSessions {
		return nil, fmt.Errorf("最多同时打开 %d 个终端会话", termMaxSessions)
	}

	shell := detectShell()
	cmd := exec.Command(shell, "-l")
	cmd.Dir = cwd
	cmd.Env = termEnv()
	ptmx, err := startInPTY(cmd, cols, rows)
	if err != nil {
		return nil, fmt.Errorf("启动终端失败: %v", err)
	}

	m.seq++
	s := &TermSession{
		ID:         fmt.Sprintf("term%d-%d", time.Now().Unix(), m.seq),
		Title:      fmt.Sprintf("%s %d", filepath.Base(shell), m.seq),
		Cwd:        cwd,
		Shell:      shell,
		Created:    time.Now(),
		cmd:        cmd,
		ptmx:       ptmx,
		clients:    map[*termClient]struct{}{},
		readerDone: make(chan struct{}),
	}
	m.sessions[s.ID] = s
	go s.readLoop()
	go s.waitLoop()
	return s, nil
}

func (m *TermManager) Close(id string) bool {
	m.mu.Lock()
	s := m.sessions[id]
	delete(m.sessions, id)
	m.mu.Unlock()
	if s == nil {
		return false
	}
	s.terminate()
	return true
}

// ---------------- 本机访问保护 ----------------

func isLoopbackHost(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func isSameOrigin(origin, host string) bool {
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return (u.Scheme == "http" || u.Scheme == "https") && strings.EqualFold(u.Host, host)
}

// localOnly 保护所有 /api/ 接口只能由本机 Mobile Studio 页面调用：
//   - Host 必须是回环地址：防御 DNS 重绑定（恶意域名解析到 127.0.0.1）
//   - Sec-Fetch-Site / Origin 必须同源：防御浏览器中其它网站发起的跨站请求
//
// 服务只监听 127.0.0.1，但手机浏览器中打开的任意网页都能向 127.0.0.1 发请求，
// 而终端、Git 克隆 + Gradle 构建等接口可以执行任意命令，因此必须拦截跨站访问。
// curl 等非浏览器客户端不带这些头，不受影响。
func localOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			if !isLoopbackHost(r.Host) {
				http.Error(w, "forbidden: non-local host", http.StatusForbidden)
				return
			}
			if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
				http.Error(w, "forbidden: cross-site request", http.StatusForbidden)
				return
			}
			if origin := r.Header.Get("Origin"); origin != "" && !isSameOrigin(origin, r.Host) {
				http.Error(w, "forbidden: cross-origin request", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// ---------------- HTTP 接口 ----------------

// GET /api/term/list
func handleTermList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "sessions": terms.List()})
}

// GET /api/term/create?cwd=&cols=&rows=
func handleTermCreate(w http.ResponseWriter, r *http.Request) {
	cwd := strings.TrimSpace(r.URL.Query().Get("cwd"))
	dir := ""
	if cwd != "" {
		if abs, err := resolveProjectRoot(cwd); err == nil {
			dir = abs
		}
	}
	if dir == "" {
		dir, _ = os.UserHomeDir()
	}
	cols, _ := strconv.Atoi(r.URL.Query().Get("cols"))
	rows, _ := strconv.Atoi(r.URL.Query().Get("rows"))
	s, err := terms.Create(dir, cols, rows)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "session": s.Info()})
}

// GET /api/term/close?id=
func handleTermClose(w http.ResponseWriter, r *http.Request) {
	if !terms.Close(r.URL.Query().Get("id")) {
		writeErr(w, http.StatusNotFound, "终端会话不存在")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

type termClientMsg struct {
	Type string `json:"type"`
	Data string `json:"data"`
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
}

// GET /api/term/ws?id=   WebSocket：二进制帧下行终端输出，文本帧 JSON 上行输入与窗口尺寸
func handleTermWS(w http.ResponseWriter, r *http.Request) {
	// WebSocket 握手必定携带 Origin，这里强制要求同源（localOnly 已校验格式）
	if origin := r.Header.Get("Origin"); origin == "" || !isSameOrigin(origin, r.Host) {
		http.Error(w, "forbidden: origin required", http.StatusForbidden)
		return
	}
	s := terms.Get(r.URL.Query().Get("id"))
	if s == nil {
		http.Error(w, "terminal session not found", http.StatusNotFound)
		return
	}
	ws, err := wsUpgrade(w, r)
	if err != nil {
		return
	}
	c := s.attach(ws)

	// 写协程：按顺序发送输出与控制消息，并定期 ping 以尽早发现断开的连接
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		ping := time.NewTicker(25 * time.Second)
		defer ping.Stop()
		for {
			select {
			case m, ok := <-c.ch:
				if !ok {
					ws.Close(1000, "session closed")
					return
				}
				var err error
				if m.bin != nil {
					err = ws.WriteBinary(m.bin)
				} else {
					err = ws.WriteText(m.text)
				}
				if err != nil {
					ws.conn.Close()
					return
				}
			case <-ping.C:
				if ws.Ping() != nil {
					ws.conn.Close()
					return
				}
			}
		}
	}()

	for {
		op, data, err := ws.ReadMessage()
		if err != nil {
			break
		}
		if op != wsOpText {
			continue
		}
		var msg termClientMsg
		if json.Unmarshal(data, &msg) != nil {
			continue
		}
		switch msg.Type {
		case "input":
			if msg.Data != "" {
				s.ptmx.Write([]byte(msg.Data))
			}
		case "resize":
			setPTYSize(s.ptmx, msg.Cols, msg.Rows)
		}
	}
	s.detach(c)
	<-writerDone
	ws.conn.Close()
}
