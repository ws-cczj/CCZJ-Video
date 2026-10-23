package com.wails.app;

import android.app.Activity;
import android.content.ClipData;
import android.content.Intent;
import android.net.Uri;
import android.util.Log;
import androidx.core.content.FileProvider;
import java.io.File;
import java.util.concurrent.FutureTask;
import java.util.concurrent.TimeUnit;

/** Shares completed exports through a read-only content URI. Never exposes the
 * database, preferences or an arbitrary filesystem path to other applications. */
final class CczjDiagnosticFiles {
    private final Activity activity;
    CczjDiagnosticFiles(Activity activity) { this.activity = activity; }

    String share(String path) {
        try {
            if (path == null || path.length() > 4096) return "Invalid export path";
            File file = new File(path).getCanonicalFile();
            File exports = new File(activity.getFilesDir(), "CCZJ Video/exports").getCanonicalFile();
            String name = file.getName().toLowerCase(java.util.Locale.ROOT);
            boolean permitted = false;
            if (exports.equals(file.getParentFile()) && name.startsWith("backup_")
                    && (name.endsWith(".json.br") || name.endsWith(".json.gz") || name.endsWith(".json"))) permitted = true;
            for (String directory : new String[]{"diagnostics", "logs"}) {
                String root = new File(exports, directory).getCanonicalPath() + File.separator;
                if (file.getPath().startsWith(root)) permitted = true;
            }
            if (!permitted || !file.isFile()) return "Export file is unavailable";
            String mime = name.endsWith(".zip") ? "application/zip" : name.endsWith(".json") ? "application/json" : name.endsWith(".csv") ? "text/csv" : name.endsWith(".pprof") || name.endsWith(".br") || name.endsWith(".gz") ? "application/octet-stream" : "text/plain";
            Uri uri = FileProvider.getUriForFile(activity, activity.getPackageName() + ".fileprovider", file);
            Intent send = new Intent(Intent.ACTION_SEND).setType(mime)
                .putExtra(Intent.EXTRA_STREAM, uri).addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION);
            send.setClipData(ClipData.newRawUri(file.getName(), uri));
            FutureTask<String> launch = new FutureTask<>(() -> {
                try { activity.startActivity(Intent.createChooser(send, file.getName())); return ""; }
                catch (Exception error) { Log.w("CczjDiagnostics", "Unable to open export chooser", error); return "Unable to open export chooser"; }
            });
            activity.runOnUiThread(launch);
            return launch.get(5, TimeUnit.SECONDS);
        } catch (Exception error) {
            Log.w("CczjDiagnostics", "Unable to prepare export", error);
            return "Unable to share export";
        }
    }
}
