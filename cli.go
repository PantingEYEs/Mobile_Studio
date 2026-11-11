package main

// Mobile Studio 命令行接口：m_s <命令> [参数]
//
// 除 config / help / version 外，所有命令都通过 HTTP 调用正在运行的本地服务，
// 因此 CLI 发起的构建、终端会话等会实时出现在 Web UI 中（反之亦然）。
//
// 面向 Agent 的约定：
//   - 任意命令加 --json 输出机器可读结果；流式命令输出 NDJSON（每行一个 JSON 对象）
//   - 退出码：0 成功；1 执行失败；2 用法错误；3 服务未运行；130 任务被停止 / 中断
//   - 长任务默认跟随日志直到结束，--detach 仅启动并返回任务 ID

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"
)

const (
	exitOK      = 0
	exitFail    = 1
	exitUsage   = 2
	exitDown    = 3
	exitStopped = 130
)

var cliValueFlags = map[string]bool{
	"p": true, "project": true, "pkg": true, "dir": true, "type": true, "lang": true,
	"min-sdk": true, "target-sdk": true, "compile-sdk": true, "template": true, "name": true,
	"cwd": true, "tail": true, "limit": true, "shell": true,
}

var cliBoolFlags = map[string]bool{
	"json": true, "detach": true, "follow": true, "f": true, "all": true, "refresh": true,
	"enter": true, "ctrl-c": true, "raw": true, "folder": true, "help": true, "h": true,
}

type cliCtx struct {
	json  bool
	flags map[string]string
	pos   []string
}

type usageError struct{ msg string }

func (e usageError) Error() string { return e.msg }

var errServerDown = errors.New("Mobile Studio 服务未运行（执行 m_s start 启动）")

func parseCLIArgs(args []string) (*cliCtx, error) {
	c := &cliCtx{flags: map[string]string{}}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			c.pos = append(c.pos, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(a, "-") || a == "-" || (len(a) > 1 && a[1] >= '0' && a[1] <= '9') {
			c.pos = append(c.pos, a)
			continue
		}
		name := strings.TrimLeft(a, "-")
		val, hasVal := "", false
		if k, v, ok := strings.Cut(name, "="); ok {
			name, val, hasVal = k, v, true
		}
		switch {
		case cliValueFlags[name]:
			if !hasVal {
				if i+1 >= len(args) {
					return nil, usageError{fmt.Sprintf("参数 --%s 需要一个值", name)}
				}
				i++
				val = args[i]
			}
			c.flags[name] = val
		case cliBoolFlags[name]:
			if !hasVal {
				val = "true"
			}
			c.flags[name] = val
		default:
			return nil, usageError{fmt.Sprintf("未知参数 %s（m_s help 查看用法）", a)}
		}
	}
	c.json = c.flag("json") == "true"
	return c, nil
}

func (c *cliCtx) flag(names ...string) string {
	for _, n := range names {
		if v, ok := c.flags[n]; ok {
			return v
		}
	}
	return ""
}

func (c *cliCtx) has(names ...string) bool { return c.flag(names...) == "true" }

func (c *cliCtx) arg(i int) string {
	if i < len(c.pos) {
		return c.pos[i]
	}
	return ""
}

// project 解析目标项目：--project/-p > 当前目录所属的已注册项目 > 当前目录。
func (c *cliCtx) project() string {
	if p := c.flag("project", "p"); p != "" {
		return canonicalPath(p)
	}
	wd, _ := os.Getwd()
	if p := findProjectFor(wd); p != "" {
		return p
	}
	return wd
}

// ---------------- 输出 ----------------

func (c *cliCtx) printJSON(v interface{}) {
	b, _ := json.MarshalIndent(v, "", "  ")
	fmt.Println(string(b))
}

func (c *cliCtx) fail(err error) int {
	code := exitFail
	var ue usageError
	switch {
	case errors.As(err, &ue):
		code = exitUsage
	case errors.Is(err, errServerDown):
		code = exitDown
	}
	if c != nil && c.json {
		c.printJSON(map[string]interface{}{"success": false, "error": err.Error(), "exitCode": code})
	} else {
		fmt.Fprintln(os.Stderr, "m_s: "+err.Error())
	}
	return code
}

func (c *cliCtx) ok(jsonVal interface{}, human string) int {
	if c.json {
		c.printJSON(jsonVal)
	} else if human != "" {
		fmt.Println(human)
	}
	return exitOK
}

func newTable() *tabwriter.Writer { return tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0) }

// ---------------- HTTP ----------------

func baseURL() string { return fmt.Sprintf("http://127.0.0.1:%d", cfgInt("server.port")) }

