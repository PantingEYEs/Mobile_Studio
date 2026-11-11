package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAndroidScaffoldParameters(t *testing.T) {
	s, err := newAndroidScaffold("Demo", url.Values{})
	if err != nil {
		t.Fatal(err)
	}
	if s.Package != "com.example.demo" || s.Lang != "kotlin" || s.MinSdk != 24 || s.TargetSdk != 35 || s.CompileSdk != 35 || s.Template != "empty_views" {
		t.Fatalf("unexpected defaults: %+v", s)
	}
	for _, q := range []url.Values{
		{"pkg": {"com.class.demo"}},
		{"pkg": {"invalid"}},
		{"lang": {"swift"}},
		{"template": {"typo"}},
		{"minSdk": {"invalid"}},
		{"minSdk": {"20"}},
		{"minSdk": {"35"}, "targetSdk": {"34"}},
		{"targetSdk": {"35"}, "compileSdk": {"34"}},
	} {
		if _, err := newAndroidScaffold("Demo", q); err == nil {
			t.Errorf("expected invalid parameters to fail: %v", q)
		}
	}
}

func TestAndroidScaffoldCreateAPI(t *testing.T) {
	t.Setenv("MS_CONFIG_DIR", t.TempDir())
	parent := t.TempDir()
	for _, lang := range []string{"java", "kotlin"} {
		for _, kind := range []string{"empty_views", "trampoline", "none"} {
			t.Run(lang+"/"+kind, func(t *testing.T) {
				name := lang + "-" + kind
				q := url.Values{"dir": {parent}, "name": {name}, "type": {"android"}, "pkg": {"org.example.custom.demo"}, "lang": {lang}, "template": {kind}, "minSdk": {"26"}, "targetSdk": {"33"}, "compileSdk": {"34"}}
				rec := httptest.NewRecorder()
				handleProjectCreate(rec, httptest.NewRequest("GET", "/api/fs/project/create?"+q.Encode(), nil))
				if rec.Code != http.StatusOK {
					t.Fatalf("create failed: %s", rec.Body.String())
				}
				var result struct {
					Success bool   `json:"success"`
					Path    string `json:"path"`
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil || !result.Success || result.Path != filepath.Join(parent, name) {
					t.Fatalf("unexpected response: %s", rec.Body.String())
				}
				read := func(rel string) string {
					t.Helper()
					b, err := os.ReadFile(filepath.Join(result.Path, rel))
					if err != nil {
						t.Fatal(err)
					}
					return string(b)
				}
				gradle := read("app/build.gradle")
				for _, want := range []string{"org.example.custom.demo", "minSdk 26", "targetSdk 33", "compileSdk 34"} {
					if !strings.Contains(gradle, want) {
						t.Errorf("missing Gradle setting %q", want)
					}
				}
				if strings.Contains(read("gradle.properties"), "android.aapt2FromMavenOverride=") {
					t.Fatal("generated project must not hardcode the device aapt2 path")
				}
				wrapper, err := os.Stat(filepath.Join(result.Path, "gradlew"))
				if err != nil || wrapper.Mode()&0100 == 0 {
					t.Fatal("missing executable Gradle Wrapper")
				}
				if read("gradle/wrapper/gradle-wrapper.jar") != mustReadWrapper(t) {
					t.Fatal("Gradle Wrapper jar changed during generation")
				}
				var manifest manifestXML
				if err := xml.Unmarshal([]byte(read("app/src/main/AndroidManifest.xml")), &manifest); err != nil {
					t.Fatal(err)
				}
				srcRoot, ext := "java", "java"
				if lang == "kotlin" {
					srcRoot, ext = "kotlin", "kt"
				}
				source := "app/src/main/" + srcRoot + "/org/example/custom/demo/MainActivity." + ext
				if kind == "none" {
					if len(manifest.Application.Activities) != 0 {
						t.Fatal("No Activity template unexpectedly has an activity")
					}
					if _, err := os.Stat(filepath.Join(result.Path, source)); !os.IsNotExist(err) {
						t.Fatal("No Activity template unexpectedly has a MainActivity")
					}
					return
				}
				if act := launcherFromManifest(result.Path, "app"); act != "org.example.custom.demo.MainActivity" {
					t.Fatalf("unexpected launcher: %q", act)
				}
				activity := read(source)
				if kind == "trampoline" {
					if !strings.Contains(activity, "finish()") || strings.Contains(activity, "setContentView") || !strings.Contains(read("app/src/main/AndroidManifest.xml"), "Theme.Translucent.NoTitleBar") {
						t.Fatal("trampoline must exit without creating a view")
					}
					if _, err := os.Stat(filepath.Join(result.Path, "app/src/main/res/layout/activity_main.xml")); !os.IsNotExist(err) {
						t.Fatal("trampoline must not create a layout")
					}
				} else if !strings.Contains(activity, "setContentView") || read("app/src/main/res/layout/activity_main.xml") == "" {
					t.Fatal("empty activity must display its layout")
				}
			})
		}
	}
}

func mustReadWrapper(t *testing.T) string {
	t.Helper()
	b, err := scaffoldGradleFiles.ReadFile("gradle/wrapper/gradle-wrapper.jar")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Opt in on a prepared Android build host:
// MS_SCAFFOLD_BUILD_TEST=1 go test -run TestAndroidScaffoldBuild -v -timeout 30m
// Set MS_SCAFFOLD_OUTPUT_DIR to retain generated projects and APKs for review.
func TestAndroidScaffoldBuild(t *testing.T) {
	if os.Getenv("MS_SCAFFOLD_BUILD_TEST") != "1" {
		t.Skip("set MS_SCAFFOLD_BUILD_TEST=1 to build all six generated variants")
	}
	sdk := detectSdk(selfRoot())
	if sdk == nil {
		t.Fatal("Android SDK required")
	}
	t.Setenv("ANDROID_HOME", sdk.Path)
	t.Setenv("MS_CONFIG_DIR", t.TempDir())
	aapt2 := termuxAapt2()
	if aapt2 == "" {
		if len(sdk.BuildTools) == 0 {
			t.Fatal("Android Build Tools required")
		}
		aapt2 = filepath.Join(sdk.Path, "build-tools", sdk.BuildTools[len(sdk.BuildTools)-1], "aapt2")
	}
	parent := os.Getenv("MS_SCAFFOLD_OUTPUT_DIR")
	if parent == "" {
		parent = t.TempDir()
	}
	if err := os.MkdirAll(parent, 0755); err != nil {
		t.Fatal(err)
	}
	for _, lang := range []string{"java", "kotlin"} {
		for _, kind := range []string{"empty_views", "trampoline", "none"} {
			t.Run(lang+"/"+kind, func(t *testing.T) {
				dir, err := os.MkdirTemp(parent, lang+"-"+kind+"-")
				if err != nil {
					t.Fatal(err)
				}
				q := url.Values{"lang": {lang}, "template": {kind}, "pkg": {"org.example.scaffold.demo"}}
				s, err := newAndroidScaffold("Demo 'Quoted' $Name", q)
				if err != nil {
					t.Fatal(err)
				}
				if err := s.write(dir); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
				defer cancel()
				task := &Task{notify: make(chan struct{})}
				apk, err := doBuild(ctx, task, dir)
				if err != nil {
					t.Log(strings.Join(task.lines, "\n"))
					t.Fatal(err)
				}
				out, err := exec.CommandContext(ctx, aapt2, "dump", "badging", apk).CombinedOutput()
				if err != nil {
					t.Fatalf("inspect APK: %v: %s", err, out)
				}
				for _, want := range []string{"name='org.example.scaffold.demo'", "sdkVersion:'24'", "targetSdkVersion:'35'"} {
					// aapt2 uses minSdkVersion on newer builds and sdkVersion on older ones.
					if !strings.Contains(string(out), want) && !(want == "sdkVersion:'24'" && strings.Contains(string(out), "minSdkVersion:'24'")) {
						t.Errorf("APK metadata missing %q: %s", want, out)
					}
				}
				if strings.Contains(string(out), "launchable-activity:") != (kind != "none") {
					t.Fatalf("unexpected APK launcher: %s", out)
				}
				if kind != "none" {
					z, err := zip.OpenReader(apk)
					if err != nil {
						t.Fatal(err)
					}
					defer z.Close()
					compiledActivity := false
					for _, file := range z.File {
						if !strings.HasSuffix(file.Name, ".dex") {
							continue
						}
						r, err := file.Open()
						if err != nil {
							t.Fatal(err)
						}
						b, err := io.ReadAll(r)
						r.Close()
						if err != nil {
							t.Fatal(err)
						}
						compiledActivity = compiledActivity || bytes.Contains(b, []byte("Lorg/example/scaffold/demo/MainActivity;"))
					}
					if !compiledActivity {
						t.Fatal("launcher declared in Manifest but MainActivity is absent from compiled DEX")
					}
				}
				t.Logf("built and inspected %s", apk)
			})
		}
	}
}
