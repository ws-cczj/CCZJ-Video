package com.cczj.video.upgradeaudit;

import android.app.Instrumentation;
import android.content.Context;
import android.content.pm.PackageInfo;
import android.database.Cursor;
import android.database.sqlite.SQLiteDatabase;
import android.net.ConnectivityManager;
import android.net.NetworkCapabilities;
import android.os.Bundle;
import android.provider.Settings;
import org.json.JSONArray;
import org.json.JSONObject;
import java.io.File;
import java.io.FileInputStream;
import java.io.FileOutputStream;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;

/** Signed test instrumentation reads a real release's data without changing its APK. */
public class UpgradeAudit extends Instrumentation {
    private Bundle arguments;
    @Override public void onCreate(Bundle args) { arguments = args; start(); }
    @Override public void onStart() {
        Bundle result = new Bundle();
        try {
            Context context = getTargetContext();
            String label = arguments.getString("label", "snapshot");
            if (!label.matches("[a-zA-Z0-9_-]{1,64}")) throw new Exception("Invalid audit label");
            File data = new File(context.getFilesDir(), "CCZJ Video");
            File output = new File(context.getExternalFilesDir(null), "upgrade-audit/" + label);
            if (output.exists()) throw new Exception("Audit label already exists; use a new label");
            if (!output.mkdirs()) throw new Exception("Cannot create audit folder");
            JSONObject report = new JSONObject();
            PackageInfo info = context.getPackageManager().getPackageInfo(context.getPackageName(), 0);
            report.put("version", info.versionName);
            report.put("firstInstallTime", info.firstInstallTime);
            report.put("lastUpdateTime", info.lastUpdateTime);
            report.put("uid", context.getApplicationInfo().uid);
            report.put("dataDirectory", data.getCanonicalPath());
            ConnectivityManager connectivity = (ConnectivityManager) context.getSystemService(Context.CONNECTIVITY_SERVICE);
            report.put("androidProxy", String.valueOf(connectivity.getDefaultProxy()));
            report.put("globalProxy", String.valueOf(Settings.Global.getString(context.getContentResolver(), "http_proxy")));
            NetworkCapabilities capabilities = connectivity.getNetworkCapabilities(connectivity.getActiveNetwork());
            report.put("vpn", capabilities != null && capabilities.hasTransport(NetworkCapabilities.TRANSPORT_VPN));
            for (String suffix : new String[]{"", "-wal", "-shm"}) {
                File file = new File(data, "cczj_video.db" + suffix);
                if (file.isFile()) copy(file, new File(output, file.getName()));
            }
            File logFolder = new File(data, "applog");
            File[] logs = logFolder.listFiles();
            if (logs != null) for (File log : logs) if (log.isFile()) copy(log, new File(output, log.getName()));
            try (SQLiteDatabase database = SQLiteDatabase.openDatabase(new File(data,"cczj_video.db").getPath(), null, SQLiteDatabase.OPEN_READONLY)) {
                JSONObject tables = new JSONObject();
                for (String table : new String[]{"sources", "source_videos", "global_video", "favorites", "watch_history", "source_types", "global_types", "settings"}) {
                    try (Cursor rows = database.rawQuery("SELECT * FROM " + table + " ORDER BY 1", null)) {
                        JSONArray values = new JSONArray();
                        while (rows.moveToNext()) {
                            JSONArray row = new JSONArray();
                            for (int i=0;i<rows.getColumnCount();i++) row.put(rows.isNull(i)?JSONObject.NULL:rows.getString(i));
                            values.put(row);
                        }
                        JSONObject summary = new JSONObject();
                        summary.put("count",values.length());
                        summary.put("sha256",digest(values.toString().getBytes(StandardCharsets.UTF_8)));
                        tables.put(table,summary);
                    }
                }
                report.put("tables",tables);
                try (Cursor row = database.rawQuery("PRAGMA user_version",null)) { if(row.moveToFirst())report.put("schemaVersion",row.getInt(0)); }
            }
            JSONArray downloads = new JSONArray();
            File updateRoot = new File(data,"updates").getCanonicalFile();
            File[] files = updateRoot.listFiles();
            if(files!=null) for(File file:files) {
                if(!file.isFile() || !file.getCanonicalFile().getParentFile().equals(updateRoot))continue;
                String name=file.getName();
                if(!name.startsWith("CCZJ-Video-Update.") || !(name.endsWith(".apk") || name.endsWith(".apk.sha256") || name.endsWith(".part")))continue;
                JSONObject item=new JSONObject();item.put("name",name);item.put("bytes",file.length());
                if("true".equals(arguments.getString("deleteDownloads")))item.put("deleted",file.delete());
                downloads.put(item);
            }
            report.put("downloads",downloads);
            try(FileOutputStream stream=new FileOutputStream(new File(output,"snapshot.json"))) {stream.write(report.toString(2).getBytes(StandardCharsets.UTF_8));}
            result.putString("report",report.toString());result.putString("output",output.getAbsolutePath());
            finish(-1,result);
        } catch(Exception error) {result.putString("error",error.toString());finish(0,result);}
    }
    private static void copy(File source,File destination) throws Exception {
        try(FileInputStream input=new FileInputStream(source);FileOutputStream output=new FileOutputStream(destination)) {
            byte[] buffer=new byte[65536];int size;while((size=input.read(buffer))>=0)output.write(buffer,0,size);
        }
    }
    private static String digest(byte[] bytes) throws Exception {
        StringBuilder out=new StringBuilder();for(byte value:MessageDigest.getInstance("SHA-256").digest(bytes))out.append(String.format("%02x",value&255));return out.toString();
    }
}
