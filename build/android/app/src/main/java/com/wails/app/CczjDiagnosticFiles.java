package com.wails.app;

import android.app.Activity;
import android.app.DownloadManager;
import android.content.ActivityNotFoundException;
import android.content.ClipData;
import android.content.ContentResolver;
import android.content.ContentValues;
import android.content.Intent;
import android.net.Uri;
import android.os.Build;
import android.os.Environment;
import android.provider.MediaStore;
import android.util.Log;
import android.webkit.WebView;
import androidx.core.content.FileProvider;
import java.io.File;
import java.io.FileInputStream;
import java.io.IOException;
import java.io.OutputStream;
import java.util.LinkedHashMap;
import java.util.Locale;
import java.util.concurrent.Callable;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.FutureTask;
import java.util.concurrent.TimeUnit;
import org.json.JSONObject;

/** Copies completed exports to shared storage and provides read-only sharing. */
final class CczjDiagnosticFiles {
    static final int SAVE_REQUEST = 7005;
    private final Activity activity;
    private final WebView webView;
    private final ExecutorService writer = Executors.newSingleThreadExecutor();
    private final LinkedHashMap<String, SavedExport> savedExports = new LinkedHashMap<>();
    private SaveRequest pending;
    private boolean writing;
    private boolean closed;

    private static final class SaveRequest {
        final String id;
        final File file;
        SaveRequest(String id, File file) { this.id = id; this.file = file; }
    }

    private static final class SavedExport {
        final Uri uri;
        final String mime;
        final boolean downloads;
        SavedExport(Uri uri, String mime, boolean downloads) {
            this.uri = uri;
            this.mime = mime;
            this.downloads = downloads;
        }
    }

    CczjDiagnosticFiles(Activity activity, WebView webView) {
        this.activity = activity;
        this.webView = webView;
    }

    private File exportFile(String path) throws IOException {
        if (path == null || path.length() > 4096) throw new IOException("Invalid export path");
        File file = new File(path).getCanonicalFile();
        File exports = new File(activity.getFilesDir(), "CCZJ Video/exports").getCanonicalFile();
        String name = file.getName().toLowerCase(Locale.ROOT);
        boolean permitted = exports.equals(file.getParentFile()) && name.startsWith("backup_")
                && (name.endsWith(".json.br") || name.endsWith(".json.gz") || name.endsWith(".json"));
        for (String directory : new String[]{"diagnostics", "logs"}) {
            String root = new File(exports, directory).getCanonicalPath() + File.separator;
            permitted |= file.getPath().startsWith(root);
        }
        if (!permitted || !file.isFile()) throw new IOException("Export file is unavailable");
        return file;
    }

    private static String mimeType(File file) {
        String name = file.getName().toLowerCase(Locale.ROOT);
        if (name.endsWith(".zip")) return "application/zip";
        if (name.endsWith(".json")) return "application/json";
        if (name.endsWith(".csv")) return "text/csv";
        if (name.endsWith(".pprof") || name.endsWith(".br") || name.endsWith(".gz")) return "application/octet-stream";
        return "text/plain";
    }

    private String launch(Callable<String> action) throws Exception {
        FutureTask<String> task = new FutureTask<>(() -> {
            if (closed || activity.isFinishing() || activity.isDestroyed()) return "UNAVAILABLE";
            return action.call();
        });
        activity.runOnUiThread(task);
        try { return task.get(5, TimeUnit.SECONDS); }
        finally { task.cancel(false); }
    }

