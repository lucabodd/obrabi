// Entry forms in bottom sheets, tuned for quick input on a phone: sensible
// defaults (today, last category, last mark-up, last hourly rate), decimal
// keyboards, one-tap chips and "save and add another".

import { h, icon, mount } from '../dom.js';
import { post, put, del, load, cached } from '../api.js';
import {
  openSheet, field, moneyInput, combobox, formError, busy, chipGroup, toast, confirmSheet,
} from '../ui.js';
import {
  money, moneySigned, parseMoney, parseNumber, centsToInput, today, percent, plural,
} from '../format.js';
import { getPref, setPref, lastRate, setLastRate } from '../store.js';

let formSeq = 0;

async function list(path, key) {
  try {
    return (cached(path) || (await load(path)))[key];
  } catch {
    return []; // offline: the picker still allows typing a new name
  }
}

function dateInput(value) {
  return h('input.input', { type: 'date', value: value || today(), required: true });
}

function textInput(value, placeholder, extra = {}) {
  return h('input.input', { type: 'text', value: value || '', placeholder, autocapitalize: 'sentences', enterkeyhint: 'next', ...extra });
}

function submitButton(formId, label) {
  return h('button.btn.btn-primary', { type: 'submit', form: formId }, label);
}

/** Validates an amount field: returns cents (0 when empty) or throws a message. */
function amount(input, label) {
  const v = parseMoney(input.value);
  if (Number.isNaN(v)) throw new Error(`${label}: l'import no és vàlid (ex.: 130 o 130,50).`);
  if (v != null && v < 0) throw new Error(`${label}: l'import no pot ser negatiu.`);
  return v || 0;
}

// ---------------------------------------------------------------- project

/** Creates or edits a project. Resolves with the saved project or null. */
export function openProjectForm({ project = null } = {}) {
  return new Promise((resolve) => {
    let saved = null;
    const id = `form-${++formSeq}`;
    const error = formError();
    const name = textInput(project?.name, 'Ex.: Reforma del bany');
    const client = textInput(project?.client_name, 'Nom del client', { autocomplete: 'off' });
    const phone = h('input.input', { type: 'tel', inputmode: 'tel', value: project?.client_phone || '', placeholder: '600 000 000', autocomplete: 'off' });
    const address = textInput(project?.address, 'Carrer i número', { autocomplete: 'off' });
    const started = dateInput(project?.started_on);
    const notes = h('textarea.input', { placeholder: 'Mesures, acords amb el client…' });
    notes.value = project?.notes || '';
    const townHolder = h('div', h('div.skeleton', { 'aria-hidden': 'true' }));
    let town = null;

    const form = h('form.form', { id, novalidate: true },
      error,
      field("Nom de l'obra", name),
      field('Poble', townHolder),
      h('div.fields-2', field('Client', client, { optional: true }), field('Telèfon', phone, { optional: true })),
      field('Adreça', address, { optional: true }),
      field("Data d'inici", started),
      field('Notes', notes, { optional: true }));
    const submit = submitButton(id, project ? 'Guardar els canvis' : 'Crear obra');
    const sheet = openSheet({
      title: project ? "Editar l'obra" : 'Nova obra',
      body: form,
      foot: submit,
      onClose: () => resolve(saved),
    });

    list('/towns', 'towns').then((towns) => {
      const options = [...towns]
        .sort((a, b) => (b.active_projects + b.finished_projects) - (a.active_projects + a.finished_projects))
        .map((t) => ({ id: t.id, name: t.name, meta: t.active_projects ? plural(t.active_projects, 'en curs', 'en curs') : '' }));
      town = combobox({
        options,
        selected: project ? { id: project.town.id, name: project.town.name } : null,
        placeholder: 'Busca o escriu el poble',
        ariaLabel: 'Poble',
        createText: (n) => `Afegir el poble «${n}»`,
      });
      mount(townHolder, town.el);
    });

    form.addEventListener('submit', (e) => {
      e.preventDefault();
      error.hide();
      const t = town?.getValue();
      if (!name.value.trim()) return error.show("Posa-li un nom a l'obra.");
      if (!t) return error.show("Tria o escriu el poble de l'obra.");
      const body = {
        name: name.value,
        town_id: t.id || 0,
        town_name: t.id ? '' : t.name,
        client_name: client.value,
        client_phone: phone.value,
        address: address.value,
        notes: notes.value,
        started_on: started.value,
      };
      busy(submit, async () => {
        try {
          const res = project ? await put(`/projects/${project.id}`, body) : await post('/projects', body);
          saved = res.project;
          await sheet.close(saved);
          toast(project ? 'Obra guardada' : 'Obra creada');
        } catch (err) {
          error.show(err.message);
        }
      });
    });
  });
}

