// Home page: the month's benefit first, then quick access to the jobs in
// progress, this month against the last one, and the analysis charts
// (monthly series, split by category, year-over-year).

import { h, icon, mount } from '../dom.js';
import { cached, load } from '../api.js';
import { state, getPref, setPref } from '../store.js';
import { columnChart, lineChart, donutChart, tableToggle } from '../charts.js';
import {
  money, moneyRound, compact, percent, monthName, deMonth, MONTHS, MONTHS_SHORT,
  addMonths, thisMonth, plural, hours as fmtHours,
} from '../format.js';
import { segmented, skeleton, emptyState, pendingPill, toastError } from '../ui.js';

const PERIODS = [
  { value: '6m', label: '6 mesos' },
  { value: '12m', label: '1 any' },
  { value: '24m', label: '2 anys' },
  { value: 'ytd', label: 'Enguany' },
  { value: 'custom', label: 'Triar' },
];
const METRICS = [
  { value: 'benefit', label: 'Benefici', key: 'benefit_cents' },
  { value: 'revenue', label: 'Facturació', key: 'revenue_cents' },
];
const WEEKDAYS = ['diumenge', 'dilluns', 'dimarts', 'dimecres', 'dijous', 'divendres', 'dissabte'];

const cap = (s) => s.charAt(0).toUpperCase() + s.slice(1);

function periodRange(period, custom) {
  const to = thisMonth();
  switch (period) {
    case '6m': return { from: addMonths(to, -5), to };
    case '24m': return { from: addMonths(to, -23), to };
    case 'ytd': return { from: `${to.slice(0, 4)}-01`, to };
    case 'custom':
      if (custom?.from && custom?.to && custom.from <= custom.to) return custom;
      return { from: addMonths(to, -11), to };
    default: return { from: addMonths(to, -11), to };
  }
}

function periodLabel(period, range) {
  switch (period) {
    case '6m': return 'Últims 6 mesos';
    case '12m': return 'Últims 12 mesos';
    case '24m': return 'Últims 24 mesos';
    case 'ytd': return `Enguany, ${range.to.slice(0, 4)}`;
    default: return `De ${monthName(range.from, true)} a ${monthName(range.to, true)}`;
  }
}

/** Share of a total as a percentage, never showing a misleading "0 %". */
function share(ratio) {
  return ratio > 0 && ratio < 0.005 ? '<1 %' : percent(ratio);
}

function longToday() {
  const d = new Date();
  return cap(`${WEEKDAYS[d.getDay()]}, ${d.getDate()} ${deMonth(thisMonth())}`);
}

/** Direction and text of a change between two amounts. */
function change(cur, prev) {
  if (cur === prev) return { dir: 'flat', icon: 'flat', text: 'igual' };
  if (prev <= 0) {
    // A percentage over zero or a loss is meaningless: show the difference.
    const diff = cur - prev;
    return { dir: diff > 0 ? 'up' : 'down', icon: diff > 0 ? 'up' : 'down', text: `${diff > 0 ? '+' : '−'}${moneyRound(Math.abs(diff))}` };
  }
  const pct = (cur - prev) / prev;
  if (Math.abs(pct) < 0.005) return { dir: 'flat', icon: 'flat', text: 'igual' };
  return { dir: pct > 0 ? 'up' : 'down', icon: pct > 0 ? 'up' : 'down', text: `${pct > 0 ? '+' : '−'}${percent(Math.abs(pct))}` };
}

function deltaEl(cur, prev, suffix) {
  const c = change(cur, prev);
  return h('div.delta', { class: c.dir },
    h('span.delta-value', icon(c.icon), c.text),
    suffix ? h('span.vs', suffix) : null);
}

