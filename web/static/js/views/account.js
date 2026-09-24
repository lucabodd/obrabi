// Compte: name, password, installing the app on the phone, logout.

import { h, icon, mount } from '../dom.js';
import { post, put, clearCache } from '../api.js';
import { navigate } from '../router.js';
import { state } from '../store.js';
import { field, formError, busy, toast } from '../ui.js';
import { appVersion } from '../telemetry.js';

export async function logout() {
  try {
    await post('/auth/logout');
  } catch {
    // even if the request fails the local session is dropped
  }
  state.user = null;
  clearCache();
  navigate('/login', { replace: true });
}

function passwordInput(autocomplete) {
  return h('input.input', { type: 'password', autocomplete, required: true });
}

export async function accountView(ctx) {
  const user = state.user;

  // Name shown in the greeting.
  const nameError = formError();
  const displayName = h('input.input', { type: 'text', value: user.display_name, autocomplete: 'name', autocapitalize: 'words' });
  const saveName = h('button.btn', { type: 'submit' }, 'Guardar el nom');
  const nameForm = h('form.form', { novalidate: true }, nameError,
    field('El teu nom', displayName, { hint: `Usuari per a entrar: ${user.username}` }), saveName);
  nameForm.addEventListener('submit', (e) => {
    e.preventDefault();
    nameError.hide();
    busy(saveName, async () => {
      try {
        const res = await put('/auth/me', { display_name: displayName.value });
        state.user = res.user;
        toast('Nom guardat');
      } catch (err) {
        nameError.show(err.message);
      }
    });
  });

  // Password change.
  const pwError = formError();
  const current = passwordInput('current-password');
  const next = passwordInput('new-password');
  const repeat = passwordInput('new-password');
  const savePw = h('button.btn.btn-primary', { type: 'submit' }, icon('lock'), 'Canviar la contrasenya');
  const pwForm = h('form.form', { novalidate: true }, pwError,
    field('Contrasenya actual', current),
    field('Contrasenya nova', next, { hint: 'Almenys 8 caràcters. Millor una frase fàcil de recordar.' }),
    field('Repetix la contrasenya nova', repeat),
    savePw);
  pwForm.addEventListener('submit', (e) => {
    e.preventDefault();
    pwError.hide();
    if (next.value.length < 8) return pwError.show('La contrasenya nova ha de tindre almenys 8 caràcters.');
    if (next.value !== repeat.value) return pwError.show('Les dues contrasenyes noves no coincidixen.');
    busy(savePw, async () => {
      try {
        await put('/auth/password', { current_password: current.value, new_password: next.value });
        current.value = next.value = repeat.value = '';
        toast('Contrasenya canviada');
      } catch (err) {
        pwError.show(err.message);
      }
    });
  });

  const standalone = matchMedia('(display-mode: standalone)').matches || navigator.standalone;
  const isIOS = /iphone|ipad|ipod/i.test(navigator.userAgent);

  mount(ctx.main,
    h('header.topbar', h('div.title', h('h1', 'Compte'), h('p.subtitle', user.username))),
    h('div.stack-lg',
      h('section.card', h('div.card-head', h('h2', 'Perfil')), nameForm),
      h('section.card', h('div.card-head', h('h2', 'Contrasenya')), pwForm),
      standalone ? null : h('section.card',
        h('div.card-head', h('h2', "Instal·la Obrabi al mòbil")),
        h('p.ink-2.small', { style: { marginBottom: '10px' } }, "Tindràs una icona com una app i s'obrirà a pantalla completa."),
        isIOS
          ? h('ol.install-steps', h('li', 'Obri esta pàgina amb Safari.'), h('li', 'Toca el botó Compartir (el quadrat amb la fletxa).'), h('li', 'Tria «Afegir a la pantalla d\'inici».'))
          : h('ol.install-steps', h('li', 'Obri el menú del navegador (⋮).'), h('li', 'Tria «Instal·lar l\'aplicació» o «Afegir a la pantalla d\'inici».'))),
      h('button.btn.btn-block.btn-danger', { type: 'button', onclick: logout }, icon('logout'), 'Eixir'),
      h('p.version', `Obrabi · versió ${appVersion}`)));
  ctx.ready();
}
