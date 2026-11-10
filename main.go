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

func resolveSafePath(baseRoot string, relPath string) (string, error) {
if baseRoot == "" || baseRoot == "." {
baseRoot = "."
}
if strings.HasPrefix(baseRoot, "~") {
home, _ := os.UserHomeDir()
baseRoot = filepath.Join(home, strings.TrimPrefix(baseRoot, "~"))
}

relPath = strings.TrimPrefix(relPath, "/")
cleaned := filepath.Clean(filepath.FromSlash(relPath))
if strings.HasPrefix(cleaned, "..") {
return "", fmt.Errorf("invalid path traversal")
}

if cleaned == "." || cleaned == "" {
return baseRoot, nil
}
return filepath.Join(baseRoot, cleaned), nil
}

func parseRepoName(rawURL string) string {
rawURL = strings.TrimSpace(rawURL)
rawURL = strings.TrimSuffix(rawURL, "/")
rawURL = strings.TrimSuffix(rawURL, ".git")
parts := strings.FieldsFunc(rawURL, func(r rune) bool {
return r == '/' || r == ':'
})
if len(parts) > 0 {
return parts[len(parts)-1]
}
return "Cloned_Project"
}

// 专用于文件夹选择器的目录浏览接口（仅列出子文件夹，排除文件）
func handleBrowseDirs(w http.ResponseWriter, r *http.Request) {
w.Header().Set("Content-Type", "application/json; charset=utf-8")

reqDir := strings.TrimSpace(r.URL.Query().Get("dir"))
if reqDir == "" || reqDir == "~" {
home, _ := os.UserHomeDir()
reqDir = home
} else if strings.HasPrefix(reqDir, "~") {
home, _ := os.UserHomeDir()
reqDir = filepath.Join(home, strings.TrimPrefix(reqDir, "~"))
}

fullPath := filepath.Clean(reqDir)
info, err := os.Stat(fullPath)
if err != nil || !info.IsDir() {
home, _ := os.UserHomeDir()
fullPath = home
}

entries, err := os.ReadDir(fullPath)
if err != nil {
http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusInternalServerError)
return
}

var dirs []map[string]string
for _, entry := range entries {
if entry.IsDir() {
name := entry.Name()
if strings.HasPrefix(name, ".") {
continue
}
dirs = append(dirs, map[string]string{
"name": name,
"path": filepath.Join(fullPath, name),
})
}
}

sort.Slice(dirs, func(i, j int) bool { return dirs[i]["name"] < dirs[j]["name"] })

parent := filepath.Dir(fullPath)
resp := map[string]interface{}{
"current": fullPath,
"parent":  parent,
"dirs":    dirs,
}

json.NewEncoder(w).Encode(resp)
}

func handleFileTree(w http.ResponseWriter, r *http.Request) {
w.Header().Set("Content-Type", "application/json; charset=utf-8")

root := r.URL.Query().Get("root")
reqPath := r.URL.Query().Get("path")

fullPath, err := resolveSafePath(root, reqPath)
if err != nil {
http.Error(w, `{"error":"Invalid path traversal"}`, http.StatusBadRequest)
return
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

cleanedRel := filepath.Clean(filepath.FromSlash(strings.TrimPrefix(reqPath, "/")))

for _, entry := range entries {
eName := entry.Name()
if eName == ".DS_Store" {
continue
}

childRel := eName
if cleanedRel != "." && cleanedRel != "" {
childRel = filepath.Join(cleanedRel, eName)
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

func handleFileCreate(w http.ResponseWriter, r *http.Request) {
w.Header().Set("Content-Type", "application/json; charset=utf-8")

root := r.URL.Query().Get("root")
relPath := r.URL.Query().Get("path")
itemType := r.URL.Query().Get("type")

fullPath, err := resolveSafePath(root, relPath)
if err != nil {
http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusBadRequest)
return
}

if itemType == "dir" {
err = os.MkdirAll(fullPath, 0755)
} else {
err = os.MkdirAll(filepath.Dir(fullPath), 0755)
if err == nil {
var f *os.File
f, err = os.Create(fullPath)
if err == nil {
f.Close()
}
}
}

if err != nil {
http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusInternalServerError)
return
}

json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "path": relPath})
}

func handleFileRename(w http.ResponseWriter, r *http.Request) {
w.Header().Set("Content-Type", "application/json; charset=utf-8")

root := r.URL.Query().Get("root")
oldRelPath := r.URL.Query().Get("oldPath")
newName := r.URL.Query().Get("newName")

if strings.Contains(newName, "/") || strings.Contains(newName, "\\") || newName == "" {
http.Error(w, `{"error":"Invalid new name"}`, http.StatusBadRequest)
return
}

oldFullPath, err := resolveSafePath(root, oldRelPath)
if err != nil {
http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusBadRequest)
return
}

parentDir := filepath.Dir(oldFullPath)
newFullPath := filepath.Join(parentDir, newName)

if err := os.Rename(oldFullPath, newFullPath); err != nil {
http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusInternalServerError)
return
}

json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "newName": newName})
}

