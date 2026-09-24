// Minimal DOM helpers. Everything is built with createElement and
// textContent, never innerHTML with data, so user text can't inject markup,
// and styles go through the CSSOM so the strict CSP holds.

const SVG_NS = 'http://www.w3.org/2000/svg';
const SVG_TAGS = new Set(['svg', 'g', 'path', 'rect', 'circle', 'line', 'polyline', 'text', 'title', 'defs', 'clipPath']);
const PROPS = new Set(['value', 'checked', 'disabled', 'selected', 'hidden', 'indeterminate', 'multiple', 'readOnly', 'required']);

/**
 * h('button.btn.btn-primary', { onclick, type: 'submit' }, 'Guardar')
 * Children may be strings, numbers, nodes, arrays, or null/false (skipped).
 */
export function h(tag, props, ...children) {
  let [name, ...classes] = tag.split('.');
  let id = null;
  if (name.includes('#')) [name, id] = name.split('#');
  name = name || 'div';
  const isSvg = SVG_TAGS.has(name);
  const el = isSvg ? document.createElementNS(SVG_NS, name) : document.createElement(name);
  if (id) el.id = id;
  if (classes.length) el.setAttribute('class', classes.join(' '));

  if (props != null && (typeof props !== 'object' || props instanceof Node || Array.isArray(props))) {
    children.unshift(props);
    props = null;
  }
  for (const [key, val] of Object.entries(props || {})) {
    if (val == null || val === false) continue;
    if (key === 'class') {
      const cur = el.getAttribute('class');
      el.setAttribute('class', cur ? `${cur} ${val}` : val);
    } else if (key.startsWith('on') && typeof val === 'function') {
      el.addEventListener(key.slice(2).toLowerCase(), val);
    } else if (key === 'style' && typeof val === 'object') {
      setStyle(el, val);
    } else if (key === 'dataset') {
      Object.assign(el.dataset, val);
    } else if (!isSvg && PROPS.has(key)) {
      el[key] = val;
    } else {
      el.setAttribute(key, val === true ? '' : String(val));
    }
  }
  append(el, children);
  return el;
}

/** Sets inline styles through the CSSOM (allowed by the CSP). */
export function setStyle(el, styles) {
  for (const [k, v] of Object.entries(styles)) {
    if (k.startsWith('--')) el.style.setProperty(k, v);
    else el.style[k] = v;
  }
}

export function append(el, children) {
  for (const child of children.flat(Infinity)) {
    if (child == null || child === false || child === true) continue;
    el.append(child instanceof Node ? child : document.createTextNode(String(child)));
  }
  return el;
}

/** Replaces all the children of el. */
export function mount(el, ...children) {
  el.replaceChildren();
  return append(el, children);
}

