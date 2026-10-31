package main

import (
"embed"
"encoding/json"
"fmt"
"io/fs"
"net/http"
"os/exec"
"strconv"
"strings"
)

//go:embed all:frontend/dist
var frontendFS embed.FS

type CommitNode struct {
Hash      string   `json:"hash"`
ShortHash string   `json:"shortHash"`
Parents   []string `json:"parents"`
Author    string   `json:"author"`
Timestamp int64    `json:"timestamp"`
Date      string   `json:"date"`
Message   string   `json:"message"`
Refs      []string `json:"refs"`
}

func handleGitGraph(w http.ResponseWriter, r *http.Request) {
w.Header().Set("Content-Type", "application/json; charset=utf-8")

// 使用零字节 (%x00) 分隔字段，记录分隔符 (%x1e) 分隔提交，彻底杜绝特殊字符截断
cmd := exec.Command("git", "log", "--all", "--topo-order", "--pretty=format:%H%x00%P%x00%an%x00%at%x00%cr%x00%s%x00%D%x1e")
out, err := cmd.Output()
if err != nil {
http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusInternalServerError)
return
}

raw := string(out)
if len(raw) == 0 {
w.Write([]byte("[]"))
return
}

records := strings.Split(raw, "\x1e")
commits := make([]CommitNode, 0, len(records))

for _, rec := range records {
rec = strings.TrimSpace(rec)
if len(rec) == 0 {
continue
}
parts := strings.Split(rec, "\x00")
if len(parts) < 7 {
continue
}

hash := parts[0]
shortHash := hash
if len(hash) > 7 {
shortHash = hash[:7]
}

var parents []string
if strings.TrimSpace(parts[1]) != "" {
parents = strings.Fields(parts[1])
} else {
parents = []string{}
}

ts, _ := strconv.ParseInt(parts[3], 10, 64)

var refs []string
if strings.TrimSpace(parts[6]) != "" {
rawRefs := strings.Split(parts[6], ",")
for _, ref := range rawRefs {
ref = strings.TrimSpace(ref)
if ref != "" {
refs = append(refs, ref)
}
}
} else {
refs = []string{}
}

commits = append(commits, CommitNode{
Hash:      hash,
ShortHash: shortHash,
Parents:   parents,
Author:    parts[2],
Timestamp: ts,
Date:      parts[4],
Message:   parts[5],
Refs:      refs,
})
}

json.NewEncoder(w).Encode(commits)
}

func main() {
distFS, err := fs.Sub(frontendFS, "frontend/dist")
if err != nil {
panic(fmt.Sprintf("Failed to load embedded frontend: %v", err))
}

// 1. 注册 Git 图谱数据 API
http.HandleFunc("/api/git/graph", handleGitGraph)

// 2. 静态页面接管默认路由
http.Handle("/", http.FileServer(http.FS(distFS)))

fmt.Println("Mobile Studio is running at http://127.0.0.1:8080 ...")
if err := http.ListenAndServe("127.0.0.1:8080", nil); err != nil {
fmt.Printf("Server exited: %v\n", err)
}
}
