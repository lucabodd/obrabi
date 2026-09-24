// Reports unexpected errors of the web app to the feedback service, which
// e-mails a bug report to the maintainer. Never throws, never blocks the UI.

const seen = new Set();
let sent = 0;
const MAX_PER_SESSION = 10;

export const appVersion = document.querySelector('meta[name="obrabi-version"]')?.content || 'dev';

export function report(event) {
  try {
    const key = `${event.type}|${event.message}|${event.script || ''}|${event.line || ''}`;
    if (seen.has(key) || sent >= MAX_PER_SESSION) return;
    seen.add(key);
    sent++;
    const body = JSON.stringify({
      ...event,
      message: String(event.message || 'Error desconegut').slice(0, 2000),
      stack: String(event.stack || '').slice(0, 8000),
      page: location.pathname + location.search,
      viewport: `${window.innerWidth}x${window.innerHeight}`,
      app_version: appVersion,
    });
    fetch('/api/telemetry', {
      method: 'POST',
      credentials: 'same-origin',
      keepalive: true,
      headers: { 'Content-Type': 'application/json', 'X-Obrabi': '1' },
      body,
    }).catch(() => {});
  } catch {
    // reporting must never break anything
  }
}

export function installErrorHandlers() {
  window.addEventListener('error', (e) => {
    // Resource load errors (img/script) have no message; ignore extensions.
    if (!e.message || (e.filename && !e.filename.startsWith(location.origin))) return;
    report({
      type: 'error',
      message: e.message,
      stack: e.error?.stack,
      script: e.filename,
      line: e.lineno,
      column: e.colno,
    });
  });
  window.addEventListener('unhandledrejection', (e) => {
    const r = e.reason;
    if (r?.name === 'AbortError') return;
    // Expected API failures (validation, offline…) are shown to the user,
    // and server errors are already reported by the backend.
    if (r?.constructor?.name === 'ApiError') return;
    report({ type: 'unhandledrejection', message: r?.message || String(r), stack: r?.stack });
  });
}
