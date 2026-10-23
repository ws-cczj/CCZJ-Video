package com.wails.app;

import android.os.Handler;
import android.os.Looper;
import android.os.SystemClock;
import android.util.Log;
import android.webkit.JavascriptInterface;
import android.webkit.WebView;
import com.wails.app.BuildConfig;

import java.util.concurrent.ArrayBlockingQueue;
import java.util.concurrent.RejectedExecutionException;
import java.util.concurrent.ThreadPoolExecutor;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicBoolean;

/**
 * WailsJSBridge provides the JavaScript interface that allows the web frontend
 * to communicate with the Go backend. This is exposed to JavaScript as the
 * `window.wails` object.
 *
 * Similar to iOS's WKScriptMessageHandler but using Android's addJavascriptInterface.
 */
public class WailsJSBridge {
    private static final String TAG = "WailsJSBridge";
    private static final boolean DEBUG = BuildConfig.DEBUG;
    private static final int CORE_THREADS = 2;
    private static final int MAX_THREADS = 4;
    private static final int QUEUE_CAPACITY = 64;

    private final WailsBridge bridge;
    private volatile WebView webView;
    private final CczjPlayerDevice player;
    private final CczjDiagnosticFiles diagnosticFiles;
    private final Handler mainHandler = new Handler(Looper.getMainLooper());
    private final AtomicBoolean closed = new AtomicBoolean(false);
    private final ThreadPoolExecutor executor;

    public WailsJSBridge(WailsBridge bridge, WebView webView, CczjPlayerDevice player, CczjDiagnosticFiles diagnosticFiles) {
        this.bridge = bridge;
        this.webView = webView;
        this.player = player;
        this.diagnosticFiles = diagnosticFiles;
        this.executor = new ThreadPoolExecutor(
                CORE_THREADS,
                MAX_THREADS,
                30L,
                TimeUnit.SECONDS,
                new ArrayBlockingQueue<>(QUEUE_CAPACITY),
                runnable -> {
                    Thread thread = new Thread(runnable, "wails-js");
                    thread.setDaemon(true);
                    return thread;
                },
                new ThreadPoolExecutor.AbortPolicy()
        );
        this.executor.allowCoreThreadTimeOut(true);
    }

    /**
     * Stop accepting calls before the Activity releases its WebView. The
     * native call itself is not interruptible, so in-flight work may finish,
     * but it can no longer enqueue JavaScript against the destroyed page.
     */
    public void shutdown() {
        if (!closed.compareAndSet(false, true)) return;
        mainHandler.removeCallbacksAndMessages(this);
        webView = null;
        executor.shutdownNow();
    }

    @JavascriptInterface public String playerState() { return player.state(); }
    @JavascriptInterface public String startupState() {
        WebView view = webView;
        if (view == null) return "{}";
        android.content.Context context = view.getContext();
        return context instanceof MainActivity ? ((MainActivity) context).startupState() : "{}";
    }
    @JavascriptInterface public void cacheLaunchBackground(String color) {
        WebView view = webView;
        if (view == null) return;
        android.content.Context context = view.getContext();
        if (context instanceof MainActivity) ((MainActivity) context).cacheLaunchBackground(color);
    }
    @JavascriptInterface public void playerVolume(double value) { player.volume(value); }
    @JavascriptInterface public void playerBrightness(double value) { player.brightness(value); }
    @JavascriptInterface public void playerRestoreBrightness() { player.restoreBrightness(); }
    @JavascriptInterface public void playerEnterPip(int width, int height) { player.pip(width, height); }
    @JavascriptInterface public void playerMulticast(boolean enabled) { player.multicast(enabled); }
    @JavascriptInterface public String diagnosticShare(String path) { return diagnosticFiles.share(path); }
    @JavascriptInterface public String installApk(String path) {
        WebView view = webView;
        if (view == null || closed.get() || !(view.getContext() instanceof android.app.Activity)) return "Android host unavailable";
        return new CczjUpdateInstaller((android.app.Activity) view.getContext()).install(path);
    }
    @JavascriptInterface public void exitApp() {
        WebView view = webView;
        if (view == null || closed.get()) return;
        view.post(() -> {
            if (closed.get()) return;
            android.content.Context context = view.getContext();
            if (context instanceof android.app.Activity) ((android.app.Activity) context).finishAndRemoveTask();
        });
    }

