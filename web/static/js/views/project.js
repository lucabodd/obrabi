// Detail of an obra: what the client owes, the benefit, the line items and
// the payments, with quick entry of new ones.

import { h, icon, mount } from '../dom.js';
import { cached, load, post, del } from '../api.js';
import { navigate } from '../router.js';
import {
  actionSheet, confirmSheet, emptyState, skeleton, toast, toastError, segmented,
} from '../ui.js';
import {
  money, moneySigned, dateShort, dateNumeric, isoDate, hours as fmtHours, plural, percent,
} from '../format.js';
import { pushRecent, forgetRecent } from '../store.js';
import { openItemForm, openPaymentForm, openProjectForm } from './forms.js';

export async function projectView(ctx) {
  const id = ctx.params.id;
  const path = `/projects/${id}`;
  let project = cached(path)?.project || null;
  let tab = 'items';

  const root = h('div');
  mount(ctx.main, root);

  const render = () => {
    if (!ctx.alive() || !project) return;
    document.title = `${project.name} · Obrabi`;
    mount(root, header(), summary(), panels(), footer(), fab());
  };

  const setProject = (p) => {
    project = p;
    render();
  };

  if (project) {
    render();
    ctx.ready();
  } else {
    mount(root, h('div.stack', skeleton('hero'), skeleton('block', 3)));
  }
  try {
    const res = await load(path, { signal: ctx.signal });
    project = res.project;
    pushRecent(project);
    render();
  } catch (err) {
    if (err.name === 'AbortError' || !ctx.alive()) return;
    if (err.status === 404) {
      forgetRecent(Number(id));
      mount(root, emptyState({
        iconName: 'alert', title: "No s'ha trobat l'obra",
        text: 'Potser s\'ha eliminat.',
        action: h('a.btn.btn-primary', { href: '/obres' }, 'Tornar a les obres'),
      }));
      return;
    }
    if (!project) {
      mount(root, emptyState({ iconName: 'alert', title: "No s'ha pogut carregar l'obra", text: err.message,
        action: h('button.btn', { type: 'button', onclick: () => navigate(location.pathname, { replace: true }) }, icon('refresh'), 'Tornar-ho a provar') }));
    } else {
      toastError(err);
    }
  }

  // ------------------------------------------------------------ sections

  function header() {
    const back = project.status === 'finished' ? '/arxiu' : '/obres';
    const where = [
      h('span', icon('pin'), ' ', project.town.name),
      project.client_name ? h('span', icon('user'), ' ', project.client_name) : null,
      project.client_phone ? h('a', { href: `tel:${project.client_phone.replace(/[^\d+]/g, '')}`, 'data-external': '' }, icon('phone'), project.client_phone) : null,
    ];
    return h('div.detail-head',
      h('a.icon-btn', { href: back, 'aria-label': project.status === 'finished' ? "Tornar a l'arxiu" : 'Tornar a les obres' }, icon('back')),
      h('div.title',
        h('h1', project.name),
        h('div.where', where),
        project.status === 'finished'
          ? h('div', { class: 'meta-line', style: { marginTop: '8px' } }, h('span.pill.neutral', icon('archive'), `Finalitzada el ${dateNumeric(isoDate(new Date(project.finished_at)))}`))
          : null),
      h('button.icon-btn', { type: 'button', 'aria-label': "Opcions de l'obra", onclick: menu }, icon('more')));
  }

  function summary() {
    const t = project.totals;
    let label = 'Pendent de cobrar';
    let big = money(t.pending_cents);
    let cls = 'pending';
    if (t.price_cents === 0) {
      label = 'Encara no hi ha partides';
      big = money(0);
      cls = 'muted';
    } else if (t.pending_cents === 0) {
      label = 'Tot cobrat';
      big = money(t.benefit_cents);
      cls = 'good';
    } else if (t.pending_cents < 0) {
      label = 'El client ha pagat de més';
      big = money(-t.pending_cents);
      cls = 'ink-2';
    }
    const paidRatio = t.price_cents > 0 ? Math.min(1, t.collected_cents / t.price_cents) : 0;
    const bar = h('span');
    bar.style.width = `${Math.round(paidRatio * 100)}%`;
    return h('section.card.summary', { 'aria-label': 'Resum econòmic' },
      h('div',
        h('div.big-label', t.pending_cents === 0 && t.price_cents > 0 ? [icon('check'), `${label} · benefici`] : label),
        h('div.big', { class: cls }, big)),
      t.price_cents > 0 ? h('div',
        h('div.progress', { role: 'progressbar', 'aria-valuemin': '0', 'aria-valuemax': '100', 'aria-valuenow': String(Math.round(paidRatio * 100)), 'aria-label': 'Part cobrada' }, bar),
        h('div.progress-caption', h('span', `Cobrat ${money(t.collected_cents)}`), h('span', `de ${money(t.price_cents)}`))) : null,
      h('div.figures',
        figure('Total client', money(t.price_cents)),
        figure('Despeses', money(t.cost_cents), 'El que has pagat tu'),
        figure('Benefici', money(t.benefit_cents), null, t.benefit_cents < 0 ? 'danger' : 'good')),
      h('div.meta-line',
        h('span', { title: 'Cobrat menys el que has pagat' }, icon('wallet'), `Balanç de caixa: ${moneySigned(t.balance_cents)}`),
        t.labor_hours > 0 ? h('span', icon('clock'), `${fmtHours(t.labor_hours)} h de mà d'obra`) : null,
        t.price_cents > 0 ? h('span', icon('chart'), `Marge ${percent(t.benefit_cents / t.price_cents)}`) : null));
  }

  function figure(label, value, title, cls) {
    return h('div.figure', { title }, h('div.label', label), h('div.value', { class: cls }, value));
  }

  function panels() {
    const itemsPanel = h('section', { 'aria-label': 'Partides', hidden: tab !== 'items' },
      h('div.panel-head', h('h2', 'Partides'), h('button.btn.btn-ghost.btn-sm', { type: 'button', onclick: addItemMenu }, icon('plus'), 'Afegir')),
      itemsList());
    const paysPanel = h('section', { 'aria-label': 'Cobraments', hidden: tab !== 'payments' },
      h('div.panel-head', h('h2', 'Cobraments'), h('button.btn.btn-ghost.btn-sm', { type: 'button', onclick: addPayment }, icon('plus'), 'Afegir')),
      paymentsList());
    const tabs = segmented([
      { value: 'items', label: `Partides · ${project.items.length}` },
      { value: 'payments', label: `Cobraments · ${project.payments.length}` },
    ], tab, (v) => {
      tab = v;
      itemsPanel.hidden = v !== 'items';
      paysPanel.hidden = v !== 'payments';
    }, { block: true, label: 'Secció' });
    return h('div', h('div.panel-tabs', tabs), h('div.panels', itemsPanel, paysPanel));
  }

  function itemsList() {
    if (!project.items.length) {
      return h('div.card', emptyState({
        iconName: 'box', title: 'Encara no hi ha partides',
        text: 'Afegix el material, les despeses i la mà d\'obra.',
        action: h('button.btn.btn-primary', { type: 'button', onclick: addItemMenu }, icon('plus'), 'Afegir partida'),
      }));
    }
    return h('div.list', project.items.map((it) => {
      const labor = it.kind === 'labor';
      const title = labor ? "Mà d'obra" : it.category?.name || 'Sense categoria';
      const sub = [
        labor && it.hours ? `${fmtHours(it.hours)} h` : null,
        it.description || null,
        dateShort(it.item_date),
      ].filter(Boolean).join(' · ');
      const benefit = it.price_cents - it.cost_cents;
      return h('button.list-row', { type: 'button', onclick: () => editItem(it), 'aria-label': `${title}, ${money(it.price_cents)}. Editar` },
        h('span.lead', { class: labor ? 'labor' : '' }, icon(labor ? 'helmet' : 'box')),
        h('span.main-col', h('div.t1', title), h('div.t2', sub)),
        h('span.end',
          h('div.v1', money(it.price_cents)),
          h('div.v2', { class: benefit < 0 ? 'danger' : benefit > 0 ? 'good' : 'muted' }, moneySigned(benefit))));
    }));
  }

  function paymentsList() {
    if (!project.payments.length) {
      return h('div.card', emptyState({
        iconName: 'wallet', title: 'Encara no hi ha cobraments',
        text: 'Quan el client pague, apunta-ho ací i es descomptarà del pendent.',
        action: h('button.btn.btn-primary', { type: 'button', onclick: addPayment }, icon('plus'), 'Afegir cobrament'),
      }));
    }
    return h('div.list', project.payments.map((p) => h('button.list-row', {
      type: 'button', onclick: () => editPayment(p), 'aria-label': `Cobrament de ${money(p.amount_cents)}. Editar`,
    },
    h('span.lead.pay', icon('wallet')),
    h('span.main-col', h('div.t1', p.note || 'Cobrament'), h('div.t2', dateShort(p.paid_on))),
    h('span.end', h('div.v1.good', money(p.amount_cents))))));
  }

  function footer() {
    const parts = [];
    if (project.notes) {
      parts.push(h('section.card', h('div.panel-head', h('h2', 'Notes')), h('p.notes', project.notes)));
    }
    parts.push(h('div.finish-bar',
      project.status === 'active'
        ? h('button.btn.btn-block', { type: 'button', onclick: finish }, icon('archive'), "Finalitzar l'obra")
        : h('button.btn.btn-block', { type: 'button', onclick: reopen }, icon('undo'), "Reobrir l'obra"),
      h('a.btn.btn-block', { href: pdfUrl('intern'), target: '_blank', rel: 'noopener' }, icon('file'), 'Exportar en PDF')));
    parts.push(h('div.fab-spacer', { 'aria-hidden': 'true' }));
    return h('div', { class: 'stack', style: { marginTop: '18px' } }, parts);
  }

  function fab() {
    return h('button.fab', { type: 'button', onclick: addItemMenu, 'aria-label': 'Afegir partida o cobrament' }, icon('plus'), 'Afegir');
  }

  // ------------------------------------------------------------ actions

  async function addItemMenu() {
    const choice = await actionSheet({
      title: 'Què vols afegir?',
      actions: [
        { value: 'expense', icon: 'box', label: 'Material o despesa', hint: 'El que pagues i el que cobres al client' },
        { value: 'labor', icon: 'helmet', tone: 'labor', label: "Mà d'obra", hint: 'Hores treballades i preu' },
        { value: 'payment', icon: 'wallet', tone: 'pay', label: 'Cobrament del client', hint: 'Diners que t\'ha pagat el client' },
      ],
    });
    if (choice === 'payment') addPayment();
    else if (choice) {
      tab = 'items';
      await openItemForm({ project, kind: choice, onSaved: setProject });
    }
  }

  function editItem(item) {
    openItemForm({ project, item, onSaved: setProject });
  }

  async function addPayment() {
    tab = 'payments';
    render();
    await openPaymentForm({ project, onSaved: setProject });
  }

  function editPayment(payment) {
    openPaymentForm({ project, payment, onSaved: setProject });
  }

  async function menu() {
    const finished = project.status === 'finished';
    // Start downloading the client PDF now, so that "Compartir" can hand it
    // to the share sheet within the tap (browsers require a user gesture).
    const canShare = typeof navigator.canShare === 'function' && matchMedia('(pointer: coarse)').matches;
    let sharedFile = null;
    if (canShare) {
      fetch(pdfUrl('client'), { credentials: 'same-origin' })
        .then(async (res) => {
          if (!res.ok) return;
          const name = (res.headers.get('Content-Disposition') || '').match(/filename="([^"]+)"/)?.[1] || 'obra.pdf';
          const file = new File([await res.blob()], name, { type: 'application/pdf' });
          if (navigator.canShare({ files: [file] })) sharedFile = file;
        })
        .catch(() => {});
    }
    const actions = [
      { value: 'edit', icon: 'edit', label: "Editar les dades de l'obra" },
      { value: 'pdf', icon: 'file', label: 'PDF complet', hint: 'Amb costos i benefici, per a tu', href: pdfUrl('intern') },
      { value: 'pdf-client', icon: 'file', label: 'PDF per al client', hint: 'Només preus, pagaments i pendent', href: pdfUrl('client') },
      canShare ? {
        value: 'share', icon: 'share', label: 'Compartir el PDF per al client', hint: 'Per WhatsApp, correu…',
        run: () => {
          if (sharedFile) navigator.share({ files: [sharedFile], title: project.name }).catch(() => {});
          else toast('El PDF encara es prepara. Torna-ho a provar en un moment.');
        },
      } : null,
      finished
        ? { value: 'reopen', icon: 'undo', label: "Reobrir l'obra", hint: "Torna a la llista d'obres en curs" }
        : { value: 'finish', icon: 'archive', label: "Finalitzar l'obra", hint: "La mou a l'arxiu" },
      { value: 'delete', icon: 'trash', label: "Eliminar l'obra", danger: true },
    ].filter(Boolean);
    const choice = await actionSheet({ title: project.name, actions });
    if (choice === 'edit') {
      const saved = await openProjectForm({ project });
      if (saved) setProject(saved);
    } else if (choice === 'finish') finish();
    else if (choice === 'reopen') reopen();
    else if (choice === 'delete') remove();
  }

  async function finish() {
    const pending = project.totals.pending_cents;
    const ok = await confirmSheet({
      title: "Finalitzar l'obra",
      message: pending > 0
        ? `Encara queden ${money(pending)} per cobrar. La pots finalitzar igualment: seguirà apareixent com a pendent a l'arxiu i a l'inici.`
        : "L'obra passarà a l'arxiu. La podràs reobrir quan vulgues.",
      confirmLabel: 'Finalitzar',
    });
    if (!ok) return;
    try {
      const res = await post(`/projects/${project.id}/finish`);
      setProject(res.project);
      toast("Obra finalitzada i moguda a l'arxiu");
    } catch (err) {
      toastError(err);
    }
  }

  async function reopen() {
    try {
      const res = await post(`/projects/${project.id}/reopen`);
      setProject(res.project);
      toast('Obra reoberta');
    } catch (err) {
      toastError(err);
    }
  }

  async function remove() {
    const ok = await confirmSheet({
      title: "Eliminar l'obra",
      message: `S'eliminarà «${project.name}» amb ${plural(project.items.length, 'partida', 'partides')} i ${plural(project.payments.length, 'cobrament', 'cobraments')}. No es pot desfer.`,
      confirmLabel: 'Eliminar definitivament',
      danger: true,
    });
    if (!ok) return;
    try {
      await del(`/projects/${project.id}`);
      forgetRecent(project.id);
      toast('Obra eliminada');
      navigate(project.status === 'finished' ? '/arxiu' : '/obres', { replace: true });
    } catch (err) {
      toastError(err);
    }
  }

  function pdfUrl(variant) {
    return `/api/projects/${project.id}/pdf${variant === 'client' ? '?variant=client' : ''}`;
  }
}
