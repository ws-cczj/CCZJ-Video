package com.wails.app;

import android.app.Activity;
import android.app.AppOpsManager;
import android.app.PictureInPictureParams;
import android.content.Context;
import android.content.Intent;
import android.content.pm.PackageManager;
import android.media.AudioManager;
import android.net.ConnectivityManager;
import android.net.LinkAddress;
import android.net.LinkProperties;
import android.net.Network;
import android.net.NetworkCapabilities;
import android.net.wifi.WifiManager;
import android.os.Build;
import android.provider.Settings;
import android.system.Os;
import android.util.Rational;
import android.view.WindowManager;
import android.webkit.WebView;
import org.json.JSONObject;
import org.json.JSONArray;
import java.net.Inet4Address;
import java.net.InetAddress;
import java.net.NetworkInterface;
import java.util.HashSet;
import java.util.concurrent.FutureTask;
import java.util.concurrent.TimeUnit;

/** Native, activity-scoped playback capabilities. No persistent brightness changes. */
final class CczjPlayerDevice {
    private final Activity activity;
    private final WebView webView;
    private final AudioManager audio;
    private WifiManager.MulticastLock multicast;
    private Float originalBrightness;
    private boolean closed;
    private final android.content.BroadcastReceiver noisyReceiver = new android.content.BroadcastReceiver() {
        @Override public void onReceive(Context context, Intent intent) {
            if (AudioManager.ACTION_AUDIO_BECOMING_NOISY.equals(intent.getAction())) interruption("noisy");
        }
    };
    CczjPlayerDevice(Activity activity, WebView webView) {
        this.activity = activity; this.webView = webView;
        audio = (AudioManager) activity.getSystemService(Context.AUDIO_SERVICE);
        activity.setVolumeControlStream(AudioManager.STREAM_MUSIC);
        android.content.IntentFilter filter = new android.content.IntentFilter(AudioManager.ACTION_AUDIO_BECOMING_NOISY);
        if (Build.VERSION.SDK_INT >= 33) activity.registerReceiver(noisyReceiver, filter, Context.RECEIVER_NOT_EXPORTED);
        else activity.registerReceiver(noisyReceiver, filter);
    }
    void interruption(String reason) {
        if (closed) return;
        // Chromium owns audio focus; another native request would compete with it.
        webView.evaluateJavascript("window.dispatchEvent(new CustomEvent('cczj:player-interruption',{detail:" + JSONObject.quote(reason) + "}))", null);
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
                if (permission == AppOpsManager.MODE_ERRORED || permission == AppOpsManager.MODE_IGNORED) out.put("pipAllowed", false);
                else if (permission == AppOpsManager.MODE_ALLOWED) out.put("pipAllowed", true);
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
                    // Stay inside Android's boundary after rational rounding.
                    ratio = Math.max(1 / 2.38, Math.min(2.38, ratio));
                    entered = activity.enterPictureInPictureMode(new PictureInPictureParams.Builder()
                        .setAspectRatio(new Rational((int)Math.round(ratio * 10000), 10000)).build());
                }
            } catch (SecurityException denied) {
                failure = "permission";
            } catch (Exception error) {
                android.util.Log.w("CCZJPlayerDevice", "System rejected picture-in-picture", error);
            }
            if (!entered) {
                android.util.Log.w("CCZJPlayerDevice", "PiP entry failed: " + failure + "; sdk=" + Build.VERSION.SDK_INT
                    + "; feature=" + activity.getPackageManager().hasSystemFeature(PackageManager.FEATURE_PICTURE_IN_PICTURE));
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
    String castInterfaces() {
        try {
            if (closed) throw new IllegalStateException("Player device is closed");
            ConnectivityManager connectivity = (ConnectivityManager) activity.getSystemService(Context.CONNECTIVITY_SERVICE);
            if (connectivity == null) throw new IllegalStateException("Connectivity service unavailable");
            JSONArray interfaces = new JSONArray();
            HashSet<String> seen = new HashSet<>();
            boolean missingIndex = false;
            for (Network network : connectivity.getAllNetworks()) {
                NetworkCapabilities capabilities = connectivity.getNetworkCapabilities(network);
                if (capabilities == null || capabilities.hasTransport(NetworkCapabilities.TRANSPORT_VPN)
                    || (!capabilities.hasTransport(NetworkCapabilities.TRANSPORT_WIFI)
                        && !capabilities.hasTransport(NetworkCapabilities.TRANSPORT_ETHERNET))) continue;
                LinkProperties properties = connectivity.getLinkProperties(network);
                if (properties == null || properties.getInterfaceName() == null) continue;
                String name = properties.getInterfaceName();
                int index;
                if (Build.VERSION.SDK_INT >= 26) index = Os.if_nametoindex(name);
                else {
                    // API 24/25 lack Os.if_nametoindex. Query only the interface
                    // supplied by ConnectivityManager; never enumerate interfaces.
                    NetworkInterface known = NetworkInterface.getByName(name);
                    index = known == null ? 0 : known.getIndex();
                }
                if (index <= 0) { missingIndex = true; continue; }
                for (LinkAddress link : properties.getLinkAddresses()) {
                    InetAddress address = link.getAddress();
                    if (!(address instanceof Inet4Address) || !address.isSiteLocalAddress()
                        || address.isLoopbackAddress() || address.isLinkLocalAddress()) continue;
                    String ip = address.getHostAddress();
                    if (!seen.add(index + ":" + ip)) continue;
                    interfaces.put(new JSONObject().put("index", index).put("address", ip));
                    if (interfaces.length() == 16) break;
                }
                if (interfaces.length() == 16) break;
            }
            if (interfaces.length() == 0 && missingIndex) throw new IllegalStateException("LAN interface index unavailable");
            return new JSONObject().put("interfaces", interfaces).toString();
        } catch (Exception unavailable) {
            android.util.Log.w("CCZJPlayerDevice", "Unable to read casting LAN interfaces", unavailable);
            return "{\"interfaces\":[],\"error\":\"UNAVAILABLE\"}";
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
        if (closed) return;
        closed = true;
        activity.unregisterReceiver(noisyReceiver);
        multicast(false); restoreBrightness();
    }
}
