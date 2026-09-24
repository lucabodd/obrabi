// Lists of obres grouped by poble: /obres (in progress) and /arxiu (finished).
// Built for finding a job fast: instant accent-insensitive search, recently
// opened jobs on top, collapsible towns.

import { h, icon, mount, debounce } from '../dom.js';
import { cached, load } from '../api.js';
import { navigate } from '../router.js';
import { emptyState, skeleton, pendingPill, chipGroup, toastError } from '../ui.js';
import { money, moneySigned, fold, byName, plural, dateShort, isoDate } from '../format.js';
import { getPref, setPref, recents } from '../store.js';
import { openProjectForm } from './forms.js';

export function projectsView(status) {
  const archive = status === 'finished';

  return async (ctx) => {
    const path = `/projects?status=${status}`;
    let projects = cached(path)?.projects || null;
    let query = ctx.query.get('q') || '';
    let filter = ctx.query.get('f') === 'pending' ? 'pending' : 'all';
    const collapsedKey = `collapsed.${status}`;
    const collapsed = new Set(getPref(collapsedKey, []));

    const count = h('p.subtitle');
    const searchInput = h('input', {
      type: 'search', value: query, placeholder: 'Busca per obra, poble o client', 'aria-label': 'Buscar obres',
      autocomplete: 'off', autocapitalize: 'none', enterkeyhint: 'search',
    });
    const clear = h('button.icon-btn', { type: 'button', 'aria-label': 'Esborrar la cerca', hidden: !query }, icon('close'));
    const filters = chipGroup([
      { value: 'all', label: 'Totes' },
      { value: 'pending', label: 'Amb pendent de cobrar', icon: 'clock' },
    ], filter, (v) => {
      filter = v;
      filters.set(v);
      syncUrl();
      renderList();
    }, { label: 'Filtre' });
    const listBox = h('div');

    const syncUrl = () => {
      const params = new URLSearchParams();
      if (query) params.set('q', query);
      if (filter === 'pending') params.set('f', 'pending');
      const url = location.pathname + (params.toString() ? `?${params}` : '');
      history.replaceState(history.state, '', url);
    };
    const onSearch = debounce(() => { syncUrl(); }, 400);
    searchInput.addEventListener('input', () => {
      query = searchInput.value;
      clear.hidden = !query;
      renderList();
      onSearch();
    });
    clear.addEventListener('click', () => {
      searchInput.value = '';
      query = '';
      clear.hidden = true;
      syncUrl();
      renderList();
      searchInput.focus();
    });

    mount(ctx.main,
      h('header.topbar', h('div.title', h('h1', archive ? 'Arxiu' : 'Obres'), count)),
      h('div.searchbar', h('label.search', icon('search'), searchInput, clear)),
      h('div', { style: { marginBottom: '12px' } }, filters),
      listBox,
      archive ? null : h('div.fab-spacer', { 'aria-hidden': 'true' }),
      archive ? null : h('button.fab', { type: 'button', onclick: newProject }, icon('plus'), 'Nova obra'));

    function renderList() {
      if (!projects) {
        mount(listBox, h('div.stack', skeleton('block', 4)));
        return;
      }
      const towns = new Set(projects.map((p) => p.town.id));
      count.textContent = projects.length
        ? `${plural(projects.length, archive ? 'obra finalitzada' : 'obra en curs', archive ? 'obres finalitzades' : 'obres en curs')} · ${plural(towns.size, 'poble', 'pobles')}`
        : '';

      if (!projects.length) {
        mount(listBox, archive
          ? emptyState({ iconName: 'archive', title: "L'arxiu està buit", text: 'Quan finalitzes una obra, la trobaràs ací.' })
          : emptyState({
            iconName: 'bricks', title: 'Encara no tens obres en curs',
            text: 'Crea la primera obra i afegix-li les despeses, la mà d\'obra i els cobraments.',
            action: h('button.btn.btn-primary', { type: 'button', onclick: newProject }, icon('plus'), 'Nova obra'),
          }));
        return;
      }

      const q = fold(query);
      let shown = projects.filter((p) => !q || fold(`${p.name} ${p.town.name} ${p.client_name} ${p.address}`).includes(q));
      if (filter === 'pending') shown = shown.filter((p) => p.totals.pending_cents > 0);

      if (!shown.length) {
        mount(listBox, emptyState({
          iconName: 'search', title: 'Cap obra coincidix',
          text: q ? `No hi ha cap obra amb «${query.trim()}».` : 'No hi ha obres amb diners pendents de cobrar.',
        }));
        return;
      }

      const parts = [];
      // Recently opened (only when not searching): one tap back to them.
      if (!q && filter === 'all' && !archive) {
        const ids = new Set(shown.map((p) => p.id));
        const rec = recents().filter((r) => ids.has(r.id)).slice(0, 4);
        if (rec.length >= 2) {
          parts.push(h('div.group',
            h('div.group-head', { role: 'heading', 'aria-level': '2' }, icon('clock'), 'Recents'),
            h('div.chips.scroll', rec.map((r) => h('a.chip', { href: `/obres/${r.id}` }, h('span.truncate', r.name), h('span.muted', r.town))))));
        }
      }

      // Group by town, towns in alphabetical order.
      const groups = new Map();
      for (const p of shown) {
        if (!groups.has(p.town.id)) groups.set(p.town.id, { town: p.town, items: [] });
        groups.get(p.town.id).items.push(p);
      }
      const ordered = [...groups.values()].sort((a, b) => byName(a.town.name, b.town.name));
      for (const g of ordered) {
        const isCollapsed = !q && collapsed.has(g.town.id);
        const group = h('section.group', { dataset: { collapsed: String(isCollapsed) } });
        const head = h('button.group-head', {
          type: 'button', 'aria-expanded': String(!isCollapsed),
          onclick: () => {
            const now = group.dataset.collapsed !== 'true';
            group.dataset.collapsed = String(now);
            head.setAttribute('aria-expanded', String(!now));
            if (now) collapsed.add(g.town.id); else collapsed.delete(g.town.id);
            setPref(collapsedKey, [...collapsed]);
          },
        }, icon('pin'), h('span', g.town.name), h('span.count', String(g.items.length)), icon('chevronDown', 'chev'));
        // A per-town total only adds information when several jobs owe money.
        const owing = g.items.filter((p) => p.totals.pending_cents > 0);
        if (owing.length > 1) {
          const sum = owing.reduce((a, p) => a + p.totals.pending_cents, 0);
          head.insertBefore(h('span.pend.num', { title: 'Pendent de cobrar en este poble' }, money(sum)), head.lastChild);
        }
        group.append(head, h('div.list', g.items.map(row)));
        parts.push(group);
      }
      mount(listBox, parts);
    }

    function row(p) {
      const t = p.totals;
      const sub = [
        p.client_name || null,
        archive && p.finished_at ? `finalitzada ${dateShort(isoDate(new Date(p.finished_at)))}` : null,
        t.price_cents > 0 ? `benefici ${moneySigned(t.benefit_cents)}` : plural(t.items, 'partida', 'partides'),
      ].filter(Boolean).join(' · ');
      return h('a.list-row', { href: `/obres/${p.id}` },
        h('span.main-col',
          h('div.t1.wrap2', p.name),
          h('div.t2', sub)),
        h('span.end', pendingPill(t, { short: true })),
        icon('chevronRight', 'chev'));
    }

    async function newProject() {
      const saved = await openProjectForm();
      if (saved) navigate(`/obres/${saved.id}`);
    }

    renderList();
    if (projects) ctx.ready();
    try {
      projects = (await load(path, { signal: ctx.signal })).projects;
      if (ctx.alive()) renderList();
    } catch (err) {
      if (err.name === 'AbortError' || !ctx.alive()) return;
      if (!projects) {
        mount(listBox, emptyState({ iconName: 'alert', title: "No s'han pogut carregar les obres", text: err.message }));
      } else {
        toastError(err);
      }
    }
    if (ctx.query.get('nova') === '1' && !archive) {
      history.replaceState(history.state, '', location.pathname);
      newProject();
    }
  };
}