    /**
     * Send a message to Go and return the response synchronously.
     * Called from JavaScript: wails.invoke(message)
     *
     * @param message The message to send (JSON string)
     * @return The response from Go (JSON string)
     */
    @JavascriptInterface
    public String invoke(String message) {
        if (closed.get()) return "{\"error\":\"Bridge closed\"}";
        if (DEBUG) Log.d(TAG, "Invoke called: " + message);
        return bridge.handleMessage(message);
    }

    /**
     * Send a message to Go asynchronously.
     * The response will be sent back via a callback.
     * Called from JavaScript: wails.invokeAsync(callbackId, message)
     *
     * @param callbackId The callback ID to use for the response
     * @param message The message to send (JSON string)
     */
    @JavascriptInterface
    public void invokeAsync(final String callbackId, final String payload) {
        if (closed.get()) return;
        if (DEBUG) Log.d(TAG, "InvokeAsync called: " + payload);

        // Handle off the JS thread so we don't block the WebView.
        try {
            executor.execute(() -> {
                if (closed.get()) return;
                try {
                    String response = bridge.handleRuntimeCall(payload);
                    sendCallback(callbackId, response, null);
                } catch (Exception e) {
                    Log.e(TAG, "Error in async invoke", e);
                    sendCallback(callbackId, null, e.getMessage());
                }
            });
        } catch (RejectedExecutionException e) {
            // A full queue is a real transport error; report it instead of
            // leaving the JavaScript promise waiting forever.
            sendCallback(callbackId, null, "Android bridge is busy; try again");
        }
    }

    /**
     * Log a message from JavaScript to Android's logcat
     * Called from JavaScript: wails.log(level, message)
     *
     * @param level The log level (debug, info, warn, error)
     * @param message The message to log
     */
    @JavascriptInterface
    public void log(String level, String message) {
        switch (level.toLowerCase()) {
            case "debug":
                Log.d(TAG + "/JS", message);
                break;
            case "info":
                Log.i(TAG + "/JS", message);
                break;
            case "warn":
                Log.w(TAG + "/JS", message);
                break;
            case "error":
                Log.e(TAG + "/JS", message);
                break;
            default:
                Log.v(TAG + "/JS", message);
                break;
        }
    }

    /**
     * Get the platform name
     * Called from JavaScript: wails.platform()
     *
     * @return "android"
     */
    @JavascriptInterface
    public String platform() {
        return "android";
    }

    /**
     * Check if we're running in debug mode
     * Called from JavaScript: wails.isDebug()
     *
     * @return true if debug build, false otherwise
     */
    @JavascriptInterface
    public boolean isDebug() {
        return BuildConfig.DEBUG;
    }

    /**
     * Send a callback response to JavaScript
     */
    private void sendCallback(String callbackId, String result, String error) {
        final String js;
        if (error != null) {
            js = String.format(
                    "window._wailsAndroidCallback && window._wailsAndroidCallback('%s', null, '%s');",
                    escapeJsString(callbackId),
                    escapeJsString(error)
            );
        } else {
            js = String.format(
                    "window._wailsAndroidCallback && window._wailsAndroidCallback('%s', '%s', null);",
                    escapeJsString(callbackId),
                    escapeJsString(result != null ? result : "")
            );
        }

        if (closed.get()) return;
        mainHandler.postAtTime(() -> {
            if (closed.get()) return;
            WebView view = webView;
            if (view != null) view.evaluateJavascript(js, null);
        }, this, SystemClock.uptimeMillis());
    }

    private String escapeJsString(String str) {
        if (str == null) return "";
        return str.replace("\\", "\\\\")
                .replace("'", "\\'")
                .replace("\n", "\\n")
                .replace("\r", "\\r")
                // JS line terminators (U+2028/U+2029) must be escaped too; built via
                // (char) casts so the Java lexer does not reinterpret them as newlines.
                .replace(String.valueOf((char) 0x2028), "\\u2028")
                .replace(String.valueOf((char) 0x2029), "\\u2029");
    }
}
