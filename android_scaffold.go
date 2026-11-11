package main

// Android 项目脚手架：生成可直接通过 Gradle 构建出 APK 的最小工程。
// 仅依赖 Android 框架 API（android.app.Activity + 系统 Material 主题），
// 不引入 AndroidX 依赖，以降低移动网络下首次构建需要下载的依赖量。

import (
	"embed"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"
)

//go:embed gradlew gradlew.bat gradle/wrapper/gradle-wrapper.jar gradle/wrapper/gradle-wrapper.properties
var scaffoldGradleFiles embed.FS

const (
	agpVersion    = "8.7.3"
	kotlinVersion = "2.0.21"
)

type androidScaffold struct {
	ProjectName string
	AppName     string // 已做 XML 转义
	Package     string
	Lang        string // kotlin | java
	MinSdk      int
	TargetSdk   int
	CompileSdk  int
	Template    string // empty_views | trampoline | none
	AgpVersion  string
	KotlinVer   string
}

func (s androidScaffold) Kotlin() bool     { return s.Lang == "kotlin" }
func (s androidScaffold) Trampoline() bool { return s.Template == "trampoline" }

// Groovy 单引号字符串不会将项目名中的 $ 当作插值代码。
func (s androidScaffold) GradleProjectName() string {
	return "'" + strings.NewReplacer("\\", "\\\\", "'", "\\'", "\r", "\\r", "\n", "\\n", "\t", "\\t").Replace(s.ProjectName) + "'"
}

var javaKeywords = map[string]bool{
	"abstract": true, "assert": true, "boolean": true, "break": true, "byte": true, "case": true,
	"catch": true, "char": true, "class": true, "const": true, "continue": true, "default": true,
	"do": true, "double": true, "else": true, "enum": true, "extends": true, "final": true,
	"finally": true, "float": true, "for": true, "goto": true, "if": true, "implements": true,
	"import": true, "instanceof": true, "int": true, "interface": true, "long": true, "native": true,
	"new": true, "package": true, "private": true, "protected": true, "public": true, "return": true,
	"short": true, "static": true, "strictfp": true, "super": true, "switch": true, "synchronized": true,
	"this": true, "throw": true, "throws": true, "transient": true, "try": true, "void": true,
	"volatile": true, "while": true, "true": true, "false": true, "null": true,
	// Kotlin 硬关键字
	"as": true, "fun": true, "in": true, "is": true, "object": true, "typealias": true,
	"typeof": true, "val": true, "var": true, "when": true,
}

func validatePackageName(pkg string) error {
	if !pkgNameRe.MatchString(pkg) {
		return fmt.Errorf("包名格式无效: %q（需形如 com.example.app，至少两段，每段以字母开头）", pkg)
	}
	for _, seg := range strings.Split(pkg, ".") {
		if javaKeywords[seg] {
			return fmt.Errorf("包名中不能包含关键字: %q", seg)
		}
	}
	return nil
}

func xmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, `\"`, "'", `\'`, "@", `\@`, "?", `\?`)
	// 资源字符串中 @ ? 开头有特殊含义，引号需反斜杠转义
	return r.Replace(s)
}

func sdkParam(q url.Values, key string, def int) (int, error) {
	v := strings.TrimSpace(q.Get(key))
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 21 || n > 40 {
		return 0, fmt.Errorf("%s 取值无效: %q", key, v)
	}
	return n, nil
}

func newAndroidScaffold(projName string, q url.Values) (*androidScaffold, error) {
	s := &androidScaffold{
		ProjectName: projName,
		AppName:     xmlEscape(projName),
		AgpVersion:  agpVersion,
		KotlinVer:   kotlinVersion,
	}

	s.Package = strings.TrimSpace(q.Get("pkg"))
	if s.Package == "" {
		sub := strings.ToLower(projName)
		sub = strings.Map(func(r rune) rune {
			if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
				return r
			}
			return '_'
		}, sub)
		if sub == "" || (sub[0] >= '0' && sub[0] <= '9') || sub[0] == '_' {
			sub = "app" + sub
		}
		s.Package = "com.example." + sub
	}
	if err := validatePackageName(s.Package); err != nil {
		return nil, err
	}

	s.Lang = strings.TrimSpace(q.Get("lang"))
	if s.Lang == "" {
		s.Lang = "kotlin"
	}
	if s.Lang != "java" && s.Lang != "kotlin" {
		return nil, fmt.Errorf("不支持的开发语言: %q（可选 kotlin / java）", s.Lang)
	}
	s.Template = strings.TrimSpace(q.Get("template"))
	if s.Template == "" {
		s.Template = "empty_views"
	}
	if s.Template != "empty_views" && s.Template != "trampoline" && s.Template != "none" {
		return nil, fmt.Errorf("不支持的模板: %q（可选 empty_views / trampoline / none）", s.Template)
	}

	var err error
	if s.MinSdk, err = sdkParam(q, "minSdk", 24); err != nil {
		return nil, err
	}
	if s.TargetSdk, err = sdkParam(q, "targetSdk", 35); err != nil {
		return nil, err
	}
	if s.CompileSdk, err = sdkParam(q, "compileSdk", s.TargetSdk); err != nil {
		return nil, err
	}
	if s.MinSdk > s.TargetSdk {
		return nil, fmt.Errorf("minSdk (%d) 不能大于 targetSdk (%d)", s.MinSdk, s.TargetSdk)
	}
	if s.TargetSdk > s.CompileSdk {
		return nil, fmt.Errorf("targetSdk (%d) 不能大于 compileSdk (%d)", s.TargetSdk, s.CompileSdk)
	}
	return s, nil
}

