import { h, icon, mount } from '../dom.js';
import { post } from '../api.js';
import { navigate } from '../router.js';
import { state } from '../store.js';
import { field, formError, busy } from '../ui.js';

export async function loginView(ctx) {
  if (state.user) {
    navigate(safeNext(ctx.query.get('next')), { replace: true });
    return;
  }
  const error = formError();
  const username = h('input.input', {
    type: 'text', name: 'username', autocomplete: 'username', autocapitalize: 'none',
    autocorrect: 'off', spellcheck: 'false', enterkeyhint: 'next', required: true,
  });
  const password = h('input.input', {
    type: 'password', name: 'password', autocomplete: 'current-password', enterkeyhint: 'go', required: true,
  });
  const toggle = h('button.icon-btn', { type: 'button', 'aria-label': 'Mostrar la contrasenya' }, icon('eye'));
  toggle.addEventListener('click', () => {
    const show = password.type === 'password';
    password.type = show ? 'text' : 'password';
    toggle.setAttribute('aria-label', show ? 'Amagar la contrasenya' : 'Mostrar la contrasenya');
    mount(toggle, icon(show ? 'eyeOff' : 'eye'));
  });
  const submit = h('button.btn.btn-primary.btn-block', { type: 'submit' }, 'Entrar');

  const form = h('form.form', { novalidate: true },
    error,
    field('Usuari', username),
    field('Contrasenya', h('div.password-wrap', password, toggle)),
    submit);

  form.addEventListener('submit', (e) => {
    e.preventDefault();
    error.hide();
    if (!username.value.trim() || !password.value) {
      error.show("Escriu l'usuari i la contrasenya.");
      return;
    }
    busy(submit, async () => {
      try {
        const res = await post('/auth/login', { username: username.value.trim(), password: password.value });
        state.user = res.user;
        navigate(safeNext(ctx.query.get('next')), { replace: true });
      } catch (err) {
        error.show(err.message);
        password.select();
      }
    });
  });

  mount(ctx.main, h('div.login-card',
    h('div.login-brand',
      h('span.brand-mark', { 'aria-hidden': 'true' }),
      h('h1', 'Obrabi'),
      h('p', 'Els comptes de les teues obres')),
    h('div.card', form)));
  if (matchMedia('(min-width: 640px)').matches) username.focus();
}

/** Only allow redirects inside the app. */
function safeNext(next) {
  return next && next.startsWith('/') && !next.startsWith('//') && !next.startsWith('/login') ? next : '/';
}
