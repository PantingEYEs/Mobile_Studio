package main

// Android 构建与设备环境探测：JDK / Gradle / Android SDK / Termux aapt2，
// 以及用于安装、启动、Logcat 的特权通道（Shizuku rish → 无线调试 adb → 本地降级）。

import (
	"bufio"
	"context"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

const termuxPrefix = "/data/data/com.termux/files/usr"

func isTermux() bool {
	if strings.Contains(os.Getenv("PREFIX"), "com.termux") {
		return true
	}
	_, err := os.Stat(termuxPrefix)
	return err == nil
}

// termuxAapt2 返回 Termux 原生 aarch64 aapt2 路径（Maven 上的 aapt2 只有 x86_64 版本）。
func termuxAapt2() string {
	if !isTermux() {
		return ""
	}
	if p, err := exec.LookPath("aapt2"); err == nil {
		return p
	}
	p := filepath.Join(termuxPrefix, "bin", "aapt2")
	if _, err := os.Stat(p); err == nil {
		return p
	}
	return ""
}

func expandHome(p string) string {
	if strings.HasPrefix(p, "~") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, strings.TrimPrefix(p, "~"))
	}
	return p
}

// resolveProjectRoot 把前端传来的项目路径转换为绝对路径并校验其为目录。
func resolveProjectRoot(root string) (string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		root = "."
	}
	abs, err := filepath.Abs(expandHome(root))
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", os.ErrInvalid
	}
	return abs, nil
}

// ---------------- local.properties ----------------

