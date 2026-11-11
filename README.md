# Mobile Studio

> 基于 Android 15 + One UI 7.0 宿主环境的无 Root、不破坏 Knox 的轻量级本地应用与 Web 服务闭环系统。

## 架构概览

- 宿主环境：Android 15 / One UI 7.0 (Samsung Knox 保持 0x0, 无 Root)
- 终端运行时：Termux (aarch64)
- 后端服务：Go (Golang) 本地 HTTP 服务 + PTY 终端桥接
- 前端技术：Svelte (编译产物通过 Go embed 静态打包为单二进制文件)
- 权限与意图：Shizuku (rish) / 本地无线调试 / Android Intent 通信

## 已完成核心特性 (0.0.1-dev)

1. **工业级 Git 分支拓扑可视化引擎 (比肩 Android Studio / GitKraken)**：
   - 主轴自由切换与拉直：在 UI 中自由指定任意现存分支为主轴，基于 First-Parent 递归溯源强制拉直居中垂线。
   - 区间图复用与相对步进：引入基于生存区间的泳道复用算法 (Interval Lane Reuse)，互不冲突的分支自动复用贴近主干的第 1 列 (22px)，彻底根治历史分支横向无限漂移；同源多分支天然相对步进。
   - 全真贝塞尔连线：基于真实 DAG 关系绘制平滑有向边，根除断点与虚假枝干。
   - 极简双层排版：短哈希在上、分支徽标在下紧贴不遮挡；左右两翼实现径向对称镜像对齐。
   - 精准手势与触控：支持双指捏合缩放 (0.5x ~ 1.8x)、单指平移拖拽与一键居中复位；14px 独立触控垫防误触，点击节点圆心放大并亮起白芯，点击空白处平滑取消激活。
2. **轻量抽屉式项目文件树 (FilePanel.svelte)**：
   - 工业级单层惰性加载 (On-Demand Lazy Loading)：按需仅读取直属下一层，即使浏览 node_modules 或 .git 也绝无内存溢出风险。
   - 默认完全折叠，支持多级目录逐层手动展开/收起，文件项具备点击高亮反馈。
3. **高内聚低耦合解耦架构**：
   - graphLayout.js (纯数学拓扑引擎)
   - ControlPanel.svelte (顶部紧凑悬浮栏)
   - CommitNode.svelte (节点与卡片原子组件)
   - FilePanel.svelte (左侧文件树抽屉)
   - App.svelte (轻量级视口主控舞台，代码量缩减至不足百行)

## 快速使用

### Android 桌面启动器（第一阶段）

项目根目录包含 `app` Android 应用模块，可在 Mobile Studio 自身项目的运行面板中构建 APK。点击应用图标会通过 Termux 启动服务并打开系统浏览器，无应用内页面。首次授权、环境准备与构建步骤见 [Android 启动器说明](docs/android-launcher.md)。

### 启动服务并打开预设浏览器
在 Termux 终端任意目录下执行：

    m_s

该指令将自动完成：
1. 依赖环境按需自检 (Go、Git、Node、npm)。
2. 检测前端依赖、前端源码与 Go 源码变动并按需增量安装 / 构建 / 编译。
3. 后台启动本地 HTTP 服务（监听 127.0.0.1，默认端口 8080，可通过 `m_s config set server.port` 修改）。
4. 调起系统预设浏览器访问服务（`m_s start --no-browser` 或配置 `server.open_browser=false` 可跳过）。

### 停止后台服务 (Vim 风格)

    m_s :q