export async function dashboardView(ctx) {
  const name = state.user?.display_name || state.user?.username || '';
  let period = getPref('dash.period', '12m');
  let custom = getPref('dash.custom', null);
  let metric = getPref('dash.metric', 'benefit');
  let range = periodRange(period, custom);

  const heroBox = h('div', skeleton('hero'));
  const activeBox = h('section', { 'aria-label': 'Obres en curs' });
  const compareBox = h('section.card', { 'aria-label': 'Este mes i el passat' }, skeleton('block', 2));
  const monthlyBox = h('section.card', skeleton('block', 3));
  const catBox = h('section.card', skeleton('block', 3));
  const yoyBox = h('section.card.span-2', skeleton('block', 3));

  let overview = cached('/stats/overview');
  let active = cached('/projects?status=active')?.projects;
  let monthly = null;
  let cats = null;
  let stable = null;
  let yoy = null;

  // ------------------------------------------------------------ filters
  const customBox = h('div.custom-range', { hidden: period !== 'custom' });
  const fromIn = h('input.input', { type: 'month', value: range.from, max: thisMonth(), 'aria-label': 'Des del mes' });
  const toIn = h('input.input', { type: 'month', value: range.to, max: thisMonth(), 'aria-label': 'Fins al mes' });
  customBox.append(h('span.small.muted', 'De'), fromIn, h('span.small.muted', 'a'), toIn);
  const onCustom = () => {
    if (!fromIn.value || !toIn.value || fromIn.value > toIn.value) return;
    custom = { from: fromIn.value, to: toIn.value };
    setPref('dash.custom', custom);
    reloadPeriod();
  };
  fromIn.addEventListener('change', onCustom);
  toIn.addEventListener('change', onCustom);

  const periodSeg = segmented(PERIODS, period, (v) => {
    period = v;
    setPref('dash.period', v);
    customBox.hidden = v !== 'custom';
    reloadPeriod();
  }, { label: 'Període' });
  const metricSeg = segmented(METRICS, metric, (v) => {
    metric = v;
    setPref('dash.metric', v);
    renderMonthly();
    renderCategories();
    renderYoy();
  }, { label: 'Mesura' });

  mount(ctx.main,
    h('header.topbar', h('div.title', h('h1', name ? `Hola, ${name}` : 'Hola'), h('p.subtitle', longToday()))),
    heroBox,
    h('div.dash-grid', { style: { marginTop: '14px' } }, activeBox, compareBox),
    h('div.filters', { role: 'group', 'aria-label': 'Filtres dels gràfics' }, periodSeg, customBox, metricSeg),
    h('div.dash-grid', monthlyBox, catBox, yoyBox));

  // ------------------------------------------------------------ renderers

  function renderHero() {
    if (!overview) return;
    const cur = overview.month;
    const prev = overview.previous_month_to_date;
    const out = overview.outstanding;
    const kpi = (label, value, foot, href) => h(href ? 'a.kpi' : 'div.kpi', href ? { href } : {},
      h('span.label', label), h('span.value', value), foot ? h('span.foot', foot) : null);
    mount(heroBox,
      h('section.hero', { 'aria-label': `Benefici ${deMonth(overview.this_month)}` },
        h('div.label', `Benefici ${deMonth(overview.this_month)}`),
        h('div.value', { class: cur.benefit_cents < 0 ? 'neg' : '' }, moneyRound(cur.benefit_cents)),
        deltaEl(cur.benefit_cents, prev.benefit_cents, ` vs. els mateixos dies ${deMonth(overview.last_month)}`)),
      h('div.kpis',
        kpi('Facturat', moneyRound(cur.revenue_cents), `Despeses ${moneyRound(cur.cost_cents)}`),
        kpi('Cobrat', moneyRound(cur.collected_cents), `este mes`),
        kpi('Pendent de cobrar', moneyRound(out.pending_cents), out.projects_with_pending ? `en ${plural(out.projects_with_pending, 'obra', 'obres')}` : 'tot cobrat', '/obres?f=pending'),
        kpi('Obres en curs', String(out.active_projects), cur.labor_hours ? `${fmtHours(cur.labor_hours)} h treballades` : 'este mes', '/obres')));
  }

  function renderActive() {
    if (!active) {
      mount(activeBox, h('div.section-title', h('h2', 'Obres en curs')), skeleton('block', 2));
      return;
    }
    const top = [...active].sort((a, b) => b.updated_at.localeCompare(a.updated_at)).slice(0, 5);
    mount(activeBox,
      h('div.section-title', { style: { marginTop: '0' } },
        h('h2', 'Obres en curs'),
        active.length > top.length
          ? h('a.btn.btn-ghost.btn-sm', { href: '/obres' }, `Veure-les totes (${active.length})`)
          : h('a.btn.btn-ghost.btn-sm', { href: '/obres?nova=1' }, icon('plus'), 'Nova obra')),
      top.length
        ? h('div.list', top.map((p) => h('a.list-row', { href: `/obres/${p.id}` },
          h('span.main-col', h('div.t1.wrap2', p.name), h('div.t2', [p.town.name, p.client_name].filter(Boolean).join(' · '))),
          h('span.end', pendingPill(p.totals, { short: true })),
          icon('chevronRight', 'chev'))))
        : h('div.card', emptyState({
          iconName: 'bricks', title: 'Cap obra en curs',
          text: 'Crea una obra per a començar a portar-ne els comptes.',
          action: h('a.btn.btn-primary', { href: '/obres?nova=1' }, icon('plus'), 'Nova obra'),
        })));
  }

  function renderCompare() {
    if (!overview) return;
    const cur = overview.month;
    const prev = overview.previous_month_to_date;
    const full = overview.previous_month;
    const rows = [
      { label: 'Facturat', key: 'revenue_cents' },
      { label: 'Despeses', key: 'cost_cents', neutral: true },
      { label: 'Benefici', key: 'benefit_cents' },
      { label: 'Cobrat', key: 'collected_cents' },
    ];
    const max = Math.max(1, ...rows.flatMap((r) => [cur[r.key], prev[r.key]]));
    const width = (v) => `${Math.max(0, (v / max) * 100).toFixed(1)}%`;
    const thisName = monthName(overview.this_month);
    const lastName = monthName(overview.last_month);
    const day = Number(overview.today.slice(8, 10));
    mount(compareBox,
      h('div.card-head', h('div',
        h('h2', 'Este mes i el passat'),
        h('div.sub', day === 1 ? `El dia 1 de cada mes` : `De l'1 al ${day} de cada mes`))),
      h('div.legend', { style: { marginBottom: '12px' } },
        h('span.key', h('span.swatch'), cap(thisName)),
        h('span.key', h('span.swatch.prev'), cap(lastName))),
      h('div.compare', rows.map((r) => {
        const barCur = h('div.compare-bar');
        barCur.style.width = width(cur[r.key]);
        const barPrev = h('div.compare-bar.prev');
        barPrev.style.width = width(prev[r.key]);
        const c = change(cur[r.key], prev[r.key]);
        return h('div.compare-row',
          h('div.names',
            h('span.metric', r.label),
            h('span.vals', h('strong', { class: cur[r.key] < 0 ? 'danger' : '' }, moneyRound(cur[r.key])),
              r.neutral ? null : h('span.delta', { class: c.dir, style: { marginLeft: '8px' } }, icon(c.icon), c.text))),
          h('div.bars', { 'aria-hidden': 'true' }, barCur, barPrev),
          h('div.prevline', h('span', `${cap(lastName)}: ${moneyRound(prev[r.key])}`), h('span', `${cap(lastName)} sencer: ${moneyRound(full[r.key])}`)));
      })));
  }

  function metricInfo() {
    return METRICS.find((m) => m.value === metric) || METRICS[0];
  }

  function monthLabel(ym, spanYears) {
    const m = Number(ym.slice(5, 7));
    const short = MONTHS_SHORT[m - 1];
    return spanYears && m === 1 ? `${short} ${ym.slice(2, 4)}` : short;
  }

  function renderMonthly() {
    if (!monthly) return;
    const m = metricInfo();
    const months = monthly.months;
    const spanYears = months[0].month.slice(0, 4) !== months[months.length - 1].month.slice(0, 4);
    const total = months.reduce((a, d) => a + d[m.key], 0);
    const holder = h('div');
    const allZero = months.every((d) => d[m.key] === 0);
    holder.append(allZero
      ? h('div.chart-empty', 'Encara no hi ha dades en este període.')
      : columnChart({
        data: months.map((d) => ({ key: d.month, label: monthLabel(d.month, spanYears), title: cap(monthName(d.month, true)), value: d[m.key] })),
        format: (v, short) => (short ? moneyRound(v) : money(v)),
        formatTick: compact,
        highlightKey: months.some((d) => d.month === thisMonth()) ? thisMonth() : months[months.length - 1].month,
        ariaLabel: `${m.label} per mes. ${periodLabel(period, range)}.`,
      }));
    const tt = tableToggle(holder, ['Mes', 'Facturat', 'Despeses', 'Benefici', 'Cobrat'], () => months.map((d) => [
      cap(monthName(d.month, true)), money(d.revenue_cents), money(d.cost_cents), money(d.benefit_cents), money(d.collected_cents),
    ]));
    mount(monthlyBox,
      h('div.card-head',
        h('div', h('h2', `${m.label} per mes`), h('div.sub', `${periodLabel(period, range)} · total ${moneyRound(total)}`)),
        tt.button),
      holder, tt.table);
  }

  function renderCategories() {
    if (!cats || !stable) return;
    const m = metricInfo();
    // Colour follows the category, not its rank in the period: the three
    // categories with most turnover overall keep slots 1–3 (the palette's
    // first three stay distinguishable in any arrangement, colour-blind
    // included); every other category is "Altres".
    const keyOf = (c) => (c.kind === 'labor' ? 'labor' : `c${c.id}`);
    const fixed = [...stable.categories].sort((a, b) => b.revenue_cents - a.revenue_cents).slice(0, 3).map(keyOf);
    const rows = cats.categories.map((c) => {
      const slot = fixed.indexOf(keyOf(c));
      return { key: keyOf(c), name: c.name, value: c[m.key], cls: slot >= 0 ? `c${slot + 1}` : 'c-other' };
    }).sort((a, b) => b.value - a.value);
    const positive = rows.filter((r) => r.value > 0);
    const total = positive.reduce((a, r) => a + r.value, 0);

    const segments = positive.filter((r) => r.cls !== 'c-other').sort((a, b) => a.cls.localeCompare(b.cls))
      .map((r) => ({ key: r.key, label: r.name, value: r.value, cls: r.cls }));
    const others = positive.filter((r) => r.cls === 'c-other');
    if (others.length) {
      segments.push({ key: 'other', label: others.length === 1 ? others[0].name : `Altres (${others.length})`, value: others.reduce((a, r) => a + r.value, 0), cls: 'c-other' });
    }

    const tbody = h('tbody');
    for (const r of rows) {
      const tr = h('tr', { dataset: { key: r.cls === 'c-other' ? 'other' : r.key } },
        h('td.sw', h('span', { class: r.value > 0 ? r.cls : '' })),
        h('td', r.name, r.value < 0 ? h('div.note', 'amb pèrdues') : null),
        h('td.v', { class: r.value < 0 ? 'danger' : '' }, moneyRound(r.value)),
        h('td.p', r.value > 0 && total > 0 ? share(r.value / total) : ''));
      tbody.append(tr);
    }
    const grey = rows.filter((r) => r.value > 0 && r.cls === 'c-other').length;
    let donut = null;
    const highlight = (key) => {
      tbody.querySelectorAll('tr').forEach((tr) => tr.classList.toggle('active', key != null && tr.dataset.key === key));
    };
    const title = m.value === 'benefit' ? 'Benefici per categoria' : 'Facturació per categoria';
    const body = !rows.length || total <= 0
      ? h('div.chart-empty', 'Encara no hi ha partides en este període.')
      : h('div.donut-wrap',
        donut = donutChart({
          segments,
          centerValue: moneyRound(total),
          centerCaption: m.value === 'benefit' ? 'de benefici' : 'facturats',
          format: money,
          ariaLabel: `${title}. ${periodLabel(period, range)}.`,
          onActive: highlight,
        }),
        h('div',
          h('table.cat-table', h('caption.sr-only', title), tbody),
          grey > 1 ? h('p.xsmall.muted', { style: { marginTop: '8px' } }, 'Les categories en gris van juntes al gràfic, com a «Altres».') : null));
    tbody.addEventListener('pointerover', (e) => {
      const tr = e.target.closest('tr');
      if (tr && donut) { donut.setActive(tr.dataset.key); highlight(tr.dataset.key); }
    });
    tbody.addEventListener('pointerleave', () => { donut?.setActive(null); highlight(null); });
    mount(catBox,
      h('div.card-head', h('div', h('h2', title), h('div.sub', periodLabel(period, range)))),
      body);
  }

  function renderYoy() {
    if (!yoy || !overview) return;
    const m = metricInfo();
    const nowMonth = Number(thisMonth().slice(5, 7));
    const isCurrentYear = yoy.year === Number(thisMonth().slice(0, 4));
    const cur = yoy.months.map((x) => (!isCurrentYear || x.month <= nowMonth ? x.current[m.key] : null));
    const prev = yoy.months.map((x) => x.previous[m.key]);
    const ytd = overview.year_to_date[m.key];
    const ytdPrev = overview.previous_year_to_date[m.key];
    const holder = h('div');
    const empty = cur.every((v) => !v) && prev.every((v) => !v);
    holder.append(empty
      ? h('div.chart-empty', `Encara no hi ha dades de ${yoy.year} ni de ${yoy.previous_year}.`)
      : lineChart({
        labels: MONTHS_SHORT,
        titles: MONTHS.map(cap),
        series: [
          { name: String(yoy.year), cls: 's1', values: cur },
          { name: String(yoy.previous_year), cls: 'context', values: prev },
        ],
        format: money,
        formatTick: compact,
        ariaLabel: `${m.label} de ${yoy.year} comparat amb ${yoy.previous_year}, mes a mes`,
      }));
    const tt = tableToggle(holder, ['Mes', String(yoy.year), String(yoy.previous_year), 'Variació'], () => yoy.months.map((x, i) => [
      cap(MONTHS[i]),
      cur[i] == null ? '—' : money(cur[i]),
      money(prev[i]),
      cur[i] == null ? '—' : change(cur[i], prev[i]).text,
    ]));
    const day = Number(overview.today.slice(8, 10));
    mount(yoyBox,
      h('div.card-head',
        h('div', h('h2', 'Creixement interanual'), h('div.sub', `${m.label} de ${yoy.year} i ${yoy.previous_year}, mes a mes`)),
        tt.button),
      h('div.row.wrap', { style: { marginBottom: '10px', gap: '6px 12px' } },
        h('strong', `${moneyRound(ytd)} en el que va d'any`),
        deltaEl(ytd, ytdPrev, ` vs. ${moneyRound(ytdPrev)} fins al ${day} ${deMonth(overview.this_month)} de ${yoy.previous_year}`)),
      h('div.legend', { style: { marginBottom: '8px' } },
        h('span.key', h('span.line'), String(yoy.year)),
        h('span.key', h('span.line.context'), String(yoy.previous_year))),
      holder, tt.table);
  }

  // ------------------------------------------------------------ loading

  async function fetchPeriod() {
    const q = `from=${range.from}&to=${range.to}`;
    const [mo, ca] = await Promise.all([
      load(`/stats/monthly?${q}`, { signal: ctx.signal }),
      load(`/stats/categories?${q}`, { signal: ctx.signal }),
    ]);
    monthly = mo;
    cats = ca;
  }

  async function reloadPeriod() {
    range = periodRange(period, custom);
    fromIn.value = range.from;
    toIn.value = range.to;
    // Keep the previous charts, dimmed, while the new slice loads.
    monthlyBox.classList.add('stale');
    catBox.classList.add('stale');
    try {
      await fetchPeriod();
      if (!ctx.alive()) return;
      renderMonthly();
      renderCategories();
    } catch (err) {
      if (err.name !== 'AbortError') toastError(err);
    } finally {
      monthlyBox.classList.remove('stale');
      catBox.classList.remove('stale');
    }
  }

  // Instant paint from the cache, then refresh everything in parallel.
  renderHero();
  renderActive();
  renderCompare();
  if (overview) ctx.ready();

  const longFrom = addMonths(thisMonth(), -119);
  const results = await Promise.allSettled([
    load('/stats/overview', { signal: ctx.signal }).then((r) => { overview = r; }),
    load('/projects?status=active', { signal: ctx.signal }).then((r) => { active = r.projects; }),
    fetchPeriod(),
    load(`/stats/categories?from=${longFrom}&to=${thisMonth()}`, { signal: ctx.signal }).then((r) => { stable = r; }),
    load('/stats/yoy', { signal: ctx.signal }).then((r) => { yoy = r; }),
  ]);
  if (!ctx.alive()) return;
  renderHero();
  renderActive();
  renderCompare();
  renderMonthly();
  renderCategories();
  renderYoy();
  ctx.ready();

  const failed = results.find((r) => r.status === 'rejected' && r.reason?.name !== 'AbortError');
  if (failed) toastError(failed.reason);
}