func apiRaw(method, path string, q url.Values, body io.Reader, timeout time.Duration) ([]byte, int, error) {
	u := baseURL() + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequest(method, u, body)
	if err != nil {
		return nil, 0, err
	}
	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		var ne *net.OpError
		if errors.As(err, &ne) && ne.Op == "dial" {
			return nil, 0, errServerDown
		}
		return nil, 0, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	return b, resp.StatusCode, err
}

func apiErrorFrom(b []byte, status int) error {
	var env struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(b, &env) == nil && env.Error != "" {
		return errors.New(env.Error)
	}
	msg := strings.TrimSpace(string(b))
	if msg == "" {
		msg = http.StatusText(status)
	}
	return fmt.Errorf("HTTP %d: %s", status, msg)
}

// api 发起 GET 请求，校验 success 字段并把响应解码到 out。
func api(path string, q url.Values, out interface{}, timeout time.Duration) error {
	b, status, err := apiRaw(http.MethodGet, path, q, nil, timeout)
	if err != nil {
		return err
	}
	var env struct {
		Success *bool `json:"success"`
	}
	json.Unmarshal(b, &env)
	if status >= 400 || (env.Success != nil && !*env.Success) {
		return apiErrorFrom(b, status)
	}
	if out != nil {
		if err := json.Unmarshal(b, out); err != nil {
			return fmt.Errorf("无法解析服务响应: %v", err)
		}
	}
	return nil
}

func vals(kv ...string) url.Values {
	q := url.Values{}
	for i := 0; i+1 < len(kv); i += 2 {
		if kv[i+1] != "" {
			q.Set(kv[i], kv[i+1])
		}
	}
	return q
}

// ---------------- 任务跟随 ----------------

func statusExitCode(s TaskStatus) int {
	switch s {
	case TaskSuccess, TaskRunning:
		return exitOK
	case TaskStopped:
		return exitStopped
	default:
		return exitFail
	}
}

