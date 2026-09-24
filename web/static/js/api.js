// Thin client for the gateway API with a small in-memory cache, so going back
// to a screen shows the last data instantly while it refreshes.

export class ApiError extends Error {
  constructor(status, code, message) {
    super(message);
    this.status = status;
    this.code = code;
  }
}

const MESSAGES = {
  0: "No hi ha connexió. Comprova la cobertura i torna-ho a provar.",
  401: 'Cal iniciar la sessió.',
  403: 'Operació no permesa.',
  404: "No s'ha trobat.",
  429: 'Massa peticions seguides. Espera un moment.',
  500: "Alguna cosa ha anat malament. S'ha enviat un avís automàtic per a arreglar-ho.",
  502: "Ara mateix no es pot connectar amb el servidor. Torna-ho a provar d'ací a un moment.",
};

let unauthorizedHandler = () => {};
export function onUnauthorized(fn) { unauthorizedHandler = fn; }

const cache = new Map();

export async function request(method, path, body, { signal } = {}) {
  const headers = { Accept: 'application/json', 'X-Obrabi': '1' };
  const init = { method, headers, credentials: 'same-origin', signal };
  if (body !== undefined) {
    headers['Content-Type'] = 'application/json';
    init.body = JSON.stringify(body);
  }
  let res;
  try {
    res = await fetch('/api' + path, init);
  } catch (err) {
    if (err.name === 'AbortError') throw err;
    throw new ApiError(0, 'network', MESSAGES[0]);
  }

  let data = null;
  if (res.status !== 204 && (res.headers.get('Content-Type') || '').includes('application/json')) {
    try {
      data = await res.json();
    } catch {
      data = null;
    }
  }
  if (!res.ok) {
    if (res.status === 401 && path !== '/auth/login') unauthorizedHandler();
    const fallback = MESSAGES[res.status] || MESSAGES[res.status >= 500 ? 500 : 403];
    throw new ApiError(res.status, data?.error || `http_${res.status}`, data?.message || fallback);
  }
  if (method !== 'GET') cache.clear(); // any change may affect any list or total
  return data;
}

export const get = (path, opts) => request('GET', path, undefined, opts);
export const post = (path, body, opts) => request('POST', path, body ?? {}, opts);
export const put = (path, body, opts) => request('PUT', path, body, opts);
export const del = (path, opts) => request('DELETE', path, undefined, opts);

/** Last cached response for a GET path, if any. */
export const cached = (path) => cache.get(path);

/** GET and remember the response. */
export async function load(path, opts) {
  const data = await get(path, opts);
  cache.set(path, data);
  return data;
}

export function clearCache() { cache.clear(); }