Web UI 中的全部操作均有对应的命令行，详见下文「[命令行 (m_s)](#命令行-m_s)」。

### Web 终端

点击右上角「⌨ 终端」进入全屏终端，在当前项目目录启动 Termux 登录 Shell，无需在 Termux 与浏览器之间来回切换。

- 多会话标签页，会话独立于页面存活：关闭页面或刷新后重新打开，会回放最近 512KB 输出并继续交互。
- 底部为与 Termux 一致的额外按键行（ESC / - HOME ↑ END PGUP，TAB CTRL ALT ← ↓ → PGDN）；CTRL / ALT 为粘滞键，作用于下一个按键，方向键长按连发。
- 工具栏支持粘贴剪贴板（自动适配 bracketed paste）与字号调节；Shell 退出后按回车在原目录重新启动。
- 安全：所有 `/api/` 接口仅接受来自本机 Mobile Studio 页面的请求（校验回环 Host 与同源 Origin / Sec-Fetch-Site），防止浏览器中其它网站借助 127.0.0.1 执行命令。

### Android 构建 / 安装 / 运行 / 日志

点击右上角「▶ 运行」打开运行面板：一键执行 构建 (assembleDebug) → 安装 → 启动，并自动打开仅包含本应用进程的 Logcat。

Termux 端一次性准备：

    pkg install openjdk-17 gradle aapt2
    mkdir -p ~/android-sdk/cmdline-tools && cd ~/android-sdk/cmdline-tools
    # 从 developer.android.com 下载 commandlinetools-linux-*_latest.zip 后：
    unzip commandlinetools-linux-*_latest.zip && mv cmdline-tools latest
    yes | ~/android-sdk/cmdline-tools/latest/bin/sdkmanager --licenses
    ~/android-sdk/cmdline-tools/latest/bin/sdkmanager "platforms;android-35" "build-tools;34.0.0"

- aarch64 下的 aapt2 覆盖在构建时以 `-Pandroid.aapt2FromMavenOverride` 注入，不写入项目文件，项目在 PC / Android Studio 上可直接打开构建。
- 设备通道按 Shizuku (`rish`) → 无线调试 (`adb`) → 无特权 的顺序自动选择。无特权时在 WebUI 点击「下载安装」，由浏览器下载 APK，打开下载项后确认安装；首次需允许浏览器安装未知应用，安装后可在安装器中点击「打开」。无需切换到 Termux；无法强停与读取应用日志。
- 无特权下「运行」只完成构建和准备 APK，显示「待手动安装」；CLI 的安装任务返回 `manualInstall=true` 和 `downloadUrl`，不代表安装完成。
- 运行面板顶部「环境」徽标会列出缺失的依赖及修复命令。

### 已知限制：移动数据下的 Shizuku

在当前三星 Android 15 / One UI 7.0 设备上，已实测仅从 Wi-Fi 切换到移动数据也会出现 Wireless Debugging 断开、`rish` 返回 `Server is not running`，无法继续通过 Shizuku 安装和启动应用。此问题暂缓处理，尚无本机验证有效的规避方案；不能保证提前在 Wi-Fi 下启动 Shizuku 后就能持续使用。

通道失效时仍可构建 APK，并通过 WebUI「下载安装」手动安装。ADB TCP 模式和第三方分支尚未在本机验证。复现过程和后续调查范围见 [已知问题记录](docs/known-issues.md)。

## 命令行 (m_s)

`m_s` 既是启动器，也是 Mobile Studio 的完整命令行入口。服务生命周期命令由脚本 `scripts/m_s` 处理，其余命令由 Go 二进制通过 HTTP 调用正在运行的本地服务执行（服务未运行时会自动在后台启动，不打开浏览器）。因此 **命令行发起的构建、终端会话等会实时出现在 Web UI 中**，反之亦然；`m_s help` 可随时查看完整用法。

### 服务

| 命令 | 说明 |
|---|---|
| `m_s` / `m_s start [--no-browser]` | 按需构建并启动服务，默认打开浏览器 |
| `m_s stop` / `m_s :q` | 停止服务（同时停止运行中的构建 / Logcat 任务并关闭终端会话） |
| `m_s restart [--open]` | 重启服务（更新代码或修改端口后使用） |
| `m_s status` | 服务状态：版本、PID、运行时长、运行中任务数、终端会话数 |
| `m_s open` | 在浏览器中打开 Web UI |
| `m_s logs [-f]` | 查看服务日志（`server.log`） |
| `m_s rebuild` | 强制重新构建前端与后端 |
| `m_s version` / `m_s help` | 版本 / 帮助 |

### 项目

命令默认作用于 **当前目录所属的已登记项目**（在项目任意子目录中执行均可），也可用 `-p` / `--project <目录>` 指定。项目列表保存在服务端，与 Web UI 左侧「项目」抽屉共享。

| 命令 | 说明 |
|---|---|
| `m_s project list` | 列出已登记项目（`*` 为当前项目） |
| `m_s project current` | 显示当前目录对应的项目根目录 |
| `m_s project create <名称> [选项]` | 新建项目并登记，选项见下 |
| `m_s project import <目录>` | 登记已有目录 |
| `m_s project clone <仓库地址> [--dir 父目录]` | 克隆并登记 |
| `m_s project remove <名称\|路径>` | 移出列表（不删除磁盘文件） |

`project create` 选项：`--type android|blank`（默认 blank）、`--dir 父目录`（默认 `projects.default_dir`）、`--pkg 包名`、`--lang kotlin|java`、`--min-sdk N`、`--target-sdk N`、`--compile-sdk N`、`--template empty_views|none`。例如：

    m_s project create MyApp --type android --lang kotlin --pkg com.example.myapp --min-sdk 26 --target-sdk 35

### 文件与 Git

| 命令 | 说明 |
|---|---|
| `m_s fs ls [相对路径]` | 列出目录（子目录以 `/` 结尾） |
| `m_s fs new <相对路径> [--folder]` | 新建文件（或文件夹），自动创建上级目录 |
| `m_s fs rename <相对路径> <新名称>` | 重命名 |
| `m_s git graph [--limit N]` | 当前分支、全部分支与提交拓扑（`M` 标记合并提交；`--limit 0` 输出全部） |

### Android

以下命令亦可写作 `m_s android <子命令>`。任务类命令默认 **跟随日志直到结束**，退出码反映任务结果；`--detach` 仅启动并返回任务 ID；`--shell rish|adb|local` 可临时指定设备通道；Ctrl-C 会同时停止服务端任务。

| 命令 | 说明 |
|---|---|
| `m_s env [--refresh]` | 构建环境与设备通道检查（存在阻断问题时退出码 1，`--refresh` 重新探测设备通道） |
| `m_s build` | 构建 debug APK（`assembleDebug`） |
| `m_s install` | 安装最近一次构建的 APK |
| `m_s launch [--pkg 包名]` | 强停后重新启动应用 |
| `m_s run [--pkg 包名]` | 构建 → 安装 → 启动 |
| `m_s android stop [--pkg 包名]` | 强制停止应用（需 Shizuku 或 adb） |
| `m_s logcat [--pkg 包名] [--all]` | 实时应用日志（默认仅本应用进程，`--all` 为全部进程） |

### 任务

| 命令 | 说明 |
|---|---|
| `m_s task list [--all]` | 当前项目（`--all` 为全部项目）的任务 |
| `m_s task log <ID> [-f]` | 输出任务日志，`-f` 持续跟随至结束 |
| `m_s task status <ID>` | 输出任务状态，退出码反映结果 |
| `m_s task stop <ID>` | 停止任务 |

### 终端

与 Web UI「⌨ 终端」中的标签页是同一批会话：Agent 在命令行发送的命令，用户可在页面中实时看到并接手，反之亦然。`<ID>` 可写作 `last` 表示最新的会话。

| 命令 | 说明 |
|---|---|
| `m_s term list` | 列出会话（含正在查看的页面数） |
| `m_s term new [--cwd 目录]` | 新建会话（默认当前项目目录），输出会话 ID |
| `m_s term send <ID> <文本> [--enter] [--ctrl-c]` | 向会话输入文本；`--enter` 追加回车，`--ctrl-c` 先发送中断 |
| `m_s term read <ID> [--tail N] [--raw]` | 读取输出（默认去除 ANSI 转义并还原为纯文本，末 40 行；`--tail 0` 全部） |
| `m_s term close <ID>` | 结束会话 |

### 配置

配置保存在 `~/.mobile_studio/config.json`（`m_s config path` 查看），只记录修改过的键；除 `server.port` 外修改后立即生效。

    m_s config list                         # 查看全部配置、当前值与说明（* 为已修改）
    m_s config get <键>                      # 输出单个值（便于脚本读取）
    m_s config set <键> <值>
    m_s config unset <键>                    # 恢复默认值

| 键 | 默认值 | 说明 |
|---|---|---|
| `server.port` | `8080` | Web 服务端口，修改后执行 `m_s restart` |
| `server.open_browser` | `true` | `m_s` / `m_s start` 时是否打开浏览器 |
| `projects.default_dir` | `~` | 新建 / 克隆项目的默认父目录 |
| `android.sdk_dir` | 自动 | Android SDK 路径（自动：local.properties → ANDROID_HOME → 常见位置） |
| `android.java_home` | 自动 | JDK 目录（自动：JAVA_HOME → PATH 中的 java） |
| `android.gradle` | 自动 | Gradle 可执行文件（自动：项目 gradlew → PATH 中的 gradle） |
| `android.gradle_args` | 空 | 附加 Gradle 参数，如 `--offline --no-daemon` |
| `android.aapt2` | 自动 | aapt2 覆盖路径（Termux 下自动使用 `$PREFIX/bin/aapt2`） |
| `device.shell` | `auto` | 设备通道：`auto`（rish → adb → local）/ `rish` / `adb` / `local` |
| `device.adb_serial` | 空 | adb 目标设备序列号（空则使用第一个已连接设备） |
| `logcat.tail` | `300` | 开始 Logcat 时回放的历史行数 |
| `terminal.shell` | 自动 | Web 终端 Shell（自动：$SHELL → bash → sh） |

数据目录 `~/.mobile_studio/`：`config.json`（配置）、`projects.json`（项目列表）、`server.pid`（服务进程号）。环境变量 `MS_ROOT` 可覆盖 Mobile Studio 源码目录（默认 `~/Mobile_Studio`），`MS_CONFIG_DIR` 可覆盖数据目录。

### 面向 Agent 的约定

为便于 Agent CLI（如 Claude Code、Gemini CLI 等在 Termux 中运行的编程助手）操作 Mobile Studio，命令行遵循以下约定：

- **机器可读输出**：任意命令加 `--json` 输出 JSON；失败时输出 `{"success": false, "error": "...", "exitCode": N}`。`m_s` 脚本自身的提示信息一律写入 stderr，stdout 可直接解析。
- **流式任务**：`build` / `install` / `launch` / `run` / `logcat` / `task log -f` 在 `--json` 下输出 NDJSON，每行为 `{"type":"log","line":"..."}`，最后一行为 `{"type":"status","task":{...}}`。
- **退出码**：`0` 成功 · `1` 执行失败（含构建失败、`env` 发现问题）· `2` 用法错误 · `3` 服务未运行 · `130` 任务被停止或中断。
- **非阻塞**：长任务可加 `--detach` 立即返回任务 ID，之后用 `m_s task status <ID>` / `m_s task log <ID> -f` 获取结果。
- **与用户协同**：Agent 发起的任务和终端会话在 Web UI 中实时可见；用户在页面中的操作也可通过 `m_s task list`、`m_s term read` 观察。

典型流程示例：

    m_s env --json                                   # 检查环境，退出码非 0 时读取 problems 字段
    m_s project create Demo --type android --json    # 新建项目
    cd ~/Demo && m_s run --json                      # 构建 → 安装 → 启动，逐行读取 NDJSON
    id=$(m_s logcat --detach --json | jq -r .task.id)
    m_s task log "$id" --json                        # 读取当前日志快照（不跟随）
    m_s task stop "$id"

## 目录结构

    Mobile_Studio/
    ├── bin/                          # 本地编译生成的二进制文件 (git ignore)
    ├── docs/                         # 项目设计文档与规范
    │   ├── 0.0.1-development-plan.md   # 0.0.1 研发规划与技术路线图
    │   ├── 0.0.1-git-topology-rfc.md   # Git 拓扑数学建模 RFC (Rev. 2)
    │   ├── README.md                 # 文档中心索引
    │   └── archive/                  # 历史设计草案与阶段归档
    ├── frontend/                     # Svelte 前端工程 (Vite 构建)
    │   ├── src/
    │   │   ├── components/           # 原子化 UI 组件 (CommitNode, ControlPanel, FilePanel, RunPanel, TerminalView)
    │   │   ├── lib/                  # 纯算法模块 (graphLayout.js)
    │   │   └── App.svelte            # 主视口容器
    │   └── dist/                     # 前端编译静态产物 (git ignore)
    ├── scripts/                      # 自动化脚本与 CLI 工具 (m_s)
    ├── .gitignore                    # 版本控制忽略规则
    ├── AGENT.md                      # AI 协作与工程纪律规范指南
    ├── CHANGELOG.md                  # 版本变更记录
    ├── go.mod                        # Go 模块定义
    ├── main.go                       # 程序入口（无参数 / serve 启动服务，其余为 CLI）与文件树、拓扑 API
    ├── serve.go                      # HTTP 服务、路由注册与优雅退出
    ├── cli.go                        # m_s 命令行实现
    ├── config.go / projects.go       # 配置与项目注册表（~/.mobile_studio）
    ├── android_*.go / tasks.go       # Android 脚手架、环境探测、构建运行与后台任务
    ├── term.go / pty_*.go / websocket.go  # Web 终端：伪终端、WebSocket 与会话管理
    └── README.md                     # 项目说明文档

## 协作与工程规约
详见 [AGENT.md](AGENT.md)，严格执行「全局禁止 Fast-Forward 非快进合并」与「Issue 分支及时 Rebase」规约。
