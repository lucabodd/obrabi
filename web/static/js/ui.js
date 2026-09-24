// Reusable interface pieces: toasts, bottom sheets (dialogs that also close
// with the phone's back button), confirmations, pickers and form fields.

import { h, icon, mount } from './dom.js';
import { fold, parseMoney, money } from './format.js';

// ---------------------------------------------------------------- toasts

let toastBox;
export function toast(message, { error = false, duration = 3200 } = {}) {
  if (!toastBox) toastBox = h('div.toasts', { role: 'status', 'aria-live': 'polite' });
  // Open dialogs live in the browser's top layer, above any z-index: while a
  // sheet is open the toasts must be shown inside it to be visible.
  const host = [...document.querySelectorAll('dialog[open]')].pop() || document.body;
  if (toastBox.parentNode !== host) host.append(toastBox);
  const el = h('div.toast', { class: error ? 'error' : '' }, icon(error ? 'alert' : 'check'), h('span', message));
  toastBox.append(el);
  setTimeout(() => el.remove(), duration);
}

/** Shows an error (ApiError or anything else) as a toast. */
export function toastError(err) {
  toast(err?.message || 'Alguna cosa ha anat malament.', { error: true, duration: 5000 });
}

// ---------------------------------------------------------------- sheets

const sheets = [];
let swallowPops = 0;
const popWaiters = [];

/**
 * Called by the router on popstate. Returns true when the event belonged to a
 * sheet (so the page must not re-render).
 */
export function handleBack() {
  if (swallowPops > 0) {
    swallowPops--;
    popWaiters.splice(0).forEach((resolve) => resolve());
    return true;
  }
  const top = sheets[sheets.length - 1];
  if (top) {
    top.finish(undefined);
    return true;
  }
  return false;
}

/** Closes every open sheet without touching history (used on navigation). */
export function closeAllSheets() {
  while (sheets.length) sheets[sheets.length - 1].finish(undefined);
}

/**
 * Opens a sheet: a bottom sheet on phones, a centred dialog on wider screens.
 * body/foot are nodes. Returns { close(result), closed: Promise, body }.
 */
export function openSheet({ title, body, foot, onClose, labelledBy }) {
  const titleId = `sheet-title-${Math.random().toString(36).slice(2, 8)}`;
  const closeBtn = h('button.icon-btn', { type: 'button', 'aria-label': 'Tancar' }, icon('close'));
  const bodyEl = h('div.sheet-body', body);
  const dialog = h('dialog.sheet', { 'aria-labelledby': labelledBy || titleId },
    h('div.sheet-grip', { 'aria-hidden': 'true' }),
    h('div.sheet-head', h('h2', { id: titleId }, title), closeBtn),
    bodyEl,
    foot ? h('div.sheet-foot', foot) : null,
  );

  // `closed` settles only once the history entry pushed for the sheet is gone,
  // so whatever the caller does next (open another sheet, navigate) starts
  // from a consistent history.
  let resolveClosed;
  let settled = false;
  const closed = new Promise((r) => { resolveClosed = r; });
  const settle = (result) => {
    if (settled) return;
    settled = true;
    onClose?.(result);
    resolveClosed(result);
  };
  const sheet = {
    dialog,
    body: bodyEl,
    closed,
    done: false,
    /** Removes the dialog; settles unless a history pop is still pending. */
    finish(result, { pendingPop = false } = {}) {
      if (sheet.done) return;
      sheet.done = true;
      sheets.splice(sheets.indexOf(sheet), 1);
      try { dialog.close(); } catch { /* already closed */ }
      dialog.remove();
      if (!pendingPop) settle(result);
    },
    /** Closes from the UI, consuming the history entry the sheet pushed. */
    close(result) {
      if (sheet.done) return closed;
      sheet.finish(result, { pendingPop: true });
      const popped = new Promise((resolve) => {
        popWaiters.push(resolve);
        setTimeout(resolve, 500); // safety net if popstate never arrives
      });
      swallowPops++;
      history.back();
      popped.then(() => settle(result));
      return closed;
    },
  };

  closeBtn.addEventListener('click', () => sheet.close());
  dialog.addEventListener('cancel', (e) => { e.preventDefault(); sheet.close(); });
  dialog.addEventListener('click', (e) => {
    if (e.target !== dialog) return;
    const r = dialog.getBoundingClientRect();
    const inside = e.clientX >= r.left && e.clientX <= r.right && e.clientY >= r.top && e.clientY <= r.bottom;
    if (!inside) sheet.close();
  });

  document.body.append(dialog);
  sheets.push(sheet);
  history.pushState({ ...(history.state || {}), sheet: sheets.length }, '', location.href);
  dialog.showModal();
  // Focus the first field rather than the close button.
  const first = bodyEl.querySelector('[autofocus], input:not([type=hidden]), textarea, select');
  if (first && matchMedia('(min-width: 640px)').matches) first.focus();
  else closeBtn.blur();
  return sheet;
}

