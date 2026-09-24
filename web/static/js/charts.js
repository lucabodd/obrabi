// Dependency-free SVG charts following the data-viz rules: thin marks
// (columns ≤ 24px, 4px rounded data-end, 2px surface gaps), hairline grid,
// one axis, selective direct labels, a hover/tap/focus tooltip on every mark,
// and a table view as the accessible twin of each chart.

import { h, mount } from './dom.js';

/** Clean tick values (1/2/2.5/5 × 10ⁿ) spanning min..max and zero. */
export function niceScale(min, max, count = 4) {
  min = Math.min(0, min);
  max = Math.max(0, max);
  if (min === max) max = min + 100;
  const span = max - min;
  const raw = span / count;
  const pow = Math.pow(10, Math.floor(Math.log10(raw)));
  const step = [1, 2, 2.5, 5, 10].map((m) => m * pow).find((s) => span / s <= count) || 10 * pow;
  const lo = Math.floor(min / step) * step;
  const hi = Math.ceil(max / step) * step;
  const ticks = [];
  for (let v = lo; v <= hi + step / 2; v += step) ticks.push(Math.round(v));
  return { lo, hi, ticks };
}

/** Renders on first layout and again whenever the container width changes. */
function responsive(container, draw) {
  let lastWidth = 0;
  const run = () => {
    const w = Math.round(container.getBoundingClientRect().width);
    if (w > 0 && w !== lastWidth) {
      lastWidth = w;
      draw(w);
    }
  };
  if ('ResizeObserver' in window) new ResizeObserver(run).observe(container);
  requestAnimationFrame(run);
}

function svgEl(width, height, label) {
  return h('svg', { width, height, viewBox: `0 0 ${width} ${height}`, role: 'img', 'aria-label': label });
}

// A tooltip shared by the marks of one chart.
function tooltip(container) {
  const tip = h('div.tooltip', { role: 'status', hidden: true });
  container.append(tip);
  return {
    show(x, y, content) {
      mount(tip, content);
      tip.hidden = false;
      const cw = container.clientWidth;
      const tw = tip.offsetWidth;
      const th = tip.offsetHeight;
      let left = x - tw / 2;
      left = Math.max(0, Math.min(cw - tw, left));
      let top = y - th - 12;
      if (top < -8) top = y + 14;
      tip.style.left = `${left}px`;
      tip.style.top = `${top}px`;
    },
    hide() { tip.hidden = true; },
  };
}

function ttRow(value, name, keyClass = '') {
  return h('div.tt-row', h('span.tt-key', { class: keyClass }), h('strong', value), name ? h('span.tt-name', name) : null);
}

/** Column path with a 4px rounded data end and a square base. */
function columnPath(x, w, y0, y1) {
  const up = y1 < y0;
  const hgt = Math.abs(y1 - y0);
  const r = Math.min(4, w / 2, hgt);
  if (hgt < 0.5) return `M${x},${y0}h${w}v-0.5h${-w}z`;
  if (up) {
    return `M${x},${y0}V${y1 + r}Q${x},${y1} ${x + r},${y1}H${x + w - r}Q${x + w},${y1} ${x + w},${y1 + r}V${y0}Z`;
  }
  return `M${x},${y0}V${y1 - r}Q${x},${y1} ${x + r},${y1}H${x + w - r}Q${x + w},${y1} ${x + w},${y1 - r}V${y0}Z`;
}

function measureText(text, size = 11) {
  // Rough width of tabular digits in the system font; good enough for layout.
  return String(text).length * size * 0.6;
}

/**
 * Column chart for one series over time.
 * data: [{ key, label, title, value }]; highlightKey: the column to label.
 */
