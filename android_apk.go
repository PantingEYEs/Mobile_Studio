package main

import (
	"mime"
	"net/http"
	"net/url"
	"path/filepath"
)

func apkDownloadURL(root string) string {
	return "/api/android/apk?root=" + url.QueryEscape(root)
}

// handleAndroidAPK serves only the latest debug APK of the selected application
// module. The caller cannot supply an arbitrary file path. localOnly protects
// this route just like the other APIs, including browser download navigations.
func handleAndroidAPK(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	root, err := resolveProjectRoot(r.URL.Query().Get("root"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "%v", err)
		return
	}
	info := inspectAndroidProject(root)
	if !info.IsAndroid || info.AppModule == "" {
		writeErr(w, http.StatusNotFound, "当前项目没有 Android 应用模块")
		return
	}
	apks := findApks(root, info.AppModule)
	if len(apks) == 0 {
		writeErr(w, http.StatusNotFound, "未找到 APK，请先构建")
		return
	}
	w.Header().Set("Content-Type", "application/vnd.android.package-archive")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filepath.Base(apks[0])}))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeFile(w, r, apks[0])
}
