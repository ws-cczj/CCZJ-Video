package com.wails.app;

import android.app.Activity;
import android.content.Intent;
import android.net.Uri;
import android.util.Log;
import android.webkit.ValueCallback;
import android.webkit.WebChromeClient;
import java.util.ArrayList;
import java.util.List;

/** Owns HTML file-input callbacks separately from Wails' native file picker. */
final class CczjWebFileChooser {
    static final int REQUEST = 7004;
    private final Activity activity;
    private ValueCallback<Uri[]> pending;

    CczjWebFileChooser(Activity activity) { this.activity = activity; }

    boolean open(ValueCallback<Uri[]> callback, WebChromeClient.FileChooserParams params) {
        cancel();
        pending = callback;
        Intent intent = new Intent(Intent.ACTION_OPEN_DOCUMENT).addCategory(Intent.CATEGORY_OPENABLE);
        List<String> types = new ArrayList<>();
        boolean unrestricted = false;
        for (String type : params.getAcceptTypes()) {
            // Backup suffixes such as .br have inconsistent provider MIME types.
            if (type == null || !type.contains("/")) { unrestricted = true; continue; }
            types.add(type);
        }
        intent.setType(!unrestricted && types.size() == 1 ? types.get(0) : "*/*");
        if (!unrestricted && types.size() > 1) intent.putExtra(Intent.EXTRA_MIME_TYPES, types.toArray(new String[0]));
        intent.putExtra(Intent.EXTRA_ALLOW_MULTIPLE, params.getMode() == WebChromeClient.FileChooserParams.MODE_OPEN_MULTIPLE);
        intent.addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION);
        try { activity.startActivityForResult(intent, REQUEST); }
        catch (Exception error) {
            Log.w("CczjFileChooser", "Document picker unavailable", error);
            cancel();
        }
        return true;
    }

    void result(int resultCode, Intent data) {
        ValueCallback<Uri[]> callback = pending;
        pending = null;
        if (callback == null) return;
        List<Uri> selected = new ArrayList<>();
        if (resultCode == Activity.RESULT_OK && data != null) {
            if (data.getClipData() != null) {
                for (int i = 0; i < data.getClipData().getItemCount(); i++) add(selected, data.getClipData().getItemAt(i).getUri());
            } else add(selected, data.getData());
        }
        callback.onReceiveValue(selected.isEmpty() ? null : selected.toArray(new Uri[0]));
    }

    private void add(List<Uri> selected, Uri uri) {
        // External picker results are untrusted. Never let them name a local
        // file or this app's provider, even when the result reports success.
        if (uri != null && "content".equals(uri.getScheme())
                && !(activity.getPackageName()+".fileprovider").equals(uri.getAuthority())) selected.add(uri);
    }

    void cancel() {
        ValueCallback<Uri[]> callback = pending;
        pending = null;
        if (callback != null) callback.onReceiveValue(null);
    }
}
