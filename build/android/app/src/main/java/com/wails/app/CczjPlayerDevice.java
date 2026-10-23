package com.wails.app;

import android.app.Activity;
import android.app.AppOpsManager;
import android.app.PictureInPictureParams;
import android.content.Context;
import android.content.Intent;
import android.content.pm.PackageManager;
import android.media.AudioManager;
import android.net.wifi.WifiManager;
import android.os.Build;
import android.provider.Settings;
import android.util.Rational;
import android.view.WindowManager;
import android.webkit.WebView;
import org.json.JSONObject;
import java.util.concurrent.FutureTask;
import java.util.concurrent.TimeUnit;

/** Native, activity-scoped playback capabilities. No persistent brightness changes. */
final class CczjPlayerDevice {
    private final Activity activity;
    private final WebView webView;
    private final AudioManager audio;
    private WifiManager.MulticastLock multicast;
    private Float originalBrightness;
    CczjPlayerDevice(Activity activity, WebView webView) {
        this.activity = activity; this.webView = webView;
        audio = (AudioManager) activity.getSystemService(Context.AUDIO_SERVICE);
        activity.setVolumeControlStream(AudioManager.STREAM_MUSIC);
    }
    String state() {
        JSONObject out = new JSONObject();
        try {
            out.put("pip", Build.VERSION.SDK_INT >= 26 && activity.getPackageManager().hasSystemFeature(PackageManager.FEATURE_PICTURE_IN_PICTURE));
            out.put("castSettings", new Intent(Settings.ACTION_CAST_SETTINGS).resolveActivity(activity.getPackageManager()) != null);
            // Audio/brightness services can be temporarily unavailable while
            // an OEM host resumes. They must not erase an independent PiP flag.
            out.put("brightness", .5);
            out.put("volume", .5);
            try {
                AppOpsManager operations = (AppOpsManager) activity.getSystemService(Context.APP_OPS_SERVICE);
                int permission = Build.VERSION.SDK_INT >= 26 && operations != null
                    ? operations.checkOpNoThrow(AppOpsManager.OPSTR_PICTURE_IN_PICTURE, android.os.Process.myUid(), activity.getPackageName())
                    : AppOpsManager.MODE_DEFAULT;
                out.put("pipAllowed", permission == AppOpsManager.MODE_ALLOWED);
            } catch (RuntimeException unavailable) {
                // Some OEMs do not expose this operation; an unknown permission
                // probe must not become a permanent "device unsupported" result.
            }
            float b = activity.getWindow().getAttributes().screenBrightness;
            if (b < 0) b = Settings.System.getInt(activity.getContentResolver(), Settings.System.SCREEN_BRIGHTNESS, 128) / 255f;
            out.put("brightness", b);
            out.put("volume", audio.getStreamVolume(AudioManager.STREAM_MUSIC) / (double)Math.max(1, audio.getStreamMaxVolume(AudioManager.STREAM_MUSIC)));
        } catch (Exception ignored) { }
        return out.toString();
    }
    void volume(double value) {
        if (!Double.isFinite(value)) return;
        int level = (int)Math.round(Math.max(0, Math.min(1, value)) * audio.getStreamMaxVolume(AudioManager.STREAM_MUSIC));
        audio.setStreamVolume(AudioManager.STREAM_MUSIC, level, 0);
    }
    void brightness(double value) {
        if (!Double.isFinite(value)) return;
        activity.runOnUiThread(() -> {
            WindowManager.LayoutParams lp = activity.getWindow().getAttributes();
            if (originalBrightness == null) originalBrightness = lp.screenBrightness;
            lp.screenBrightness = (float)Math.max(.02, Math.min(1, value));
            activity.getWindow().setAttributes(lp);
        });
    }
    void restoreBrightness() {
        activity.runOnUiThread(() -> {
            if (originalBrightness == null) return;
            WindowManager.LayoutParams lp = activity.getWindow().getAttributes();
            lp.screenBrightness = originalBrightness; originalBrightness = null;
            activity.getWindow().setAttributes(lp);
        });
    }
    void pip(int width, int height) {
        activity.runOnUiThread(() -> {
            boolean entered = false;
            String failure = "rejected";
            try {
                if (Build.VERSION.SDK_INT >= 26) {
                    double ratio = width / (double)Math.max(height, 1);
                    ratio = Math.max(1 / 2.39, Math.min(2.39, ratio));
                    entered = activity.enterPictureInPictureMode(new PictureInPictureParams.Builder()
                        .setAspectRatio(new Rational((int)(ratio * 1000), 1000)).build());
                }
            } catch (SecurityException denied) {
                failure = "permission";
            } catch (Exception error) {
                android.util.Log.w("CCZJPlayerDevice", "System rejected picture-in-picture", error);
            }
            if (!entered) {
                notifyPip(false);
                webView.evaluateJavascript("window.dispatchEvent(new CustomEvent('cczj:player-pip-error',{detail:'" + failure + "'}))", null);
            }
        });
    }
    void notifyPip(boolean enabled) {
        webView.evaluateJavascript("window.dispatchEvent(new CustomEvent('cczj:player-pip',{detail:" + enabled + "}))", null);
    }
    synchronized void multicast(boolean enabled) {
        if (multicast != null) { if (multicast.isHeld()) multicast.release(); multicast = null; }
        if (enabled) {
            WifiManager wifi = (WifiManager)activity.getApplicationContext().getSystemService(Context.WIFI_SERVICE);
            if (wifi != null) { multicast = wifi.createMulticastLock("cczj-dlna"); multicast.setReferenceCounted(false); multicast.acquire(); }
        }
    }
    String openCastSettings() {
        FutureTask<String> launch = new FutureTask<>(() -> {
            try { activity.startActivity(new Intent(Settings.ACTION_CAST_SETTINGS)); return ""; }
            catch (RuntimeException error) { return "UNSUPPORTED"; }
        });
        activity.runOnUiThread(launch);
        try { return launch.get(5, TimeUnit.SECONDS); }
        catch (Exception error) { launch.cancel(false); return "UNAVAILABLE"; }
    }
    void close() {
        multicast(false); restoreBrightness();
    }
}
