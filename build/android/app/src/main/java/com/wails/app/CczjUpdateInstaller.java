package com.wails.app;

import android.app.Activity;
import android.content.Intent;
import android.content.pm.PackageInfo;
import android.content.pm.PackageManager;
import android.content.pm.Signature;
import android.net.Uri;
import android.os.Build;
import android.provider.Settings;
import androidx.core.content.FileProvider;
import java.io.File;
import java.io.FileInputStream;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.util.Arrays;
import java.util.concurrent.FutureTask;
import java.util.concurrent.TimeUnit;

/** Opens the system installer only for a verified, same-package update in the
 * private update directory. Installation remains a user-confirmed OS action. */
final class CczjUpdateInstaller {
    private final Activity activity;
    CczjUpdateInstaller(Activity activity) { this.activity = activity; }

    String install(String path) {
        try {
            if (path == null || path.length() > 4096) return "Invalid update path";
            File root = new File(activity.getFilesDir(), "CCZJ Video/updates").getCanonicalFile();
            File file = new File(path).getCanonicalFile();
            if (!root.equals(file.getParentFile()) || !file.isFile()
                    || !file.getName().startsWith("CCZJ-Video-Update.") || !file.getName().endsWith(".apk"))
                return "Only downloaded APK updates can be installed";
            File receipt = new File(file.getPath() + ".sha256");
            if (!receipt.isFile() || receipt.length() > 128) return "Update checksum receipt missing";
            byte[] receiptBytes = new byte[128]; int count;
            try (FileInputStream input = new FileInputStream(receipt)) { count = input.read(receiptBytes); }
            if (count <= 0) return "Empty update checksum receipt";
            String expected = new String(receiptBytes, 0, count, StandardCharsets.US_ASCII).trim();
            if (!expected.matches("[a-fA-F0-9]{64}")) return "Invalid update checksum receipt";
            MessageDigest digest = MessageDigest.getInstance("SHA-256");
            try (FileInputStream input = new FileInputStream(file)) {
                byte[] buffer = new byte[65536]; int n;
                while ((n = input.read(buffer)) != -1) digest.update(buffer, 0, n);
            }
            StringBuilder actual = new StringBuilder();
            for (byte value : digest.digest()) actual.append(String.format(java.util.Locale.ROOT, "%02x", value & 255));
            if (!actual.toString().equalsIgnoreCase(expected)) return "APK checksum mismatch; download again";

            PackageManager manager = activity.getPackageManager();
            int flags = Build.VERSION.SDK_INT >= 28 ? PackageManager.GET_SIGNING_CERTIFICATES : PackageManager.GET_SIGNATURES;
            PackageInfo incoming = manager.getPackageArchiveInfo(file.getPath(), flags);
            PackageInfo installed = manager.getPackageInfo(activity.getPackageName(), flags);
            if (incoming == null || !activity.getPackageName().equals(incoming.packageName)) return "APK belongs to a different application";
            long next = Build.VERSION.SDK_INT >= 28 ? incoming.getLongVersionCode() : incoming.versionCode;
            long current = Build.VERSION.SDK_INT >= 28 ? installed.getLongVersionCode() : installed.versionCode;
            if (next < current) return "APK is older than the installed application";
            Signature[] incomingKeys = Build.VERSION.SDK_INT >= 28 ? incoming.signingInfo.getApkContentsSigners() : incoming.signatures;
            Signature[] currentKeys = Build.VERSION.SDK_INT >= 28 ? installed.signingInfo.getApkContentsSigners() : installed.signatures;
            if (incomingKeys == null || currentKeys == null || incomingKeys.length == 0 || !Arrays.equals(incomingKeys, currentKeys))
                return "APK signing certificate does not match this installation";

            if (Build.VERSION.SDK_INT >= 26 && !manager.canRequestPackageInstalls()) {
                String error = open(new Intent(Settings.ACTION_MANAGE_UNKNOWN_APP_SOURCES, Uri.parse("package:"+activity.getPackageName())));
                return error.isEmpty() ? "Allow this app to install updates, then return and tap Install again." : error;
            }
            Uri uri = FileProvider.getUriForFile(activity, activity.getPackageName()+".fileprovider", file);
            return open(new Intent(Intent.ACTION_VIEW).setDataAndType(uri,"application/vnd.android.package-archive")
                .addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION));
        } catch (Exception error) {
            android.util.Log.w("CCZJUpdate", "Cannot open APK installer", error);
            return "Cannot open the Android update installer";
        }
    }

    private String open(Intent intent) throws Exception {
        FutureTask<String> launch = new FutureTask<>(() -> {
            try { activity.startActivity(intent); return ""; }
            catch (Exception error) { android.util.Log.w("CCZJUpdate", "Installer activity unavailable", error); return "System installer unavailable"; }
        });
        activity.runOnUiThread(launch);
        return launch.get(5, TimeUnit.SECONDS);
    }
}
