package main

// 持久化配置：~/.mobile_studio/config.json（可用 MS_CONFIG_DIR 覆盖目录）。
// 文件中只保存用户显式设置过的键，其余取默认值；服务端按文件修改时间热加载，
// 除 server.port 外修改后无需重启。

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type configKey struct {
	Key     string
	Type    string // string | int | bool | enum
	Default interface{}
	Enum    []string
	Desc    string
}

var configKeys = []configKey{
	{"server.port", "int", 8080, nil, "Web 服务监听端口（127.0.0.1），修改后需 m_s restart"},
	{"server.open_browser", "bool", true, nil, "执行 m_s / m_s start 时是否自动打开浏览器"},
	{"projects.default_dir", "string", "~", nil, "新建 / 克隆项目时的默认父目录"},
	{"android.sdk_dir", "string", "", nil, "Android SDK 路径；留空自动探测（local.properties → ANDROID_HOME → 常见位置）"},
	{"android.java_home", "string", "", nil, "JDK 目录；留空使用 JAVA_HOME 或 PATH 中的 java"},
	{"android.gradle", "string", "", nil, "Gradle 可执行文件；留空时优先项目 gradlew，其次 PATH 中的 gradle"},
	{"android.gradle_args", "string", "", nil, "附加给 Gradle 的参数，如 \"--offline --no-daemon\""},
	{"android.aapt2", "string", "", nil, "aapt2 覆盖路径；留空时 Termux 下自动使用 $PREFIX/bin/aapt2"},
	{"device.shell", "enum", "auto", []string{"auto", "rish", "adb", "local"}, "安装 / 启动 / Logcat 使用的设备通道"},
	{"device.adb_serial", "string", "", nil, "adb 目标设备序列号；留空使用第一个已连接设备"},
	{"logcat.tail", "int", 300, nil, "开始 Logcat 时回放的历史日志行数"},
	{"terminal.shell", "string", "", nil, "Web 终端使用的 Shell；留空依次尝试 $SHELL、bash、sh"},
}

func findConfigKey(key string) *configKey {
	for i := range configKeys {
		if configKeys[i].Key == key {
			return &configKeys[i]
		}
	}
	return nil
}

func configDir() string {
	if d := os.Getenv("MS_CONFIG_DIR"); d != "" {
		return expandHome(d)
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".mobile_studio")
}

func configPath() string { return filepath.Join(configDir(), "config.json") }

var (
	cfgMu    sync.Mutex
	cfgCache map[string]interface{}
	cfgMtime time.Time
	cfgPath  string
)

// loadConfigValues 读取用户设置的键值（带修改时间缓存）。
func loadConfigValues() map[string]interface{} {
	cfgMu.Lock()
	defer cfgMu.Unlock()
	p := configPath()
	st, err := os.Stat(p)
	if err != nil {
		cfgCache, cfgPath = map[string]interface{}{}, p
		return cfgCache
	}
	if cfgCache != nil && cfgPath == p && st.ModTime().Equal(cfgMtime) {
		return cfgCache
	}
	vals := map[string]interface{}{}
	if b, err := os.ReadFile(p); err == nil {
		json.Unmarshal(b, &vals)
	}
	cfgCache, cfgMtime, cfgPath = vals, st.ModTime(), p
	return vals
}

func cfgValue(key string) interface{} {
	k := findConfigKey(key)
	if k == nil {
		return nil
	}
	if v, ok := loadConfigValues()[key]; ok {
		return v
	}
	return k.Default
}

func cfgString(key string) string {
	switch v := cfgValue(key).(type) {
	case string:
		return strings.TrimSpace(v)
	case nil:
		return ""
	default:
		return fmt.Sprint(v)
	}
}

func cfgInt(key string) int {
	switch v := cfgValue(key).(type) {
	case float64:
		return int(v)
	case int:
		return v
	case string:
		n, _ := strconv.Atoi(v)
		return n
	}
	return 0
}

func cfgBool(key string) bool {
	switch v := cfgValue(key).(type) {
	case bool:
		return v
	case string:
		b, _ := strconv.ParseBool(v)
		return b
	}
	return false
}

// parseConfigValue 按键类型校验并转换命令行传入的字符串。
func parseConfigValue(k *configKey, raw string) (interface{}, error) {
	switch k.Type {
	case "int":
		n, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil {
			return nil, fmt.Errorf("%s 需要整数，收到 %q", k.Key, raw)
		}
		if k.Key == "server.port" && (n < 1024 || n > 65535) {
			return nil, fmt.Errorf("端口需在 1024-65535 之间")
		}
		if n < 0 {
			return nil, fmt.Errorf("%s 不能为负数", k.Key)
		}
		return n, nil
	case "bool":
		b, err := strconv.ParseBool(strings.TrimSpace(raw))
		if err != nil {
			return nil, fmt.Errorf("%s 需要 true / false，收到 %q", k.Key, raw)
		}
		return b, nil
	case "enum":
		for _, e := range k.Enum {
			if raw == e {
				return raw, nil
			}
		}
		return nil, fmt.Errorf("%s 可选值: %s", k.Key, strings.Join(k.Enum, " | "))
	default:
		return raw, nil
	}
}

func writeJSONFileAtomic(path string, v interface{}) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func saveConfigValues(vals map[string]interface{}) error {
	if err := writeJSONFileAtomic(configPath(), vals); err != nil {
		return err
	}
	cfgMu.Lock()
	cfgCache = nil
	cfgMu.Unlock()
	return nil
}

func setConfig(key, raw string) (interface{}, error) {
	k := findConfigKey(key)
	if k == nil {
		return nil, fmt.Errorf("未知配置项 %q（m_s config list 查看全部）", key)
	}
	v, err := parseConfigValue(k, raw)
	if err != nil {
		return nil, err
	}
	vals := map[string]interface{}{}
	for kk, vv := range loadConfigValues() {
		vals[kk] = vv
	}
	vals[key] = v
	return v, saveConfigValues(vals)
}

func unsetConfig(key string) error {
	if findConfigKey(key) == nil {
		return fmt.Errorf("未知配置项 %q", key)
	}
	vals := map[string]interface{}{}
	for kk, vv := range loadConfigValues() {
		if kk != key {
			vals[kk] = vv
		}
	}
	return saveConfigValues(vals)
}

type ConfigEntry struct {
	Key     string      `json:"key"`
	Value   interface{} `json:"value"`
	Default interface{} `json:"default"`
	IsSet   bool        `json:"isSet"`
	Type    string      `json:"type"`
	Enum    []string    `json:"enum,omitempty"`
	Desc    string      `json:"desc"`
}

func listConfig() []ConfigEntry {
	vals := loadConfigValues()
	out := make([]ConfigEntry, 0, len(configKeys))
	for _, k := range configKeys {
		v, set := vals[k.Key]
		if !set {
			v = k.Default
		}
		if f, ok := v.(float64); ok && k.Type == "int" {
			v = int(f)
		}
		out = append(out, ConfigEntry{Key: k.Key, Value: v, Default: k.Default, IsSet: set, Type: k.Type, Enum: k.Enum, Desc: k.Desc})
	}
	return out
}
