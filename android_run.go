package main

// Android 构建 → 安装 → 启动 → 日志 全链路。
// 所有长耗时操作都以后台任务运行，日志经 /api/task/stream 推送到前端。

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

var pkgNameRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*(\.[A-Za-z][A-Za-z0-9_]*)+$`)

// ---------------- 构建 ----------------

func ensureLocalProperties(t *Task, root, sdkPath string) {
	lp := filepath.Join(root, "local.properties")
	props := readProperties(lp)
	if v := props["sdk.dir"]; v != "" && looksLikeSdk(expandHome(v)) {
		return
	}
	b, _ := os.ReadFile(lp)
	content := string(b)
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	// sdk.dir 已存在但无效时追加的新值会覆盖旧值（properties 后者生效）
	content += "sdk.dir=" + sdkPath + "\n"
	if err := os.WriteFile(lp, []byte(content), 0644); err != nil {
		t.Log("⚠ 写入 local.properties 失败: %v", err)
		return
	}
	t.Log("· 已写入 local.properties: sdk.dir=%s", sdkPath)
}

func javaHomeFrom(java *ToolInfo) string {
	if jh := os.Getenv("JAVA_HOME"); jh != "" {
		return jh
	}
	if java == nil {
		return ""
	}
	real, err := filepath.EvalSymlinks(java.Path)
	if err != nil {
		return ""
	}
	return filepath.Dir(filepath.Dir(real))
}

// findApks 返回模块 debug 输出目录下的 APK，按修改时间倒序。
func findApks(root, module string) []string {
	if module == "" {
		module = "app"
	}
	matches, _ := filepath.Glob(filepath.Join(root, module, "build", "outputs", "apk", "debug", "*.apk"))
	sort.Slice(matches, func(i, j int) bool {
		a, _ := os.Stat(matches[i])
		b, _ := os.Stat(matches[j])
		return a.ModTime().After(b.ModTime())
	})
	return matches
}

func doBuild(ctx context.Context, t *Task, root string) (string, error) {
	t.Log("━━━ 构建 (assembleDebug) ━━━")
	env := buildEnvReport(root, false)
	t.Log("· 平台 %s%s", env.Arch, map[bool]string{true: " (Termux)", false: ""}[env.Termux])
	if env.Java != nil {
		t.Log("· JDK    %s  [%s]", env.Java.Version, env.Java.Path)
	}
	if env.Gradle != nil {
		t.Log("· Gradle %s  [%s]", env.Gradle.Version, env.Gradle.Path)
	}
	if env.Sdk != nil {
		t.Log("· SDK    %s  (来源: %s)", env.Sdk.Path, env.Sdk.Source)
	}
	for _, p := range env.Problems {
		t.Log("⚠ %s", p)
	}
	if env.Java == nil || env.Gradle == nil || env.Sdk == nil {
		return "", errors.New("构建环境不完整，请先按上方提示安装依赖")
	}
	if !env.Project.IsAndroid || env.Project.AppModule == "" {
		return "", errors.New("当前项目不是 Android 应用项目（未找到应用 com.android.application 插件的模块）")
	}

	ensureLocalProperties(t, root, env.Sdk.Path)

	module := env.Project.AppModule
	args := []string{":" + module + ":assembleDebug", "--console=plain"}
	if env.Aapt2 != "" {
		// 以命令行属性注入，避免把设备相关路径写进项目文件，项目拿到 PC 上仍可直接构建
		args = append(args, "-Pandroid.aapt2FromMavenOverride="+env.Aapt2)
		t.Log("· aapt2  %s (aarch64 覆盖)", env.Aapt2)
	}

	gradleBin := env.Gradle.Path
	if env.Gradle.Version == "wrapper" {
		os.Chmod(gradleBin, 0755)
	}

	cmd := exec.CommandContext(ctx, gradleBin, args...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(),
		"ANDROID_HOME="+env.Sdk.Path,
		"ANDROID_SDK_ROOT="+env.Sdk.Path,
	)
	if jh := javaHomeFrom(env.Java); jh != "" {
		cmd.Env = append(cmd.Env, "JAVA_HOME="+jh)
	}
	t.Log("$ %s %s", filepath.Base(gradleBin), strings.Join(args, " "))

	started := time.Now()
	if _, err := runStreaming(ctx, t, cmd, StreamOpts{}); err != nil {
		return "", fmt.Errorf("Gradle 构建失败 (%v)", err)
	}

	apks := findApks(root, module)
	if len(apks) == 0 {
		return "", errors.New("构建成功但未找到 APK 产物")
	}
	apk := apks[0]
	if st, err := os.Stat(apk); err == nil {
		t.Log("✓ 构建成功，用时 %.1fs → %s (%.1f KB)", time.Since(started).Seconds(), apk, float64(st.Size())/1024)
	}
	t.SetResult("apk", apk)
	return apk, nil
}

// ---------------- 安装 ----------------

func sharedStorageDir() string {
	home, _ := os.UserHomeDir()
	for _, p := range []string{"/sdcard/Download", "/storage/emulated/0/Download", filepath.Join(home, "storage", "downloads")} {
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			test := filepath.Join(p, ".ms_write_test")
			if os.WriteFile(test, []byte("x"), 0644) == nil {
				os.Remove(test)
				return p
			}
		}
	}
	return ""
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func doInstall(ctx context.Context, t *Task, root, apk string, sh DeviceShell) error {
	t.Log("━━━ 安装 ━━━")
	if apk == "" {
		info := inspectAndroidProject(root)
		apks := findApks(root, info.AppModule)
		if len(apks) == 0 {
			return errors.New("未找到可安装的 APK，请先构建")
		}
		apk = apks[0]
	}
	st, err := os.Stat(apk)
	if err != nil {
		return fmt.Errorf("APK 不存在: %v", err)
	}
	t.Log("· APK  %s", apk)
	t.Log("· 通道 %s — %s", sh.Mode, sh.Detail)

	switch sh.Mode {
	case "rish":
		// 方式一：通过标准输入流式传输给 pm（shell 用户无法读取 Termux 私有目录）
		f, err := os.Open(apk)
		if err != nil {
			return err
		}
		t.Log("$ rish -c 'pm install -r -t -S %d'  < app.apk", st.Size())
		out, err := runStreaming(ctx, t, sh.Command(ctx, fmt.Sprintf("pm install -r -t -S %d", st.Size())), StreamOpts{Stdin: f, Capture: true})
		f.Close()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err == nil && strings.Contains(out, "Success") {
			t.Log("✓ 安装成功")
			return nil
		}
		// 方式二：复制到共享存储后按路径安装
		dir := sharedStorageDir()
		if dir == "" {
			return errors.New("pm 流式安装失败，且无法写入共享存储（可执行 termux-setup-storage 授权后重试）")
		}
		t.Log("· 流式安装失败，改为经共享存储安装")
		tmpDir := filepath.Join(dir, "MobileStudio")
		os.MkdirAll(tmpDir, 0755)
		dst := filepath.Join(tmpDir, "install.apk")
		if err := copyFile(apk, dst); err != nil {
			return fmt.Errorf("复制 APK 失败: %v", err)
		}
		defer os.Remove(dst)
		devPath := strings.Replace(dst, filepath.Join(os.Getenv("HOME"), "storage", "downloads"), "/sdcard/Download", 1)
		out, err = runStreaming(ctx, t, sh.Command(ctx, "pm install -r -t '"+devPath+"'"), StreamOpts{Capture: true})
		if err == nil && strings.Contains(out, "Success") {
			t.Log("✓ 安装成功")
			return nil
		}
		return fmt.Errorf("安装失败: %v", err)

	case "adb":
		args := []string{}
		if sh.Serial != "" {
			args = append(args, "-s", sh.Serial)
		}
		args = append(args, "install", "-r", "-t", apk)
		t.Log("$ adb %s", strings.Join(args, " "))
		out, err := runStreaming(ctx, t, exec.CommandContext(ctx, "adb", args...), StreamOpts{Capture: true})
		if err == nil && strings.Contains(out, "Success") {
			t.Log("✓ 安装成功")
			return nil
		}
		return fmt.Errorf("adb 安装失败: %v", err)

	default:
		// 无特权：交给系统安装器，需要用户手动确认
		if _, err := exec.LookPath("termux-open"); err != nil {
			return errors.New("无特权通道且未找到 termux-open，无法安装。请启用 Shizuku 或无线调试")
		}
		t.Log("$ termux-open --content-type application/vnd.android.package-archive %s", apk)
		if _, err := runStreaming(ctx, t, exec.CommandContext(ctx, "termux-open", "--content-type", "application/vnd.android.package-archive", apk), StreamOpts{}); err != nil {
			return fmt.Errorf("调起系统安装器失败: %v", err)
		}
		t.Log("✓ 已调起系统安装器，请在弹窗中确认安装（首次需允许 Termux 安装未知应用）")
		t.SetResult("manualInstall", true)
		return nil
	}
}

// ---------------- 启动 / 停止 ----------------

type manifestXML struct {
	Package     string `xml:"package,attr"`
	Application struct {
		Activities []manifestActivity `xml:"activity"`
		Aliases    []manifestActivity `xml:"activity-alias"`
	} `xml:"application"`
}

type manifestActivity struct {
	Name          string `xml:"http://schemas.android.com/apk/res/android name,attr"`
	IntentFilters []struct {
		Actions []struct {
			Name string `xml:"http://schemas.android.com/apk/res/android name,attr"`
		} `xml:"action"`
		Categories []struct {
			Name string `xml:"http://schemas.android.com/apk/res/android name,attr"`
		} `xml:"category"`
	} `xml:"intent-filter"`
}

// launcherFromManifest 解析源码 Manifest 中带 MAIN/LAUNCHER 的 Activity，返回完整类名。
func launcherFromManifest(root, module string) string {
	if module == "" {
		module = "app"
	}
	b, err := os.ReadFile(filepath.Join(root, module, "src", "main", "AndroidManifest.xml"))
	if err != nil {
		return ""
	}
	var m manifestXML
	if xml.Unmarshal(b, &m) != nil {
		return ""
	}
	ns := m.Package
	for _, f := range []string{"build.gradle", "build.gradle.kts"} {
		if src, err := os.ReadFile(filepath.Join(root, module, f)); err == nil {
			if mm := namespaceRe.FindStringSubmatch(string(src)); mm != nil {
				ns = mm[1]
			}
		}
	}
	all := append(m.Application.Activities, m.Application.Aliases...)
	for _, a := range all {
		for _, f := range a.IntentFilters {
			hasMain, hasLauncher := false, false
			for _, ac := range f.Actions {
				hasMain = hasMain || ac.Name == "android.intent.action.MAIN"
			}
			for _, c := range f.Categories {
				hasLauncher = hasLauncher || c.Name == "android.intent.category.LAUNCHER"
			}
			if hasMain && hasLauncher {
				switch {
				case strings.HasPrefix(a.Name, "."):
					return ns + a.Name
				case !strings.Contains(a.Name, "."):
					return ns + "." + a.Name
				default:
					return a.Name
				}
			}
		}
	}
	return ""
}

func doLaunch(ctx context.Context, t *Task, root, pkg string, sh DeviceShell) error {
	t.Log("━━━ 启动 %s ━━━", pkg)
	if sh.Privileged() {
		script := fmt.Sprintf(`comp=$(cmd package resolve-activity --brief -a android.intent.action.MAIN -c android.intent.category.LAUNCHER %[1]s | tail -n 1)
