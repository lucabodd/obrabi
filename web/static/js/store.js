// App-wide state and small per-device preferences. localStorage is only a
// convenience here (remembered filters, recently opened projects): every read
// and write is guarded so a private window or blocked storage never breaks
// the app.

export const state = {
  user: null, // { id, username, display_name }
};

const PREFIX = 'obrabi.';

export function getPref(key, fallback = null) {
  try {
    const raw = localStorage.getItem(PREFIX + key);
    return raw == null ? fallback : JSON.parse(raw);
  } catch {
    return fallback;
  }
}

export function setPref(key, value) {
  try {
    if (value == null) localStorage.removeItem(PREFIX + key);
    else localStorage.setItem(PREFIX + key, JSON.stringify(value));
  } catch {
    // storage unavailable: preferences simply are not remembered
  }
}

/** Remembers a project as recently opened (most recent first, max 6). */
export function pushRecent(project) {
  const list = getPref('recent', []).filter((r) => r.id !== project.id);
  list.unshift({ id: project.id, name: project.name, town: project.town?.name || '' });
  setPref('recent', list.slice(0, 6));
}

export function forgetRecent(id) {
  setPref('recent', getPref('recent', []).filter((r) => r.id !== id));
}

export const recents = () => getPref('recent', []);

/** Remembers the last hourly rate used for labour, in cents. */
export const lastRate = () => getPref('labor.rate', null);
export const setLastRate = (cents) => setPref('labor.rate', cents);
