package com.wails.app;

import android.content.Context;
import android.os.Process;
import android.os.SystemClock;
import org.json.JSONArray;
import org.json.JSONObject;
import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.io.InputStream;
import java.util.Map;
import java.util.TreeMap;
import java.util.concurrent.ScheduledThreadPoolExecutor;
import java.util.concurrent.ScheduledFuture;
import java.util.concurrent.TimeUnit;
import java.util.regex.Matcher;
import java.util.regex.Pattern;

/** Optional system-dump adapter. Only returns numeric data for processes whose
 * package ownership is confirmed before and after the bounded collection. */
final class CczjProcessMemory implements AutoCloseable {
    private static final Pattern HEADER = Pattern.compile("ProcessRecord\\{(\\S+) (\\d+):([^/]+)");
    private static final Pattern PACKAGES = Pattern.compile("packageList=\\{([^}]+)\\}");
    private static final Pattern TOTAL = Pattern.compile("TOTAL PSS:\\s+(\\d+)");
    private static final Pattern RSS = Pattern.compile("TOTAL RSS:\\s+(\\d+)");
    private final String packageName;
    private final ScheduledThreadPoolExecutor deadlines = new ScheduledThreadPoolExecutor(1, runnable -> {
        Thread thread = new Thread(runnable, "cczj-memory-deadline");
        thread.setDaemon(true);
        return thread;
    });
    private java.lang.Process running;
    private boolean closed;

    CczjProcessMemory(Context context) {
        packageName = context.getPackageName();
        deadlines.setRemoveOnCancelPolicy(true);
        deadlines.setKeepAliveTime(30, TimeUnit.SECONDS);
        deadlines.allowCoreThreadTimeOut(true);
    }

    JSONObject capture() throws org.json.JSONException {
        JSONObject result = new JSONObject();
        result.put("available", false);
        result.put("source", "dumpsys.meminfo.local");
        result.put("scope", "confirmed_owned_processes");
        result.put("queried_at_ms", System.currentTimeMillis());
        long deadline = SystemClock.elapsedRealtime() + 4000;
        try {
            Map<Integer, String> before = owned(dump(deadline, "activity", "processes", packageName));
            if (!before.containsKey(Process.myPid()) || before.size() > 8) throw new IOException("OWNERSHIP_UNAVAILABLE");
            JSONArray processes = new JSONArray();
            long total = 0;
            for (Map.Entry<Integer, String> entry : before.entrySet()) {
                String memory = dump(deadline, "meminfo", "--local", Integer.toString(entry.getKey()));
                if (!memory.contains("** MEMINFO in pid " + entry.getKey() + " [")) throw new IOException("PROCESS_CHANGED");
                Matcher pss = TOTAL.matcher(memory), rss = RSS.matcher(memory);
                if (!pss.find()) throw new IOException("MEMORY_UNAVAILABLE");
                long bytes = Long.parseLong(pss.group(1)) * 1024L;
                JSONObject row = new JSONObject();
                row.put("pid", entry.getKey());
                row.put("role", entry.getKey() == Process.myPid() ? "app" : entry.getValue().contains("sandboxed_process") ? "renderer" : "app_child");
                row.put("pss_bytes", bytes);
                if (rss.find()) row.put("rss_bytes", Long.parseLong(rss.group(1)) * 1024L);
                processes.put(row);
                total += bytes;
            }
            if (!before.equals(owned(dump(deadline, "activity", "processes", packageName)))) throw new IOException("PROCESS_CHANGED");
            result.put("processes", processes);
            result.put("pss_bytes", total);
            result.put("available", true);
        } catch (Exception failure) {
            // No raw system dump, package list or arbitrary exception text is
            // sent across the bridge or retained in the diagnostic recording.
            String code = failure.getMessage();
            result.put("error_code", code != null && code.matches("[A-Z_]{1,40}") ? code : "MEMORY_UNAVAILABLE");
        }
        return result;
    }

    private Map<Integer, String> owned(String dump) {
        Map<Integer, String> found = new TreeMap<>();
        for (String block : dump.split("(?m)(?=^\\s*\\*APP\\*)")) {
            Matcher header = HEADER.matcher(block), packages = PACKAGES.matcher(block);
            if (!header.find() || !packages.find()) continue;
            for (String name : packages.group(1).split(",")) if (packageName.equals(name.trim())) {
                // Include the ProcessRecord identity to detect PID reuse.
                found.put(Integer.parseInt(header.group(2)), header.group(1) + ":" + header.group(3));
                break;
            }
        }
        return found;
    }

    private String dump(long deadline, String... arguments) throws Exception {
        long remaining = Math.min(1500, deadline - SystemClock.elapsedRealtime());
        if (remaining <= 0) throw new IOException("TIMEOUT");
        String[] command = new String[arguments.length + 1];
        command[0] = "/system/bin/dumpsys";
        System.arraycopy(arguments, 0, command, 1, arguments.length);
        java.lang.Process process;
        synchronized (this) {
            if (closed) throw new IOException("CANCELLED");
            process = new ProcessBuilder(command).redirectErrorStream(true).start();
            running = process;
        }
        ScheduledFuture<?> timeout = deadlines.schedule(process::destroy, remaining, TimeUnit.MILLISECONDS);
        try (InputStream input = process.getInputStream(); ByteArrayOutputStream bytes = new ByteArrayOutputStream()) {
            byte[] buffer = new byte[4096];
            int count;
            while ((count = input.read(buffer)) != -1) {
                if (bytes.size() + count > 512 * 1024) throw new IOException("OUTPUT_LIMIT");
                bytes.write(buffer, 0, count);
            }
            if (process.waitFor() != 0 || SystemClock.elapsedRealtime() >= deadline) throw new IOException("TIMEOUT");
            String text = bytes.toString("UTF-8");
            if (text.contains("Permission Denial")) throw new IOException("PERMISSION_DENIED");
            return text;
        } finally {
            timeout.cancel(false);
            process.destroy();
            synchronized (this) { if (running == process) running = null; }
        }
    }

    @Override public synchronized void close() {
        closed = true;
        if (running != null) running.destroy();
        deadlines.shutdownNow();
    }
}
