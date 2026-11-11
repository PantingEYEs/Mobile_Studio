# Mobile Studio Android 启动器（第一阶段）

APK 只充当桌面跳板：点击图标 → 请求 Termux 执行命令 → 启动现有 Go 服务 → 系统浏览器打开现有页面。没有 WebView，也不打包 Go、Node 或构建工具。关闭浏览器或启动器不会停止服务，停止仍使用 `m_s :q`。

## 首次准备

1. 安装并打开 Termux，在 `~/Mobile_Studio` 准备好本项目，确认 `bash ~/Mobile_Studio/scripts/m_s start` 可以正常打开页面。
2. 在 `~/.termux/termux.properties` 中新增或修改唯一一项 `allow-external-apps = true`，然后执行 `termux-reload-settings`。这允许获授权的外部应用调用 Termux。
3. 安装 APK 后，首次点击图标时允许「在 Termux 环境中运行命令」。如果已拒绝，到系统设置 → 应用 → Mobile Studio → 权限中授权。
4. Android 若阻止 Termux 自动切换到前台，点击 Termux 通知继续；需要自动切换时，可以在系统设置中为 Termux 允许「显示在其他应用上层」。

接口和授权要求见 [Termux 官方 RUN_COMMAND 文档](https://github.com/termux/termux-app/wiki/RUN_COMMAND-Intent)。启动器使用前台终端会话，便于查看构建与启动错误。

默认源码位置为 `~/Mobile_Studio`；自定义路径可在 Termux 的 `~/.bash_profile` 中导出 `MS_ROOT`。启动器使用 Bash 登录 Shell 读取该配置。不会接收外部 Intent 中的路径或命令。

## 在 Mobile Studio 内构建

根目录现在也是 Android Gradle 工程，应用模块为 `app`，包名为 `dev.mobilestudio.launcher`。Java 和 Android Gradle 插件版本与现有脚手架一致：JDK 17、AGP 8.7.3、compile/target SDK 35、最低 Android 8.0。

先按主 README「Android 构建 / 安装 / 运行 / 日志」准备 JDK、aapt2 和 Android SDK。本工程附带 Gradle Wrapper，固定使用与 AGP 8.7.3 配套的 Gradle 8.9，并校验下载包 SHA-256；首次构建会下载 Gradle。Mobile Studio 会优先使用项目的 `gradlew`，无需依赖系统 Gradle 版本。

在 Mobile Studio 中选择自身项目，打开「运行」面板执行构建；也可在项目根目录执行：

```sh
bash scripts/m_s env
bash scripts/m_s build
```

成功后 APK 位于 `app/build/outputs/apk/debug/app-debug.apk`。普通权限下在 WebUI 点击「下载安装」，下载后从浏览器打开 APK 并确认安装，再在安装器中点击「打开」，无需切回 Termux。首次需允许浏览器安装未知应用。Shizuku/adb 通道可用时，可以通过「运行」完成构建、安装和启动。启动器首次运行仍需要 Termux 执行命令权限。

APK 点击时执行现有 `open` 命令，它会按需构建、启动服务并打开浏览器，因此会读取现有端口配置，并且即使 `server.open_browser=false` 也会打开浏览器。已有服务会复用；源码变动时仍由原启动脚本进行增量构建，旧服务版本的更新沿用现有 `m_s restart` 流程。

## 验收

- 首次授权后能从桌面图标打开浏览器中的 Mobile Studio。
- 已有服务运行时再次点击图标仍能打开页面。
- 修改 `server.port` 并重启服务后，图标打开新端口。
- 服务停止后点击图标能重新启动。
- 拒绝权限、未安装 Termux 时有提示；未开启外部调用或脚本启动失败时查看 Termux 错误信息。
- 浏览器中的文件、终端与构建功能继续由原 Termux 服务提供。
