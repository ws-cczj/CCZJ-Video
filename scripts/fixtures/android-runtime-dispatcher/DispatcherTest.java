package com.wails.app;

import java.util.concurrent.CountDownLatch;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicInteger;

public class DispatcherTest {
    static void require(boolean value, String message) {
        if (!value) throw new AssertionError(message);
    }
    static void await(CountDownLatch latch) throws Exception {
        require(latch.await(3, TimeUnit.SECONDS), "timed out");
    }
    public static void main(String[] args) throws Exception {
        CountDownLatch started = new CountDownLatch(2), cancelled = new CountDownLatch(1);
        CountDownLatch done = new CountDownLatch(4);
        AtomicInteger queuedStarts = new AtomicInteger();
        ConcurrentHashMap<String,String> replies = new ConcurrentHashMap<>();
        try (CczjRuntimeDispatcher dispatcher = new CczjRuntimeDispatcher(payload -> {
            if (payload.startsWith("cancel")) { cancelled.countDown(); return "{}"; }
            if (payload.equals("queued")) queuedStarts.incrementAndGet();
            else {
                started.countDown();
                try { await(cancelled); } catch (Exception e) { throw new RuntimeException(e); }
            }
            return "{}";
        }, (id,response,error) -> { replies.put(id,error == null ? response : error); done.countDown(); })) {
            dispatcher.submit("one","blocking","one","cancel-one");
            dispatcher.submit("two","blocking","two","cancel-two");
            await(started);
            dispatcher.submit("queued","queued","queued","cancel-queued");
            // Cancelling queued work must not enter native code at all.
            dispatcher.cancel("cancel-ack","queued","cancel-queued");
            await(done);
            require(queuedStarts.get() == 0,"cancelled queue entry reached native code");
            require(replies.get("queued").contains("CANCELLED"),"missing queued cancellation reply");
            require(replies.containsKey("cancel-ack"),"cancellation acknowledgement was blocked");
        }
        CountDownLatch active = new CountDownLatch(2), release = new CountDownLatch(1), ended = new CountDownLatch(2);
        CczjRuntimeDispatcher dispatcher = new CczjRuntimeDispatcher(payload -> {
            if (payload.startsWith("cancel")) { release.countDown(); return "{}"; }
            active.countDown();
            try { await(release); } catch(Exception e) { throw new RuntimeException(e); }
            return "{}";
        }, (id,response,error) -> ended.countDown());
        dispatcher.submit("first","block","first","cancel-first");
        dispatcher.submit("second","block","second","cancel-second");
        await(active);
        dispatcher.close();
        await(ended);
        dispatcher.close();
        System.out.println("PASS: cancellation bypasses occupied workers; queued work never starts; host close cancels native work; close is idempotent");
    }
}