func handleProjectCreate(w http.ResponseWriter, r *http.Request) {
w.Header().Set("Content-Type", "application/json; charset=utf-8")

targetDir := r.URL.Query().Get("dir")
projName := r.URL.Query().Get("name")
projType := r.URL.Query().Get("type")

if projName == "" || strings.Contains(projName, "/") {
http.Error(w, `{"error":"Invalid project name"}`, http.StatusBadRequest)
return
}

if targetDir == "" {
targetDir, _ = os.UserHomeDir()
} else if strings.HasPrefix(targetDir, "~") {
home, _ := os.UserHomeDir()
targetDir = filepath.Join(home, strings.TrimPrefix(targetDir, "~"))
}

var scaffold *androidScaffold
if projType == "android" {
sc, err := newAndroidScaffold(projName, r.URL.Query())
if err != nil {
writeErr(w, http.StatusBadRequest, "%v", err)
return
}
scaffold = sc
}

projFullPath := filepath.Join(targetDir, projName)
if entries, err := os.ReadDir(projFullPath); err == nil && len(entries) > 0 {
writeErr(w, http.StatusConflict, "目录已存在且不为空: %s", projFullPath)
return
}
if err := os.MkdirAll(projFullPath, 0755); err != nil {
writeErr(w, http.StatusInternalServerError, "%v", err)
return
}

if scaffold != nil {
if err := scaffold.write(projFullPath); err != nil {
writeErr(w, http.StatusInternalServerError, "%v", err)
return
}
}

json.NewEncoder(w).Encode(map[string]interface{}{
"success": true,
"name":    projName,
"path":    projFullPath,
})
}

func handleProjectImport(w http.ResponseWriter, r *http.Request) {
w.Header().Set("Content-Type", "application/json; charset=utf-8")

targetPath := r.URL.Query().Get("path")
fullPath, err := resolveSafePath(targetPath, "")
if err != nil {
http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusBadRequest)
return
}

info, err := os.Stat(fullPath)
if err != nil {
http.Error(w, fmt.Sprintf(`{"error":"目录不存在: %v"}`, err), http.StatusNotFound)
return
}

if !info.IsDir() {
http.Error(w, `{"error":"目标路径不是一个文件夹"}`, http.StatusBadRequest)
return
}

name := filepath.Base(fullPath)
json.NewEncoder(w).Encode(map[string]interface{}{
"success": true,
"name":    name,
"path":    fullPath,
})
}

func handleProjectClone(w http.ResponseWriter, r *http.Request) {
w.Header().Set("Content-Type", "application/json; charset=utf-8")

repoURL := strings.TrimSpace(r.URL.Query().Get("url"))
targetDir := strings.TrimSpace(r.URL.Query().Get("dir"))

if repoURL == "" {
http.Error(w, `{"error":"Git 仓库地址不能为空"}`, http.StatusBadRequest)
return
}

if targetDir == "" {
targetDir, _ = os.UserHomeDir()
} else if strings.HasPrefix(targetDir, "~") {
home, _ := os.UserHomeDir()
targetDir = filepath.Join(home, strings.TrimPrefix(targetDir, "~"))
}

if err := os.MkdirAll(targetDir, 0755); err != nil {
http.Error(w, fmt.Sprintf(`{"error":"目标目录创建失败: %v"}`, err), http.StatusInternalServerError)
return
}

cmd := exec.Command("git", "clone", repoURL)
cmd.Dir = targetDir
out, err := cmd.CombinedOutput()
if err != nil {
w.WriteHeader(http.StatusInternalServerError)
json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": fmt.Sprintf("Git 克隆失败: %v\n%s", err, strings.TrimSpace(string(out)))})
return
}

repoName := parseRepoName(repoURL)
projFullPath := filepath.Join(targetDir, repoName)

json.NewEncoder(w).Encode(map[string]interface{}{
"success": true,
"name":    repoName,
"path":    projFullPath,
})
}

func handleGitGraph(w http.ResponseWriter, r *http.Request) {
w.Header().Set("Content-Type", "application/json; charset=utf-8")

rootParam := r.URL.Query().Get("root")
repoDir, err := resolveSafePath(rootParam, "")
if err != nil {
w.WriteHeader(http.StatusBadRequest)
json.NewEncoder(w).Encode(map[string]interface{}{"error": err.Error()})
return
}

curBranchCmd := exec.Command("git", "branch", "--show-current")
curBranchCmd.Dir = repoDir
curBranchOut, _ := curBranchCmd.Output()
currentBranch := strings.TrimSpace(string(curBranchOut))

branchesCmd := exec.Command("git", "for-each-ref", "--format=%(refname:short)", "refs/heads/")
branchesCmd.Dir = repoDir
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
cmd.Dir = repoDir
out, err := cmd.Output()
if err != nil {
json.NewEncoder(w).Encode(GraphResponse{
CurrentBranch: "",
Branches:      []string{},
Commits:       []CommitNode{},
})
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
http.HandleFunc("/api/fs/create", handleFileCreate)
http.HandleFunc("/api/fs/rename", handleFileRename)
http.HandleFunc("/api/fs/project/create", handleProjectCreate)
http.HandleFunc("/api/fs/project/import", handleProjectImport)
http.HandleFunc("/api/fs/project/clone", handleProjectClone)
http.HandleFunc("/api/fs/browse", handleBrowseDirs)
http.HandleFunc("/api/android/env", handleAndroidEnv)
http.HandleFunc("/api/android/build", handleAndroidBuild)
http.HandleFunc("/api/android/install", handleAndroidInstall)
http.HandleFunc("/api/android/launch", handleAndroidLaunch)
http.HandleFunc("/api/android/run", handleAndroidRun)
http.HandleFunc("/api/android/stop", handleAndroidStop)
http.HandleFunc("/api/android/logcat", handleAndroidLogcat)
http.HandleFunc("/api/task/stream", handleTaskStream)
http.HandleFunc("/api/task/stop", handleTaskStop)
http.HandleFunc("/api/task/active", handleTaskActive)
http.Handle("/", http.FileServer(http.FS(distFS)))

fmt.Println("Mobile Studio is running at http://127.0.0.1:8080 ...")
if err := http.ListenAndServe("127.0.0.1:8080", nil); err != nil {
fmt.Printf("Server exited: %v\n", err)
}
}
