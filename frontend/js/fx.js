// Corrupted Aeternum on a canvas: drifting smoke, rising embers and, now and
// then, a crimson bolt. Light on purpose: ~30 fps, paused while the window is hidden.
(function () {
  "use strict";
  const canvas = document.getElementById("sky");
  if (!canvas) return;
  const ctx = canvas.getContext("2d");
  const calm = window.matchMedia && window.matchMedia("(prefers-reduced-motion: reduce)").matches;
  let W = 0, H = 0;

  const rand = (a, b) => a + Math.random() * (b - a);

  const smoke = [], embers = [];
  let bolts = [];
  let nextBolt = 0;

  // ---------- embers ----------
  function newEmber(anywhere) {
    // born at the bottom, spread across the whole width
    const x = rand(0, W);
    return {
      x, y: anywhere ? rand(H * 0.15, H) : H + rand(4, 30),
      px: x, py: 0,
      r: rand(0.6, 2.3), v: rand(22, 70), sway: rand(6, 22), ph: Math.random() * 6.28, f: rand(2, 7),
      life: anywhere ? rand(0, 0.6) : 0, span: rand(0.75, 1),
    };
  }

  // ---------- corruption bolt ----------
  function boltPath(x0, y0, x1, y1, disp) {
    let pts = [[x0, y0], [x1, y1]];
    for (let d = disp; d > 2; d /= 2) {
      const out = [pts[0]];
      for (let i = 1; i < pts.length; i++) {
        const [ax, ay] = pts[i - 1], [bx, by] = pts[i];
        const mx = (ax + bx) / 2, my = (ay + by) / 2;
        const nx = -(by - ay), ny = bx - ax, nl = Math.hypot(nx, ny) || 1;
        const o = rand(-d, d);
        out.push([mx + (nx / nl) * o, my + (ny / nl) * o], pts[i]);
      }
      pts = out;
    }
    return pts;
  }
  function spawnBolt() {
    // on either side of the centered hero (outside the title)
    const x0 = Math.random() < 0.5 ? rand(W * 0.04, W * 0.2) : rand(W * 0.8, W * 0.96);
    const y0 = rand(-10, H * 0.12);
    const x1 = x0 + rand(-90, 90), y1 = rand(H * 0.45, H * 0.85);
    const main = boltPath(x0, y0, x1, y1, 70);
    const paths = [main];
    const n = (Math.random() * 3) | 0;
    for (let i = 0; i < n + 1; i++) {
      const k = (main.length * rand(0.2, 0.7)) | 0;
      const [bx, by] = main[k];
      paths.push(boltPath(bx, by, bx + rand(-120, 120), by + rand(50, 160), 30));
    }
    bolts.push({ paths, t: 0, dur: rand(0.45, 0.7) });
  }
  function boltAlpha(b) {
    const p = b.t / b.dur;
    if (p < 0.08) return 1;
    if (p < 0.16) return 0.25;
    if (p < 0.26) return 0.95;
    return Math.max(0, 1 - (p - 0.26) / 0.74) * 0.8;
  }
  function strokePath(pts) {
    ctx.beginPath(); ctx.moveTo(pts[0][0], pts[0][1]);
    for (let i = 1; i < pts.length; i++) ctx.lineTo(pts[i][0], pts[i][1]);
    ctx.stroke();
  }

  function build() {
    smoke.length = 0; embers.length = 0; bolts = [];
    for (let i = 0; i < 8; i++) {
      smoke.push({ x: Math.random() * W, y: rand(H * 0.3, H * 1.05), rx: rand(240, 460), ry: rand(80, 170), v: rand(5, 13) * (Math.random() < 0.5 ? -1 : 1),
        a: rand(0.05, 0.1), hue: Math.random() < 0.6 ? "120,36,26" : "70,52,48" });
    }
    for (let i = 0; i < 95; i++) embers.push(newEmber(true));
    // ?bolt=1 (visual review in a browser) forces a bolt right at the start
    nextBolt = /[?&]bolt=1/.test(location.search) ? 0 : rand(2.5, 5);
  }

  function resize() {
    const r = canvas.getBoundingClientRect();
    W = canvas.width = Math.max(1, Math.round(r.width));
    H = canvas.height = Math.max(1, Math.round(r.height));
    build();
  }

  let last = 0, t = 0;
  function frame(ts) {
    requestAnimationFrame(frame);
    if (document.hidden) return;
    if (ts - last < 33) return;
    const dt = Math.min(0.1, (ts - last) / 1000 || 0.033);
    last = ts; t += dt;

    ctx.clearRect(0, 0, W, H);

    // smoke
    ctx.globalCompositeOperation = "lighter";
    for (const f of smoke) {
      f.x += f.v * dt;
      if (f.x < -f.rx) f.x = W + f.rx; else if (f.x > W + f.rx) f.x = -f.rx;
      const g = ctx.createRadialGradient(f.x, f.y, 0, f.x, f.y, f.rx);
      g.addColorStop(0, `rgba(${f.hue},${f.a})`);
      g.addColorStop(1, `rgba(${f.hue},0)`);
      ctx.fillStyle = g;
      ctx.save(); ctx.translate(f.x, f.y); ctx.scale(1, f.ry / f.rx); ctx.translate(-f.x, -f.y);
      ctx.beginPath(); ctx.arc(f.x, f.y, f.rx, 0, Math.PI * 2); ctx.fill();
      ctx.restore();
    }

    // bolts
    if (!calm) {
      nextBolt -= dt;
      if (nextBolt <= 0) { spawnBolt(); nextBolt = rand(7, 14); }
    }
    for (const b of bolts) {
      b.t += dt;
      const a = boltAlpha(b);
      if (a <= 0) continue;
      ctx.fillStyle = `rgba(150,24,14,${0.07 * a})`;
      ctx.fillRect(0, 0, W, H);
      ctx.lineCap = "round"; ctx.lineJoin = "round";
      b.paths.forEach((p, i) => {
        const w = i === 0 ? 1 : 0.6;
        ctx.shadowColor = "rgba(235,50,25,.95)"; ctx.shadowBlur = 22;
        ctx.strokeStyle = `rgba(225,45,25,${0.32 * a})`; ctx.lineWidth = 7 * w; strokePath(p);
        ctx.shadowBlur = 8;
        ctx.strokeStyle = `rgba(255,120,70,${0.85 * a})`; ctx.lineWidth = 2.2 * w; strokePath(p);
        ctx.shadowBlur = 0;
        ctx.strokeStyle = `rgba(255,232,210,${a})`; ctx.lineWidth = 0.9 * w; strokePath(p);
      });
    }
    bolts = bolts.filter((b) => b.t < b.dur);

    // embers
    for (let i = 0; i < embers.length; i++) {
      const e = embers[i];
      e.px = e.x; e.py = e.y;
      e.y -= e.v * dt;
      e.x += (Math.sin(t * 0.9 + e.ph) * e.sway + 10) * dt; // a light wind to the right
      e.life += dt * 0.11 / e.span;
      if (e.y < -10 || e.life > 1) { embers[i] = newEmber(false); continue; }
      const fade = Math.min(1, e.life * 6) * (1 - e.life);
      const flick = 0.65 + 0.35 * Math.sin(t * e.f + e.ph);
      const a = fade * flick;
      // hot (yellow) at the start, cooling to red
      const gC = (190 - 140 * e.life) | 0, bC = (110 - 90 * e.life) | 0;
      const col = `255,${gC},${bC}`;
      ctx.shadowColor = `rgba(255,${gC},${bC},${a})`; ctx.shadowBlur = 7;
      if (e.r > 1.5) {
        ctx.strokeStyle = `rgba(${col},${a * 0.6})`; ctx.lineWidth = e.r * 0.8;
        ctx.beginPath(); ctx.moveTo(e.px, e.py + 3); ctx.lineTo(e.x, e.y); ctx.stroke();
      }
      ctx.fillStyle = `rgba(${col},${a})`;
      ctx.beginPath(); ctx.arc(e.x, e.y, e.r, 0, Math.PI * 2); ctx.fill();
    }
    ctx.shadowBlur = 0;
    ctx.globalCompositeOperation = "source-over";
  }

  window.addEventListener("resize", resize);
  resize();
  requestAnimationFrame(frame);
})();