export function columnChart({ data, format, formatTick, highlightKey, ariaLabel, height: fixedHeight }) {
  const container = h('div.chart');
  const tip = tooltip(container);

  responsive(container, (width) => {
    // Taller on wide screens, where the card sits next to the category split.
    const height = fixedHeight || (width >= 440 && window.innerWidth >= 1000 ? 300 : 220);
    const values = data.map((d) => d.value);
    const scale = niceScale(Math.min(...values, 0), Math.max(...values, 0));
    const tickW = Math.max(...scale.ticks.map((t) => measureText(formatTick(t)))) + 8;
    const m = { top: 24, right: 6, bottom: 26, left: Math.max(30, tickW) };
    const pw = width - m.left - m.right;
    const ph = height - m.top - m.bottom;
    const y = (v) => m.top + ph - ((v - scale.lo) / (scale.hi - scale.lo)) * ph;
    const band = pw / data.length;
    const barW = Math.max(2, Math.min(24, band - 2));
    const y0 = y(0);

    const svg = svgEl(width, height, ariaLabel);
    for (const t of scale.ticks) {
      const ty = Math.round(y(t)) + 0.5;
      svg.append(h('line', { class: t === 0 ? 'base-line' : 'grid-line', x1: m.left, x2: width - m.right, y1: ty, y2: ty }));
      svg.append(h('text', { class: 'tick', x: m.left - 8, y: ty + 4, 'text-anchor': 'end' }, formatTick(t)));
    }

    const maxLabels = Math.max(2, Math.floor(pw / 38));
    const every = Math.ceil(data.length / maxLabels);
    data.forEach((d, i) => {
      const cx = m.left + band * i + band / 2;
      const x = cx - barW / 2;
      const active = d.key === highlightKey;
      const bar = h('path', { class: `bar${d.value < 0 ? ' neg' : ''}${active ? ' active' : ''}`, d: columnPath(x, barW, y0, y(d.value)) });
      svg.append(bar);
      if ((data.length - 1 - i) % every === 0) {
        svg.append(h('text', { class: 'tick', x: cx, y: height - 8, 'text-anchor': 'middle' }, d.label));
      }
      if (active && d.value !== 0) {
        const vy = d.value >= 0 ? y(d.value) - 7 : y(d.value) + 15;
        const lx = Math.max(m.left + 20, Math.min(width - 24, cx));
        svg.append(h('text', { class: 'value-label', x: lx, y: vy, 'text-anchor': 'middle' }, format(d.value, true)));
      }
      // The hit target is the whole band, not only the painted bar.
      const hit = h('rect', {
        class: 'hit', x: m.left + band * i, y: m.top, width: band, height: ph,
        tabindex: 0, role: 'img', 'aria-label': `${d.title}: ${format(d.value)}`,
      });
      const show = () => {
        container.classList.add('has-active');
        svg.querySelectorAll('.bar').forEach((b) => b.classList.remove('active'));
        bar.classList.add('active');
        tip.show(cx, Math.min(y(d.value), y0), [h('div.tt-title', d.title), ttRow(format(d.value))]);
      };
      const hide = () => {
        container.classList.remove('has-active');
        bar.classList.toggle('active', active);
        tip.hide();
      };
      hit.addEventListener('pointerenter', show);
      hit.addEventListener('pointerdown', show);
      hit.addEventListener('pointerleave', hide);
      hit.addEventListener('focus', show);
      hit.addEventListener('blur', hide);
      svg.append(hit);
    });
    mount(container, svg, container.querySelector('.tooltip'));
  });
  return container;
}

/**
 * Line chart comparing series over the same x positions (e.g. the months of
 * this year and last year). series: [{ name, cls: 's1' | 'context', values }]
 * with null for missing points. A crosshair snaps to the nearest x and the
 * tooltip lists every series there.
 */