// ---------------------------------------------------------------- expense / labour

const MARKUPS = [
  { value: 0, label: 'Sense recàrrec' },
  { value: 10, label: '+10 %' },
  { value: 15, label: '+15 %' },
  { value: 20, label: '+20 %' },
  { value: 30, label: '+30 %' },
];

/**
 * Adds or edits a line item. kind: "expense" | "labor". onSaved(project) runs
 * after every successful save (the sheet may stay open for another entry).
 */
export function openItemForm({ project, kind, item = null, onSaved }) {
  kind = item?.kind || kind;
  const id = `form-${++formSeq}`;
  const error = formError();
  const date = dateInput(item?.item_date);
  const description = textInput(item?.description, kind === 'labor' ? 'Ex.: Muratura, enrajolat…' : 'Ex.: Sacs de ciment');
  const calc = h('div.calc-line', { 'aria-live': 'polite' });
  const fields = kind === 'labor' ? laborFields(item, calc) : expenseFields(item, calc);

  const form = h('form.form', { id, novalidate: true },
    error,
    fields.top,
    field('Descripció', description, { optional: true }),
    fields.amounts,
    calc,
    field('Data', date),
    item ? h('button.btn.btn-danger.btn-sm', { type: 'button', onclick: remove }, icon('trash'), 'Eliminar la partida') : null);

  const save = submitButton(id, 'Guardar');
  const saveMore = item ? null : h('button.btn', { type: 'button', title: 'Guardar i afegir-ne una altra', 'aria-label': 'Guardar i afegir-ne una altra' }, icon('plus'), 'Guardar i nova');
  const title = item ? 'Editar la partida' : (kind === 'labor' ? "Mà d'obra" : 'Material o despesa');
  const sheet = openSheet({ title, body: form, foot: h('div.btn-row', saveMore, save) });
  fields.update();

  async function submit(again) {
    error.hide();
    let body;
    try {
      body = { kind, description: description.value, item_date: date.value, ...fields.values() };
    } catch (err) {
      error.show(err.message);
      return;
    }
    const button = again ? saveMore : save;
    await busy(button, async () => {
      try {
        const res = item
          ? await put(`/projects/${project.id}/items/${item.id}`, body)
          : await post(`/projects/${project.id}/items`, body);
        onSaved?.(res.project);
        fields.remember();
        if (again) {
          fields.reset();
          description.value = '';
          toast('Partida guardada. Pots afegir-ne una altra.');
          fields.focusFirst();
        } else {
          await sheet.close();
          toast(item ? 'Partida actualitzada' : 'Partida afegida');
        }
      } catch (err) {
        error.show(err.message);
      }
    });
  }

  async function remove() {
    const ok = await confirmSheet({
      title: 'Eliminar la partida',
      message: 'Segur que vols eliminar esta partida? No es pot desfer.',
      confirmLabel: 'Eliminar',
      danger: true,
    });
    if (!ok) return;
    try {
      const res = await del(`/projects/${project.id}/items/${item.id}`);
      onSaved?.(res.project);
      await sheet.close();
      toast('Partida eliminada');
    } catch (err) {
      error.show(err.message);
    }
  }

  form.addEventListener('submit', (e) => { e.preventDefault(); submit(false); });
  saveMore?.addEventListener('click', () => submit(true));
  return sheet.closed;
}

