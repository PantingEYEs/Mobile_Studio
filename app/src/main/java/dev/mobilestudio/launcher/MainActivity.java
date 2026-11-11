package dev.mobilestudio.launcher;

import android.app.Activity;
import android.content.Intent;
import android.content.pm.PackageManager;
import android.os.Bundle;
import android.widget.Toast;

/** First-stage launcher: Termux owns the runtime; the browser owns the UI. */
public final class MainActivity extends Activity {
    private static final String PERMISSION = "com.termux.permission.RUN_COMMAND";
    private static final int REQUEST_COMMAND = 1;

    @Override
    public void onCreate(Bundle state) {
        super.onCreate(state);
        try {
            getPackageManager().getPackageInfo("com.termux", 0);
        } catch (PackageManager.NameNotFoundException e) {
            fail("请先安装并打开 Termux，完成 Mobile Studio 环境准备。");
            return;
        }
        if (checkSelfPermission(PERMISSION) == PackageManager.PERMISSION_GRANTED) {
            launch();
        } else if (state == null) {
            requestPermissions(new String[]{PERMISSION}, REQUEST_COMMAND);
        }
    }

    @Override
    public void onRequestPermissionsResult(int requestCode, String[] permissions, int[] results) {
        super.onRequestPermissionsResult(requestCode, permissions, results);
        if (requestCode != REQUEST_COMMAND) return;
        if (results.length > 0 && results[0] == PackageManager.PERMISSION_GRANTED) {
            launch();
        } else {
            fail("请在系统设置 → Mobile Studio → 权限中允许在 Termux 中执行命令。");
        }
    }

    private void launch() {
        // Fixed command only: never execute data from the exported activity's intent.
        // Use a visible Termux session so startup/build errors remain available there.
        String command = "root=\"${MS_ROOT:-$HOME/Mobile_Studio}\"; "
                + "if [ ! -f \"$root/scripts/m_s\" ]; then "
                + "printf '%s\\n' \"Mobile Studio 未找到：$root/scripts/m_s\" >&2; exit 1; fi; "
                + "export MS_ROOT=\"$root\"; "
                + "bash \"$root/scripts/m_s\" open";
        Intent intent = new Intent("com.termux.RUN_COMMAND");
        intent.setClassName("com.termux", "com.termux.app.RunCommandService");
        intent.putExtra("com.termux.RUN_COMMAND_PATH", "/data/data/com.termux/files/usr/bin/bash");
        intent.putExtra("com.termux.RUN_COMMAND_ARGUMENTS", new String[]{"-lc", command});
        intent.putExtra("com.termux.RUN_COMMAND_BACKGROUND", false);
        intent.putExtra("com.termux.RUN_COMMAND_SESSION_ACTION", "0");
        intent.putExtra("com.termux.RUN_COMMAND_COMMAND_LABEL", "Mobile Studio");
        try {
            if (startService(intent) == null) {
                fail("Termux 启动失败，请检查 Termux 安装。");
                return;
            }
            finish();
        } catch (RuntimeException e) {
            fail("无法调用 Termux，请检查执行命令权限与 allow-external-apps 设置。");
        }
    }

    private void fail(String message) {
        Toast.makeText(this, message, Toast.LENGTH_LONG).show();
        finish();
    }
}
