# 已知问题

## Wi-Fi 切换到移动数据后 Shizuku 通道失效

- 记录日期：2026-11-12。
- 状态：未解决，暂缓处理；未实施修复，也没有已验证的规避方案。
- 实测环境：本项目使用的三星 Android 15 / One UI 7.0、无 Root、Termux 与 Shizuku/rish。

### 复现与结果

1. 在 Wi-Fi 下启动 Shizuku，允许 Termux 使用 Shizuku。
2. `rish -c id` 返回 `uid=2000(shell)`。
3. 通过 Mobile Studio 安装启动器 APK，`pm install` 返回 `Success`；启动返回 `Status: ok`。
4. 仅将网络从 Wi-Fi 切换到移动数据，用户观察到 Wireless Debugging 断开。
5. `rish -c id` 返回 `Server is not running`，Mobile Studio 刷新环境后检测到 Shizuku 不可用，回退到普通权限通道。

用户再次尝试仅切换网络，仍未能保持 Shizuku 可用。不能将“Wi-Fi 下启动一次后切到移动数据仍可继续使用”作为本设备的已验证能力。

### 影响与当前可用流程

Shizuku 失效期间无法通过该通道直接安装、强停或读取应用日志。构建不依赖此通道；可以在浏览器 WebUI 点击「下载安装」，下载后打开 APK 手动安装，并在安装器中点击「打开」。

### 尚待验证

目前只确认了切网时的现象，尚未定位 Shizuku 停止的具体机制。ADB TCP 模式及第三方 Shizuku 分支的无 Wi-Fi 启动方式仅完成资料调研，未在本设备上验证，也未接入 Mobile Studio。后续恢复调查时再测试，暂不更换 Shizuku 或修改调试、网络及系统设置。
