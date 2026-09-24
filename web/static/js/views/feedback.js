// Suggeriments i errors: Nando writes what could be better or what broke,
// and Luca receives it by e-mail.

import { h, icon, mount } from '../dom.js';
import { cached, load, post } from '../api.js';
import { segmented, formError, busy, toast, toastError, skeleton } from '../ui.js';
import { dateTime } from '../format.js';
import { appVersion } from '../telemetry.js';
import { getPreviousPath } from '../router.js';

export async function feedbackView(ctx) {
  let kind = 'suggestion';
  let reports = cached('/feedback')?.reports || null;
  const listBox = h('div');
  const error = formError();
  const message = h('textarea.input', {
    rows: 5, placeholder: 'Conta-m\'ho amb les teues paraules…', maxlength: 5000, autocapitalize: 'sentences',
  });
  const hint = h('p.small.muted');
  const setHint = () => {
    hint.textContent = kind === 'bug'
      ? 'Explica què estaves fent i què ha passat. Si pots, digues en quina obra.'
      : 'Què t\'agradaria canviar o tindre? Cap idea és massa xicoteta.';
  };
  setHint();
  const kindSeg = segmented([
    { value: 'suggestion', label: 'Suggeriment' },
    { value: 'bug', label: 'Error' },
  ], kind, (v) => { kind = v; setHint(); }, { block: true, label: 'Tipus de missatge' });
  const send = h('button.btn.btn-primary.btn-block', { type: 'submit' }, icon('message'), 'Enviar');
  const form = h('form.form.card', { novalidate: true }, error, kindSeg, hint, message, send);

  form.addEventListener('submit', (e) => {
    e.preventDefault();
    error.hide();
    if (!message.value.trim()) return error.show('Escriu el missatge abans d\'enviar-lo.');
    busy(send, async () => {
      try {
        await post('/feedback', {
          kind,
          message: message.value,
          page: getPreviousPath() || location.pathname,
          viewport: `${window.innerWidth}x${window.innerHeight}`,
          app_version: appVersion,
        });
        message.value = '';
        toast('Enviat! Gràcies, Luca ho rebrà per correu.');
        refresh();
      } catch (err) {
        error.show(err.message);
      }
    });
  });

  mount(ctx.main,
    h('header.topbar', h('div.title', h('h1', 'Suggeriments i errors'), h('p.subtitle', 'Ajuda a millorar Obrabi'))),
    form,
    h('div.section-title', h('h2', 'Els teus missatges')),
    listBox,
    h('p.small.muted', { style: { marginTop: '16px' } },
      'Quan alguna cosa falla, Obrabi també envia un avís automàtic amb els detalls tècnics, sense que hages de fer res.'));

  function render() {
    if (!reports) {
      mount(listBox, skeleton('block', 2));
      return;
    }
    if (!reports.length) {
      mount(listBox, h('p.muted.small', 'Encara no has enviat cap missatge.'));
      return;
    }
    mount(listBox, h('div.list', reports.map((r) => h('div.list-row',
      h('span.lead', { class: r.kind === 'bug' ? 'labor' : '' }, icon(r.kind === 'bug' ? 'alert' : 'message')),
      h('span.main-col',
        h('div.t1', r.kind === 'bug' ? 'Error' : 'Suggeriment'),
        h('div', { class: 'small ink-2', style: { whiteSpace: 'pre-wrap', overflowWrap: 'anywhere', marginTop: '2px' } }, r.message),
        h('div.t2', dateTime(r.created_at))),
      h('span.end', r.email_status === 'sent'
        ? h('span.pill.good', icon('check'), 'Enviat')
        : h('span.pill.neutral', 'Rebut'))))));
  }

  async function refresh() {
    try {
      reports = (await load('/feedback', { signal: ctx.signal })).reports;
      if (ctx.alive()) render();
    } catch (err) {
      if (err.name !== 'AbortError' && ctx.alive()) toastError(err);
    }
  }

  render();
  ctx.ready();
  await refresh();
}
