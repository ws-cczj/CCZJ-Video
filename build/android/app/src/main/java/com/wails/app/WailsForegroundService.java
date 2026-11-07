package com.wails.app;

import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.app.PendingIntent;
import android.content.Intent;
import android.content.pm.ServiceInfo;
import android.os.Build;
import android.os.IBinder;
import android.os.PowerManager;
import android.net.wifi.WifiManager;

import androidx.annotation.Nullable;
import androidx.core.app.NotificationCompat;

/**
 * A minimal started foreground service. It does no work of its own — its purpose
 * is to keep the app's process alive (with the required ongoing notification) so
 * the developer's Go goroutines keep running while the app is backgrounded,
 * which Android would otherwise be free to kill. Start it from
 * {@link WailsBridge#startForegroundService(String)} and stop it with
 * {@link WailsBridge#stopForegroundService()}.
 */
public class WailsForegroundService extends android.app.Service {
    public static final String ACTION_START = "com.wails.app.FGS_START";
    private static final String CHANNEL_ID = "wails_foreground";
    private static final int NOTIFICATION_ID = 0x57A1; // "WAI"
    private CczjStartupTrace trace;
    private String castSession="";
    private void mark(String phase){if(trace!=null)trace.mark(phase,castSession);}
    private PowerManager.WakeLock wake;
    private WifiManager.WifiLock wifi;

    @Override
    public int onStartCommand(Intent intent, int flags, int startId) {
        boolean cast = intent != null && "connectedDevice".equals(intent.getStringExtra("type"));
        if(cast && trace==null){trace=new CczjStartupTrace(this,"cast-native");castSession=intent.getStringExtra("cast_session_id");mark("cast_start_requested");}
        try {
        String title = "Wails";
        String text = "Running in the background";
        if (intent != null) {
            if (intent.getStringExtra("title") != null) title = intent.getStringExtra("title");
            if (intent.getStringExtra("text") != null) text = intent.getStringExtra("text");
        }

        NotificationManager nm = (NotificationManager) getSystemService(NOTIFICATION_SERVICE);
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            NotificationChannel ch = new NotificationChannel(
                    CHANNEL_ID, getString(R.string.app_name), NotificationManager.IMPORTANCE_LOW);
            nm.createNotificationChannel(ch);
        }

        PendingIntent contentIntent = null;
        Intent launch = getPackageManager().getLaunchIntentForPackage(getPackageName());
        if (launch != null) {
            int piFlags = Build.VERSION.SDK_INT >= Build.VERSION_CODES.M
                    ? PendingIntent.FLAG_IMMUTABLE : 0;
            contentIntent = PendingIntent.getActivity(this, 0, launch, piFlags);
        }

        Notification n = new NotificationCompat.Builder(this, CHANNEL_ID)
                .setSmallIcon(android.R.drawable.ic_popup_sync)
                .setContentTitle(title)
                .setContentText(text)
                .setOngoing(true)
                .setContentIntent(contentIntent)
                .build();

        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            startForeground(NOTIFICATION_ID, n, cast ? ServiceInfo.FOREGROUND_SERVICE_TYPE_CONNECTED_DEVICE : ServiceInfo.FOREGROUND_SERVICE_TYPE_DATA_SYNC);
        } else {
            startForeground(NOTIFICATION_ID, n);
        }
        if(cast)mark("cast_service_started");
        if (cast && wake == null) {
          try {
            PowerManager power = (PowerManager)getSystemService(POWER_SERVICE);
            if (power != null) {
                wake = power.newWakeLock(PowerManager.PARTIAL_WAKE_LOCK, "CCZJ:cast-relay");
                wake.setReferenceCounted(false);
                wake.acquire(12 * 60 * 60 * 1000L);
            }
            WifiManager manager = (WifiManager)getApplicationContext().getSystemService(WIFI_SERVICE);
            if (manager != null) {
                wifi = manager.createWifiLock(WifiManager.WIFI_MODE_FULL_HIGH_PERF, "CCZJ:cast-relay");
                wifi.setReferenceCounted(false);
                wifi.acquire();
            }
          if(wake!=null&&wake.isHeld()&&wifi!=null&&wifi.isHeld())mark("cast_locks_acquired");else{mark("cast_locks_failed");releaseLocks();}
          } catch (RuntimeException unavailable) {
            mark(unavailable instanceof SecurityException?"cast_permission_denied":"cast_locks_failed");
            releaseLocks();
            android.util.Log.w("CCZJCast", "Background streaming lock unavailable", unavailable);
          }
        }
        // Capability URLs belong to the current Go process and cannot survive
        // a process restart. Never restart an orphan foreground notification.
        return cast ? START_NOT_STICKY : START_STICKY;
        } catch(RuntimeException error) {
            mark(error instanceof SecurityException?"cast_permission_denied":"cast_service_failed");releaseLocks();stopSelf();
            return START_NOT_STICKY;
        }
    }

    @Override
    public void onDestroy() {
        releaseLocks();mark("cast_service_stopped");if(trace!=null){trace.close();trace=null;}
        super.onDestroy();
    }

    private void releaseLocks() {
        boolean released=true;
        try { if(wifi!=null&&wifi.isHeld())wifi.release(); }
        catch(RuntimeException error){released=false;mark("cast_release_failed");}
        try { if(wake!=null&&wake.isHeld())wake.release(); }
        catch(RuntimeException error){released=false;mark("cast_release_failed");}
        if(released){
            if(wifi!=null||wake!=null)mark("cast_locks_released");
            wifi=null;wake=null;
        }
    }

    @Nullable
    @Override
    public IBinder onBind(Intent intent) {
        return null;
    }
}
