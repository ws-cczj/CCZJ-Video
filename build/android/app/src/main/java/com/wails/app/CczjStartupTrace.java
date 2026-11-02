package com.wails.app;

import android.content.Context;
import android.os.SystemClock;
import android.util.Log;
import java.io.File;
import java.io.FileOutputStream;
import java.nio.charset.StandardCharsets;
import java.util.Arrays;
import java.util.HashSet;
import java.util.Set;
import java.util.concurrent.ArrayBlockingQueue;
import java.util.concurrent.ThreadPoolExecutor;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicInteger;
import org.json.JSONObject;

/** Early native evidence survives independently of Go, SQLite and the WebView. */
final class CczjStartupTrace {
    private static final Set<String> PHASES=new HashSet<>(Arrays.asList(
        "activity_create","layout_ready","temporary_storage_ready","native_library_load","native_library_ready",
        "go_initialize","go_init_returned","webview_create","webview_ready","page_load","page_started","page_committed","page_finished",
        "resume","pause","stop","destroy","frontend_modules_ready","frontend_runtime","frontend_typography","frontend_locale","frontend_motion",
        "frontend_mode","frontend_mode_shell","frontend_theme","frontend_vue_mount","frontend_first_screen","frontend_first_render_opportunity",
        "frontend_image_decode","frontend_splash_reveal","frontend_splash_closed"));
    private final long began=SystemClock.elapsedRealtime();
    private final int pid=android.os.Process.myPid();
    private final AtomicInteger accepted=new AtomicInteger(),written=new AtomicInteger(),dropped=new AtomicInteger(),failed=new AtomicInteger();
    private final File directory,file;
    private volatile String lastPhase="";
    private volatile boolean closed;
    private final ThreadPoolExecutor writer=new ThreadPoolExecutor(1,1,0,TimeUnit.MILLISECONDS,new ArrayBlockingQueue<>(64),task->{Thread thread=new Thread(task,"cczj-startup-trace");thread.setDaemon(true);return thread;},(task,pool)->dropped.incrementAndGet());
    CczjStartupTrace(Context context) {
        directory=new File(context.getFilesDir(),"CCZJ Video/applog");
        file=new File(directory,"startup-native-"+System.currentTimeMillis()+"-"+pid+".jsonl");
        writer.execute(()->{
            try {
                if(!directory.isDirectory()&&!directory.mkdirs())throw new java.io.IOException();
                File[] files=directory.listFiles((dir,name)->name.matches("startup-native-[0-9]+-[0-9]+\\.jsonl"));
                if(files!=null){Arrays.sort(files,(a,b)->b.getName().compareTo(a.getName()));for(int i=3;i<files.length;i++)if(!files[i].delete())failed.incrementAndGet();}
            } catch(Exception error){failed.incrementAndGet();}
        });
    }
    synchronized void mark(String phase) {
        if(closed||!PHASES.contains(phase))return;
        if(accepted.get()>=64){dropped.incrementAndGet();return;}
        try {
            int sequence=accepted.incrementAndGet();lastPhase=phase;
            JSONObject row=new JSONObject();row.put("phase",phase);row.put("sequence",sequence);row.put("pid",pid);
            row.put("at_ms",System.currentTimeMillis());row.put("elapsed_ms",SystemClock.elapsedRealtime()-began);
            row.put("version",BuildConfig.VERSION_NAME);row.put("host_abi",android.os.Build.SUPPORTED_ABIS[0]);
            byte[] line=(row.toString()+"\n").getBytes(StandardCharsets.UTF_8);
            if(line.length>512){dropped.incrementAndGet();return;}
            Log.i("CCZJStartup","phase="+phase+" pid="+pid+" sequence="+sequence);
            writer.execute(()->{try(FileOutputStream stream=new FileOutputStream(file,true)){stream.write(line);written.incrementAndGet();}catch(Exception error){failed.incrementAndGet();}});
        }catch(Exception error){failed.incrementAndGet();}
    }
    synchronized void close(){if(closed)return;mark("destroy");closed=true;writer.shutdown();}
    JSONObject snapshot() {
        JSONObject row=new JSONObject();
        try{row.put("last_phase",lastPhase);row.put("accepted",accepted.get());row.put("written",written.get());row.put("dropped",dropped.get());row.put("failed",failed.get());}catch(Exception ignored){}
        return row;
    }
}
