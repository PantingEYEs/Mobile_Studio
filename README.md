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

### 启动服务并打开预设浏览器
在 Termux 终端任意目录下执行：

    m_s

该指令将自动完成：
1. 依赖环境按需自检 (Go、Git、Node、npm)。
2. 检测 Go 源码与前端产物变动并按需增量编译。
3. 后台启动本地 HTTP 服务 (监听 127.0.0.1:8080)。
4. 调起系统预设浏览器访问服务。

### 停止后台服务 (Vim 风格)

    m_s :q

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
- 设备通道按 Shizuku (`rish`) → 无线调试 (`adb`) → 无特权 的顺序自动选择。无特权时安装会跳转系统安装器、无法强停与读取应用日志。
- 运行面板顶部「环境」徽标会列出缺失的依赖及修复命令。

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
    │   │   ├── components/           # 原子化 UI 组件 (CommitNode, ControlPanel, FilePanel)
    │   │   ├── lib/                  # 纯算法模块 (graphLayout.js)
    │   │   └── App.svelte            # 主视口容器
    │   └── dist/                     # 前端编译静态产物 (git ignore)
    ├── scripts/                      # 自动化脚本与 CLI 工具 (m_s)
    ├── .gitignore                    # 版本控制忽略规则
    ├── AGENT.md                      # AI 协作与工程纪律规范指南
    ├── CHANGELOG.md                  # 版本变更记录
    ├── go.mod                        # Go 模块定义
    ├── main.go                       # Go 后端主入口 (内置文件树与拓扑 API)
    └── README.md                     # 项目说明文档

## 协作与工程规约
详见 [AGENT.md](AGENT.md)，严格执行「全局禁止 Fast-Forward 非快进合并」与「Issue 分支及时 Rebase」规约。
