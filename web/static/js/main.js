// Entry point: app shell (sidebar on desktop, tab bar on phones), routes and
// session bootstrap.

import { h, icon, mount } from './dom.js';
import { route, notFound, onBeforeRender, start, navigate } from './router.js';
import { get, onUnauthorized, clearCache } from './api.js';
import { installErrorHandlers } from './telemetry.js';
import { state } from './store.js';
import { loginView } from './views/login.js';
import { dashboardView } from './views/dashboard.js';
import { projectsView } from './views/projects.js';
import { projectView } from './views/project.js';
import { townsView } from './views/towns.js';
import { categoriesView } from './views/categories.js';
import { feedbackView } from './views/feedback.js';
import { accountView, logout } from './views/account.js';
import { moreView } from './views/more.js';
import { emptyState } from './ui.js';

installErrorHandlers();

const app = document.getElementById('app');

const TABS = [
  { href: '/', label: 'Inici', icon: 'home', active: (p) => p === '/' },
  { href: '/obres', label: 'Obres', icon: 'bricks', active: (p) => p.startsWith('/obres') },
  { href: '/arxiu', label: 'Arxiu', icon: 'archive', active: (p) => p.startsWith('/arxiu') },
  { href: '/mes', label: 'Més', icon: 'menu', active: (p) => ['/mes', '/pobles', '/categories', '/suggeriments', '/compte'].some((x) => p.startsWith(x)) },
];
const SIDE_EXTRA = [
  { href: '/pobles', label: 'Pobles', icon: 'pin' },
  { href: '/categories', label: 'Categories', icon: 'tag' },
  { href: '/suggeriments', label: 'Suggeriments i errors', icon: 'message' },
  { href: '/compte', label: 'Compte', icon: 'user' },
];

let shell = null;

function buildShell() {
  const sideLinks = [...TABS.filter((t) => t.href !== '/mes'), ...SIDE_EXTRA].map((t) =>
    h('a.nav-link', { href: t.href, dataset: { href: t.href } }, icon(t.icon), t.label));
  const tabLinks = TABS.map((t) => h('a.tab', { href: t.href, dataset: { href: t.href } }, icon(t.icon), h('span', t.label)));
  const main = h('main.main', { id: 'main', tabindex: '-1' });
  const offline = h('div.offline-banner', { role: 'status', hidden: navigator.onLine }, "Sense connexió: els canvis no es podran guardar fins que tornes a tindre cobertura.");
  window.addEventListener('online', () => { offline.hidden = true; });
  window.addEventListener('offline', () => { offline.hidden = false; });

  const root = h('div.shell',
    h('nav.sidebar', { 'aria-label': 'Navegació principal' },
      h('a.brand', { href: '/' }, h('span.brand-mark', { 'aria-hidden': 'true' }), 'Obrabi'),
      sideLinks,
      h('div.spacer'),
      h('div.who', state.user ? `Sessió de ${state.user.display_name || state.user.username}` : ''),
      h('button.nav-link', { type: 'button', onclick: logout, class: 'btn-reset' }, icon('logout'), 'Eixir')),
    h('div', offline, main),
    h('nav.tabbar', { 'aria-label': 'Navegació' }, tabLinks));

  const update = (path) => {
    const isActive = (href) => {
      const tab = TABS.find((t) => t.href === href);
      if (tab) return tab.active(path);
      return path.startsWith(href);
    };
    for (const a of [...sideLinks, ...tabLinks]) {
      if (isActive(a.dataset.href)) a.setAttribute('aria-current', 'page');
      else a.removeAttribute('aria-current');
    }
  };
  mount(app, root);
  app.classList.remove('boot');
  return { root, main, update };
}

onBeforeRender((ctx) => {
  if (ctx.meta.public) {
    shell = null;
    app.classList.remove('boot');
    const main = h('main.login');
    mount(app, main);
    document.title = 'Obrabi';
    return main;
  }
  if (!shell) shell = buildShell();
  shell.update(ctx.path);
  shell.main.replaceChildren();
  return shell.main;
});

/** Wraps views that need a session. */
const auth = (view) => async (ctx) => {
  if (!state.user) {
    navigate(`/login?next=${encodeURIComponent(location.pathname + location.search)}`, { replace: true });
    return;
  }
  document.title = ctx.meta.title ? `${ctx.meta.title} · Obrabi` : 'Obrabi';
  await view(ctx);
};

route('/login', loginView, { public: true });
route('/', auth(dashboardView), { title: 'Inici' });
route('/obres', auth(projectsView('active')), { title: 'Obres' });
route('/obres/:id', auth(projectView), { title: 'Obra' });
route('/arxiu', auth(projectsView('finished')), { title: 'Arxiu' });
route('/pobles', auth(townsView), { title: 'Pobles' });
route('/categories', auth(categoriesView), { title: 'Categories' });
route('/suggeriments', auth(feedbackView), { title: 'Suggeriments i errors' });
route('/compte', auth(accountView), { title: 'Compte' });
route('/mes', auth(moreView), { title: 'Més' });
notFound(auth(async (ctx) => {
  mount(ctx.main, emptyState({
    iconName: 'alert',
    title: 'Esta pàgina no existix',
    text: 'Pot ser que l\'enllaç estiga mal copiat.',
    action: h('a.btn.btn-primary', { href: '/' }, 'Anar a l\'inici'),
  }));
}));

onUnauthorized(() => {
  if (!state.user) return;
  state.user = null;
  shell = null;
  clearCache();
  navigate(`/login?next=${encodeURIComponent(location.pathname + location.search)}`, { replace: true });
});

async function boot() {
  try {
    const res = await get('/auth/me');
    state.user = res.user;
  } catch {
    state.user = null; // 401 or offline: the login page takes over
  }
  start();
  if ('serviceWorker' in navigator) {
    navigator.serviceWorker.register('/sw.js').catch(() => {});
  }
}

boot();
