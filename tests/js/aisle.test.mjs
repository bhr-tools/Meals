// The JavaScript port of Normalize and Classify must agree with the Go code:
// both are checked against tests/fixtures/normalize.json.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { normalize, classify } from '../../web/js/aisle.js';

const fixture = JSON.parse(readFileSync(new URL('../fixtures/normalize.json', import.meta.url)));

test('normalize matches the Go reference', () => {
  for (const c of fixture.normalize) assert.equal(normalize(c.name), c.want, JSON.stringify(c.name));
});

test('classify matches the Go reference', () => {
  for (const c of fixture.classify) {
    assert.equal(classify(normalize(c.name), fixture.dictionary) ?? '', c.key, c.name);
  }
});

test('classify: the most specific keyword wins, whole words only', () => {
  const dictionary = [
    { words: ['creme', 'fraiche'], key: 'dairy' },
    { words: ['creme'], key: 'pantry' },
    { words: ['riz'], key: 'pantry' },
  ];
  assert.equal(classify('creme fraiche epaisse', dictionary), 'dairy');
  assert.equal(classify('creme de marron', dictionary), 'pantry');
  assert.equal(classify('rizotto', dictionary), null);
  assert.equal(classify('', dictionary), null);
});
