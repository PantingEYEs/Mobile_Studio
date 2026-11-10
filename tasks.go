package main

// 通用后台任务执行器：用于 Gradle 构建、安装、启动与 Logcat 等长耗时操作。
// 每个任务维护一份有上限的日志缓冲，并通过 SSE (/api/task/stream) 推送给前端，
// 前端断线或刷新页面后可凭任务 ID 重新接入并回放历史日志。

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const maxTaskLines = 5000

type TaskStatus string

const (
	TaskRunning TaskStatus = "running"
	TaskSuccess TaskStatus = "success"
	TaskFailed  TaskStatus = "failed"
	TaskStopped TaskStatus = "stopped"
)

type Task struct {
	ID        string
	Kind      string
	Root      string
	StartedAt time.Time

	mu       sync.Mutex
	status   TaskStatus
	errMsg   string
	result   map[string]interface{}
	lines    []string
	dropped  int // 因超出缓冲上限而被丢弃的行数（用于计算全局行号）
	notify   chan struct{}
	cancel   context.CancelFunc
	finished time.Time
}

type TaskInfo struct {
	ID        string                 `json:"id"`
	Kind      string                 `json:"kind"`
	Root      string                 `json:"root"`
	Status    TaskStatus             `json:"status"`
	Error     string                 `json:"error,omitempty"`
	Result    map[string]interface{} `json:"result,omitempty"`
	StartedAt int64                  `json:"startedAt"`
	Lines     int                    `json:"lines"`
}

func (t *Task) Info() TaskInfo {
	t.mu.Lock()
	defer t.mu.Unlock()
	return TaskInfo{
		ID: t.ID, Kind: t.Kind, Root: t.Root, Status: t.status, Error: t.errMsg,
		Result: t.result, StartedAt: t.StartedAt.UnixMilli(), Lines: t.dropped + len(t.lines),
	}
}

// Log 追加一行日志并唤醒所有订阅者。
func (t *Task) Log(format string, args ...interface{}) {
	line := format
	if len(args) > 0 {
		line = fmt.Sprintf(format, args...)
	}
	t.mu.Lock()
	t.lines = append(t.lines, line)
	if len(t.lines) > maxTaskLines {
		cut := len(t.lines) - maxTaskLines
		t.lines = append([]string(nil), t.lines[cut:]...)
		t.dropped += cut
	}
	close(t.notify)
	t.notify = make(chan struct{})
	t.mu.Unlock()
}

func (t *Task) SetResult(key string, val interface{}) {
	t.mu.Lock()
	if t.result == nil {
		t.result = map[string]interface{}{}
	}
	t.result[key] = val
	t.mu.Unlock()
}

func (t *Task) finish(status TaskStatus, errMsg string) {
	t.mu.Lock()
	t.status = status
	t.errMsg = errMsg
	t.finished = time.Now()
	close(t.notify)
	t.notify = make(chan struct{})
	t.mu.Unlock()
}

// snapshot 返回从全局行号 from 开始的日志、下一次读取的行号、任务是否结束以及等待通道。
func (t *Task) snapshot(from int) ([]string, int, bool, <-chan struct{}) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if from < t.dropped {
		from = t.dropped
	}
	idx := from - t.dropped
	var out []string
	if idx < len(t.lines) {
		out = append(out, t.lines[idx:]...)
	}
	return out, t.dropped + len(t.lines), t.status != TaskRunning, t.notify
}

type TaskManager struct {
	mu    sync.Mutex
	seq   int
	tasks map[string]*Task
}

var tasks = &TaskManager{tasks: map[string]*Task{}}

// Start 启动一个后台任务。同一项目同一类别 (exclusiveKey) 只允许一个运行中的任务。
func (m *TaskManager) Start(kind, root, exclusiveKey string, fn func(ctx context.Context, t *Task) error) (*Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.gcLocked()
	for _, t := range m.tasks {
		if t.Root == root && exclusiveKeyOf(t.Kind) == exclusiveKey && t.Info().Status == TaskRunning {
			return t, fmt.Errorf("该项目已有正在进行的%s任务", kindLabel(t.Kind))
		}
	}

	m.seq++
	ctx, cancel := context.WithCancel(context.Background())
	t := &Task{
		ID:        fmt.Sprintf("t%d-%d", time.Now().Unix(), m.seq),
		Kind:      kind,
		Root:      root,
		StartedAt: time.Now(),
		status:    TaskRunning,
		notify:    make(chan struct{}),
		cancel:    cancel,
	}
	m.tasks[t.ID] = t

	go func() {
		defer cancel()
		defer func() {
			if r := recover(); r != nil {
				t.Log("[内部错误] %v", r)
				t.finish(TaskFailed, fmt.Sprint(r))
			}
		}()
		err := fn(ctx, t)
		switch {
		case ctx.Err() != nil:
			t.Log("■ 任务已被手动停止")
			t.finish(TaskStopped, "")
		case err != nil:
			t.Log("✗ %v", err)
			t.finish(TaskFailed, err.Error())
		default:
			t.finish(TaskSuccess, "")
		}
	}()
	return t, nil
}

func (m *TaskManager) Get(id string) *Task {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.tasks[id]
}

