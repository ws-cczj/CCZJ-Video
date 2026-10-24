package com.wails.app;

import java.util.concurrent.ArrayBlockingQueue;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.RejectedExecutionException;
import java.util.concurrent.ThreadPoolExecutor;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicBoolean;

/** Owns one WebView's runtime calls. Cancellation is a control operation and
 * never waits behind the business calls it needs to stop. */
final class CczjRuntimeDispatcher implements AutoCloseable {
    interface NativeCall { String call(String payload); }
    interface Reply { void send(String callbackID, String response, String error); }
    private static final String CANCELLED = "{\"ok\":false,\"error\":{\"code\":\"CANCELLED\",\"message\":\"Operation cancelled\"}}";
    private final NativeCall nativeCall;
    private final Reply reply;
    private final ThreadPoolExecutor executor;
    private final ConcurrentHashMap<String, Job> bindings = new ConcurrentHashMap<>();
    private final ConcurrentHashMap<String, Job> jobs = new ConcurrentHashMap<>();
    private final AtomicBoolean closed = new AtomicBoolean();

    CczjRuntimeDispatcher(NativeCall nativeCall, Reply reply) {
        this.nativeCall = nativeCall;
        this.reply = reply;
        this.executor = new ThreadPoolExecutor(2, 4, 30, TimeUnit.SECONDS,
                new ArrayBlockingQueue<>(64), runnable -> {
                    Thread thread = new Thread(runnable, "cczj-runtime");
                    thread.setDaemon(true);
                    return thread;
                }, new ThreadPoolExecutor.AbortPolicy());
        executor.allowCoreThreadTimeOut(true);
    }

    void submit(String callbackID, String payload, String bindingID, String cancelPayload) {
        if (closed.get()) { reply.send(callbackID, null, "Android bridge closed"); return; }
        Job job = new Job(callbackID, payload, bindingID, cancelPayload);
        if (jobs.putIfAbsent(callbackID, job) != null) {
            reply.send(callbackID, null, "Duplicate Android callback identity");
            return;
        }
        if (bindingID != null && bindings.putIfAbsent(bindingID, job) != null) {
            job.finish(null, "Duplicate binding identity");
            return;
        }
        try {
            if (closed.get()) { job.cancel(); return; }
            executor.execute(job);
        } catch (RejectedExecutionException failure) {
            job.finish(null, "Android bridge is busy; try again");
        }
    }

    void cancel(String callbackID, String bindingID, String payload) {
        Job job = bindings.get(bindingID);
        if (job != null) job.cancel();
        // Also deliver to Go: it may already own this call (including calls
        // dispatched before this WebView's adapter was installed).
        try { reply.send(callbackID, nativeCall.call(payload), null); }
        catch (Exception failure) { reply.send(callbackID, null, failure.toString()); }
    }

    @Override public void close() {
        if (!closed.compareAndSet(false, true)) return;
        for (Job job : jobs.values()) job.cancel();
        executor.shutdownNow();
    }

    private final class Job implements Runnable {
        final String callbackID, payload, bindingID, cancelPayload;
        final AtomicBoolean finished = new AtomicBoolean();
        boolean running, cancelled;
        Job(String callbackID, String payload, String bindingID, String cancelPayload) {
            this.callbackID = callbackID; this.payload = payload;
            this.bindingID = bindingID; this.cancelPayload = cancelPayload;
        }
        @Override public void run() {
            synchronized (this) {
                if (cancelled) { finish(CANCELLED, null); return; }
                running = true;
            }
            try { finish(nativeCall.call(payload), null); }
            catch (Exception failure) { finish(null, failure.toString()); }
        }
        void cancel() {
            boolean wasRunning;
            synchronized (this) { cancelled = true; wasRunning = running; }
            if (!wasRunning) {
                executor.remove(this);
                finish(CANCELLED, null);
            } else if (cancelPayload != null) {
                try { nativeCall.call(cancelPayload); }
                catch (Exception failure) { finish(null, failure.toString()); }
            }
        }
        void finish(String response, String error) {
            if (!finished.compareAndSet(false, true)) return;
            jobs.remove(callbackID, this);
            if (bindingID != null) bindings.remove(bindingID, this);
            reply.send(callbackID, response, error);
        }
    }
}
