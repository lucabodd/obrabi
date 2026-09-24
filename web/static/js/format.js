// Formatting and parsing in Valencian conventions: 1.234,56 €, 24/09/2026.

const moneyFmt = new Intl.NumberFormat('ca-ES', {
  style: 'currency', currency: 'EUR', minimumFractionDigits: 2, maximumFractionDigits: 2, useGrouping: 'always',
});
const moneyFmt0 = new Intl.NumberFormat('ca-ES', {
  style: 'currency', currency: 'EUR', minimumFractionDigits: 0, maximumFractionDigits: 0, useGrouping: 'always',
});
const numFmt1 = new Intl.NumberFormat('ca-ES', { maximumFractionDigits: 1, useGrouping: 'always' });
const numFmt2 = new Intl.NumberFormat('ca-ES', { maximumFractionDigits: 2, useGrouping: 'always' });

/** 123456 → "1.234,56 €" */
export const money = (cents) => moneyFmt.format((cents || 0) / 100);

/** 123456 → "1.235 €" (for tiles where cents are noise). */
export const moneyRound = (cents) => moneyFmt0.format(Math.round((cents || 0) / 100));

/** Signed amount: "+30,00 €" / "−12,00 €". */
export function moneySigned(cents) {
  if (!cents) return money(0);
  return (cents > 0 ? '+' : '−') + money(Math.abs(cents));
}

/** Axis ticks: 150000 → "1,5k", 2500000000 → "25M", 80000 → "800". */
export function compact(cents) {
  const v = cents / 100;
  const a = Math.abs(v);
  if (a >= 1e6) return numFmt1.format(v / 1e6) + 'M';
  if (a >= 1e3) return numFmt1.format(v / 1e3) + 'k';
  return numFmt1.format(Math.round(v));
}

/** 7.5 → "7,5" */
export const hours = (h) => numFmt2.format(h || 0);

/** 0.1234 → "12 %" */
export function percent(ratio, digits = 0) {
  return new Intl.NumberFormat('ca-ES', { maximumFractionDigits: digits }).format(ratio * 100) + ' %';
}

/**
 * Parses what people type as an amount: "130", "130,5", "1.300,50", "1300.5",
 * "1.300". Returns cents, null for an empty field, or NaN when invalid.
 */
export function parseMoney(input) {
  if (input == null) return null;
  let s = String(input).trim().replace(/[€\s ]/g, '');
  if (!s) return null;
  let neg = false;
  if (s.startsWith('-') || s.startsWith('−')) { neg = true; s = s.slice(1); }
  if (!/^[\d.,]+$/.test(s)) return NaN;
  const commas = (s.match(/,/g) || []).length;
  const dots = (s.match(/\./g) || []).length;
  let intPart = s;
  let decPart = '';
  if (commas && dots) {
    // The last separator is the decimal one; the other must group thousands.
    const dec = s.lastIndexOf(',') > s.lastIndexOf('.') ? ',' : '.';
    const thousands = dec === ',' ? '.' : ',';
    const idx = s.lastIndexOf(dec);
    const head = s.slice(0, idx);
    const grouped = new RegExp(`^\\d{1,3}(\\${thousands}\\d{3})*$`);
    if (s.indexOf(dec) !== idx || !grouped.test(head)) return NaN;
    intPart = head.split(thousands).join('');
    decPart = s.slice(idx + 1);
  } else if (commas) {
    if (commas > 1) {
      if (!/^\d{1,3}(,\d{3})+$/.test(s)) return NaN;
      intPart = s.replace(/,/g, '');
    } else [intPart, decPart] = s.split(',');
  } else if (dots) {
    if (/^\d{1,3}(\.\d{3})+$/.test(s)) intPart = s.replace(/\./g, ''); // 1.300 = thousands
    else if (dots > 1) return NaN;
    else [intPart, decPart] = s.split('.');
  }
  if (decPart.length > 2 || (intPart === '' && decPart === '')) return NaN;
  const cents = parseInt(intPart || '0', 10) * 100 + parseInt((decPart + '00').slice(0, 2), 10);
  if (!Number.isFinite(cents)) return NaN;
  return neg ? -cents : cents;
}