// ---------------------------------------------------------------- icons
// 24×24 stroke icons drawn for Obrabi. A string is a path; an object is
// another SVG shape.
const ICONS = {
  home: ['M3 10.5 12 3l9 7.5', 'M5.5 9v11a1 1 0 0 0 1 1H10v-6h4v6h3.5a1 1 0 0 0 1-1V9'],
  bricks: ['M3 5h18v14H3z', 'M3 9.7h18', 'M3 14.3h18', 'M9 5v4.7', 'M15 5v4.7', 'M6 9.7v4.6', 'M12 9.7v4.6', 'M18 9.7v4.6', 'M9 14.3V19', 'M15 14.3V19'],
  archive: [{ rect: [3, 4, 18, 4, 1] }, 'M5 8v11a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1V8', 'M10 12h4'],
  menu: ['M4 7h16', 'M4 12h16', 'M4 17h16'],
  more: [{ circle: [5, 12, 1.4] }, { circle: [12, 12, 1.4] }, { circle: [19, 12, 1.4] }],
  plus: ['M12 5v14', 'M5 12h14'],
  search: [{ circle: [11, 11, 7] }, 'm20 20-4.2-4.2'],
  back: ['m15 18-6-6 6-6'],
  chevronRight: ['m9 18 6-6-6-6'],
  chevronDown: ['m6 9 6 6 6-6'],
  close: ['M18 6 6 18', 'M6 6l12 12'],
  check: ['M20 6 9 17l-5-5'],
  edit: ['M16.5 3.5a2.1 2.1 0 0 1 3 3L8 18l-4 1 1-4z', 'M14.5 5.5l3 3'],
  trash: ['M3 6h18', 'M8 6V4h8v2', 'M6 6l1 14a1 1 0 0 0 1 1h8a1 1 0 0 0 1-1l1-14', 'M10 11v6', 'M14 11v6'],
  file: ['M14 3H6a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V9z', 'M14 3v6h6', 'M8 13h8', 'M8 17h5'],
  share: ['M4 12v7a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-7', 'm16 6-4-4-4 4', 'M12 2v14'],
  download: ['M12 3v12', 'm7 10 5 5 5-5', 'M5 21h14'],
  pin: ['M12 21s-7-6.2-7-11.5a7 7 0 0 1 14 0C19 14.8 12 21 12 21z', { circle: [12, 9.5, 2.5] }],
  tag: ['M3 12V4a1 1 0 0 1 1-1h8l9 9-9 9z', { circle: [7.5, 7.5, 1.5] }],
  user: [{ circle: [12, 8, 4] }, 'M4 21a8 8 0 0 1 16 0'],
  logout: ['M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4', 'm16 17 5-5-5-5', 'M21 12H9'],
  message: ['M21 12a8 8 0 0 1-11.6 7.1L3 21l1.9-6.4A8 8 0 1 1 21 12z'],
  clock: [{ circle: [12, 12, 9] }, 'M12 7v5l3 2'],
  wallet: ['M19 7V5a1 1 0 0 0-1-1H5a2 2 0 0 0 0 4h15a1 1 0 0 1 1 1v10a1 1 0 0 1-1 1H5a2 2 0 0 1-2-2V6', { circle: [16.5, 13.5, 1.3] }],
  box: ['M21 8 12 3 3 8v8l9 5 9-5z', 'M3 8l9 5 9-5', 'M12 13v8'],
  helmet: ['M2 18h20', 'M4 18v-3a8 8 0 0 1 16 0v3', 'M10 7V4h4v3'],
  phone: ['M5 4h3.5l2 5-2.3 1.4a11 11 0 0 0 5.4 5.4L15 13.5l5 2V19a2 2 0 0 1-2 2A16 16 0 0 1 3 6a2 2 0 0 1 2-2'],
  info: [{ circle: [12, 12, 9] }, 'M12 16v-5', 'M12 8h.01'],
  alert: ['M10.3 3.9 1.8 18a2 2 0 0 0 1.7 3h17a2 2 0 0 0 1.7-3L13.7 3.9a2 2 0 0 0-3.4 0z', 'M12 9v4', 'M12 17h.01'],
  up: ['M7 17 17 7', 'M8 7h9v9'],
  down: ['M7 7l10 10', 'M17 8v9H8'],
  flat: ['M5 12h14'],
  eye: ['M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12z', { circle: [12, 12, 3] }],
  eyeOff: ['M3 3l18 18', 'M10.6 5.1A10 10 0 0 1 12 5c6.5 0 10 7 10 7a17 17 0 0 1-2.2 3.2', 'M6.6 6.6C3.9 8.4 2 12 2 12s3.5 7 10 7a9.7 9.7 0 0 0 5.4-1.6', 'M9.9 9.9a3 3 0 0 0 4.2 4.2'],
  calendar: [{ rect: [3, 5, 18, 16, 2] }, 'M3 10h18', 'M8 3v4', 'M16 3v4'],
  lock: [{ rect: [4, 11, 16, 10, 2] }, 'M8 11V7a4 4 0 0 1 8 0v4'],
  phoneApp: [{ rect: [6, 2, 12, 20, 2] }, 'M11 18h2'],
  refresh: ['M21 12a9 9 0 1 1-2.6-6.4', 'M21 4v5h-5'],
  undo: ['M9 14 4 9l5-5', 'M4 9h11a5 5 0 0 1 0 10h-3'],
  table: [{ rect: [3, 4, 18, 16, 2] }, 'M3 10h18', 'M3 15h18', 'M10 4v16'],
  chart: ['M4 20V10', 'M10 20V4', 'M16 20v-7', 'M22 20H2'],
};

export function icon(name, extraClass = '') {
  const svg = document.createElementNS(SVG_NS, 'svg');
  svg.setAttribute('viewBox', '0 0 24 24');
  svg.setAttribute('class', `icon ${extraClass}`.trim());
  svg.setAttribute('aria-hidden', 'true');
  svg.setAttribute('focusable', 'false');
  for (const part of ICONS[name] || []) {
    let el;
    if (typeof part === 'string') {
      el = document.createElementNS(SVG_NS, 'path');
      el.setAttribute('d', part);
    } else if (part.circle) {
      el = document.createElementNS(SVG_NS, 'circle');
      const [cx, cy, r] = part.circle;
      el.setAttribute('cx', cx); el.setAttribute('cy', cy); el.setAttribute('r', r);
    } else if (part.rect) {
      el = document.createElementNS(SVG_NS, 'rect');
      const [x, y, w, hgt, rx] = part.rect;
      el.setAttribute('x', x); el.setAttribute('y', y); el.setAttribute('width', w); el.setAttribute('height', hgt);
      if (rx) el.setAttribute('rx', rx);
    }
    svg.append(el);
  }
  return svg;
}

/** Debounce for input handlers. */
export function debounce(fn, ms = 200) {
  let t;
  return (...args) => {
    clearTimeout(t);
    t = setTimeout(() => fn(...args), ms);
  };
}
