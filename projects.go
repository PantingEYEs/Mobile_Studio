package main

// 项目注册表：~/.mobile_studio/projects.json。
// 由 Web UI 与 CLI 共享（此前仅存于浏览器 localStorage，CLI / Agent 无法感知）。

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type Project struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type ProjectView struct {
	Project
	Exists bool `json:"exists"`
	Self   bool `json:"self"` // Mobile Studio 自身项目
}

var projectsMu sync.Mutex

func projectsPath() string { return filepath.Join(configDir(), "projects.json") }

// canonicalPath 将路径展开为绝对路径（"." / 空 指向 Mobile Studio 自身目录）。
func canonicalPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" || p == "." {
		return selfRoot()
	}
	abs, err := filepath.Abs(expandHome(p))
	if err != nil {
		return filepath.Clean(p)
	}
	return abs
}

func loadProjectsLocked() []Project {
	b, err := os.ReadFile(projectsPath())
	if err != nil {
		return []Project{{Name: filepath.Base(selfRoot()), Path: selfRoot()}}
	}
	var doc struct {
		Projects []Project `json:"projects"`
	}
	if json.Unmarshal(b, &doc) != nil || doc.Projects == nil {
		return []Project{}
	}
	return doc.Projects
}

func saveProjectsLocked(list []Project) error {
	return writeJSONFileAtomic(projectsPath(), map[string]interface{}{"projects": list})
}

func listProjects() []ProjectView {
	projectsMu.Lock()
	list := loadProjectsLocked()
	projectsMu.Unlock()
	self := selfRoot()
	out := make([]ProjectView, 0, len(list))
	for _, p := range list {
		st, err := os.Stat(p.Path)
		out = append(out, ProjectView{Project: p, Exists: err == nil && st.IsDir(), Self: p.Path == self})
	}
	return out
}

// addProject 注册项目（按规范化路径去重），返回注册后的条目。
func addProject(path, name string) (Project, bool, error) {
	cp := canonicalPath(path)
	st, err := os.Stat(cp)
	if err != nil {
		return Project{}, false, fmt.Errorf("目录不存在: %s", cp)
	}
	if !st.IsDir() {
		return Project{}, false, fmt.Errorf("不是文件夹: %s", cp)
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = filepath.Base(cp)
	}

	projectsMu.Lock()
	defer projectsMu.Unlock()
	list := loadProjectsLocked()
	for _, p := range list {
		if p.Path == cp {
			return p, false, nil
		}
	}
	p := Project{Name: name, Path: cp}
	list = append(list, p)
	return p, true, saveProjectsLocked(list)
}

// removeProject 按路径或名称移出注册表（不删除磁盘文件）。
func removeProject(ref string) (Project, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return Project{}, fmt.Errorf("需要指定项目名称或路径")
	}
	cp := canonicalPath(ref)
	projectsMu.Lock()
	defer projectsMu.Unlock()
	list := loadProjectsLocked()
	idx := -1
	for i, p := range list {
		if p.Path == cp {
			idx = i
			break
		}
	}
	if idx < 0 {
		var hits []int
		for i, p := range list {
			if p.Name == ref {
				hits = append(hits, i)
			}
		}
		if len(hits) > 1 {
			return Project{}, fmt.Errorf("有多个名为 %q 的项目，请改用路径指定", ref)
		}
		if len(hits) == 1 {
			idx = hits[0]
		}
	}
	if idx < 0 {
		return Project{}, fmt.Errorf("项目未注册: %s", ref)
	}
	removed := list[idx]
	list = append(list[:idx], list[idx+1:]...)
	return removed, saveProjectsLocked(list)
}

// findProjectFor 返回包含 dir 的已注册项目（取最深的匹配），无匹配返回空串。
func findProjectFor(dir string) string {
	dir = canonicalPath(dir)
	best := ""
	for _, p := range listProjects() {
		if dir == p.Path || strings.HasPrefix(dir, p.Path+string(filepath.Separator)) {
			if len(p.Path) > len(best) {
				best = p.Path
			}
		}
	}
	return best
}

// GET /api/projects
func handleProjects(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "projects": listProjects()})
}

// GET /api/projects/add?path=&name=
func handleProjectsAdd(w http.ResponseWriter, r *http.Request) {
	p, added, err := addProject(r.URL.Query().Get("path"), r.URL.Query().Get("name"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "project": p, "added": added})
}

// GET /api/projects/remove?path=   （path 也可为项目名称）
func handleProjectsRemove(w http.ResponseWriter, r *http.Request) {
	ref := r.URL.Query().Get("path")
	if ref == "" {
		ref = r.URL.Query().Get("name")
	}
	p, err := removeProject(ref)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "project": p})
}