// gcLocked 清理结束超过 30 分钟的任务，避免内存无限增长。
func (m *TaskManager) gcLocked() {
	for id, t := range m.tasks {
		t.mu.Lock()
		old := t.status != TaskRunning && time.Since(t.finished) > 30*time.Minute
		t.mu.Unlock()
		if old {
			delete(m.tasks, id)
		}
	}
}

func exclusiveKeyOf(kind string) string {
	if kind == "logcat" {
		return "logcat"
	}
	return "pipeline" // build / install / launch / run 互斥，避免同时操作同一个 APK
}

func kindLabel(kind string) string {
	switch kind {
	case "build":
		return "构建"
	case "install":
		return "安装"
	case "launch":
		return "启动"
	case "run":
		return "运行"
	case "logcat":
		return "日志"
	}
	return kind
}

// StreamOpts 控制 runStreaming 的行为。
type StreamOpts struct {
	Stdin   io.Reader
	Filter  func(line string) bool // 返回 false 的行不写入任务日志（仍会被 Capture 收集）
	Capture bool                   // 收集输出尾部（最多 64KB）用于结果判定
}

// runStreaming 运行外部命令，并把 stdout/stderr 逐行写入任务日志。
// 进程以独立进程组启动，取消时整组杀死（Gradle 会派生子进程）。
func runStreaming(ctx context.Context, t *Task, cmd *exec.Cmd, opts StreamOpts) (string, error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		return nil
	}
	cmd.WaitDelay = 3 * time.Second
	if opts.Stdin != nil {
		cmd.Stdin = opts.Stdin
	}

	pr, pw := io.Pipe()
	cmd.Stdout = pw
	cmd.Stderr = pw

	if err := cmd.Start(); err != nil {
		pw.Close()
		return "", fmt.Errorf("无法启动 %s: %v", cmd.Path, err)
	}

	var captured strings.Builder
	done := make(chan struct{})
	go func() {
		defer close(done)
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		for sc.Scan() {
			line := strings.TrimRight(sc.Text(), "\r")
			if opts.Capture {
				if captured.Len() > 64*1024 {
					rest := captured.String()[32*1024:]
					captured.Reset()
					captured.WriteString(rest)
				}
				captured.WriteString(line)
				captured.WriteByte('\n')
			}
			if opts.Filter == nil || opts.Filter(line) {
				t.Log("%s", line)
			}
		}
		io.Copy(io.Discard, pr)
	}()

	err := cmd.Wait()
	pw.Close()
	<-done
	if ctx.Err() != nil {
		return captured.String(), ctx.Err()
	}
	return captured.String(), err
}

// ---------------- HTTP 接口 ----------------

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, format string, args ...interface{}) {
	writeJSON(w, status, map[string]interface{}{"success": false, "error": fmt.Sprintf(format, args...)})
}

// GET /api/task/stream?id=...&from=N  (Server-Sent Events)
func handleTaskStream(w http.ResponseWriter, r *http.Request) {
	t := tasks.Get(r.URL.Query().Get("id"))
	if t == nil {
		writeErr(w, http.StatusNotFound, "任务不存在或已过期")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	from, _ := strconv.Atoi(r.URL.Query().Get("from"))
	follow := r.URL.Query().Get("follow") != "0"

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	sendStatus := func() {
		b, _ := json.Marshal(t.Info())
		fmt.Fprintf(w, "event: status\ndata: %s\n\n", b)
	}
	sendStatus()
	flusher.Flush()

	keepAlive := time.NewTicker(15 * time.Second)
	defer keepAlive.Stop()

	for {
		lines, next, finished, wait := t.snapshot(from)
		if len(lines) > 0 {
			// 一个事件携带多行（JSON 数组），降低高频日志下的事件数量
			b, _ := json.Marshal(lines)
			fmt.Fprintf(w, "id: %d\nevent: log\ndata: %s\n\n", next, b)
			from = next
			flusher.Flush()
		}
		if finished || !follow {
			sendStatus()
			fmt.Fprint(w, "event: end\ndata: {}\n\n")
			flusher.Flush()
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-wait:
			// 稍作合并，避免每行一次 flush
			time.Sleep(60 * time.Millisecond)
		case <-keepAlive.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}

// GET /api/task/stop?id=...
func handleTaskStop(w http.ResponseWriter, r *http.Request) {
	t := tasks.Get(r.URL.Query().Get("id"))
	if t == nil {
		writeErr(w, http.StatusNotFound, "任务不存在或已过期")
		return
	}
	t.cancel()
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

// GET /api/task/active?root=...  返回该项目仍在运行（或最近结束）的任务，便于页面刷新后重新接入。
func handleTaskActive(w http.ResponseWriter, r *http.Request) {
	root := r.URL.Query().Get("root")
	if root != "" {
		if abs, err := resolveProjectRoot(root); err == nil {
			root = abs
		}
	}
	tasks.mu.Lock()
	var list []TaskInfo
	for _, t := range tasks.tasks {
		if root == "" || t.Root == root {
			list = append(list, t.Info())
		}
	}
	tasks.mu.Unlock()
	if list == nil {
		list = []TaskInfo{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "tasks": list})
}