/** Yes/no question. Resolves true when confirmed. */
export function confirmSheet({ title, message, confirmLabel = 'Confirmar', danger = false }) {
  return new Promise((resolve) => {
    let answer = false;
    const ok = h('button.btn', { type: 'button', class: danger ? 'btn-danger' : 'btn-primary' }, confirmLabel);
    const cancel = h('button.btn', { type: 'button' }, 'Cancel·lar');
    const sheet = openSheet({
      title,
      body: h('p.ink-2', message),
      foot: h('div.btn-row', cancel, ok),
      onClose: () => resolve(answer),
    });
    ok.addEventListener('click', () => { answer = true; sheet.close(); });
    cancel.addEventListener('click', () => sheet.close());
  });
}

/**
 * A list of big buttons; resolves with the chosen value (or undefined).
 * An action with `href` is a real link (opened in a new tab: never blocked as
 * a pop-up); one with `run` calls it synchronously inside the tap, which APIs
 * such as navigator.share require.
 */
export function actionSheet({ title, actions }) {
  return new Promise((resolve) => {
    let chosen;
    const list = h('div.action-list');
    const sheet = openSheet({ title, body: list, onClose: () => resolve(chosen) });
    for (const a of actions) {
      const content = [
        h('span.lead', { class: a.tone || '' }, icon(a.icon)),
        h('span.grow', h('div.t1', a.label), a.hint ? h('div.t2', a.hint) : null),
      ];
      if (a.href) {
        list.append(h('a.action', {
          href: a.href, target: '_blank', rel: 'noopener',
          onclick: () => { chosen = a.value; sheet.close(); },
        }, content));
        continue;
      }
      list.append(h('button.action', {
        type: 'button',
        class: a.danger ? 'danger' : '',
        onclick: () => {
          chosen = a.value;
          a.run?.();
          sheet.close();
        },
      }, content));
    }
  });
}

// ---------------------------------------------------------------- form pieces

let fieldSeq = 0;
/** Label + control + optional hint. */
export function field(label, control, { hint, optional } = {}) {
  const id = control.id || `f${++fieldSeq}`;
  const input = control.matches?.('input, textarea, select') ? control : control.querySelector?.('input, textarea, select');
  if (input && !input.id) input.id = id;
  return h('div.field',
    h('label', { for: input?.id || id }, label, optional ? h('span.opt', ' · opcional') : null),
    control,
    hint ? h('div.hint', hint) : null);
}

/** Euro amount input with a € suffix and a decimal keyboard on phones. */
export function moneyInput({ value = '', placeholder = '0,00', oninput, name } = {}) {
  const input = h('input.input', {
    type: 'text', inputmode: 'decimal', autocomplete: 'off', enterkeyhint: 'next',
    placeholder, value, name, oninput,
  });
  return { el: h('div.affix', input, h('span.suffix', '€')), input, cents: () => parseMoney(input.value) };
}

/** Segmented control. options: [{value, label}] */
export function segmented(options, value, onChange, { block = false, label } = {}) {
  const el = h('div.seg', { class: block ? 'block' : '', role: 'group', 'aria-label': label });
  const buttons = options.map((o) => h('button', {
    type: 'button',
    'aria-pressed': String(o.value === value),
    onclick: () => {
      buttons.forEach((b) => b.setAttribute('aria-pressed', String(b === btnFor(o.value))));
      onChange(o.value);
    },
  }, o.label));
  const btnFor = (v) => buttons[options.findIndex((o) => o.value === v)];
  el.append(...buttons);
  el.set = (v) => buttons.forEach((b, i) => b.setAttribute('aria-pressed', String(options[i].value === v)));
  return el;
}

/** Row of toggle chips; returns the element with .set(value). */
export function chipGroup(options, value, onChange, { scroll = false, label } = {}) {
  const el = h('div.chips', { class: scroll ? 'scroll' : '', role: 'group', 'aria-label': label });
  const chips = options.map((o) => h('button.chip', {
    type: 'button',
    'aria-pressed': String(o.value === value),
    onclick: () => onChange(o.value),
  }, o.icon ? icon(o.icon) : null, o.label));
  el.append(...chips);
  el.set = (v) => chips.forEach((c, i) => c.setAttribute('aria-pressed', String(options[i].value === v)));
  return el;
}

/** Inline error box inside a form. */
export function formError() {
  const el = h('div.form-error', { role: 'alert', hidden: true });
  el.show = (msg) => { mount(el, icon('alert'), h('span', msg)); el.hidden = false; el.scrollIntoView({ block: 'nearest' }); };
  el.hide = () => { el.hidden = true; };
  return el;
}

/** Disables a button while an async action runs. */
export async function busy(button, fn) {
  const was = button.disabled;
  button.disabled = true;
  try {
    return await fn();
  } finally {
    button.disabled = was;
  }
}