// followTask 通过 SSE 输出任务日志；follow=false 时只输出当前快照。
// stopOnInterrupt 为 true 时 Ctrl-C 会停止服务端任务（与普通命令行为一致）。
func followTask(c *cliCtx, id string, follow, stopOnInterrupt, quiet bool) int {
	q := vals("id", id)
	if !follow {
		q.Set("follow", "0")
	}
	req, _ := http.NewRequest(http.MethodGet, baseURL()+"/api/task/stream?"+q.Encode(), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		var ne *net.OpError
		if errors.As(err, &ne) && ne.Op == "dial" {
			return c.fail(errServerDown)
		}
		return c.fail(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return c.fail(apiErrorFrom(b, resp.StatusCode))
	}

	if follow {
		sig := make(chan os.Signal, 2)
		signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
		defer signal.Stop(sig)
		go func() {
			<-sig
			if stopOnInterrupt {
				fmt.Fprintln(os.Stderr, "\nm_s: 正在停止任务 "+id+" ...（再次 Ctrl-C 直接退出）")
				api("/api/task/stop", vals("id", id), nil, 10*time.Second)
				<-sig
			}
			os.Exit(exitStopped)
		}()
	}

	var last TaskInfo
	enc := json.NewEncoder(os.Stdout)
	br := bufio.NewReaderSize(resp.Body, 64*1024)
	event := ""
	for {
		line, err := br.ReadString('\n')
		line = strings.TrimRight(line, "\r\n")
		switch {
		case strings.HasPrefix(line, "event: "):
			event = line[7:]
		case strings.HasPrefix(line, "data: "):
			data := line[6:]
			switch event {
			case "log":
				var lines []string
				json.Unmarshal([]byte(data), &lines)
				if quiet {
					break
				}
				for _, l := range lines {
					if c.json {
						enc.Encode(map[string]string{"type": "log", "line": l})
					} else {
						fmt.Println(l)
					}
				}
			case "status":
				json.Unmarshal([]byte(data), &last)
			}
		case line == "" && event == "end":
			err = io.EOF
		}
		if err != nil {
			break
		}
	}

	if c.json {
		enc.Encode(map[string]interface{}{"type": "status", "task": last})
	} else if last.ID != "" {
		label := map[TaskStatus]string{TaskRunning: "运行中", TaskSuccess: "成功", TaskFailed: "失败", TaskStopped: "已停止"}[last.Status]
		fmt.Fprintf(os.Stderr, "m_s: 任务 %s（%s）%s\n", last.ID, kindLabel(last.Kind), label)
	}
	return statusExitCode(last.Status)
}

// startTask 启动后台任务：默认跟随日志至结束，--detach 时立即返回任务 ID。
func startTask(c *cliCtx, endpoint string, q url.Values) int {
	b, status, err := apiRaw(http.MethodGet, endpoint, q, nil, 60*time.Second)
	if err != nil {
		return c.fail(err)
	}
	var resp struct {
		Success bool      `json:"success"`
		Error   string    `json:"error"`
		Task    *TaskInfo `json:"task"`
	}
	if json.Unmarshal(b, &resp) != nil {
		return c.fail(apiErrorFrom(b, status))
	}
	if !resp.Success {
		if resp.Task != nil {
			if c.json {
				c.printJSON(map[string]interface{}{"success": false, "error": resp.Error, "task": resp.Task, "exitCode": exitFail})
				return exitFail
			}
			fmt.Fprintf(os.Stderr, "m_s: %s：%s\n      查看日志: m_s task log %s -f\n", resp.Error, resp.Task.ID, resp.Task.ID)
			return exitFail
		}
		return c.fail(apiErrorFrom(b, status))
	}
	if c.has("detach") {
		return c.ok(map[string]interface{}{"success": true, "task": resp.Task},
			fmt.Sprintf("任务已启动: %s\n查看日志: m_s task log %s -f", resp.Task.ID, resp.Task.ID))
	}
	return followTask(c, resp.Task.ID, true, true, false)
}

// ---------------- 终端输出清洗 ----------------

var ansiRe = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|\x1b[()][0-9A-Za-z]|\x1b[=>78NOM]`)

// plainTerminalText 去除 ANSI 转义，并按回车 / 退格近似还原屏幕上的文本行。
func plainTerminalText(raw []byte) []string {
	s := ansiRe.ReplaceAllString(string(raw), "")
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if i := strings.LastIndex(strings.TrimRight(line, "\r"), "\r"); i >= 0 {
			line = line[i+1:]
		}
		line = strings.TrimRight(line, "\r")
		if strings.ContainsRune(line, '\b') {
			var rs []rune
			for _, r := range line {
				if r == '\b' {
					if len(rs) > 0 {
						rs = rs[:len(rs)-1]
					}
					continue
				}
				rs = append(rs, r)
			}
			line = string(rs)
		}
		out = append(out, strings.Map(func(r rune) rune {
			if r < 0x20 && r != '\t' {
				return -1
			}
			return r
		}, line))
	}
	return out
}

func resolveTermID(id string) (string, error) {
	if id != "" && id != "last" {
		return id, nil
	}
	var resp struct {
		Sessions []TermInfo `json:"sessions"`
	}
	if err := api("/api/term/list", nil, &resp, 10*time.Second); err != nil {
		return "", err
	}
	if len(resp.Sessions) == 0 {
		return "", errors.New("没有终端会话（m_s term new 新建）")
	}
	if id == "" && len(resp.Sessions) > 1 {
		return "", usageError{"存在多个终端会话，请指定 ID 或使用 last（m_s term list 查看）"}
	}
	return resp.Sessions[len(resp.Sessions)-1].ID, nil
}

// ---------------- 命令实现 ----------------

func runCLI(args []string) int {
	if len(args) == 0 {
		fmt.Print(cliHelp)
		return exitOK
	}
	cmd := args[0]
	c, err := parseCLIArgs(args[1:])
	if err != nil {
		return (&cliCtx{}).fail(err)
	}
	if c.has("help", "h") {
		fmt.Print(cliHelp)
		return exitOK
	}

	switch cmd {
	case "help", "-h", "--help":
		fmt.Print(cliHelp)
		return exitOK
	case "version", "--version", "-v":
		return c.ok(map[string]string{"version": msVersion}, "Mobile Studio "+msVersion)
	case "status":
		return cmdStatus(c)
	case "config":
		return cmdConfig(c)
	case "project", "projects":
		return cmdProject(c)
	case "fs":
		return cmdFS(c)
	case "git":
		return cmdGit(c)
	case "android":
		if len(c.pos) == 0 {
			return c.fail(usageError{"用法: m_s android env|build|install|launch|run|stop|logcat"})
		}
		sub := c.pos[0]
		c.pos = c.pos[1:]
		return cmdAndroid(c, sub)
	case "env", "build", "install", "launch", "run", "logcat":
		return cmdAndroid(c, cmd)
	case "task", "tasks":
		return cmdTask(c)
	case "term", "terminal":
		return cmdTerm(c)
	case "start", "stop", "restart", "open", "logs", "rebuild", ":q":
		return c.fail(usageError{fmt.Sprintf("「%s」由启动脚本处理，请通过 m_s %s 执行", cmd, cmd)})
	default:
		return c.fail(usageError{fmt.Sprintf("未知命令 %q（m_s help 查看全部命令）", cmd)})
	}
}

func cmdStatus(c *cliCtx) int {
	var st struct {
		Version      string `json:"version"`
		Pid          int    `json:"pid"`
		Port         int    `json:"port"`
		Root         string `json:"root"`
		ConfigDir    string `json:"configDir"`
		StartedAt    int64  `json:"startedAt"`
		TasksRunning int    `json:"tasksRunning"`
		Terminals    int    `json:"terminals"`
	}
	if err := api("/api/status", nil, &st, 5*time.Second); err != nil {
		if errors.Is(err, errServerDown) {
			if c.json {
				c.printJSON(map[string]interface{}{"running": false, "port": cfgInt("server.port")})
			} else {
				fmt.Printf("未运行（端口 %d）\n", cfgInt("server.port"))
			}
			return exitDown
		}
		return c.fail(err)
	}
	if c.json {
		c.printJSON(map[string]interface{}{"running": true, "status": st, "url": baseURL()})
		return exitOK
	}
	up := time.Since(time.UnixMilli(st.StartedAt)).Round(time.Second)
	fmt.Printf("运行中  %s\n版本     %s\nPID      %d\n运行时长 %s\n项目根   %s\n配置目录 %s\n运行任务 %d\n终端会话 %d\n",
		baseURL(), st.Version, st.Pid, up, st.Root, st.ConfigDir, st.TasksRunning, st.Terminals)
	return exitOK
}

func cmdConfig(c *cliCtx) int {
	sub := c.arg(0)
	switch sub {
	case "", "list", "ls":
		entries := listConfig()
		if c.json {
			c.printJSON(entries)
			return exitOK
		}
		tw := newTable()
		fmt.Fprintln(tw, "KEY\tVALUE\tDESCRIPTION")
		for _, e := range entries {
			v := fmt.Sprint(e.Value)
			if v == "" {
				v = "(自动)"
			}
			if e.IsSet {
				v += " *"
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\n", e.Key, v, e.Desc)
		}
		tw.Flush()
		fmt.Println("\n* 为已修改的值；配置文件: " + configPath())
		return exitOK
	case "get":
		key := c.arg(1)
		k := findConfigKey(key)
		if k == nil {
			return c.fail(usageError{fmt.Sprintf("未知配置项 %q（m_s config list 查看全部）", key)})
		}
		v := cfgValue(key)
		if f, ok := v.(float64); ok && k.Type == "int" {
			v = int(f)
		}
		return c.ok(map[string]interface{}{"key": key, "value": v}, fmt.Sprint(v))
	case "set":
		key, raw := c.arg(1), c.arg(2)
		if key == "" || len(c.pos) < 3 {
			return c.fail(usageError{"用法: m_s config set <key> <value>"})
		}
		v, err := setConfig(key, raw)
		if err != nil {
			return c.fail(err)
		}
		msg := fmt.Sprintf("%s = %v", key, v)
		if key == "server.port" {
			msg += "\n端口修改需执行 m_s restart 后生效"
		}
		return c.ok(map[string]interface{}{"success": true, "key": key, "value": v}, msg)
	case "unset", "reset":
		key := c.arg(1)
		if err := unsetConfig(key); err != nil {
			return c.fail(err)
		}
		return c.ok(map[string]interface{}{"success": true, "key": key}, key+" 已恢复默认值")
	case "path":
		return c.ok(map[string]string{"path": configPath(), "dir": configDir()}, configPath())
	default:
		return c.fail(usageError{"用法: m_s config list|get <key>|set <key> <value>|unset <key>|path"})
	}
}

func cmdProject(c *cliCtx) int {
	sub := c.arg(0)
	switch sub {
	case "", "list", "ls":
		var resp struct {
			Projects []ProjectView `json:"projects"`
		}
		if err := api("/api/projects", nil, &resp, 10*time.Second); err != nil {
			return c.fail(err)
		}
		if c.json {
			c.printJSON(resp.Projects)
			return exitOK
		}
		cur := c.project()
		tw := newTable()
		fmt.Fprintln(tw, "\tNAME\tPATH\t")
		for _, p := range resp.Projects {
			mark, note := " ", ""
			if p.Path == cur {
				mark = "*"
			}
			if p.Self {
				note = "(Mobile Studio)"
			}
			if !p.Exists {
				note = "(目录不存在)"
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", mark, p.Name, p.Path, note)
		}
		tw.Flush()
		return exitOK
	case "current":
		p := c.project()
		return c.ok(map[string]string{"path": p, "name": filepath.Base(p)}, p)
	case "create", "new":
		name := c.arg(1)
		if name == "" {
			return c.fail(usageError{"用法: m_s project create <名称> [--type android|blank] [--dir 父目录] [--pkg 包名] [--lang kotlin|java] [--min-sdk N] [--target-sdk N] [--compile-sdk N] [--template empty_views|trampoline|none]"})
		}
		dir := c.flag("dir")
		if dir != "" {
			dir = canonicalPath(dir)
		}
		typ := c.flag("type")
		if typ == "" {
			typ = "blank"
		}
		q := vals("name", name, "dir", dir, "type", typ, "pkg", c.flag("pkg"), "lang", c.flag("lang"),
			"minSdk", c.flag("min-sdk"), "targetSdk", c.flag("target-sdk"), "compileSdk", c.flag("compile-sdk"),
			"template", c.flag("template"))
		var resp Project
		if err := api("/api/fs/project/create", q, &resp, 60*time.Second); err != nil {
			return c.fail(err)
		}
		return c.ok(map[string]interface{}{"success": true, "project": resp}, "已创建并登记: "+resp.Path)
	case "import", "add":
		p := c.arg(1)
		if p == "" {
			return c.fail(usageError{"用法: m_s project import <目录>"})
		}
		var resp Project
		if err := api("/api/fs/project/import", vals("path", canonicalPath(p)), &resp, 30*time.Second); err != nil {
			return c.fail(err)
		}
		return c.ok(map[string]interface{}{"success": true, "project": resp}, "已登记: "+resp.Path)
	case "clone":
		u := c.arg(1)
		if u == "" {
			return c.fail(usageError{"用法: m_s project clone <仓库地址> [--dir 父目录]"})
		}
		dir := c.flag("dir")
		if dir != "" {
			dir = canonicalPath(dir)
		}
		if !c.json {
			fmt.Fprintln(os.Stderr, "m_s: 正在克隆 "+u+" ...")
		}
		var resp Project
		if err := api("/api/fs/project/clone", vals("url", u, "dir", dir), &resp, 30*time.Minute); err != nil {
			return c.fail(err)
		}
		return c.ok(map[string]interface{}{"success": true, "project": resp}, "已克隆并登记: "+resp.Path)
	case "remove", "rm":
		ref := c.arg(1)
		if ref == "" {
			return c.fail(usageError{"用法: m_s project remove <名称|路径>（仅移出列表，不删除文件）"})
		}
		if ref == "." || strings.ContainsAny(ref, "/~") {
			ref = canonicalPath(ref)
		}
		var resp struct {
			Project Project `json:"project"`
		}
		if err := api("/api/projects/remove", vals("path", ref), &resp, 10*time.Second); err != nil {
			return c.fail(err)
		}
		return c.ok(map[string]interface{}{"success": true, "project": resp.Project}, "已移出列表（磁盘文件保留）: "+resp.Project.Path)
	default:
		return c.fail(usageError{"用法: m_s project list|current|create|import|clone|remove"})
	}
}

func cmdFS(c *cliCtx) int {
	root := c.project()
	switch c.arg(0) {
	case "ls", "list", "":
		var resp struct {
			Children []FileNode `json:"children"`
		}
		if err := api("/api/fs/tree", vals("root", root, "path", c.arg(1)), &resp, 10*time.Second); err != nil {
			return c.fail(err)
		}
		if c.json {
			c.printJSON(resp.Children)
			return exitOK
		}
		for _, n := range resp.Children {
			if n.IsDir {
				fmt.Println(n.Name + "/")
			} else {
				fmt.Println(n.Name)
			}
		}
		return exitOK
	case "new", "create":
		p := c.arg(1)
		if p == "" {
			return c.fail(usageError{"用法: m_s fs new <相对路径> [--folder]"})
		}
		typ := "file"
		if c.has("folder") {
			typ = "dir"
		}
		if err := api("/api/fs/create", vals("root", root, "path", p, "type", typ), nil, 10*time.Second); err != nil {
			return c.fail(err)
		}
		return c.ok(map[string]interface{}{"success": true, "path": p, "type": typ}, "已创建: "+filepath.Join(root, p))
	case "rename", "mv":
		p, n := c.arg(1), c.arg(2)
		if p == "" || n == "" {
			return c.fail(usageError{"用法: m_s fs rename <相对路径> <新名称>"})
		}
		if err := api("/api/fs/rename", vals("root", root, "oldPath", p, "newName", n), nil, 10*time.Second); err != nil {
			return c.fail(err)
		}
		return c.ok(map[string]interface{}{"success": true, "path": p, "newName": n}, "已重命名为: "+n)
	default:
		return c.fail(usageError{"用法: m_s fs ls [路径] | new <路径> [--folder] | rename <路径> <新名称>"})
	}
}

func cmdGit(c *cliCtx) int {
	switch c.arg(0) {
	case "graph", "log", "":
		var g GraphResponse
		if err := api("/api/git/graph", vals("root", c.project()), &g, 30*time.Second); err != nil {
			return c.fail(err)
		}
		limit := 20
		if v := c.flag("limit"); v != "" {
			limit, _ = strconv.Atoi(v)
		}
		if limit > 0 && len(g.Commits) > limit {
			g.Commits = g.Commits[:limit]
		}
		if c.json {
			c.printJSON(g)
			return exitOK
		}
		if len(g.Commits) == 0 {
			fmt.Println("该项目暂无 Git 提交记录（不是 Git 仓库或尚未提交）")
			return exitOK
		}
		fmt.Printf("当前分支: %s\n分支: %s\n\n", g.CurrentBranch, strings.Join(g.Branches, ", "))
		for _, cm := range g.Commits {
			refs := ""
			if len(cm.Refs) > 0 {
				refs = " (" + strings.Join(cm.Refs, ", ") + ")"
			}
			merge := " "
			if len(cm.Parents) > 1 {
				merge = "M"
			}
			fmt.Printf("%s %s%s %s  — %s, %s\n", merge, cm.ShortHash, refs, cm.Message, cm.Author, cm.Date)
		}
		return exitOK
	default:
		return c.fail(usageError{"用法: m_s git graph [--limit N]"})
	}
}

func cmdAndroid(c *cliCtx, sub string) int {
	root := c.project()
	switch sub {
	case "env":
		q := vals("root", root)
		if c.has("refresh") {
			q.Set("refresh", "1")
		}
		var rep EnvReport
		if err := api("/api/android/env", q, &rep, 30*time.Second); err != nil {
			return c.fail(err)
		}
		code := exitOK
		if len(rep.Problems) > 0 {
			code = exitFail
		}
		if c.json {
			c.printJSON(rep)
			return code
		}
		show := func(label string, t *ToolInfo) {
			if t == nil {
				fmt.Printf("%-8s ✗ 未找到\n", label)
			} else {
				fmt.Printf("%-8s ✓ %s  [%s]\n", label, t.Version, t.Path)
			}
		}
		fmt.Printf("平台     %s%s\n", rep.Arch, map[bool]string{true: " (Termux)"}[rep.Termux])
		show("JDK", rep.Java)
		show("Gradle", rep.Gradle)
		if rep.Sdk != nil {
			fmt.Printf("SDK      ✓ %s（%s）platforms: %s\n", rep.Sdk.Path, rep.Sdk.Source, strings.Join(rep.Sdk.Platforms, ", "))
		} else {
			fmt.Println("SDK      ✗ 未找到")
		}
		if rep.Termux || rep.Aapt2 != "" {
			fmt.Printf("aapt2    %s\n", map[bool]string{true: "✓ " + rep.Aapt2, false: "✗ 未找到"}[rep.Aapt2 != ""])
		}
		fmt.Printf("设备通道 %s — %s\n", rep.Shell.Mode, rep.Shell.Detail)
		if rep.Project.IsAndroid && rep.Project.AppModule != "" {
			fmt.Printf("项目     %s  模块 :%s  包名 %s  compileSdk %s\n", root, rep.Project.AppModule, rep.Project.ApplicationID, rep.Project.CompileSdk)
		} else {
			fmt.Printf("项目     %s（非 Android 应用项目）\n", root)
		}
		for _, p := range rep.Problems {
			fmt.Println("✗ " + p)
		}
		for _, h := range rep.Hints {
			fmt.Println("ⓘ " + h)
		}
		return code
	case "build", "install", "launch", "run":
		q := vals("root", root, "pkg", c.flag("pkg"), "shell", c.flag("shell"))
		return startTask(c, "/api/android/"+sub, q)
	case "logcat":
		q := vals("root", root, "pkg", c.flag("pkg"), "shell", c.flag("shell"))
		if c.has("all") {
			q.Set("all", "1")
		}
		return startTask(c, "/api/android/logcat", q)
	case "stop":
		q := vals("root", root, "pkg", c.flag("pkg"), "shell", c.flag("shell"))
		if err := api("/api/android/stop", q, nil, 15*time.Second); err != nil {
			return c.fail(err)
		}
		return c.ok(map[string]interface{}{"success": true}, "应用已强制停止")
	default:
		return c.fail(usageError{"用法: m_s android env|build|install|launch|run|stop|logcat"})
	}
}

func cmdTask(c *cliCtx) int {
	switch c.arg(0) {
	case "", "list", "ls":
		root := ""
		if !c.has("all") {
			root = c.project()
		}
		var resp struct {
			Tasks []TaskInfo `json:"tasks"`
		}
		if err := api("/api/task/active", vals("root", root), &resp, 10*time.Second); err != nil {
			return c.fail(err)
		}
		if c.json {
			c.printJSON(resp.Tasks)
			return exitOK
		}
		if len(resp.Tasks) == 0 {
			fmt.Println("没有任务（--all 查看所有项目）")
			return exitOK
		}
		tw := newTable()
		fmt.Fprintln(tw, "ID\tKIND\tSTATUS\tSTARTED\tPROJECT")
		for _, t := range resp.Tasks {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", t.ID, t.Kind, t.Status, time.UnixMilli(t.StartedAt).Format("15:04:05"), t.Root)
		}
		tw.Flush()
		return exitOK
	case "log", "logs":
		id := c.arg(1)
		if id == "" {
			return c.fail(usageError{"用法: m_s task log <任务ID> [-f]"})
		}
		return followTask(c, id, c.has("f", "follow"), false, false)
	case "status":
		id := c.arg(1)
		if id == "" {
			return c.fail(usageError{"用法: m_s task status <任务ID>"})
		}
		return followTask(c, id, false, false, true)
	case "stop", "cancel":
		id := c.arg(1)
		if id == "" {
			return c.fail(usageError{"用法: m_s task stop <任务ID>"})
		}
		if err := api("/api/task/stop", vals("id", id), nil, 10*time.Second); err != nil {
			return c.fail(err)
		}
		return c.ok(map[string]interface{}{"success": true, "id": id}, "已请求停止任务 "+id)
	default:
		return c.fail(usageError{"用法: m_s task list [--all] | log <ID> [-f] | status <ID> | stop <ID>"})
	}
}

func cmdTerm(c *cliCtx) int {
	switch c.arg(0) {
	case "", "list", "ls":
		var resp struct {
			Sessions []TermInfo `json:"sessions"`
		}
		if err := api("/api/term/list", nil, &resp, 10*time.Second); err != nil {
			return c.fail(err)
		}
		if c.json {
			c.printJSON(resp.Sessions)
			return exitOK
		}
		if len(resp.Sessions) == 0 {
			fmt.Println("没有终端会话（m_s term new 新建）")
			return exitOK
		}
		tw := newTable()
		fmt.Fprintln(tw, "ID\tTITLE\tSTATE\tVIEWERS\tCWD")
		for _, s := range resp.Sessions {
			state := "运行中"
			if s.Exited {
				state = fmt.Sprintf("已退出(%d)", s.ExitCode)
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\n", s.ID, s.Title, state, s.Clients, s.Cwd)
		}
		tw.Flush()
		return exitOK
	case "new", "create":
		cwd := c.flag("cwd")
		if cwd != "" {
			cwd = canonicalPath(cwd)
		} else {
			cwd = c.project()
		}
		var resp struct {
			Session TermInfo `json:"session"`
		}
		if err := api("/api/term/create", vals("cwd", cwd, "cols", "120", "rows", "40"), &resp, 15*time.Second); err != nil {
			return c.fail(err)
		}
		return c.ok(map[string]interface{}{"success": true, "session": resp.Session}, resp.Session.ID)
	case "send", "input":
		id, err := resolveTermID(c.arg(1))
		if err != nil {
			return c.fail(err)
		}
		text := strings.Join(c.pos[min(2, len(c.pos)):], " ")
		data := ""
		if c.has("ctrl-c") {
			data += "\x03"
		}
		data += text
		if c.has("enter") {
			data += "\r"
		}
		if data == "" {
			return c.fail(usageError{"用法: m_s term send <ID|last> <文本> [--enter] [--ctrl-c]"})
		}
		b, status, err := apiRaw(http.MethodPost, "/api/term/input", vals("id", id), strings.NewReader(data), 10*time.Second)
		if err != nil {
			return c.fail(err)
		}
		if status >= 400 {
			return c.fail(apiErrorFrom(b, status))
		}
		return c.ok(map[string]interface{}{"success": true, "id": id, "bytes": len(data)}, "")
	case "read", "output":
		id, err := resolveTermID(c.arg(1))
		if err != nil {
			return c.fail(err)
		}
		b, status, err := apiRaw(http.MethodGet, "/api/term/read", vals("id", id), nil, 10*time.Second)
		if err != nil {
			return c.fail(err)
		}
		if status >= 400 {
			return c.fail(apiErrorFrom(b, status))
		}
		if c.has("raw") {
			os.Stdout.Write(b)
			return exitOK
		}
		lines := plainTerminalText(b)
		for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
			lines = lines[:len(lines)-1]
		}
		tail := 40
		if v := c.flag("tail"); v != "" {
			tail, _ = strconv.Atoi(v)
		}
		if tail > 0 && len(lines) > tail {
			lines = lines[len(lines)-tail:]
		}
		if c.json {
			c.printJSON(map[string]interface{}{"id": id, "lines": lines})
			return exitOK
		}
		fmt.Println(strings.Join(lines, "\n"))
		return exitOK
	case "close", "kill":
		id, err := resolveTermID(c.arg(1))
		if err != nil {
			return c.fail(err)
		}
		if err := api("/api/term/close", vals("id", id), nil, 10*time.Second); err != nil {
			return c.fail(err)
		}
		return c.ok(map[string]interface{}{"success": true, "id": id}, "已关闭终端 "+id)
	default:
		return c.fail(usageError{"用法: m_s term list | new [--cwd 目录] | send <ID|last> <文本> [--enter] [--ctrl-c] | read <ID|last> [--tail N] [--raw] | close <ID|last>"})
	}
}

const cliHelp = `Mobile Studio 命令行 (m_s)

用法: m_s [命令] [参数] [--json]

服务（由启动脚本处理）
  m_s                         启动服务并打开浏览器（按 server.open_browser 配置）
  m_s start [--no-browser]    启动服务
  m_s stop | m_s :q           停止服务
  m_s restart                 重启服务（代码或端口变更后使用）
  m_s status                  服务状态（未运行时退出码 3）
  m_s open                    在浏览器中打开 Web UI
  m_s logs [-f]               查看服务日志
  m_s rebuild                 强制重新构建前端与后端

项目（默认作用于当前目录所属的已登记项目，可用 -p/--project <目录> 指定）
  m_s project list            列出已登记项目（* 为当前项目）
  m_s project current         显示当前目录对应的项目
  m_s project create <名称> [--type android|blank] [--dir 父目录]
        [--pkg 包名] [--lang kotlin|java] [--min-sdk N] [--target-sdk N]
        [--compile-sdk N] [--template empty_views|trampoline|none]
  m_s project import <目录>   登记已有目录
  m_s project clone <地址> [--dir 父目录]
  m_s project remove <名称|路径>  移出列表（不删除文件）

文件与 Git
  m_s fs ls [相对路径]
  m_s fs new <相对路径> [--folder]
  m_s fs rename <相对路径> <新名称>
  m_s git graph [--limit N]   提交拓扑（--limit 0 输出全部）

Android（亦可写作 m_s android <子命令>）
  m_s env [--refresh]         构建环境与设备通道检查（存在问题时退出码 1）
  m_s build                   assembleDebug
  m_s install                 安装最近一次构建的 APK
  m_s launch [--pkg 包名]     启动应用
  m_s run [--pkg 包名]        构建 → 安装 → 启动
  m_s android stop [--pkg]    强制停止应用
  m_s logcat [--pkg 包名] [--all]   应用日志（Ctrl-C 停止）
    以上任务命令支持 --detach（立即返回任务 ID）与 --shell rish|adb|local

任务
  m_s task list [--all]       当前项目（或全部）任务
  m_s task log <ID> [-f]      输出任务日志，-f 持续跟随
  m_s task status <ID>        任务状态（退出码反映结果）
  m_s task stop <ID>          停止任务

终端（与 Web UI 中的终端标签页共享）
  m_s term list
  m_s term new [--cwd 目录]   新建会话并输出 ID
  m_s term send <ID|last> <文本> [--enter] [--ctrl-c]
  m_s term read <ID|last> [--tail N] [--raw]   读取输出（默认去除 ANSI，末 40 行）
  m_s term close <ID|last>

配置（~/.mobile_studio/config.json）
  m_s config list | get <键> | set <键> <值> | unset <键> | path

通用
  --json                      输出 JSON；流式任务输出 NDJSON（{"type":"log"} / {"type":"status"}）
  退出码                      0 成功 · 1 失败 · 2 用法错误 · 3 服务未运行 · 130 任务被停止
`
