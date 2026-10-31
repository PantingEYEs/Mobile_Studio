package main

import (
"embed"
"encoding/json"
"fmt"
"io/fs"
"net/http"
"os"
"os/exec"
"path/filepath"
"sort"
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

type GraphResponse struct {
CurrentBranch string       `json:"currentBranch"`
Branches      []string     `json:"branches"`
Commits       []CommitNode `json:"commits"`
}

type FileNode struct {
Name     string     `json:"name"`
Path     string     `json:"path"`
IsDir    bool       `json:"isDir"`
Children []FileNode `json:"children,omitempty"`
}

// 单层精准加载接口：每次只扫描直属下一层，彻底阻断递归膨胀
func handleFileTree(w http.ResponseWriter, r *http.Request) {
w.Header().Set("Content-Type", "application/json; charset=utf-8")

reqPath := r.URL.Query().Get("path")
reqPath = strings.TrimPrefix(reqPath, "/")

// 路径穿越安全防护 (禁止 ../ 逃逸项目根目录)
cleaned := filepath.Clean(filepath.FromSlash(reqPath))
if strings.HasPrefix(cleaned, "..") || filepath.IsAbs(cleaned) {
http.Error(w, `{"error":"Invalid path traversal"}`, http.StatusBadRequest)
return
}

fullPath := "."
if cleaned != "." && cleaned != "" {
fullPath = cleaned
}

info, err := os.Stat(fullPath)
if err != nil {
http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusNotFound)
return
}

if !info.IsDir() {
http.Error(w, `{"error":"Path is not a directory"}`, http.StatusBadRequest)
return
}

entries, err := os.ReadDir(fullPath)
if err != nil {
http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusInternalServerError)
return
}

var dirNodes []FileNode
var fileNodes []FileNode

for _, entry := range entries {
eName := entry.Name()
if eName == ".DS_Store" {
continue
}

childRel := eName
if cleaned != "." && cleaned != "" {
childRel = filepath.Join(cleaned, eName)
}
webPath := filepath.ToSlash(childRel)

if entry.IsDir() {
dirNodes = append(dirNodes, FileNode{
Name:  eName,
Path:  webPath,
IsDir: true,
})
} else {
fileNodes = append(fileNodes, FileNode{
Name:  eName,
Path:  webPath,
IsDir: false,
})
}
}

sort.Slice(dirNodes, func(i, j int) bool { return dirNodes[i].Name < dirNodes[j].Name })
sort.Slice(fileNodes, func(i, j int) bool { return fileNodes[i].Name < fileNodes[j].Name })

resp := struct {
Path     string     `json:"path"`
Children []FileNode `json:"children"`
}{
Path:     filepath.ToSlash(reqPath),
Children: append(dirNodes, fileNodes...),
}

json.NewEncoder(w).Encode(resp)
}

func handleGitGraph(w http.ResponseWriter, r *http.Request) {
w.Header().Set("Content-Type", "application/json; charset=utf-8")

curBranchCmd := exec.Command("git", "branch", "--show-current")
curBranchOut, _ := curBranchCmd.Output()
currentBranch := strings.TrimSpace(string(curBranchOut))

branchesCmd := exec.Command("git", "for-each-ref", "--format=%(refname:short)", "refs/heads/")
branchesOut, _ := branchesCmd.Output()
rawBranches := strings.Split(strings.TrimSpace(string(branchesOut)), "\n")
branches := make([]string, 0)
for _, b := range rawBranches {
b = strings.TrimSpace(b)
if b != "" {
branches = append(branches, b)
}
}

cmd := exec.Command("git", "log", "--all", "--topo-order", "--pretty=format:%H%x00%P%x00%an%x00%at%x00%cr%x00%s%x00%D%x1e")
out, err := cmd.Output()
if err != nil {
http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusInternalServerError)
return
}

raw := string(out)
commits := make([]CommitNode, 0)

if len(raw) > 0 {
records := strings.Split(raw, "\x1e")
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
}

resp := GraphResponse{
CurrentBranch: currentBranch,
Branches:      branches,
Commits:       commits,
}

json.NewEncoder(w).Encode(resp)
}

func main() {
distFS, err := fs.Sub(frontendFS, "frontend/dist")
if err != nil {
panic(fmt.Sprintf("Failed to load embedded frontend: %v", err))
}

http.HandleFunc("/api/git/graph", handleGitGraph)
http.HandleFunc("/api/fs/tree", handleFileTree)
http.Handle("/", http.FileServer(http.FS(distFS)))

fmt.Println("Mobile Studio is running at http://127.0.0.1:8080 ...")
if err := http.ListenAndServe("127.0.0.1:8080", nil); err != nil {
fmt.Printf("Server exited: %v\n", err)
}
}
