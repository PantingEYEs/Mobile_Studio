package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAndroidAPKDownload(t *testing.T) {
	root := t.TempDir()
	write := func(rel, content string) string {
		t.Helper()
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	write("settings.gradle", "include ':app'")
	write("app/build.gradle", "plugins { id 'com.android.application' }")
	old := write("app/build/outputs/apk/debug/old.apk", "old")
	if err := os.Chtimes(old, time.Unix(1, 0), time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	write("app/build/outputs/apk/debug/latest.apk", "latest-apk")
	handler := localOnly(http.HandlerFunc(handleAndroidAPK))
	for _, tc := range []struct {
		name, method, site, byteRange string
		status                        int
		body                          string
	}{
		{"download", "GET", "same-origin", "", 200, "latest-apk"},
		{"head", "HEAD", "same-origin", "", 200, ""},
		{"resume", "GET", "same-origin", "bytes=0-3", 206, "late"},
		{"external-site", "GET", "cross-site", "", 403, ""},
		{"unsupported-method", "POST", "same-origin", "", 405, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, "http://127.0.0.1:8080"+apkDownloadURL(root), nil)
			req.Header.Set("Sec-Fetch-Site", tc.site)
			req.Header.Set("Range", tc.byteRange)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tc.status {
				t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
			}
			if tc.status == 200 || tc.status == 206 {
				if rec.Body.String() != tc.body || rec.Header().Get("Content-Type") != "application/vnd.android.package-archive" || rec.Header().Get("Content-Disposition") != "attachment; filename=latest.apk" || rec.Header().Get("Cache-Control") != "no-store" {
					t.Fatalf("unexpected download: headers=%v body=%q", rec.Header(), rec.Body.String())
				}
			}
		})
	}
	if err := os.RemoveAll(filepath.Join(root, "app/build")); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "http://127.0.0.1:8080"+apkDownloadURL(root), nil))
	if rec.Code != 404 {
		t.Fatalf("missing APK status = %d", rec.Code)
	}
}

func TestLocalInstallOnlyPreparesDownload(t *testing.T) {
	root := t.TempDir()
	apk := filepath.Join(root, "test.apk")
	if err := os.WriteFile(apk, []byte("apk"), 0644); err != nil {
		t.Fatal(err)
	}
	// No termux-open or am is available: preparing the WebUI handoff must
	// still work without trying to launch an activity from the background.
	t.Setenv("PATH", "")
	task := &Task{notify: make(chan struct{})}
	if err := doInstall(context.Background(), task, root, apk, DeviceShell{Mode: "local"}); err != nil {
		t.Fatal(err)
	}
	result := task.Info().Result
	if result["manualInstall"] != true || result["downloadUrl"] != apkDownloadURL(root) {
		t.Fatalf("unexpected handoff: %v", result)
	}
}
