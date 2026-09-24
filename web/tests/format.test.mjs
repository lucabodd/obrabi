// Unit tests for the frontend formatting helpers: `node --test web/tests`.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  parseMoney, parseNumber, centsToInput, money, compact, deMonth, addMonths, fold, byName,
} from '../static/js/format.js';

test('parseMoney understands what people type', () => {
  const cases = {
    '130': 13000,
    '130,5': 13050,
    '130,50': 13050,
    '130.5': 13050,
    '1.300': 130000,
    '1.300,50': 130050,
    '1,300.50': 130050,
    '12.345.678': 1234567800,
    ' 99 € ': 9900,
    '0,05': 5,
    '-20': -2000,
  };
  for (const [input, cents] of Object.entries(cases)) {
    assert.equal(parseMoney(input), cents, `parseMoney(${JSON.stringify(input)})`);
  }
  assert.equal(parseMoney(''), null);
  assert.equal(parseMoney('   '), null);
  // Malformed input, and three decimals (rejected rather than silently
  // rounded), are invalid.
  for (const bad of ['abc', '1,234,5.6.7', '1.2.3', '12,345', '3,141', '12.34,5.6', '1,2,3']) {
    assert.ok(Number.isNaN(parseMoney(bad)), bad);
  }
});

test('parseNumber accepts a decimal comma', () => {
  assert.equal(parseNumber('7,5'), 7.5);
  assert.equal(parseNumber('8'), 8);
  assert.equal(parseNumber(''), null);
  assert.ok(Number.isNaN(parseNumber('7,5h')));
});

test('centsToInput round-trips through parseMoney', () => {
  for (const cents of [0, 5, 13000, 13050, 123456789]) {
    assert.equal(parseMoney(centsToInput(cents)), cents);
  }
  assert.equal(centsToInput(13000), '130');
  assert.equal(centsToInput(13050), '130,50');
});

test('money uses Valencian conventions', () => {
  const s = money(123456).replace(/ | /g, ' ');
  assert.equal(s, '1.234,56 €');
});

test('compact axis labels', () => {
  assert.equal(compact(80000), '800');
  assert.equal(compact(150000), '1,5k');
  assert.equal(compact(-300000), '-3k');
});

test('month helpers', () => {
  assert.equal(deMonth('2026-08'), "d'agost");
  assert.equal(deMonth('2026-09'), 'de setembre');
  assert.equal(deMonth('2026-10'), "d'octubre");
  assert.equal(addMonths('2026-01', -1), '2025-12');
  assert.equal(addMonths('2026-09', -11), '2025-10');
});

test('search folding and Valencian sorting', () => {
  assert.equal(fold('Xàtiva'), 'xativa');
  assert.equal(fold('Instal·lació'), 'installacio');
  const towns = ['Xàtiva', "l'Alcúdia", 'Rafelguaraf', 'Alzira', 'la Pobla Llarga'];
  assert.deepEqual([...towns].sort(byName), ["l'Alcúdia", 'Alzira', 'la Pobla Llarga', 'Rafelguaraf', 'Xàtiva']);
});
