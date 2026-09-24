// "Més" tab on phones: the sections that do not fit in the tab bar.

import { h, icon, mount } from '../dom.js';
import { state } from '../store.js';
import { logout } from './account.js';

export async function moreView(ctx) {
  const link = (href, iconName, label, hint) => h('a.list-row', { href },
    h('span.lead', icon(iconName)),
    h('span.main-col', h('div.t1', label), hint ? h('div.t2', hint) : null),
    icon('chevronRight', 'chev'));

  mount(ctx.main,
    h('header.topbar', h('div.title', h('h1', 'Més'), h('p.subtitle', state.user ? `Hola, ${state.user.display_name}` : ''))),
    h('div.stack-lg',
      h('div.list.more-list',
        link('/pobles', 'pin', 'Pobles', 'Afegir o canviar el nom dels pobles'),
        link('/categories', 'tag', 'Categories', 'Canviar el nom o fusionar categories'),
        link('/suggeriments', 'message', 'Suggeriments i errors', 'Conta què es pot millorar'),
        link('/compte', 'user', 'Compte', 'Nom, contrasenya i instal·lació')),
      h('button.btn.btn-block.btn-danger', { type: 'button', onclick: logout }, icon('logout'), 'Eixir')));
  ctx.ready();
}