var scaffoldTemplates = map[string]string{
	"settings.gradle": `pluginManagement {
    repositories {
        google()
        mavenCentral()
        gradlePluginPortal()
    }
}
dependencyResolutionManagement {
    repositoriesMode.set(RepositoriesMode.FAIL_ON_PROJECT_REPOS)
    repositories {
        google()
        mavenCentral()
    }
}

rootProject.name = {{.GradleProjectName}}
include ':app'
`,
	"build.gradle": `// Top-level build file
plugins {
    id 'com.android.application' version '{{.AgpVersion}}' apply false
{{- if .Kotlin}}
    id 'org.jetbrains.kotlin.android' version '{{.KotlinVer}}' apply false
{{- end}}
}
`,
	"gradle.properties": `# Gradle 守护进程内存（手机端适当收紧）
org.gradle.jvmargs=-Xmx1536m -Dfile.encoding=UTF-8
org.gradle.workers.max=2
android.useAndroidX=true
android.nonTransitiveRClass=true
{{- if .Kotlin}}
kotlin.code.style=official
{{- end}}
# 注：Termux (aarch64) 下的 aapt2 覆盖由 Mobile Studio 在构建时通过
# -Pandroid.aapt2FromMavenOverride 注入，因此本项目在 PC 上也可直接构建。
`,
	".gitignore": `*.iml
.gradle/
/local.properties
/.idea/
.DS_Store
/build/
/app/build/
/captures/
.externalNativeBuild/
.cxx/
`,
	"app/.gitignore": "/build\n",
	"app/proguard-rules.pro": `# Add project specific ProGuard rules here.
# See http://developer.android.com/guide/developing/tools/proguard.html
`,
	"app/build.gradle": `plugins {
    id 'com.android.application'
{{- if .Kotlin}}
    id 'org.jetbrains.kotlin.android'
{{- end}}
}

android {
    namespace '{{.Package}}'
    compileSdk {{.CompileSdk}}

    defaultConfig {
        applicationId "{{.Package}}"
        minSdk {{.MinSdk}}
        targetSdk {{.TargetSdk}}
        versionCode 1
        versionName "1.0"
    }

    buildTypes {
        release {
            minifyEnabled false
            proguardFiles getDefaultProguardFile('proguard-android-optimize.txt'), 'proguard-rules.pro'
        }
    }

    compileOptions {
        sourceCompatibility JavaVersion.VERSION_17
        targetCompatibility JavaVersion.VERSION_17
    }
{{- if .Kotlin}}

    kotlinOptions {
        jvmTarget = '17'
    }
{{- end}}
}

dependencies {
}
`,
	"app/src/main/res/values/strings.xml": `<resources>
    <string name="app_name">{{.AppName}}</string>
</resources>
`,
	"app/src/main/res/values/themes.xml": `<resources>
    <style name="Theme.App" parent="@android:style/Theme.Material.Light.DarkActionBar" />
</resources>
`,
}

var scaffoldManifestWithActivity = `<?xml version="1.0" encoding="utf-8"?>
<manifest xmlns:android="http://schemas.android.com/apk/res/android">

    <application
        android:allowBackup="true"
        android:icon="@android:drawable/sym_def_app_icon"
        android:label="@string/app_name"
        android:theme="@style/Theme.App">
        <activity
            android:name=".MainActivity"
{{- if .Trampoline}}
            android:theme="@android:style/Theme.Translucent.NoTitleBar"
            android:excludeFromRecents="true"
{{- end}}
            android:exported="true">
            <intent-filter>
                <action android:name="android.intent.action.MAIN" />
                <category android:name="android.intent.category.LAUNCHER" />
            </intent-filter>
        </activity>
    </application>

</manifest>
`

var scaffoldManifestNoActivity = `<?xml version="1.0" encoding="utf-8"?>
<manifest xmlns:android="http://schemas.android.com/apk/res/android">

    <application
        android:allowBackup="true"
        android:icon="@android:drawable/sym_def_app_icon"
        android:label="@string/app_name"
        android:theme="@style/Theme.App" />

</manifest>
`

