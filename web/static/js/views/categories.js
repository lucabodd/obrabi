// Categories de despesa: created on the fly from the item form; here they
// can be renamed, and duplicates merged so the statistics add up.

import { h, icon, mount } from '../dom.js';
import { cached, load, put, del } from '../api.js';
import { openSheet, field, formError, busy, confirmSheet, emptyState, skeleton, toast, toastError } from '../ui.js';
import { plural, dateShort, isoDate } from '../format.js';

export async function categoriesView(ctx) {
  let categories = cached('/categories')?.categories || null;
  const listBox = h('div');

  mount(ctx.main,
    h('header.topbar', h('div.title', h('h1', 'Categories'), h('p.subtitle', 'Es creen soles quan afegixes una partida'))),
    h('div.form-note', { style: { marginBottom: '14px' } }, icon('info'),
      h('span', "Si en tens dos que volen dir el mateix (per exemple «Fontaneria» i «Fontanería»), fusiona-les perquè les estadístiques isquen bé.")),
    listBox);

  function render() {
    if (!categories) {
      mount(listBox, h('div.stack', skeleton('block', 3)));
      return;
    }
    if (!categories.length) {
      mount(listBox, emptyState({
        iconName: 'tag', title: 'Encara no hi ha categories',
        text: 'Quan afegixes material o despeses a una obra, escriu la categoria i es crearà ací.',
      }));
      return;
    }
    const sorted = [...categories].sort((a, b) => a.name.localeCompare(b.name, 'ca'));
    mount(listBox, h('div.list', sorted.map((c) => h('button.list-row', { type: 'button', onclick: () => edit(c) },
      h('span.lead', icon('tag')),
      h('span.main-col',
        h('div.t1', c.name),
        h('div.t2', c.items ? `${plural(c.items, 'partida', 'partides')} · última ${dateShort(isoDate(new Date(c.last_used)))}` : 'Sense partides')),
      icon('edit', 'chev')))));
  }

  async function refresh() {
    try {
      categories = (await load('/categories', { signal: ctx.signal })).categories;
      if (ctx.alive()) render();
    } catch (err) {
      if (err.name !== 'AbortError' && ctx.alive()) toastError(err);
    }
  }

  function edit(cat) {
    const id = `cat-form-${Date.now()}`;
    const error = formError();
    const name = h('input.input', { type: 'text', value: cat.name, autocapitalize: 'sentences', enterkeyhint: 'done' });
    const others = categories.filter((c) => c.id !== cat.id).sort((a, b) => a.name.localeCompare(b.name, 'ca'));
    const target = h('select.input', h('option', { value: '' }, 'Tria una categoria…'),
      others.map((c) => h('option', { value: String(c.id) }, c.name)));
    const mergeBtn = h('button.btn.btn-sm', { type: 'button' }, 'Fusionar');
    const removeBtn = h('button.btn.btn-danger.btn-sm', { type: 'button' }, icon('trash'), 'Eliminar la categoria');

    const form = h('form.form', { id, novalidate: true },
      error,
      field('Nom', name),
      others.length ? h('div.field',
        h('span.label', 'Fusionar amb una altra'),
        h('div.hint', cat.items
          ? `Les ${plural(cat.items, 'partida', 'partides')} passaran a la categoria triada i «${cat.name}» desapareixerà.`
          : `«${cat.name}» desapareixerà.`),
        h('div.row', h('div.grow', target), mergeBtn)) : null,
      cat.items ? null : removeBtn);
    const save = h('button.btn.btn-primary', { type: 'submit', form: id }, 'Guardar el nom');
    const sheet = openSheet({ title: 'Editar la categoria', body: form, foot: save });

    form.addEventListener('submit', (e) => {
      e.preventDefault();
      error.hide();
      if (!name.value.trim()) return error.show('Escriu el nom de la categoria.');
      busy(save, async () => {
        try {
          await put(`/categories/${cat.id}`, { name: name.value });
          await sheet.close();
          toast('Categoria guardada');
          refresh();
        } catch (err) {
          error.show(err.message);
        }
      });
    });

    mergeBtn.addEventListener('click', async () => {
      error.hide();
      const into = others.find((c) => String(c.id) === target.value);
      if (!into) return error.show('Tria amb quina categoria la vols fusionar.');
      const ok = await confirmSheet({
        title: 'Fusionar categories',
        message: `«${cat.name}» s'unirà a «${into.name}». No es pot desfer.`,
        confirmLabel: 'Fusionar',
      });
      if (!ok) return;
      try {
        await del(`/categories/${cat.id}?merge_into=${into.id}`);
        await sheet.close();
        toast(`Fusionada amb «${into.name}»`);
        refresh();
      } catch (err) {
        error.show(err.message);
      }
    });

    removeBtn.addEventListener('click', async () => {
      const ok = await confirmSheet({ title: 'Eliminar la categoria', message: `Segur que vols eliminar «${cat.name}»?`, confirmLabel: 'Eliminar', danger: true });
      if (!ok) return;
      try {
        await del(`/categories/${cat.id}`);
        await sheet.close();
        toast('Categoria eliminada');
        refresh();
      } catch (err) {
        error.show(err.message);
      }
    });
  }

  render();
  if (categories) ctx.ready();
  await refresh();
}
