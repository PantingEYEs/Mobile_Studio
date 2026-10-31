# Mobile Studio

> 基于 Android 15 + One UI 7.0 宿主环境的无 Root、不破坏 Knox 的轻量级本地应用与 Web 服务闭环系统。

## 架构概览

- 宿主环境：Android 15 / One UI 7.0 (Samsung Knox 保持 0x0, 无 Root)
- 终端运行时：Termux (aarch64)
- 后端服务：Go (Golang) 本地 HTTP 服务 + PTY 终端桥接
- 前端技术：Svelte (编译产物通过 Go embed 静态打包为单二进制文件)
- 权限与调试：Shizuku (rish) / 本地无线调试 / Android Intent 通信

## 0.0.1 核心特性规划

详细方案请参见 docs/0.0.1-development-plan.md：
1. 项目脚手架与多 API 储备：支持初始化 Android / Go / Web 项目，预备并适配 Android API 31~35 编译环境。
2. 环境适配自动注入：新建 Android 项目自动配置 aarch64 架构专用 aapt2 覆盖。
3. 轻量文件树与 Git 拓扑：查看项目目录并一键复制路径，可视化查看非快进 (No Fast-Forward) Git 分支历史。
4. 全开放 Web 控制台：基于 xterm.js 与 PTY，开箱即用，免鉴权直连操作 Termux 终端。
5. 本地编译与构建：支持 .apk (自动自签名) 与 .aab 导出，提供 Android 15 幻影进程放宽指导。
6. Shizuku 热部署与调试：通过 rish 免弹窗静默安装，支持 am start 自动拉起与实时 Logcat 过滤。

## 快速使用

### 启动服务并打开预设浏览器
在 Termux 终端任意目录下执行：

    m_s

该指令将自动完成：
1. 依赖环境按需自检 (Go、Git)。
2. 检测 Go 源码变动并按需增量编译。
3. 后台启动本地 HTTP 服务 (监听 127.0.0.1:8080)。
4. 调起系统预设浏览器访问服务。

### 停止后台服务 (Vim 风格)

    m_s :q

## 目录结构

    Mobile_Studio/
    ├── bin/                        # 本地编译生成的二进制文件 (git ignore)
    ├── docs/                       # 项目设计文档与规范
    │   ├── 0.0.1-development-plan.md # 0.0.1 研发规划与技术路线图
    │   ├── README.md               # 文档中心索引
    │   └── archive/                # 历史设计草案与阶段归档
    ├── scripts/                    # 自动化脚本与 CLI 工具 (m_s)
    ├── .gitignore                  # 版本控制忽略规则
    ├── CHANGELOG.md                # 版本变更记录
    ├── go.mod                      # Go 模块定义
    ├── main.go                     # Go 后端主入口
    └── README.md                   # 项目说明文档

## 分支与提交规范
- 默认开发分支：0.0.1-dev
- 稳定主分支：main
- 合并策略：全局禁止 Fast-Forward，所有 PR 均采用显式 Merge Commit。