var scaffoldLayout = `<?xml version="1.0" encoding="utf-8"?>
<LinearLayout xmlns:android="http://schemas.android.com/apk/res/android"
    android:layout_width="match_parent"
    android:layout_height="match_parent"
    android:gravity="center"
    android:orientation="vertical">

    <TextView
        android:layout_width="wrap_content"
        android:layout_height="wrap_content"
        android:text="Hello Mobile Studio!"
        android:textSize="20sp" />
</LinearLayout>
`

var scaffoldKotlinActivity = `package {{.Package}}

import android.app.Activity
import android.os.Bundle
import android.util.Log

class MainActivity : Activity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
{{- if .Trampoline}}
        // TODO: 在这里启动目标 Activity、打开链接或分发其他操作。
        // 不创建应用内界面，操作分发后退出入口 Activity。
        Log.i("MainActivity", "onCreate: trampoline")
        finish()
{{- else}}
        setContentView(R.layout.activity_main)
        Log.i("MainActivity", "onCreate: Hello Mobile Studio!")
{{- end}}
    }
}
`

var scaffoldJavaActivity = `package {{.Package}};

import android.app.Activity;
import android.os.Bundle;
import android.util.Log;

public class MainActivity extends Activity {
    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
{{- if .Trampoline}}
        // TODO: 在这里启动目标 Activity、打开链接或分发其他操作。
        // 不创建应用内界面，操作分发后退出入口 Activity。
        Log.i("MainActivity", "onCreate: trampoline");
        finish();
{{- else}}
        setContentView(R.layout.activity_main);
        Log.i("MainActivity", "onCreate: Hello Mobile Studio!");
{{- end}}
    }
}
`

// files 返回 相对路径 → 文件内容 的完整清单。
func (s *androidScaffold) files() (map[string]string, error) {
	out := map[string]string{}
	render := func(name, tpl string) error {
		t, err := template.New(name).Parse(tpl)
		if err != nil {
			return err
		}
		var b strings.Builder
		if err := t.Execute(&b, s); err != nil {
			return err
		}
		out[name] = b.String()
		return nil
	}
	for name, tpl := range scaffoldTemplates {
		if err := render(name, tpl); err != nil {
			return nil, err
		}
	}
	for _, name := range []string{"gradlew", "gradlew.bat", "gradle/wrapper/gradle-wrapper.jar", "gradle/wrapper/gradle-wrapper.properties"} {
		b, err := scaffoldGradleFiles.ReadFile(name)
		if err != nil {
			return nil, fmt.Errorf("读取 Gradle Wrapper 失败 %s: %w", name, err)
		}
		out[name] = string(b)
	}

	srcRoot := "java"
	if s.Kotlin() {
		srcRoot = "kotlin" // AGP 8.7 + Kotlin 插件默认识别 src/main/kotlin
	}
	pkgDir := strings.ReplaceAll(s.Package, ".", "/")

	if s.Template != "none" {
		if err := render("app/src/main/AndroidManifest.xml", scaffoldManifestWithActivity); err != nil {
			return nil, err
		}
		if s.Template == "empty_views" {
			out["app/src/main/res/layout/activity_main.xml"] = scaffoldLayout
		}
		if s.Kotlin() {
			if err := render("app/src/main/"+srcRoot+"/"+pkgDir+"/MainActivity.kt", scaffoldKotlinActivity); err != nil {
				return nil, err
			}
		} else {
			if err := render("app/src/main/"+srcRoot+"/"+pkgDir+"/MainActivity.java", scaffoldJavaActivity); err != nil {
				return nil, err
			}
		}
	} else {
		out["app/src/main/AndroidManifest.xml"] = scaffoldManifestNoActivity
		out["app/src/main/"+srcRoot+"/"+pkgDir+"/.gitkeep"] = ""
	}
	return out, nil
}

// write 将全部文件写入 projDir；任一文件失败即返回错误。
func (s *androidScaffold) write(projDir string) error {
	files, err := s.files()
	if err != nil {
		return err
	}
	for rel, content := range files {
		full := filepath.Join(projDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			return fmt.Errorf("创建目录失败 %s: %v", rel, err)
		}
		mode := os.FileMode(0644)
		if rel == "gradlew" {
			mode = 0755
		}
		if err := os.WriteFile(full, []byte(content), mode); err != nil {
			return fmt.Errorf("写入文件失败 %s: %v", rel, err)
		}
	}
	// 若已能定位 SDK，预先写入 local.properties，便于在其他工具中直接打开
	if sdk := detectSdk(""); sdk != nil {
		os.WriteFile(filepath.Join(projDir, "local.properties"), []byte("sdk.dir="+sdk.Path+"\n"), 0644)
	}
	return nil
}
