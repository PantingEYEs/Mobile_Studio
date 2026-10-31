package main

import (
"embed"
"fmt"
"io/fs"
"net/http"
)

//go:embed all:frontend/dist
var frontendFS embed.FS

func main() {
// 剥离 "frontend/dist" 前缀，让根路径 "/" 直接访问 index.html
distFS, err := fs.Sub(frontendFS, "frontend/dist")
if err != nil {
panic(fmt.Sprintf("Failed to load embedded frontend: %v", err))
}

// 静态文件服务器接管路由
http.Handle("/", http.FileServer(http.FS(distFS)))

fmt.Println("Mobile Studio is running at http://127.0.0.1:8080 ...")
if err := http.ListenAndServe("127.0.0.1:8080", nil); err != nil {
fmt.Printf("Server exited: %v\n", err)
}
}
