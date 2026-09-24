// Pobles: the towns that group the obres.

import { h, icon, mount } from '../dom.js';
import { cached, load, post, put, del } from '../api.js';
import { openSheet, field, formError, busy, confirmSheet, emptyState, skeleton, toast, toastError } from '../ui.js';
import { byName, plural } from '../format.js';

export async function townsView(ctx) {
  let towns = cached('/towns')?.towns || null;
  const listBox = h('div');

  mount(ctx.main,
    h('header.topbar',
      h('div.title', h('h1', 'Pobles'), h('p.subtitle', 'Agrupen les obres a les llistes')),
      h('button.btn.btn-primary.btn-sm', { type: 'button', onclick: () => edit(null) }, icon('plus'), 'Nou poble')),
    listBox);

  function render() {
    if (!towns) {
      mount(listBox, h('div.stack', skeleton('block', 3)));
      return;
    }
    if (!towns.length) {
      mount(listBox, emptyState({
        iconName: 'pin', title: 'Encara no hi ha pobles',
        text: 'També els pots crear directament quan fas una obra nova.',
        action: h('button.btn.btn-primary', { type: 'button', onclick: () => edit(null) }, icon('plus'), 'Nou poble'),
      }));
      return;
    }
    const sorted = [...towns].sort((a, b) => byName(a.name, b.name));
    mount(listBox, h('div.list', sorted.map((t) => h('button.list-row', { type: 'button', onclick: () => edit(t) },
      h('span.lead', icon('pin')),
      h('span.main-col',
        h('div.t1', t.name),
        h('div.t2', [
          t.active_projects ? plural(t.active_projects, 'obra en curs', 'obres en curs') : null,
          t.finished_projects ? plural(t.finished_projects, 'finalitzada', 'finalitzades') : null,
        ].filter(Boolean).join(' · ') || 'Sense obres')),
      icon('edit', 'chev')))));
  }

  async function refresh() {
    try {
      towns = (await load('/towns', { signal: ctx.signal })).towns;
      if (ctx.alive()) render();
    } catch (err) {
      if (err.name !== 'AbortError' && ctx.alive()) toastError(err);
    }
  }

  function edit(town) {
    const id = `town-form-${Date.now()}`;
    const error = formError();
    const name = h('input.input', { type: 'text', value: town?.name || '', placeholder: "Ex.: l'Alcúdia", autocapitalize: 'words', enterkeyhint: 'done' });
    const used = town ? town.active_projects + town.finished_projects : 0;
    const removeBtn = town
      ? h('button.btn.btn-danger.btn-sm', { type: 'button', disabled: used > 0 }, icon('trash'), 'Eliminar el poble')
      : null;
    const form = h('form.form', { id, novalidate: true },
      error,
      field('Nom del poble', name, { hint: 'Escriu-lo com vulgues vore\'l a les llistes.' }),
      used > 0 ? h('p.small.muted', `No es pot eliminar mentre tinga obres (${plural(used, 'obra', 'obres')}).`) : null,
      removeBtn);
    const save = h('button.btn.btn-primary', { type: 'submit', form: id }, 'Guardar');
    const sheet = openSheet({ title: town ? 'Editar el poble' : 'Nou poble', body: form, foot: save });

    form.addEventListener('submit', (e) => {
      e.preventDefault();
      error.hide();
      if (!name.value.trim()) return error.show('Escriu el nom del poble.');
      busy(save, async () => {
        try {
          if (town) await put(`/towns/${town.id}`, { name: name.value });
          else await post('/towns', { name: name.value });
          await sheet.close();
          toast('Poble guardat');
          refresh();
        } catch (err) {
          error.show(err.message);
        }
      });
    });
    removeBtn?.addEventListener('click', async () => {
      const ok = await confirmSheet({ title: 'Eliminar el poble', message: `Segur que vols eliminar «${town.name}»?`, confirmLabel: 'Eliminar', danger: true });
      if (!ok) return;
      try {
        await del(`/towns/${town.id}`);
        await sheet.close();
        toast('Poble eliminat');
        refresh();
      } catch (err) {
        error.show(err.message);
      }
    });
  }

  render();
  if (towns) ctx.ready();
  await refresh();
}
