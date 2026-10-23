(() => {
  if (window.__readingPerf) return;
  let active = null, last = 0, motion = null;
  const errors = [];
  const observers = [];
  for (const type of ['longtask', 'layout-shift']) {
    try {
      const observer = new PerformanceObserver(list => {
        if (!active) return;
        for (const e of list.getEntries()) {
          if (e.startTime < active.start) continue;
          if (type === 'longtask' && active.longTasks.length < 1000)
            active.longTasks.push({ at: e.startTime - active.start, ms: e.duration });
          if (type === 'layout-shift' && !e.hadRecentInput) active.layoutShift += e.value;
        }
      });
      observer.observe({ type, buffered: false });
      observers.push(observer);
    } catch (_) { /* Report unsupported observers instead of inventing values. */ }
  }
  addEventListener('error', e => { if (errors.length < 100) errors.push(String(e.message)); });
  addEventListener('unhandledrejection', e => { if (errors.length < 100) errors.push(String(e.reason)); });
  function frame(now) {
    if (active && last && active.gaps.length < 20000) active.gaps.push(now - last);
    if (motion && motion.samples.length < 2000) {
      const scenes = [...document.querySelectorAll('.cczj-mobile-route-scene')].map(e => {
        const r = e.getBoundingClientRect(), s = getComputedStyle(e);
        return { path: e.dataset.path, x: r.x, width: r.width, height: r.height, opacity: Number(s.opacity), display: s.display, visible: s.visibility };
      });
      const painted = scenes.some(s => s.display !== 'none' && s.visible !== 'hidden' && s.opacity > .02 && s.height > 10 && s.x < innerWidth - 1 && s.x + s.width > 1);
      motion.samples.push({ at: now - motion.start, painted, scenes });
    }
    last = now;
    requestAnimationFrame(frame);
  }
  requestAnimationFrame(frame);
  window.__readingPerf = {
    beginMotion(name) { motion = { name, start: performance.now(), samples: [] }; },
    endMotion() {
      if (!motion) return null;
      const result = motion; motion = null;
      let blankStart = null, maxBlank = 0;
      for (const frame of result.samples) {
        if (!frame.painted && blankStart === null) blankStart = frame.at;
        if (frame.painted && blankStart !== null) { maxBlank = Math.max(maxBlank, frame.at - blankStart); blankStart = null; }
      }
      if (blankStart !== null) maxBlank = Math.max(maxBlank, performance.now() - result.start - blankStart);
      result.maxNoVisibleSceneMs = maxBlank;
      result.note = 'Visible scene geometry only; video/screenshot review is still required for visual animation quality.';
      return result;
    },
    begin(name) { last = 0; active = { name, start: performance.now(), gaps: [], longTasks: [], layoutShift: 0 }; },
    end() {
      if (!active) return null;
      const p = active; active = null;
      const sorted = [...p.gaps].sort((a, b) => a - b);
      return {
        name: p.name, elapsedMs: performance.now() - p.start,
        frames: sorted.length, rafGapP50Ms: sorted[Math.floor(sorted.length * .5)] ?? null,
        rafGapP95Ms: sorted[Math.floor(sorted.length * .95)] ?? null,
        rafGapMaxMs: sorted.at(-1) ?? null,
        rafGapsOver50: sorted.filter(n => n > 50).length,
        rafGapsOver100: sorted.filter(n => n > 100).length,
        longTasks: p.longTasks, layoutShift: p.layoutShift,
        heap: performance.memory ? { used: performance.memory.usedJSHeapSize, total: performance.memory.totalJSHeapSize } : null,
        domNodes: document.getElementsByTagName('*').length,
        images: document.images.length,
        observers: observers.length,
        note: 'rAF gaps describe WebView callback delivery, not hardware presentation FPS.'
      };
    },
    errors
  };
})();
