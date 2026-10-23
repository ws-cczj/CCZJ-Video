package com.wails.app;

import android.net.Uri;
import android.webkit.WebResourceRequest;
import android.webkit.WebResourceResponse;
import java.io.ByteArrayInputStream;
import java.io.FilterInputStream;
import java.io.IOException;
import java.io.InputStream;
import java.net.HttpURLConnection;
import java.net.URL;
import java.nio.charset.StandardCharsets;
import java.util.HashMap;

/** Debug-only frontend proxy. The document and native API keep their Wails origin. */
final class CczjFrontendDevServer {
    static WebResourceResponse serve(WebResourceRequest request, int port) {
        if (!BuildConfig.DEBUG || port < 1024 || port > 65535) return null;
        HttpURLConnection connection = null;
        try {
            Uri uri = request.getUrl();
            String path = uri.getEncodedPath();
            if (uri.getEncodedQuery() != null) path += "?" + uri.getEncodedQuery();
            // The host is fixed; an Intent can select only a local development port.
            connection = (HttpURLConnection) new URL("http://127.0.0.1:" + port + path).openConnection();
            connection.setConnectTimeout(3000);
            connection.setReadTimeout(15000);
            connection.setInstanceFollowRedirects(false);
            connection.setRequestMethod(request.getMethod());
            // Vite's HTML fallback requires Accept; Android's URLConnection
            // does not supply the browser's default */* header itself.
            connection.setRequestProperty("Accept", "*/*");
            connection.setRequestProperty("Accept-Encoding", "identity");
            int status = connection.getResponseCode();
            if (status >= 300 && status < 400) throw new IOException("Unexpected frontend redirect");
            String contentType = connection.getContentType();
            String mime = contentType == null ? "application/octet-stream" : contentType.split(";", 2)[0].trim();
            android.util.Log.d("CczjFrontendDev", status + " " + mime + " " + uri.getPath());
            InputStream stream = status >= 400 ? connection.getErrorStream() : connection.getInputStream();
            if (stream == null) stream = new ByteArrayInputStream(new byte[0]);
            final HttpURLConnection owned = connection;
            InputStream body = new FilterInputStream(stream) {
                @Override public void close() throws IOException {
                    try { super.close(); } finally { owned.disconnect(); }
                }
            };
            HashMap<String, String> headers = new HashMap<>();
            headers.put("Cache-Control", "no-store");
            return new WebResourceResponse(mime, "UTF-8", status, "Frontend", headers, body);
        } catch (IOException error) {
            android.util.Log.e("CczjFrontendDev", "Frontend request failed", error);
            if (connection != null) connection.disconnect();
            byte[] message = "Frontend dev server unavailable. Start build/android/dev.ps1 or exit development mode.".getBytes(StandardCharsets.UTF_8);
            return new WebResourceResponse("text/plain", "UTF-8", 503, "Dev server unavailable", new HashMap<>(), new ByteArrayInputStream(message));
        }
    }
}