function expenseFields(item, calc) {
  const catHolder = h('div', h('div.skeleton', { 'aria-hidden': 'true' }));
  let cat = null;
  let pendingFocus = false;
  list('/categories', 'categories').then((cats) => {
    let selected = item?.category ? { id: item.category.id, name: item.category.name } : null;
    if (!item) {
      const last = getPref('lastCategory');
      const found = last && cats.find((c) => c.id === last);
      if (found) selected = { id: found.id, name: found.name };
    }
    cat = combobox({
      options: cats.map((c) => ({ id: c.id, name: c.name, meta: c.items ? plural(c.items, 'partida', 'partides') : '' })),
      selected,
      placeholder: 'Busca o escriu una categoria',
      ariaLabel: 'Categoria',
      createText: (n) => `Crear la categoria «${n}»`,
    });
    mount(catHolder, cat.el);
    if (pendingFocus) cost.input.focus();
  });

  const cost = moneyInput({ value: item ? centsToInput(item.cost_cents) : '' });
  const price = moneyInput({ value: item ? centsToInput(item.price_cents) : '' });
  let markup = item ? null : getPref('markup', null);
  const chips = chipGroup(MARKUPS, markup, (v) => {
    markup = v;
    chips.set(v);
    applyMarkup();
    update();
  }, { label: 'Recàrrec al client' });

  function applyMarkup() {
    const c = parseMoney(cost.input.value);
    if (markup == null || c == null || Number.isNaN(c)) return;
    price.input.value = centsToInput(Math.round(c * (1 + markup / 100)));
  }
  cost.input.addEventListener('input', () => { applyMarkup(); update(); });
  price.input.addEventListener('input', () => {
    markup = null;
    chips.set(null);
    update();
  });

  function update() {
    const c = parseMoney(cost.input.value) || 0;
    const p = parseMoney(price.input.value) || 0;
    if (Number.isNaN(c) || Number.isNaN(p)) {
      mount(calc, h('span', 'Revisa els imports'), h('strong.danger', '—'));
      return;
    }
    const b = p - c;
    const share = p > 0 ? ` (${percent(b / p)} del preu)` : '';
    mount(calc, h('span', 'Benefici'), h('strong', { class: b < 0 ? 'danger' : b > 0 ? 'good' : '' }, moneySigned(b) + share));
  }

  return {
    top: field('Categoria', catHolder),
    amounts: h('div.stack',
      h('div.fields-2',
        field('Cost', cost.el, { hint: 'El que pagues tu' }),
        field('Preu al client', price.el, { hint: 'El que cobraràs' })),
      chips),
    update,
    values() {
      const v = cat?.getValue();
      if (!v) throw new Error('Tria o escriu una categoria.');
      const costCents = amount(cost.input, 'Cost');
      const priceCents = amount(price.input, 'Preu al client');
      if (!costCents && !priceCents) throw new Error('Posa almenys un import.');
      return { category_id: v.id || 0, category_name: v.id ? '' : v.name, cost_cents: costCents, price_cents: priceCents };
    },
    remember() {
      const v = cat?.getValue();
      if (v?.id) setPref('lastCategory', v.id);
      if (markup != null) setPref('markup', markup);
    },
    reset() {
      cost.input.value = '';
      price.input.value = '';
      update();
    },
    focusFirst() {
      if (cat) cost.input.focus();
      else pendingFocus = true;
    },
  };
}

function laborFields(item, calc) {
  const hoursIn = h('input.input', {
    type: 'text', inputmode: 'decimal', autocomplete: 'off', placeholder: '0',
    value: item?.hours != null ? String(item.hours).replace('.', ',') : '',
  });
  const rateCents = item?.hours ? Math.round(item.price_cents / item.hours) : lastRate();
  const rate = moneyInput({ value: rateCents ? centsToInput(rateCents) : '' });
  const total = moneyInput({ value: item ? centsToInput(item.price_cents) : '' });
  const cost = moneyInput({ value: item?.cost_cents ? centsToInput(item.cost_cents) : '' });
  const costField = field("Cost d'ajudant", cost.el, { optional: true, hint: 'Només si has pagat algú per esta faena' });
  costField.hidden = !(item?.cost_cents > 0);
  const addCost = h('button.btn.btn-ghost.btn-sm', { type: 'button', hidden: !costField.hidden }, icon('plus'), "Afegir cost d'ajudant");
  addCost.addEventListener('click', () => {
    costField.hidden = false;
    addCost.hidden = true;
    cost.input.focus();
  });

  // The total follows hours × rate until the user types a total by hand.
  let manualTotal = Boolean(item);
  const recompute = () => {
    if (manualTotal) return;
    const hh = parseNumber(hoursIn.value);
    const r = parseMoney(rate.input.value);
    if (hh == null || r == null || Number.isNaN(hh) || Number.isNaN(r)) return;
    total.input.value = centsToInput(Math.round(hh * r));
  };
  hoursIn.addEventListener('input', () => { recompute(); update(); });
  rate.input.addEventListener('input', () => { manualTotal = false; recompute(); update(); });
  total.input.addEventListener('input', () => { manualTotal = total.input.value.trim() !== ''; update(); });
  cost.input.addEventListener('input', update);

  function update() {
    const t = parseMoney(total.input.value) || 0;
    const c = parseMoney(cost.input.value) || 0;
    if (Number.isNaN(t) || Number.isNaN(c)) {
      mount(calc, h('span', 'Revisa els imports'), h('strong.danger', '—'));
      return;
    }
    mount(calc, h('span', 'Benefici'), h('strong', { class: t - c < 0 ? 'danger' : t - c > 0 ? 'good' : '' }, moneySigned(t - c)));
  }

  return {
    top: null,
    amounts: h('div.stack',
      h('div.fields-2',
        field('Hores', hoursIn),
        field('Preu per hora', rate.el)),
      field('Total', total.el, { hint: 'Es calcula amb hores × preu, però el pots canviar' }),
      addCost,
      costField),
    update,
    values() {
      const hh = parseNumber(hoursIn.value);
      if (Number.isNaN(hh)) throw new Error('Les hores no són vàlides (ex.: 8 o 7,5).');
      const t = amount(total.input, 'Total');
      const c = amount(cost.input, "Cost d'ajudant");
      if (!t && !c) throw new Error('Posa el total de la mà d\'obra.');
      return { hours: hh, price_cents: t, cost_cents: c };
    },
    remember() {
      const r = parseMoney(rate.input.value);
      if (r && !Number.isNaN(r)) setLastRate(r);
    },
    reset() {
      hoursIn.value = '';
      total.input.value = '';
      cost.input.value = '';
      manualTotal = false;
      update();
    },
    focusFirst() { hoursIn.focus(); },
  };
}