/** "7,5" → 7.5; empty → null; invalid → NaN. */
export function parseNumber(input) {
  const s = String(input ?? '').trim().replace(',', '.');
  if (!s) return null;
  if (!/^\d+(\.\d+)?$/.test(s)) return NaN;
  return parseFloat(s);
}

/** Cents as the text to put back into an input: 13000 → "130", 13050 → "130,50". */
export function centsToInput(cents) {
  if (cents == null) return '';
  const neg = cents < 0;
  const abs = Math.abs(cents);
  const s = abs % 100 === 0 ? String(abs / 100) : `${Math.floor(abs / 100)},${String(abs % 100).padStart(2, '0')}`;
  return neg ? '-' + s : s;
}

// ---------------------------------------------------------------- dates

export const MONTHS = ['gener', 'febrer', 'març', 'abril', 'maig', 'juny', 'juliol', 'agost', 'setembre', 'octubre', 'novembre', 'desembre'];
export const MONTHS_SHORT = ['gen', 'febr', 'març', 'abr', 'maig', 'juny', 'jul', 'ag', 'set', 'oct', 'nov', 'des'];

const pad = (n) => String(n).padStart(2, '0');

/** Local date as YYYY-MM-DD. */
export function isoDate(d = new Date()) {
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}
export const today = () => isoDate(new Date());

/** Current month as YYYY-MM. */
export const thisMonth = () => today().slice(0, 7);

/** "2026-09" shifted by n months. */
export function addMonths(ym, n) {
  const [y, m] = ym.split('-').map(Number);
  const d = new Date(y, m - 1 + n, 1);
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}`;
}

/** "2026-09-24" → "24/09/2026" */
export function dateNumeric(iso) {
  if (!iso) return '';
  const [y, m, d] = iso.slice(0, 10).split('-');
  return `${d}/${m}/${y}`;
}

/** "2026-09-24" → "24 set." (year added when not the current one) */
export function dateShort(iso) {
  if (!iso) return '';
  const [y, m, d] = iso.slice(0, 10).split('-').map(Number);
  const base = `${d} ${MONTHS_SHORT[m - 1]}`;
  return y === new Date().getFullYear() ? base : `${base} ${y}`;
}

/** Relative day for recent dates: "hui", "ahir", else dateShort. */
export function dateFriendly(iso) {
  if (!iso) return '';
  const t = today();
  if (iso.slice(0, 10) === t) return 'hui';
  const y = new Date();
  y.setDate(y.getDate() - 1);
  if (iso.slice(0, 10) === isoDate(y)) return 'ahir';
  return dateShort(iso);
}

/** "2026-09" → "setembre" (+ " 2026" when withYear). */
export function monthName(ym, withYear = false) {
  const [y, m] = ym.split('-').map(Number);
  return withYear ? `${MONTHS[m - 1]} ${y}` : MONTHS[m - 1];
}

/** "de setembre" / "d'agost": the preposition elides before a vowel. */
export function deMonth(ym) {
  const name = monthName(ym);
  return /^[aeiouàèéíòóú]/.test(name) ? `d'${name}` : `de ${name}`;
}

/** Timestamp → "24/09/2026 10:15" in local time. */
export function dateTime(ts) {
  const d = new Date(ts);
  return `${pad(d.getDate())}/${pad(d.getMonth() + 1)}/${d.getFullYear()} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

// ---------------------------------------------------------------- text

/** Accent-insensitive, case-insensitive search key. */
export function fold(s) {
  return String(s || '').normalize('NFD').replace(/[̀-ͯ]/g, '').replace(/·/g, '').toLowerCase().trim();
}

/** plural(3, 'partida', 'partides') → "3 partides" */
export function plural(n, one, many) {
  return `${n} ${n === 1 ? one : many}`;
}

/** Locale-aware sort for Valencian names (l'Alcúdia sorts under A). */
export function byName(a, b) {
  return sortKey(a).localeCompare(sortKey(b), 'ca');
}
function sortKey(s) {
  return String(s).replace(/^(l'|la |el |els |les )/i, '');
}
