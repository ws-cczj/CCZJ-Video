package com.wails.app;

import android.app.ActivityManager;
import android.app.AppOpsManager;
import android.content.Context;
import android.content.pm.PackageInfo;
import android.os.Debug;
import android.os.Process;
import android.os.SystemClock;
import androidx.webkit.WebViewCompat;
import org.json.JSONObject;
import java.util.concurrent.ArrayBlockingQueue;
import java.util.concurrent.ThreadPoolExecutor;
import java.util.concurrent.TimeUnit;

/** On-demand system memory sampling; never performs a Binder memory dump on the
 * JavaScript/UI thread or continuously samples while diagnostics are hidden. */
final class CczjMemoryDiagnostics implements AutoCloseable {
    private final ActivityManager manager;
    private final Context context;
    private final CczjProcessMemory processes;
    private final String provider, version;
    private final ThreadPoolExecutor executor = new ThreadPoolExecutor(0, 1, 30, TimeUnit.SECONDS,
            new ArrayBlockingQueue<>(1), runnable -> {
                Thread thread = new Thread(runnable, "cczj-memory");
                thread.setDaemon(true);
                return thread;
            });
    private volatile String snapshot = "{}";
    private long requestedAt = -5000;
    private boolean pending, closed;
    private int permissionState = -1, revision;

    CczjMemoryDiagnostics(Context context) {
        this.context = context;
        processes = new CczjProcessMemory(context);
        manager = (ActivityManager) context.getSystemService(Context.ACTIVITY_SERVICE);
        PackageInfo info = WebViewCompat.getCurrentWebViewPackage(context);
        provider = info == null ? "" : info.packageName;
        version = info == null ? "" : info.versionName;
    }

    synchronized String state() {
        int permissions = permissions();
        if (permissions != permissionState) {
            permissionState = permissions;
            revision++;
            snapshot = "{}";
            requestedAt = -5000;
        }
        long now = SystemClock.elapsedRealtime();
        // Android rate-limits getProcessMemoryInfo. Keep one bounded task and
        // return the timestamped previous sample immediately to the WebView.
        if (!closed && !pending && now - requestedAt >= 5000) {
            pending = true;
            requestedAt = now;
            int token = revision;
            executor.execute(() -> sample(token, permissions));
        }
        return snapshot;
    }

    private int permissions() {
        boolean dump = context.checkSelfPermission("android.permission.DUMP") == android.content.pm.PackageManager.PERMISSION_GRANTED;
        boolean usage = context.checkSelfPermission("android.permission.PACKAGE_USAGE_STATS") == android.content.pm.PackageManager.PERMISSION_GRANTED;
        AppOpsManager ops = (AppOpsManager) context.getSystemService(Context.APP_OPS_SERVICE);
        int mode = ops == null ? AppOpsManager.MODE_ERRORED : ops.checkOpNoThrow(AppOpsManager.OPSTR_GET_USAGE_STATS, Process.myUid(), context.getPackageName());
        usage = usage && (mode == AppOpsManager.MODE_ALLOWED || mode == AppOpsManager.MODE_DEFAULT);
        return (dump ? 1 : 0) | (usage ? 2 : 0);
    }

    private void sample(int token, int permissions) {
        JSONObject result = new JSONObject();
        try {
            result.put("pid", Process.myPid());
            result.put("package_name", context.getPackageName());
            result.put("dump_permission", (permissions & 1) != 0);
            result.put("usage_access", (permissions & 2) != 0);
            // Android may return a rate-limited older sample and does not expose
            // its collection time. This is the query time, not a freshness claim.
            result.put("queried_at_ms", System.currentTimeMillis());
            result.put("source", "ActivityManager.getProcessMemoryInfo");
            result.put("scope", "app_process");
            result.put("complete", false);
            result.put("webview_provider", provider);
            result.put("webview_version", version);
            Debug.MemoryInfo[] samples = manager == null ? null : manager.getProcessMemoryInfo(new int[]{Process.myPid()});
            if (samples == null || samples.length != 1 || samples[0].getTotalPss() <= 0) {
                result.put("available", false);
            } else {
                Debug.MemoryInfo memory = samples[0];
                result.put("available", true);
                result.put("pss_bytes", memory.getTotalPss() * 1024L);
                result.put("private_resident_bytes", (memory.getTotalPrivateDirty() + (long) memory.getTotalPrivateClean()) * 1024L);
                for (String key : new String[]{"java-heap", "native-heap", "code", "stack", "graphics", "private-other", "system", "total-swap"}) {
                    String value = memory.getMemoryStat("summary." + key);
                    if (value != null) result.put(key.replace('-', '_') + "_bytes", Long.parseLong(value) * 1024L);
                }
            }
            if (permissions == 3) result.put("attributed", processes.capture());
        } catch (Exception failure) {
            try { result.put("available", false); } catch (org.json.JSONException ignored) { }
        } finally {
            synchronized (this) {
                if (!closed && token == revision && permissions == permissions()) snapshot = result.toString();
                pending = false;
            }
        }
    }

    @Override public synchronized void close() {
        closed = true;
        revision++;
        processes.close();
        executor.shutdownNow();
    }
}