case "$comp" in */*) ;; *) echo "未找到 %[1]s 的启动 Activity（应用是否已安装？）"; exit 1;; esac
am force-stop %[1]s
echo "am start -W -n $comp"
am start -W -n "$comp"`, pkg)
		out, err := runStreaming(ctx, t, sh.Command(ctx, script), StreamOpts{Capture: true})
		if err != nil || strings.Contains(out, "Error:") {
			return fmt.Errorf("启动失败: %v", err)
		}
		t.Log("✓ 应用已启动")
		return nil
	}

	info := inspectAndroidProject(root)
	act := launcherFromManifest(root, info.AppModule)
	if act == "" {
		return errors.New("无法从 AndroidManifest.xml 中找到 LAUNCHER Activity")
	}
	comp := pkg + "/" + act
	t.Log("$ am start -n %s", comp)
	out, err := runStreaming(ctx, t, exec.CommandContext(ctx, "am", "start", "-n", comp), StreamOpts{Capture: true})
	if err != nil || strings.Contains(out, "Error") {
		return fmt.Errorf("启动失败（无特权模式下 Android 可能拦截后台启动，可手动点击桌面图标）: %v", err)
	}
	t.Log("✓ 已发送启动请求")
	return nil
}

// ---------------- Logcat ----------------

// logcatPidOf 解析 threadtime 格式行中的 PID：
// "11-10 12:00:00.123  1234  1250 I Tag: message"
func logcatPidOf(line string) string {
	f := strings.Fields(line)
	if len(f) >= 5 && len(f[0]) == 5 && f[0][2] == '-' {
		return f[2]
	}
	return ""
}

func doLogcat(ctx context.Context, t *Task, pkg string, all bool, sh DeviceShell) error {
	if !sh.Privileged() {
		return errors.New("读取应用日志需要 Shizuku (rish) 或无线调试 adb：Android 4.1 起普通应用只能读取自身日志")
	}
	if all {
		t.Log("━━━ Logcat（全部进程）━━━")
	} else {
		t.Log("━━━ Logcat（仅 %s）━━━", pkg)
	}

	var mu sync.Mutex
	pids := map[string]bool{} // 记录出现过的全部 PID，应用崩溃退出后仍保留其日志
	refresh := func() {
		out, _ := sh.Output(5*time.Second, "pidof "+pkg)
		mu.Lock()
		for _, p := range strings.Fields(out) {
			if !pids[p] {
				pids[p] = true
				t.Log("· 进程 %s 已附加 (pid %s)", pkg, p)
			}
		}
		mu.Unlock()
	}

	var filter func(string) bool
	if !all {
		refresh()
		go func() {
			tk := time.NewTicker(2 * time.Second)
			defer tk.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-tk.C:
					refresh()
				}
			}
		}()
		filter = func(line string) bool {
			pid := logcatPidOf(line)
			if pid == "" {
				return false
			}
			mu.Lock()
			defer mu.Unlock()
			return pids[pid]
		}
	}

	_, err := runStreaming(ctx, t, sh.Command(ctx, "logcat -v threadtime -T 300"), StreamOpts{Filter: filter})
	return err
}

// ---------------- HTTP 接口 ----------------

type runParams struct {
	root string
	pkg  string
	sh   DeviceShell
}

func parseRunParams(w http.ResponseWriter, r *http.Request, needPkg bool) (*runParams, bool) {
	root, err := resolveProjectRoot(r.URL.Query().Get("root"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "项目目录无效: %v", err)
		return nil, false
	}
	p := &runParams{root: root}
	if needPkg {
		p.pkg = strings.TrimSpace(r.URL.Query().Get("pkg"))
		if p.pkg == "" {
			p.pkg = inspectAndroidProject(root).ApplicationID
		}
		if !pkgNameRe.MatchString(p.pkg) {
			writeErr(w, http.StatusBadRequest, "无效的应用包名: %q", p.pkg)
			return nil, false
		}
	}
	p.sh = detectDeviceShell(r.URL.Query().Get("shell"), false)
	return p, true
}

func startTaskResponse(w http.ResponseWriter, t *Task, err error) {
	if err != nil {
		resp := map[string]interface{}{"success": false, "error": err.Error()}
		if t != nil {
			resp["task"] = t.Info()
		}
		writeJSON(w, http.StatusConflict, resp)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "task": t.Info()})
}

// GET /api/android/build?root=
func handleAndroidBuild(w http.ResponseWriter, r *http.Request) {
	p, ok := parseRunParams(w, r, false)
	if !ok {
		return
	}
	t, err := tasks.Start("build", p.root, "pipeline", func(ctx context.Context, t *Task) error {
		_, err := doBuild(ctx, t, p.root)
		return err
	})
	startTaskResponse(w, t, err)
}

// GET /api/android/install?root=
func handleAndroidInstall(w http.ResponseWriter, r *http.Request) {
	p, ok := parseRunParams(w, r, false)
	if !ok {
		return
	}
	t, err := tasks.Start("install", p.root, "pipeline", func(ctx context.Context, t *Task) error {
		return doInstall(ctx, t, p.root, "", p.sh)
	})
	startTaskResponse(w, t, err)
}

// GET /api/android/launch?root=&pkg=
func handleAndroidLaunch(w http.ResponseWriter, r *http.Request) {
	p, ok := parseRunParams(w, r, true)
	if !ok {
		return
	}
	t, err := tasks.Start("launch", p.root, "pipeline", func(ctx context.Context, t *Task) error {
		return doLaunch(ctx, t, p.root, p.pkg, p.sh)
	})
	startTaskResponse(w, t, err)
}

// GET /api/android/run?root=&pkg=   构建 → 安装 → 启动 一键流水线
func handleAndroidRun(w http.ResponseWriter, r *http.Request) {
	p, ok := parseRunParams(w, r, true)
	if !ok {
		return
	}
	t, err := tasks.Start("run", p.root, "pipeline", func(ctx context.Context, t *Task) error {
		apk, err := doBuild(ctx, t, p.root)
		if err != nil {
			return err
		}
		if err := doInstall(ctx, t, p.root, apk, p.sh); err != nil {
			return err
		}
		if !p.sh.Privileged() {
			t.Log("· 无特权模式：请在系统安装器完成安装后，再点击「启动」")
			return nil
		}
		return doLaunch(ctx, t, p.root, p.pkg, p.sh)
	})
	startTaskResponse(w, t, err)
}

// GET /api/android/logcat?root=&pkg=&all=1
func handleAndroidLogcat(w http.ResponseWriter, r *http.Request) {
	all := r.URL.Query().Get("all") == "1"
	p, ok := parseRunParams(w, r, !all)
	if !ok {
		return
	}
	t, err := tasks.Start("logcat", p.root, "logcat", func(ctx context.Context, t *Task) error {
		return doLogcat(ctx, t, p.pkg, all, p.sh)
	})
	startTaskResponse(w, t, err)
}

// GET /api/android/stop?root=&pkg=   强制停止应用（同步）
func handleAndroidStop(w http.ResponseWriter, r *http.Request) {
	p, ok := parseRunParams(w, r, true)
	if !ok {
		return
	}
	if !p.sh.Privileged() {
		writeErr(w, http.StatusBadRequest, "强制停止应用需要 Shizuku (rish) 或无线调试 adb")
		return
	}
	out, err := p.sh.Output(8*time.Second, "am force-stop "+p.pkg)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "停止失败: %v %s", err, strings.TrimSpace(out))
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}
