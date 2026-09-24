// History-API router with per-entry scroll restoration. Views are async
// functions receiving a context; a newer navigation aborts the older one.

import { handleBack, closeAllSheets } from './ui.js';

const routes = [];
let current = null; // { controller, key, path }
let previousPath = null;
let beforeRender = () => {};
let fallback = null;
const scrollPositions = new Map();

/** route('/obres/:id', view) */
export function route(pattern, view, meta = {}) {
  const keys = [];
  const re = new RegExp('^' + pattern.replace(/\/:([a-z]+)/gi, (_, k) => { keys.push(k); return '/([^/]+)'; }) + '/?$');
  routes.push({ re, keys, view, meta });
}

export function notFound(view) { fallback = view; }

/** The in-app path shown before the current one (for bug reports). */
export const getPreviousPath = () => previousPath;
export function onBeforeRender(fn) { beforeRender = fn; }

function entryKey() {
  if (!history.state?.key) {
    history.replaceState({ ...(history.state || {}), key: Math.random().toString(36).slice(2) }, '');
  }
  return history.state.key;
}

export function navigate(url, { replace = false } = {}) {
  if (current) scrollPositions.set(current.key, window.scrollY);
  const state = { key: Math.random().toString(36).slice(2) };
  if (replace) history.replaceState(state, '', url);
  else history.pushState(state, '', url);
  render();
}

/** Re-renders the current route (e.g. after data changed). */
export function refresh() { render({ keepScroll: true }); }

function onLinkClick(e) {
  const a = e.target.closest('a[href]');
  if (!a || e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
  if (a.target || a.hasAttribute('download') || a.dataset.external !== undefined) return;
  const url = new URL(a.href, location.href);
  if (url.origin !== location.origin || url.pathname.startsWith('/api/')) return;
  e.preventDefault();
  if (url.pathname + url.search === location.pathname + location.search) return;
  navigate(url.pathname + url.search);
}

function onPopState() {
  if (handleBack()) return; // the back button closed a sheet
  render({ restoreScroll: true });
}

async function render({ restoreScroll = false, keepScroll = false } = {}) {
  closeAllSheets();
  if (current) {
    current.controller.abort();
    if (!restoreScroll && !keepScroll) scrollPositions.set(current.key, window.scrollY);
  }
  const key = entryKey();
  const controller = new AbortController();
  const here = location.pathname + location.search;
  if (current && current.path !== here) previousPath = current.path;
  current = { controller, key, path: here };

  const path = location.pathname;
  let match = null;
  for (const r of routes) {
    const m = path.match(r.re);
    if (m) {
      match = { route: r, params: Object.fromEntries(r.keys.map((k, i) => [k, decodeURIComponent(m[i + 1])])) };
      break;
    }
  }
  const view = match?.route.view || fallback;
  const ctx = {
    path,
    params: match?.params || {},
    query: new URLSearchParams(location.search),
    meta: match?.route.meta || {},
    signal: controller.signal,
    alive: () => !controller.signal.aborted,
  };
  const container = beforeRender(ctx);
  ctx.main = container;

  // Views call ctx.ready() as soon as real content (cached or fresh) is on
  // screen, so the scroll position can be restored before slow requests end.
  const savedY = keepScroll ? window.scrollY : (restoreScroll ? scrollPositions.get(key) : 0);
  let readyDone = false;
  ctx.ready = () => {
    if (readyDone || !ctx.alive()) return;
    readyDone = true;
    window.scrollTo(0, savedY || 0);
  };
  try {
    await view(ctx);
  } catch (err) {
    if (err?.name !== 'AbortError') throw err;
  }
  ctx.ready();
}

export function start() {
  if ('scrollRestoration' in history) history.scrollRestoration = 'manual';
  document.addEventListener('click', onLinkClick);
  window.addEventListener('popstate', onPopState);
  render();
}
