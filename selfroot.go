package main

import (
	"os"
	"path/filepath"
	"sync"
)

// selfRoot 返回 Mobile Studio 自身项目目录，用于解析前端传来的 "." / 空 root。
//
// 不能直接使用进程工作目录：m_s 可在任意目录下启动服务，此时 "." 会指向启动时所在目录，
// 导致 Mobile Studio 自身的 Git 拓扑与文件树读取到错误位置（通常显示为空）。
//
// 判定顺序：
//  1. 环境变量 MS_ROOT
//  2. 可执行文件所在 bin/ 的上一级目录（存在 go.mod 时），即 m_s 编译出的 bin/server
//  3. 进程工作目录（go run 等场景的兜底）
var selfRoot = sync.OnceValue(func() string {
	if v := os.Getenv("MS_ROOT"); v != "" {
		if abs, err := filepath.Abs(expandHome(v)); err == nil {
			return abs
		}
	}
	if exe, err := os.Executable(); err == nil {
		if real, err := filepath.EvalSymlinks(exe); err == nil {
			dir := filepath.Dir(filepath.Dir(real))
			if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
				return dir
			}
		}
	}
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return "."
})