    String share(String path) {
        try {
            File file = exportFile(path);
            Uri uri = FileProvider.getUriForFile(activity, activity.getPackageName() + ".fileprovider", file);
            Intent send = new Intent(Intent.ACTION_SEND).setType(mimeType(file))
                .putExtra(Intent.EXTRA_STREAM, uri).addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION);
            send.setClipData(ClipData.newRawUri(file.getName(), uri));
            return launch(() -> {
                activity.startActivity(Intent.createChooser(send, file.getName()));
                return "";
            });
        } catch (Exception error) {
            Log.w("CczjDiagnostics", "Unable to share export", error);
            return "Unable to share export";
        }
    }

    String save(String path, String id) {
        if (id == null || !id.matches("[0-9]{1,16}")) return "UNAVAILABLE";
        File file;
        try { file = exportFile(path); }
        catch (IOException error) {
            Log.w("CczjDiagnostics", "Export source unavailable", error);
            return "MISSING";
        }
        try {
            return launch(() -> {
                if (pending != null || writing) return "BUSY";
                SaveRequest request = new SaveRequest(id, file);
                if (savesToDownloads()) {
                    write(request, () -> writeDownload(file), "saved_downloads", "DOWNLOAD_FAILED");
                } else {
                    Intent intent = new Intent(Intent.ACTION_CREATE_DOCUMENT)
                            .addCategory(Intent.CATEGORY_OPENABLE).setType(mimeType(file))
                            .putExtra(Intent.EXTRA_TITLE, file.getName())
                            .addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION | Intent.FLAG_GRANT_WRITE_URI_PERMISSION);
                    activity.startActivityForResult(intent, SAVE_REQUEST);
                    pending = request;
                }
                return "";
            });
        } catch (Exception error) {
            Log.w("CczjDiagnostics", "Unable to start export save", error);
            return savesToDownloads() ? "DOWNLOAD_FAILED" : "PICKER_UNAVAILABLE";
        }
    }

    static boolean savesToDownloads() { return Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q; }

    void saveResult(int resultCode, Intent data) {
        SaveRequest request = pending;
        pending = null;
        if (request == null || closed) return;
        if (resultCode != Activity.RESULT_OK) { notifyResult(request.id, "cancelled"); return; }
        Uri uri = data == null ? null : data.getData();
        if (uri == null || !"content".equals(uri.getScheme())
                || (activity.getPackageName() + ".fileprovider").equals(uri.getAuthority())) {
            notifyResult(request.id, "DOCUMENT_FAILED");
            return;
        }
        write(request, () -> { copy(request.file, uri); return uri; }, "saved", "DOCUMENT_FAILED");
    }

    @FunctionalInterface
    private interface WriteExport { Uri run() throws IOException; }

    private void write(SaveRequest request, WriteExport action, String completed, String failed) {
        writing = true;
        writer.execute(() -> {
            try {
                Uri uri = action.run();
                activity.runOnUiThread(() -> finishSave(request, completed, uri));
            } catch (Exception error) {
                Log.w("CczjDiagnostics", failed, error);
                activity.runOnUiThread(() -> finishSave(request, failed, null));
            }
        });
    }

    private void finishSave(SaveRequest request, String state, Uri uri) {
        writing = false;
        if (closed) return;
        if (uri != null) {
            savedExports.put(request.id, new SavedExport(uri, mimeType(request.file), "saved_downloads".equals(state)));
            if (savedExports.size() > 16) savedExports.remove(savedExports.keySet().iterator().next());
        }
        notifyResult(request.id, state);
    }

    boolean openSaved(String id) {
        try {
            return launch(() -> {
                SavedExport saved = savedExports.get(id);
                if (saved == null) return "UNAVAILABLE";
                Intent view = new Intent(Intent.ACTION_VIEW).setDataAndType(saved.uri, saved.mime)
                        .addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION);
                view.setClipData(ClipData.newRawUri("Export", saved.uri));
                try { activity.startActivity(view); }
                catch (ActivityNotFoundException | SecurityException error) {
                    if (!saved.downloads) throw error;
                    activity.startActivity(new Intent(DownloadManager.ACTION_VIEW_DOWNLOADS));
                }
                return "";
            }).isEmpty();
        } catch (Exception error) {
            Log.w("CczjDiagnostics", "Unable to open saved export", error);
            return false;
        }
    }

    @androidx.annotation.RequiresApi(Build.VERSION_CODES.Q)
    private Uri writeDownload(File file) throws IOException {
        ContentResolver resolver = activity.getContentResolver();
        ContentValues values = new ContentValues();
        values.put(MediaStore.Downloads.DISPLAY_NAME, file.getName());
        values.put(MediaStore.Downloads.MIME_TYPE, mimeType(file));
        values.put(MediaStore.Downloads.RELATIVE_PATH, Environment.DIRECTORY_DOWNLOADS);
        values.put(MediaStore.Downloads.IS_PENDING, 1);
        Uri uri = resolver.insert(MediaStore.Downloads.getContentUri(MediaStore.VOLUME_EXTERNAL_PRIMARY), values);
        if (uri == null) throw new IOException("Unable to create download");
        boolean published = false;
        try {
            copy(file, uri);
            values.clear();
            values.put(MediaStore.Downloads.IS_PENDING, 0);
            if (resolver.update(uri, values, null, null) != 1) throw new IOException("Unable to publish download");
            published = true;
            return uri;
        } finally {
            if (!published) {
                try { resolver.delete(uri, null, null); }
                catch (Exception error) { Log.w("CczjDiagnostics", "Unable to remove incomplete download", error); }
            }
        }
    }

    private void copy(File file, Uri uri) throws IOException {
        try (FileInputStream input = new FileInputStream(file);
             OutputStream output = activity.getContentResolver().openOutputStream(uri)) {
            if (output == null) throw new IOException("Unable to open export destination");
            byte[] buffer = new byte[64 * 1024];
            int count;
            while ((count = input.read(buffer)) != -1) {
                if (Thread.currentThread().isInterrupted()) throw new IOException("Export write interrupted");
                output.write(buffer, 0, count);
            }
        }
    }

    private void notifyResult(String id, String state) {
        if (closed) return;
        webView.evaluateJavascript("window.dispatchEvent(new CustomEvent('cczj:diagnostic-save',{detail:{id:"
                + JSONObject.quote(id) + ",state:" + JSONObject.quote(state) + "}}))", null);
    }

    void close() {
        closed = true;
        pending = null;
        savedExports.clear();
        writer.shutdownNow();
    }
}
