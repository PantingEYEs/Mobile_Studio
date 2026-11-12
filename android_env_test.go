package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSharedSDKDiscovery(t *testing.T) {
	t.Setenv("MS_CONFIG_DIR", t.TempDir())
	t.Setenv("ANDROID_HOME", "")
	t.Setenv("ANDROID_SDK_ROOT", "")
	studio, project := t.TempDir(), t.TempDir()
	sdk := filepath.Join(studio, ".android-tools", "sdk")
	makeSDK := func(path string) {
		t.Helper()
		for _, sub := range []string{"platforms/android-35", "build-tools/34.0.0"} {
			if err := os.MkdirAll(filepath.Join(path, sub), 0755); err != nil {
				t.Fatal(err)
			}
		}
	}
	write := func(root, value string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, "local.properties"), []byte("sdk.dir="+value+"\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	check := func(root, want, source string) {
		t.Helper()
		got := detectSdkAt(root, studio)
		if got == nil || got.Path != want || got.Source != source {
			t.Fatalf("SDK = %+v, want %s (%s)", got, want, source)
		}
		if len(got.Platforms) != 1 || got.Platforms[0] != "android-35" || len(got.BuildTools) != 1 {
			t.Fatalf("SDK components missing: %+v", got)
		}
	}
	makeSDK(sdk)
	check(project, sdk, "Mobile Studio 共享 SDK")
	check("", sdk, "Mobile Studio 共享 SDK") // 新建项目还没有目录。
	write(project, "/missing/old-machine-sdk")
	check(project, sdk, "Mobile Studio 共享 SDK")
	shared := filepath.Join(studio, "custom-sdk")
	makeSDK(shared)
	write(studio, "custom-sdk")
	check(project, shared, "Mobile Studio local.properties")
	envSDK := t.TempDir()
	makeSDK(envSDK)
	t.Setenv("ANDROID_SDK_ROOT", envSDK)
	check(project, envSDK, "ANDROID_SDK_ROOT")
	t.Setenv("ANDROID_HOME", sdk)
	check(project, sdk, "ANDROID_HOME")
	makeSDK(filepath.Join(project, "own-sdk"))
	write(project, "own-sdk")
	check(project, filepath.Join(project, "own-sdk"), "local.properties")
	if err := os.WriteFile(configPath(), []byte(`{"android.sdk_dir":"`+shared+`"}`), 0644); err != nil {
		t.Fatal(err)
	}
	check(project, shared, "config")
}