export function lineChart({ labels, titles, series, format, formatTick, ariaLabel, height = 220 }) {
  const container = h('div.chart');
  const tip = tooltip(container);

  responsive(container, (width) => {
    const all = series.flatMap((s) => s.values).filter((v) => v != null);
    const scale = niceScale(Math.min(0, ...all), Math.max(0, ...all));
    const tickW = Math.max(...scale.ticks.map((t) => measureText(formatTick(t)))) + 8;
    const m = { top: 16, right: 44, bottom: 26, left: Math.max(30, tickW) };
    const pw = width - m.left - m.right;
    const ph = height - m.top - m.bottom;
    const n = labels.length;
    const x = (i) => m.left + (n === 1 ? pw / 2 : (pw * i) / (n - 1));
    const y = (v) => m.top + ph - ((v - scale.lo) / (scale.hi - scale.lo)) * ph;

    const svg = svgEl(width, height, ariaLabel);
    for (const t of scale.ticks) {
      const ty = Math.round(y(t)) + 0.5;
      svg.append(h('line', { class: t === 0 ? 'base-line' : 'grid-line', x1: m.left, x2: m.left + pw, y1: ty, y2: ty }));
      svg.append(h('text', { class: 'tick', x: m.left - 8, y: ty + 4, 'text-anchor': 'end' }, formatTick(t)));
    }
    const every = Math.ceil(n / Math.max(2, Math.floor(pw / 34)));
    labels.forEach((l, i) => {
      if (i % every === 0) svg.append(h('text', { class: 'tick', x: x(i), y: height - 8, 'text-anchor': 'middle' }, l));
    });

    // Context series first so the highlighted one draws on top.
    const ends = [];
    for (const s of [...series].reverse()) {
      let d = '';
      let pen = false;
      s.values.forEach((v, i) => {
        if (v == null) { pen = false; return; }
        d += `${pen ? 'L' : 'M'}${x(i).toFixed(1)},${y(v).toFixed(1)}`;
        pen = true;
      });
      if (d) svg.append(h('path', { class: `series-line ${s.cls}`, d }));
      const last = s.values.reduce((acc, v, i) => (v == null ? acc : i), -1);
      if (last >= 0) {
        svg.append(h('circle', { class: `dot ${s.cls}`, cx: x(last), cy: y(s.values[last]), r: 4 }));
        ends.push({ s, i: last, y: y(s.values[last]) });
      }
    }
    // Direct end labels, unless two of them would collide (the legend and
    // the tooltip carry the identity then).
    ends.sort((a, b) => a.y - b.y);
    ends.forEach((e, k) => {
      const clash = ends.some((o, j) => j !== k && Math.abs(o.y - e.y) < 14);
      if (!clash || e.s.cls !== 'context') {
        svg.append(h('text', { class: 'end-label', x: x(e.i) + 8, y: e.y + 4 }, e.s.name));
      }
    });

    const cross = h('line', { class: 'crosshair', y1: m.top, y2: m.top + ph, x1: 0, x2: 0, visibility: 'hidden' });
    const markers = series.map((s) => h('circle', { class: `dot ${s.cls}`, r: 4, visibility: 'hidden' }));
    svg.append(cross, ...markers);

    let idx = -1;
    const showAt = (i) => {
      idx = Math.max(0, Math.min(n - 1, i));
      const cx = Math.round(x(idx)) + 0.5;
      cross.setAttribute('x1', cx);
      cross.setAttribute('x2', cx);
      cross.setAttribute('visibility', 'visible');
      let top = m.top + ph;
      series.forEach((s, k) => {
        const v = s.values[idx];
        if (v == null) { markers[k].setAttribute('visibility', 'hidden'); return; }
        markers[k].setAttribute('cx', x(idx));
        markers[k].setAttribute('cy', y(v));
        markers[k].setAttribute('visibility', 'visible');
        top = Math.min(top, y(v));
      });
      tip.show(x(idx), top, [
        h('div.tt-title', titles ? titles[idx] : labels[idx]),
        ...series.map((s) => ttRow(s.values[idx] == null ? '—' : format(s.values[idx]), s.name, s.cls === 'context' ? 'context' : '')),
      ]);
    };
    const hide = () => {
      cross.setAttribute('visibility', 'hidden');
      markers.forEach((mk) => mk.setAttribute('visibility', 'hidden'));
      tip.hide();
    };
    const overlay = h('rect', {
      class: 'hit', x: m.left - 10, y: m.top, width: pw + 20, height: ph, tabindex: 0,
      'aria-label': `${ariaLabel}. Utilitza les fletxes per a recórrer els mesos.`,
    });
    const fromEvent = (e) => {
      const r = svg.getBoundingClientRect();
      const px = ((e.clientX - r.left) / r.width) * width;
      return Math.round(((px - m.left) / pw) * (n - 1));
    };
    overlay.addEventListener('pointermove', (e) => showAt(fromEvent(e)));
    overlay.addEventListener('pointerdown', (e) => showAt(fromEvent(e)));
    overlay.addEventListener('pointerleave', hide);
    overlay.addEventListener('focus', () => showAt(idx < 0 ? n - 1 : idx));
    overlay.addEventListener('blur', hide);
    overlay.addEventListener('keydown', (e) => {
      if (e.key === 'ArrowLeft') { e.preventDefault(); showAt(idx - 1); }
      if (e.key === 'ArrowRight') { e.preventDefault(); showAt(idx + 1); }
    });
    svg.append(overlay);
    mount(container, svg, container.querySelector('.tooltip'));
  });
  return container;
}

