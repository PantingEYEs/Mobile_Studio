# Changelog

本项目的所有重要变更均记录于此文件。
格式遵循 Keep a Changelog，并遵守语义化版本。

## [0.0.1-dev] - 2026-10-31

### Added
- 初始化 Go 本地 HTTP 服务模块（hello-local -> Mobile_Studio）。
- 新增全局自动化 CLI 启动器 scripts/m_s，软链接至 $PREFIX/bin/m_s。
- 实现环境依赖（Go、Git）按需自检与自动安装机制。
- 实现 Go 源码变更检测与增量编译构建功能。
- 实现基于本地端口 127.0.0.1:8080 监听状态的后台服务守护与防冲突机制。
- 支持自动唤起 Android 系统预设浏览器并导航至目标服务页面。
- 新增 Vim 风格服务停止指令（m_s :q、m_s :q!、m_s :wq）。
- 确立项目版本控制规范：全局禁止 Fast-Forward（非快进合并），保留完整合并图谱。
- 初始化 /docs 与 /docs/archive 归档目录结构。

### Known Issues
- 当系统默认浏览器已在后台活跃运行时，受 Android 任务栈及单例启动模式影响，通用 ACTION_VIEW 广播有时仅回到最后访问的标签页，未能强制加载目标 URL。计划在未来原生 Android App 封装阶段彻底根治。