func readProperties(path string) map[string]string {
	props := map[string]string{}
	f, err := os.Open(path)
	if err != nil {
		return props
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
			continue
		}
		if i := strings.IndexAny(line, "=:"); i > 0 {
			k := strings.TrimSpace(line[:i])
			v := strings.TrimSpace(line[i+1:])
			// Java properties 中 Windows 路径会写成 C\:\\...，这里只做最常见的反转义
			v = strings.ReplaceAll(v, `\:`, ":")
			v = strings.ReplaceAll(v, `\\`, `\`)
			props[k] = v
		}
	}
	return props
}

// ---------------- Android SDK ----------------

type SdkInfo struct {
	Path       string   `json:"path"`
	Source     string   `json:"source"`
	Platforms  []string `json:"platforms"`
	BuildTools []string `json:"buildTools"`
}

func listSubdirs(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []string{}
	}
	out := []string{}
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

func looksLikeSdk(p string) bool {
	if p == "" {
		return false
	}
	for _, sub := range []string{"platforms", "build-tools", "cmdline-tools", "platform-tools"} {
		if st, err := os.Stat(filepath.Join(p, sub)); err == nil && st.IsDir() {
			return true
		}
	}
	return false
}

// detectSdk 依次检查：项目 local.properties → ANDROID_HOME → ANDROID_SDK_ROOT → 常见安装位置。
func detectSdk(projectRoot string) *SdkInfo {
	type cand struct{ path, source string }
	var cands []cand
	if projectRoot != "" {
		if v := readProperties(filepath.Join(projectRoot, "local.properties"))["sdk.dir"]; v != "" {
			cands = append(cands, cand{v, "local.properties"})
		}
	}
	cands = append(cands,
		cand{os.Getenv("ANDROID_HOME"), "ANDROID_HOME"},
		cand{os.Getenv("ANDROID_SDK_ROOT"), "ANDROID_SDK_ROOT"},
	)
	home, _ := os.UserHomeDir()
	for _, p := range []string{
		filepath.Join(home, "android-sdk"),
		filepath.Join(home, "Android", "Sdk"),
		filepath.Join(home, ".android", "sdk"),
		filepath.Join(termuxPrefix, "opt", "android-sdk"),
		filepath.Join(termuxPrefix, "share", "android-sdk"),
		"/opt/android-sdk",
	} {
		cands = append(cands, cand{p, "默认位置"})
	}
	for _, c := range cands {
		p := expandHome(strings.TrimSpace(c.path))
		if looksLikeSdk(p) {
			return &SdkInfo{
				Path:       p,
				Source:     c.source,
				Platforms:  listSubdirs(filepath.Join(p, "platforms")),
				BuildTools: listSubdirs(filepath.Join(p, "build-tools")),
			}
		}
	}
	return nil
}

// ---------------- JDK / Gradle ----------------

type ToolInfo struct {
	Path    string `json:"path"`
	Version string `json:"version"`
}

func detectJava() *ToolInfo {
	javaBin := ""
	if jh := os.Getenv("JAVA_HOME"); jh != "" {
		p := filepath.Join(jh, "bin", "java")
		if _, err := os.Stat(p); err == nil {
			javaBin = p
		}
	}
	if javaBin == "" {
		p, err := exec.LookPath("java")
		if err != nil {
			return nil
		}
		javaBin = p
	}
	out := quickOutput(5*time.Second, javaBin, "-version")
	version := ""
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "version") {
			version = strings.TrimSpace(l)
			break
		}
	}
	return &ToolInfo{Path: javaBin, Version: version}
}

var javaMajorRe = regexp.MustCompile(`version "(\d+)`)

func javaMajor(t *ToolInfo) int {
	if t == nil {
		return 0
	}
	m := javaMajorRe.FindStringSubmatch(t.Version)
	if m == nil {
		return 0
	}
	n := 0
	for _, c := range m[1] {
		n = n*10 + int(c-'0')
	}
	return n
}

// detectGradle 优先使用项目自带的 gradlew，否则使用系统 gradle。
func detectGradle(projectRoot string) *ToolInfo {
	if projectRoot != "" {
		w := filepath.Join(projectRoot, "gradlew")
		jar := filepath.Join(projectRoot, "gradle", "wrapper", "gradle-wrapper.jar")
		if _, err := os.Stat(w); err == nil {
			if _, err := os.Stat(jar); err == nil {
				return &ToolInfo{Path: w, Version: "wrapper"}
			}
		}
	}
	p, err := exec.LookPath("gradle")
	if err != nil {
		return nil
	}
	return &ToolInfo{Path: p, Version: "system"}
}

func quickOutput(timeout time.Duration, name string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, _ := exec.CommandContext(ctx, name, args...).CombinedOutput()
	return string(out)
}

// ---------------- 特权 Shell 通道 ----------------

// DeviceShell 描述执行 pm/am/logcat 所用的通道。
//   - rish:  Shizuku 提供的 shell 身份 (uid 2000)，可静默安装、强停、读取全量 logcat
//   - adb:   本机无线调试 (adb connect 127.0.0.1:port) 后的 adb shell，能力同上
//   - local: 仅 Termux 应用自身权限，只能通过系统安装器安装、Termux am 启动，无法读取他人日志
type DeviceShell struct {
	Mode   string `json:"mode"`
	Serial string `json:"serial,omitempty"`
	Detail string `json:"detail"`
}

func (d DeviceShell) Privileged() bool { return d.Mode == "rish" || d.Mode == "adb" }

// Command 构造在设备 shell 中执行 script 的命令。
func (d DeviceShell) Command(ctx context.Context, script string) *exec.Cmd {
	switch d.Mode {
	case "rish":
		return exec.CommandContext(ctx, "rish", "-c", script)
	case "adb":
		if d.Serial != "" {
			return exec.CommandContext(ctx, "adb", "-s", d.Serial, "shell", script)
		}
		return exec.CommandContext(ctx, "adb", "shell", script)
	default:
		return exec.CommandContext(ctx, "sh", "-c", script)
	}
}

// Output 同步执行一条较短的设备命令。
func (d DeviceShell) Output(timeout time.Duration, script string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := d.Command(ctx, script).CombinedOutput()
	return string(out), err
}

var (
	shellCacheMu sync.Mutex
	shellCache   *DeviceShell
	shellCacheAt time.Time
)

func probeRish() (bool, string) {
	if _, err := exec.LookPath("rish"); err != nil {
		return false, "未找到 rish"
	}
	out := quickOutput(6*time.Second, "rish", "-c", "id")
	if strings.Contains(out, "uid=2000") || strings.Contains(out, "uid=0(") {
		return true, "Shizuku (rish) 已授权"
	}
	msg := strings.TrimSpace(out)
	if len(msg) > 160 {
		msg = msg[:160]
	}
	return false, "rish 不可用: " + msg
}

func probeAdb() (bool, string, string) {
	if _, err := exec.LookPath("adb"); err != nil {
		return false, "", "未找到 adb"
	}
	want := os.Getenv("MS_ADB_SERIAL")
	out := quickOutput(6*time.Second, "adb", "devices")
	var serials []string
	for _, l := range strings.Split(out, "\n") {
		f := strings.Fields(l)
		if len(f) == 2 && f[1] == "device" {
			serials = append(serials, f[0])
		}
	}
	if len(serials) == 0 {
		return false, "", "adb 未连接任何设备（可在开发者选项开启无线调试后 adb pair / adb connect）"
	}
	serial := serials[0]
	if want != "" {
		for _, s := range serials {
			if s == want {
				serial = s
			}
		}
	}
	return true, serial, "adb 已连接 " + serial
}

// detectDeviceShell 自动选择可用的最高权限通道，结果缓存 15 秒。
// prefer 可为 rish / adb / local，用于强制指定。
func detectDeviceShell(prefer string, refresh bool) DeviceShell {
	shellCacheMu.Lock()
	defer shellCacheMu.Unlock()
	if prefer == "" && !refresh && shellCache != nil && time.Since(shellCacheAt) < 15*time.Second {
		return *shellCache
	}

	var notes []string
	pick := func() DeviceShell {
		if prefer == "" || prefer == "rish" {
			ok, msg := probeRish()
			if ok {
				return DeviceShell{Mode: "rish", Detail: msg}
			}
			notes = append(notes, msg)
		}
		if prefer == "" || prefer == "adb" {
			ok, serial, msg := probeAdb()
			if ok {
				return DeviceShell{Mode: "adb", Serial: serial, Detail: msg}
			}
			notes = append(notes, msg)
		}
		return DeviceShell{Mode: "local", Detail: "无特权通道（" + strings.Join(notes, "；") + "），仅支持系统安装器安装与普通启动"}
	}
	d := pick()
	if prefer == "" {
		shellCache = &d
		shellCacheAt = time.Now()
	}
	return d
}

// ---------------- 项目信息 ----------------

type AndroidProjectInfo struct {
	IsAndroid     bool   `json:"isAndroid"`
	AppModule     string `json:"appModule"`
	ApplicationID string `json:"applicationId"`
	CompileSdk    string `json:"compileSdk"`
	HasWrapper    bool   `json:"hasWrapper"`
}

var (
	appIDRe      = regexp.MustCompile(`applicationId\s*(?:=\s*)?["']([\w.]+)["']`)
	namespaceRe  = regexp.MustCompile(`namespace\s*(?:=\s*)?["']([\w.]+)["']`)
	compileSdkRe = regexp.MustCompile(`compileSdk(?:Version)?\s*(?:=\s*)?(\d+)`)
)

// inspectAndroidProject 读取应用模块的 build.gradle(.kts)，提取 applicationId 与 compileSdk。
// 应用模块判定：包含 com.android.application 插件的第一层子目录（优先 app）。
func inspectAndroidProject(root string) AndroidProjectInfo {
	info := AndroidProjectInfo{}
	for _, f := range []string{"settings.gradle", "settings.gradle.kts"} {
		if _, err := os.Stat(filepath.Join(root, f)); err == nil {
			info.IsAndroid = true
		}
	}
	if _, err := os.Stat(filepath.Join(root, "gradle", "wrapper", "gradle-wrapper.jar")); err == nil {
		info.HasWrapper = true
	}

	modules := []string{"app"}
	for _, d := range listSubdirs(root) {
		if d != "app" && !strings.HasPrefix(d, ".") && d != "build" && d != "gradle" {
			modules = append(modules, d)
		}
	}
	for _, m := range modules {
		for _, f := range []string{"build.gradle", "build.gradle.kts"} {
			b, err := os.ReadFile(filepath.Join(root, m, f))
			if err != nil {
				continue
			}
			src := string(b)
			if !strings.Contains(src, "com.android.application") && !strings.Contains(src, "android.application") {
				continue
			}
			info.IsAndroid = true
			info.AppModule = m
			if mm := appIDRe.FindStringSubmatch(src); mm != nil {
				info.ApplicationID = mm[1]
			} else if mm := namespaceRe.FindStringSubmatch(src); mm != nil {
				info.ApplicationID = mm[1]
			}
			if mm := compileSdkRe.FindStringSubmatch(src); mm != nil {
				info.CompileSdk = mm[1]
			}
			return info
		}
	}
	return info
}

// ---------------- 汇总接口 ----------------

type EnvReport struct {
	Success  bool               `json:"success"`
	Termux   bool               `json:"termux"`
	Arch     string             `json:"arch"`
	Java     *ToolInfo          `json:"java"`
	Gradle   *ToolInfo          `json:"gradle"`
	Sdk      *SdkInfo           `json:"sdk"`
	Aapt2    string             `json:"aapt2"`
	Shell    DeviceShell        `json:"shell"`
	Project  AndroidProjectInfo `json:"project"`
	Problems []string           `json:"problems"` // 阻断构建的问题
	Hints    []string           `json:"hints"`    // 建议性提示
}

func buildEnvReport(root string, refreshShell bool) EnvReport {
	rep := EnvReport{
		Success: true,
		Termux:  isTermux(),
		Arch:    runtime.GOOS + "/" + runtime.GOARCH,
		Java:    detectJava(),
		Gradle:  detectGradle(root),
		Sdk:     detectSdk(root),
		Aapt2:   termuxAapt2(),
		Shell:   detectDeviceShell("", refreshShell),
	}
	if root != "" {
		rep.Project = inspectAndroidProject(root)
	}

	if rep.Java == nil {
		rep.Problems = append(rep.Problems, "未找到 JDK：请执行 pkg install openjdk-17（或设置 JAVA_HOME）")
	} else if v := javaMajor(rep.Java); v > 0 && v < 17 {
		rep.Problems = append(rep.Problems, "JDK 版本过低（需要 17+）：当前 "+rep.Java.Version)
	}
	if rep.Gradle == nil {
		rep.Problems = append(rep.Problems, "未找到 Gradle：请执行 pkg install gradle，或在项目中放置 gradlew")
	}
	if rep.Sdk == nil {
		rep.Problems = append(rep.Problems, "未找到 Android SDK：请设置 ANDROID_HOME，或安装至 ~/android-sdk（需包含 platforms/android-XX 与 build-tools）")
	} else {
		if len(rep.Sdk.BuildTools) == 0 {
			rep.Hints = append(rep.Hints, "SDK 中尚无 build-tools：AGP 8.7 默认使用 34.0.0，首次构建会自动下载（需已执行 sdkmanager --licenses），也可手动执行 sdkmanager \"build-tools;34.0.0\"")
		}
		if cs := rep.Project.CompileSdk; cs != "" {
			found := false
			for _, p := range rep.Sdk.Platforms {
				if p == "android-"+cs {
					found = true
				}
			}
			if !found {
				rep.Problems = append(rep.Problems, "SDK 中缺少 platforms;android-"+cs+"：sdkmanager \"platforms;android-"+cs+"\"")
			}
		}
	}
	if rep.Termux && rep.Aapt2 == "" {
		rep.Problems = append(rep.Problems, "Termux 下需要原生 aapt2（Maven 版仅支持 x86_64）：请执行 pkg install aapt2")
	}
	if !rep.Shell.Privileged() {
		rep.Hints = append(rep.Hints, "未检测到 Shizuku/adb：安装将跳转系统安装器手动确认，且无法强制停止应用与读取应用日志。启动 Shizuku 并导出 rish 到 PATH 即可解锁完整能力")
	}
	if rep.Termux {
		rep.Hints = append(rep.Hints, "Android 12+ 的幻影进程限制可能杀死 Gradle 守护进程。可通过 Shizuku 执行：device_config put activity_manager max_phantom_processes 2147483647")
	}
	if rep.Problems == nil {
		rep.Problems = []string{}
	}
	if rep.Hints == nil {
		rep.Hints = []string{}
	}
	return rep
}

// GET /api/android/env?root=...&refresh=1
func handleAndroidEnv(w http.ResponseWriter, r *http.Request) {
	root := ""
	if q := r.URL.Query().Get("root"); q != "" {
		abs, err := resolveProjectRoot(q)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "项目目录无效: %v", err)
			return
		}
		root = abs
	}
	writeJSON(w, http.StatusOK, buildEnvReport(root, r.URL.Query().Get("refresh") == "1"))
}
