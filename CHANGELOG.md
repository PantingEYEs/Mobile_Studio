# Changelog

本项目的所有重要变更均记录于此文件。
格式遵循 Keep a Changelog，并遵守语义化版本。

## [0.0.1-dev] - 2026-10-31

### Added
- 初始化 Go 本地 HTTP 服务模块（hello-local -> Mobile_Studio）。
- 引入 Svelte 响应式前端工程，基于 Vite 构建生产环境静态资产。
- 实现 Go 原生 //go:embed 机制，将 Svelte 前端产物内嵌编译进单个可执行文件。
- 升级全局自动化启动器 scripts/m_s，支持前端产物自动检测与按需增量构建。
- 加固 m_s :q 退出机制，结合 PID 精准强杀与端口扫描，彻底避免 8080 端口残留占用。
- 支持自动唤起 Android 系统预设浏览器并导航至目标服务页面。
- 确立项目版本控制规范：全局禁止 Fast-Forward（非快进合并），保留完整合并拓扑图谱。
- 初始化 /docs 架构规划与归档目录结构。

### Known Issues
- 当系统默认浏览器已在后台活跃运行时，受 Android 任务栈及单例启动模式影响，通用 ACTION_VIEW 广播有时仅回到最后访问的标签页，未能强制加载目标 URL。计划在未来原生 Android App 封装阶段彻底根治。