// ---------------------------------------------------------------- payment

const NOTES = ['Efectiu', 'Transferència', 'Bizum', 'Senyal'];

/** Adds or edits a client payment. */
export function openPaymentForm({ project, payment = null, onSaved }) {
  const id = `form-${++formSeq}`;
  const error = formError();
  const amountIn = moneyInput({ value: payment ? centsToInput(payment.amount_cents) : '' });
  const date = dateInput(payment?.paid_on);
  const note = textInput(payment?.note, 'Ex.: Bizum, efectiu…', { autocomplete: 'off' });
  const pending = project.totals.pending_cents;
  const quick = !payment && pending > 0
    ? h('button.chip', { type: 'button', onclick: () => { amountIn.input.value = centsToInput(pending); } },
      icon('check'), `Tot el pendent: ${money(pending)}`)
    : null;
  const noteChips = h('div.chips', NOTES.map((n) => h('button.chip', { type: 'button', onclick: () => { note.value = n; } }, n)));

  const form = h('form.form', { id, novalidate: true },
    error,
    field('Import cobrat', amountIn.el),
    quick ? h('div.chips', quick) : null,
    field('Data', date),
    field('Nota', note, { optional: true }),
    noteChips,
    payment ? h('button.btn.btn-danger.btn-sm', { type: 'button', onclick: remove }, icon('trash'), 'Eliminar el cobrament') : null);
  const save = submitButton(id, 'Guardar');
  const sheet = openSheet({ title: payment ? 'Editar el cobrament' : 'Cobrament del client', body: form, foot: save });

  form.addEventListener('submit', (e) => {
    e.preventDefault();
    error.hide();
    const cents = parseMoney(amountIn.input.value);
    if (cents == null || Number.isNaN(cents) || cents <= 0) {
      error.show("Escriu l'import cobrat (ex.: 200 o 200,50).");
      return;
    }
    const body = { amount_cents: cents, paid_on: date.value, note: note.value };
    busy(save, async () => {
      try {
        const res = payment
          ? await put(`/projects/${project.id}/payments/${payment.id}`, body)
          : await post(`/projects/${project.id}/payments`, body);
        onSaved?.(res.project);
        await sheet.close();
        toast(payment ? 'Cobrament actualitzat' : 'Cobrament afegit');
      } catch (err) {
        error.show(err.message);
      }
    });
  });

  async function remove() {
    const ok = await confirmSheet({
      title: 'Eliminar el cobrament',
      message: 'Segur que vols eliminar este cobrament?',
      confirmLabel: 'Eliminar',
      danger: true,
    });
    if (!ok) return;
    try {
      const res = await del(`/projects/${project.id}/payments/${payment.id}`);
      onSaved?.(res.project);
      await sheet.close();
      toast('Cobrament eliminat');
    } catch (err) {
      error.show(err.message);
    }
  }
  return sheet.closed;
}