/**
 * Searchable picker that can also create a new entry: used for towns and
 * expense categories. options: [{ id, name, meta }] (most relevant first).
 * getValue() → { id } for an existing option, { name } for a new one, or null.
 */
export function combobox({ options, selected = null, placeholder, quickCount = 6, createText = (n) => `Crear «${n}»`, ariaLabel, onChange }) {
  const root = h('div.combo');
  let value = selected; // { id, name } | { name } | null
  let active = -1;
  let shown = [];

  const input = h('input.input', {
    type: 'text', placeholder, autocomplete: 'off', autocapitalize: 'sentences',
    enterkeyhint: 'done', role: 'combobox', 'aria-expanded': 'false', 'aria-label': ariaLabel,
  });
  const list = h('div.combo-list', { role: 'listbox', hidden: true });
  const quick = h('div.chips');

  function choose(v) {
    value = v;
    onChange?.(value);
    render();
  }

  function renderList() {
    const q = fold(input.value);
    const exact = options.find((o) => fold(o.name) === q);
    // Without a query the chips already offer the most recent entries; the
    // full list is only useful when there are more options than chips.
    shown = q ? options.filter((o) => fold(o.name).includes(q)) : (options.length > quickCount ? options : []);
    const items = shown.map((o, i) => h('button.combo-option', {
      type: 'button', role: 'option', 'data-active': String(i === active),
      onmousedown: (e) => e.preventDefault(),
      onclick: () => choose({ id: o.id, name: o.name }),
    }, h('span.truncate', o.name), o.meta ? h('span.meta', o.meta) : null));
    const typed = input.value.trim().replace(/\s+/g, ' ');
    if (typed && !exact) {
      const i = items.length;
      items.push(h('button.combo-option.create', {
        type: 'button', role: 'option', 'data-active': String(i === active),
        onmousedown: (e) => e.preventDefault(),
        onclick: () => choose({ name: typed }),
      }, icon('plus'), h('span.truncate', createText(typed))));
    }
    mount(list, items);
    list.hidden = items.length === 0 || (!q && document.activeElement !== input);
    input.setAttribute('aria-expanded', String(!list.hidden));
  }

  input.addEventListener('input', () => { active = -1; renderList(); });
  input.addEventListener('focus', renderList);
  input.addEventListener('blur', () => setTimeout(() => { list.hidden = true; }, 150));
  input.addEventListener('keydown', (e) => {
    const count = list.children.length;
    if (e.key === 'ArrowDown' && count) { e.preventDefault(); active = (active + 1) % count; renderList(); }
    else if (e.key === 'ArrowUp' && count) { e.preventDefault(); active = (active - 1 + count) % count; renderList(); }
    else if (e.key === 'Enter') {
      e.preventDefault();
      const pick = list.children[active >= 0 ? active : 0];
      if (pick && !list.hidden) pick.click();
    }
  });

  function render() {
    if (value) {
      mount(root, h('div.combo-selected',
        icon('check'),
        h('span.grow.truncate', value.name, value.id ? null : h('span.muted.small', ' · nou')),
        h('button.btn.btn-ghost.btn-sm', {
          type: 'button',
          onclick: () => { value = null; onChange?.(null); input.value = ''; render(); input.focus(); },
        }, 'Canviar')));
      return;
    }
    const top = options.slice(0, quickCount);
    mount(quick, top.map((o) => h('button.chip', { type: 'button', onclick: () => choose({ id: o.id, name: o.name }) }, o.name)));
    quick.classList.add('combo-quick');
    mount(root, top.length ? quick : null, input, list);
    renderList();
  }
  render();

  return {
    el: root,
    input,
    getValue: () => value || (input.value.trim() ? { name: input.value.trim().replace(/\s+/g, ' ') } : null),
    focus: () => (value ? null : input.focus()),
  };
}

/** Small "loading" placeholder blocks. */
export function skeleton(kind = 'block', count = 1) {
  return Array.from({ length: count }, () => h('div.skeleton', { class: kind, 'aria-hidden': 'true' }));
}

/** Empty state with an optional action button. */
export function emptyState({ iconName, title, text, action }) {
  return h('div.empty',
    h('div.art', icon(iconName)),
    h('h3', title),
    text ? h('p', text) : null,
    action || null);
}

/** Pill describing what a project still owes. */
export function pendingPill(totals, { short = false } = {}) {
  if (totals.price_cents === 0) return h('span.pill.neutral', 'Sense partides');
  if (totals.pending_cents > 0) {
    return h('span.pill.pending', icon('clock'), short ? money(totals.pending_cents) : `Pendent ${money(totals.pending_cents)}`);
  }
  if (totals.pending_cents < 0) return h('span.pill.neutral', `A favor del client ${money(-totals.pending_cents)}`);
  return h('span.pill.good', icon('check'), 'Cobrada');
}
