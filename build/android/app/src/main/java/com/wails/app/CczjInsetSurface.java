package com.wails.app;

import android.graphics.Canvas;
import android.graphics.ColorFilter;
import android.graphics.Paint;
import android.graphics.PixelFormat;
import android.graphics.Rect;
import android.graphics.drawable.Drawable;
import android.view.View;

/** Paint the already-consumed system insets without recolouring WebView content.
 * Android 15 edge-to-edge ignores Window.setStatusBarColor for target SDK 35+. */
final class CczjInsetSurface extends Drawable {
    private final View owner;
    private final Paint paint = new Paint();
    private int canvasColor, statusColor, navigationColor;

    CczjInsetSurface(View owner) { this.owner = owner; }

    void update(int canvas, int status, int navigation) {
        if (canvasColor == canvas && statusColor == status && navigationColor == navigation) return;
        canvasColor = canvas;
        statusColor = status;
        navigationColor = navigation;
        invalidateSelf();
    }

    @Override public void draw(Canvas canvas) {
        Rect bounds = getBounds();
        paint.setColor(canvasColor);
        canvas.drawRect(bounds, paint);
        paint.setColor(statusColor);
        canvas.drawRect(bounds.left, bounds.top, bounds.right, bounds.top + owner.getPaddingTop(), paint);
        paint.setColor(navigationColor);
        canvas.drawRect(bounds.left, bounds.bottom - owner.getPaddingBottom(), bounds.right, bounds.bottom, paint);
    }

    @Override public void setAlpha(int alpha) { paint.setAlpha(alpha); invalidateSelf(); }
    @Override public void setColorFilter(ColorFilter filter) { paint.setColorFilter(filter); invalidateSelf(); }
    @Override public int getOpacity() { return PixelFormat.OPAQUE; }
}
