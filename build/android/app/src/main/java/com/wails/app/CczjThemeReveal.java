package com.wails.app;

import android.animation.Animator;
import android.animation.AnimatorListenerAdapter;
import android.animation.ValueAnimator;
import android.graphics.Bitmap;
import android.graphics.Canvas;
import android.graphics.Paint;
import android.graphics.Path;
import android.view.View;
import android.view.ViewGroup;
import android.webkit.WebView;
import android.widget.FrameLayout;
import org.json.JSONObject;

/** One bounded old-frame overlay; the live WebView renders the new palette underneath. */
final class CczjThemeReveal {
    private final WebView web;
    private String identity;
    private Snapshot overlay;
    private ValueAnimator animator;
    private Runnable deadline;
    CczjThemeReveal(WebView web) { this.web=web; }

    void prepare(String id, double x, double y, double radius, double left, double top, double width, double height) {
        cancel(null);
        identity=id;
        try {
            if (!web.isAttachedToWindow() || !(web.getParent() instanceof FrameLayout)) throw new IllegalStateException();
            int w=Math.max(1,(int)Math.round(width*web.getWidth()));
            int h=Math.max(1,(int)Math.round(height*web.getHeight()));
            int l=(int)Math.round(left*web.getWidth()), t=(int)Math.round(top*web.getHeight());
            float scale=(float)Math.min(1,Math.sqrt(2000000d/((double)w*h)));
            Bitmap bitmap=Bitmap.createBitmap(Math.max(1,(int)(w*scale)),Math.max(1,(int)(h*scale)),Bitmap.Config.ARGB_8888);
            Canvas canvas=new Canvas(bitmap);
            canvas.scale(scale,scale); canvas.translate(-l,-t);
            try { web.draw(canvas); } catch (RuntimeException error) { bitmap.recycle(); throw error; }
            bitmap.prepareToDraw();
            overlay=new Snapshot(bitmap,(float)(x*web.getWidth()-l),(float)(y*web.getHeight()-t),(float)(radius*web.getWidth()));
            FrameLayout.LayoutParams params=new FrameLayout.LayoutParams(w,h);
            FrameLayout parent=(FrameLayout)web.getParent();
            params.leftMargin=web.getLeft()-parent.getPaddingLeft()+l; params.topMargin=web.getTop()-parent.getPaddingTop()+t;
            parent.addView(overlay,params);
            deadline=()->cancel(id); web.postDelayed(deadline,4000);
            signal(id,"ready");
        } catch (RuntimeException | OutOfMemoryError error) { cancel(id); }
    }
    void play(String id, int duration) {
        if (id==null || !id.equals(identity) || overlay==null) return;
        // Wait until Chromium has committed the new DOM, while the old bitmap covers it.
        web.postVisualStateCallback(0,new WebView.VisualStateCallback() {
            @Override public void onComplete(long requestId) {
                if (id==null || !id.equals(identity) || overlay==null) return;
                Snapshot target=overlay;
                float end=(float)Math.hypot(Math.max(target.x,target.getWidth()-target.x),Math.max(target.y,target.getHeight()-target.y));
                animator=ValueAnimator.ofFloat(target.initial,end);
                float scale=android.provider.Settings.Global.getFloat(web.getContext().getContentResolver(),android.provider.Settings.Global.ANIMATOR_DURATION_SCALE,1f);
                if(scale<=0){cancel(id);return;}
                // The frontend duration is wall-clock time, shared with browser snapshots.
                animator.setDuration(Math.round(Math.max(1,Math.min(2500,duration))/Math.max(.01f,scale)));
                animator.setInterpolator(new android.view.animation.PathInterpolator(.4f,0f,.2f,1f));
                animator.addUpdateListener(value->{target.radius=(float)value.getAnimatedValue();target.invalidate();});
                animator.addListener(new AnimatorListenerAdapter() {
                    @Override public void onAnimationEnd(Animator animation) { cancel(id); }
                });
                animator.start();
            }
        });
    }
    void cancel(String id) {
        if (id!=null && !id.equals(identity)) return;
        String previous=identity; identity=null;
        if (deadline!=null) web.removeCallbacks(deadline);
        deadline=null;
        if (animator!=null) { animator.removeAllListeners();animator.cancel();animator=null; }
        if (overlay!=null) {
            if (overlay.getParent() instanceof ViewGroup) ((ViewGroup)overlay.getParent()).removeView(overlay);
            overlay.bitmap.recycle();overlay=null;
        }
        if (previous!=null) signal(previous,"done");
    }
    private void signal(String id,String state) {
        if (!web.isAttachedToWindow()) return;
        web.evaluateJavascript("window.dispatchEvent(new CustomEvent('cczj:theme-reveal',{detail:{id:"+JSONObject.quote(id)+",state:"+JSONObject.quote(state)+"}}))",null);
    }
    private final class Snapshot extends View {
        final Bitmap bitmap;
        final float x,y,initial;
        float radius;
        final Paint paint=new Paint(Paint.ANTI_ALIAS_FLAG|Paint.FILTER_BITMAP_FLAG);
        final Path path=new Path();
        final android.graphics.Rect destination=new android.graphics.Rect();
        Snapshot(Bitmap bitmap,float x,float y,float initial) {
            super(web.getContext());this.bitmap=bitmap;this.x=x;this.y=y;this.initial=initial;
            setImportantForAccessibility(View.IMPORTANT_FOR_ACCESSIBILITY_NO);
            setOnTouchListener((view,event)->true);
        }
        @Override protected void onDraw(Canvas canvas) {
            path.rewind();path.setFillType(Path.FillType.EVEN_ODD);
            path.addRect(0,0,getWidth(),getHeight(),Path.Direction.CW);
            if(radius>0)path.addCircle(x,y,radius,Path.Direction.CW);
            canvas.save();canvas.clipPath(path);
            destination.set(0,0,getWidth(),getHeight());
            canvas.drawBitmap(bitmap,null,destination,paint);
            canvas.restore();
        }
    }
}