/**
 * Donut for part-to-whole (≤ 6 segments). segments: [{ key, label, value, cls }]
 * with positive values, drawn in the given order (keep it the palette order so
 * that neighbouring colours stay distinguishable). onActive(key|null) lets a
 * legend highlight the same entry.
 */
export function donutChart({ segments, centerValue, centerCaption, format, ariaLabel, onActive }) {
  const container = h('div.chart');
  const tip = tooltip(container);
  const arcs = new Map();

  responsive(container, (width) => {
    const size = Math.min(width, 240);
    const cx = width / 2;
    const cy = size / 2;
    const r = size / 2 - 4;
    const inner = r * 0.62;
    const total = segments.reduce((a, s) => a + s.value, 0);
    const svg = svgEl(width, size, ariaLabel);
    arcs.clear();

    let angle = -Math.PI / 2;
    for (const s of segments) {
      const sweep = total > 0 ? (s.value / total) * Math.PI * 2 : 0;
      if (sweep <= 0) continue;
      const a0 = angle;
      const a1 = angle + Math.min(sweep, Math.PI * 2 - 0.0001);
      angle += sweep;
      const large = a1 - a0 > Math.PI ? 1 : 0;
      const p = (rad, a) => `${(cx + rad * Math.cos(a)).toFixed(2)},${(cy + rad * Math.sin(a)).toFixed(2)}`;
      const d = `M${p(r, a0)}A${r},${r} 0 ${large} 1 ${p(r, a1)}L${p(inner, a1)}A${inner},${inner} 0 ${large} 0 ${p(inner, a0)}Z`;
      const mid = (a0 + a1) / 2;
      const arc = h('path', {
        class: `seg-arc ${s.cls}`, d, tabindex: 0, role: 'img',
        'aria-label': `${s.label}: ${format(s.value)} (${Math.round((s.value / total) * 100)} %)`,
      });
      const show = () => {
        setActive(s.key);
        onActive?.(s.key);
        tip.show(cx + (r * 0.8) * Math.cos(mid), cy + (r * 0.8) * Math.sin(mid), [
          h('div.tt-title', s.label),
          h('div.tt-row', h('strong', format(s.value)), h('span.tt-name', `${Math.round((s.value / total) * 100)} %`)),
        ]);
      };
      const hide = () => { setActive(null); onActive?.(null); tip.hide(); };
      arc.addEventListener('pointerenter', show);
      arc.addEventListener('pointerdown', show);
      arc.addEventListener('pointerleave', hide);
      arc.addEventListener('focus', show);
      arc.addEventListener('blur', hide);
      arcs.set(s.key, arc);
      svg.append(arc);
    }
    svg.append(h('text', { class: 'donut-total', x: cx, y: cy + 2, 'text-anchor': 'middle' }, centerValue));
    svg.append(h('text', { class: 'donut-caption', x: cx, y: cy + 20, 'text-anchor': 'middle' }, centerCaption));
    mount(container, svg, container.querySelector('.tooltip'));
  });

  function setActive(key) {
    container.classList.toggle('has-active', key != null);
    arcs.forEach((arc, k) => arc.classList.toggle('active', k === key));
  }
  container.setActive = setActive;
  return container;
}

/**
 * Card toolbar button that swaps a chart for its data table (the accessible
 * twin). rows: () => [[cells...]], headers: [...].
 */
export function tableToggle(chartHolder, headers, rows) {
  let showing = false;
  const table = h('div', { hidden: true });
  const btn = h('button.btn.btn-ghost.table-toggle', {
    type: 'button', 'aria-pressed': 'false',
    onclick: () => {
      showing = !showing;
      btn.setAttribute('aria-pressed', String(showing));
      btn.textContent = showing ? 'Gràfic' : 'Taula';
      if (showing) {
        mount(table, h('table.data-table',
          h('thead', h('tr', headers.map((t) => h('th', { scope: 'col' }, t)))),
          h('tbody', rows().map((r) => h('tr', r.map((c) => h('td', c)))))));
      }
      table.hidden = !showing;
      chartHolder.hidden = showing;
    },
  }, 'Taula');
  return { button: btn, table };
}
