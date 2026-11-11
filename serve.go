package main

import (
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const msVersion = "0.0.1-dev"

var serverStartedAt time.Time

// serve 启动 Web 服务（无参数或 serve 子命令）。
func serve() int {
	serverStartedAt = time.Now()
	port := cfgInt("server.port")
	fmt.Printf("Mobile Studio %s\nproject root: %s\nconfig dir:   %s\n", msVersion, selfRoot(), configDir())

	distFS, err := fs.Sub(frontendFS, "frontend/dist")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load embedded frontend: %v\n", err)
		return 1
	}

	http.HandleFunc("/api/status", handleStatus)
	http.HandleFunc("/api/projects", handleProjects)
	http.HandleFunc("/api/projects/add", handleProjectsAdd)
	http.HandleFunc("/api/projects/remove", handleProjectsRemove)
	http.HandleFunc("/api/git/graph", handleGitGraph)
	http.HandleFunc("/api/fs/tree", handleFileTree)
	http.HandleFunc("/api/fs/create", handleFileCreate)
	http.HandleFunc("/api/fs/rename", handleFileRename)
	http.HandleFunc("/api/fs/project/create", handleProjectCreate)
	http.HandleFunc("/api/fs/project/import", handleProjectImport)
	http.HandleFunc("/api/fs/project/clone", handleProjectClone)
	http.HandleFunc("/api/fs/browse", handleBrowseDirs)
	http.HandleFunc("/api/android/env", handleAndroidEnv)
	http.HandleFunc("/api/android/build", handleAndroidBuild)
	http.HandleFunc("/api/android/install", handleAndroidInstall)
	http.HandleFunc("/api/android/apk", handleAndroidAPK)
	http.HandleFunc("/api/android/launch", handleAndroidLaunch)
	http.HandleFunc("/api/android/run", handleAndroidRun)
	http.HandleFunc("/api/android/stop", handleAndroidStop)
	http.HandleFunc("/api/android/logcat", handleAndroidLogcat)
	http.HandleFunc("/api/task/stream", handleTaskStream)
	http.HandleFunc("/api/task/stop", handleTaskStop)
	http.HandleFunc("/api/task/active", handleTaskActive)
	http.HandleFunc("/api/term/list", handleTermList)
	http.HandleFunc("/api/term/create", handleTermCreate)
	http.HandleFunc("/api/term/close", handleTermClose)
	http.HandleFunc("/api/term/read", handleTermRead)
	http.HandleFunc("/api/term/input", handleTermInput)
	http.HandleFunc("/api/term/ws", handleTermWS)
	http.Handle("/", http.FileServer(http.FS(distFS)))

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, os.Interrupt)
	go func() {
		<-sig
		shutdown()
		os.Exit(0)
	}()

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	fmt.Printf("Mobile Studio is running at http://%s ...\n", addr)
	if err := http.ListenAndServe(addr, localOnly(http.DefaultServeMux)); err != nil {
		fmt.Fprintf(os.Stderr, "Server exited: %v\n", err)
		return 1
	}
	return 0
}

// GET /api/status
func handleStatus(w http.ResponseWriter, r *http.Request) {
	running := 0
	tasks.mu.Lock()
	for _, t := range tasks.tasks {
		if t.Info().Status == TaskRunning {
			running++
		}
	}
	tasks.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":      true,
		"version":      msVersion,
		"pid":          os.Getpid(),
		"port":         cfgInt("server.port"),
		"root":         selfRoot(),
		"configDir":    configDir(),
		"startedAt":    serverStartedAt.UnixMilli(),
		"tasksRunning": running,
		"terminals":    len(terms.List()),
	})
}

// shutdown 在服务退出前停止后台任务（Gradle 等位于独立进程组，否则会成为孤儿进程）并关闭终端会话。
func shutdown() {
	fmt.Println("Mobile Studio shutting down ...")
	tasks.mu.Lock()
	var running []*Task
	for _, t := range tasks.tasks {
		if t.Info().Status == TaskRunning {
			running = append(running, t)
		}
	}
	tasks.mu.Unlock()
	for _, t := range running {
		t.cancel()
	}
	for _, s := range terms.List() {
		terms.Close(s.ID)
	}
	deadline := time.Now().Add(3 * time.Second)
	for _, t := range running {
		for t.Info().Status == TaskRunning && time.Now().Before(deadline) {
			time.Sleep(50 * time.Millisecond)
		}
	}
}
