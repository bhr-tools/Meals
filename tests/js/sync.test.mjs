// Local state, views and reducer of the offline mode.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { ApiError } from '../../web/js/errors.js';
import { fromSnapshot, plan, listRecipes, shopping, listAisles } from '../../web/js/sync/state.js';
import { reduce } from '../../web/js/sync/reduce.js';
import { normalize } from '../../web/js/aisle.js';

const today = '2026-10-07'; // a Wednesday
const uid = (n) => `00000000-0000-4000-8000-${String(n).padStart(12, '0')}`;
const R1 = uid(1);
const R2 = uid(2);

const ing = (name) => ({ name, norm: normalize(name) });

function makeState() {
  return fromSnapshot(structuredClone({
    recipes: [
      { id: R1, title: 'Gratin', notes: '', created_at: '2026-01-01 10:00:00', ingredients: [ing('Courgettes'), ing('Tomate'), ing('Crème')] },
      { id: R2, title: 'risotto', notes: 'stir', created_at: '2026-01-02 10:00:00', ingredients: [ing('Riz'), ing('courgette')] },
    ],
    slots: [
      { date: '2026-10-05', meal: 'dinner', recipe_id: R2, text: '', status: 'done' },
      { date: '2026-10-07', meal: 'lunch', recipe_id: R1, text: '', status: 'pending' },
      { date: '2026-10-07', meal: 'dinner', recipe_id: null, text: 'Restaurant', status: 'done' },
      { date: '2026-10-08', meal: 'lunch', recipe_id: R2, text: '', status: 'pending' },
    ],
    aisles: [
      { id: 'other', key: 'other', name: 'Other', position: 2 },
      { id: 'produce', key: 'produce', name: 'Produce', position: 0 },
      { id: 'pantry', key: 'pantry', name: 'Pantry', position: 1 },
    ],
    overrides: {},
    checked: [],
    extras: [],
    dictionary: [
      { words: ['courgette'], key: 'produce' }, { words: ['tomate'], key: 'produce' }, { words: ['riz'], key: 'pantry' },
    ],
  }));
}

const apply = (state, type, args) => reduce(state, { id: uid(99), type, args });
const groupsOf = (state) => shopping(state).groups.map((g) => [g.aisle_id, g.items.map((i) => i.key)]);

test('plan: seven days, pending count, text slots without title', () => {
  const s = makeState();
  const p = plan(s, today);
  assert.equal(p.days.length, 7);
  assert.equal(p.days[0].date, '2026-10-07');
  assert.equal(p.days[6].date, '2026-10-13');
  assert.equal(p.pending, 2);
  assert.deepEqual(p.days[0].lunch, { recipe_id: R1, status: 'pending', title: 'Gratin' });
  assert.deepEqual(p.days[0].dinner, { recipe_id: null, status: 'done', text: 'Restaurant' });
  assert.equal(p.days[2].lunch, null);
  assert.throws(() => plan(s, '2026-02-30'), (e) => e instanceof ApiError && e.status === 400);
  assert.equal(plan(makeState(), '2026-12-30').days[3].date, '2027-01-02');
});

test('slot.set_recipe keeps the status of the same recipe, restarts pending for another', () => {
  const s = makeState();
  apply(s, 'plan.validate', { today });
  assert.equal(s.slots['2026-10-07/lunch'].status, 'listed');
  apply(s, 'slot.set_recipe', { date: '2026-10-07', meal: 'lunch', recipe_id: R1, status: '' });
  assert.equal(s.slots['2026-10-07/lunch'].status, 'listed');
  apply(s, 'slot.set_recipe', { date: '2026-10-07', meal: 'lunch', recipe_id: R2 });
  assert.deepEqual(s.slots['2026-10-07/lunch'], { date: '2026-10-07', meal: 'lunch', recipe_id: R2, text: '', status: 'pending' });
  apply(s, 'slot.set_recipe', { date: '2026-10-09', meal: 'dinner', recipe_id: R1, status: 'done' });
  assert.equal(s.slots['2026-10-09/dinner'].status, 'done');
  assert.throws(() => apply(s, 'slot.set_recipe', { date: '2026-10-09', meal: 'brunch', recipe_id: R1 }), /Unknown meal/);
  assert.throws(() => apply(s, 'slot.set_recipe', { date: '2026-10-09', meal: 'lunch', recipe_id: uid(50) }), (e) => e.status === 404);
});

test('plan.validate fills the shopping list, grouped by aisle, with counts', () => {
  const s = makeState();
  apply(s, 'slot.set_recipe', { date: '2026-10-09', meal: 'lunch', recipe_id: R1 }); // Gratin twice
  assert.deepEqual(apply(s, 'plan.validate', { today }), { validated: 3 });
  assert.deepEqual(groupsOf(s), [['produce', ['courgette', 'tomate']], ['pantry', ['riz']], ['other', ['creme']]]);
  const list = shopping(s);
  assert.equal(list.total, 4);
  assert.equal(list.remaining, 4);
  const courgette = list.groups[0].items[0];
  assert.equal(courgette.name, 'Courgettes'); // from the first recipe by title
  assert.deepEqual(courgette.recipes, [{ id: R1, title: 'Gratin', count: 2 }, { id: R2, title: 'risotto', count: 1 }]);
  assert.equal('extra_id' in courgette, false);
  assert.deepEqual(list.groups[1], { aisle_id: 'pantry', aisle: 'Pantry', items: list.groups[1].items });
});

test('shopping.check moves an item to the end of its group', () => {
  const s = makeState();
  apply(s, 'plan.validate', { today });
  apply(s, 'shopping.check', { name: 'Courgettes', checked: true });
  assert.deepEqual(groupsOf(s)[0], ['produce', ['tomate', 'courgette']]);
  assert.equal(shopping(s).remaining, 3);
  apply(s, 'shopping.check', { name: 'courgette', checked: false });
  assert.equal(shopping(s).remaining, 4);
  assert.throws(() => apply(s, 'shopping.check', { name: '  ', checked: true }), /cannot be empty/);
});

test('shopping.add_extra: new item, or the extra id on a listed one', () => {
  const s = makeState();
  apply(s, 'plan.validate', { today });
  apply(s, 'shopping.add_extra', { id: uid(10), name: ' courgettes ' });
  assert.equal(shopping(s).total, 4);
  assert.equal(shopping(s).groups[0].items.find((i) => i.key === 'courgette').extra_id, uid(10));
  apply(s, 'shopping.add_extra', { id: uid(11), name: 'Piles' });
  const other = shopping(s).groups.find((g) => g.aisle_id === 'other');
  const piles = other.items.find((i) => i.key === 'pile');
  assert.deepEqual(piles, { name: 'Piles', key: 'pile', checked: false, recipes: [], extra_id: uid(11) });
  apply(s, 'shopping.add_extra', { id: uid(12), name: 'pile' }); // same item
  assert.equal(Object.keys(s.extras).length, 2);
  apply(s, 'shopping.remove_extra', { id: uid(11) });
  assert.equal(shopping(s).total, 4);
  apply(s, 'shopping.remove_extra', { id: uid(11) }); // absent: nothing
});

test('shopping.set_aisle moves an item; deleting the aisle brings it back', () => {
  const s = makeState();
  apply(s, 'plan.validate', { today });
  apply(s, 'shopping.set_aisle', { name: 'Courgettes', aisle_id: 'pantry' });
  assert.deepEqual(groupsOf(s), [['produce', ['tomate']], ['pantry', ['courgette', 'riz']], ['other', ['creme']]]);
  apply(s, 'aisle.delete', { id: 'pantry' });
  assert.deepEqual(groupsOf(s), [['produce', ['courgette', 'tomate']], ['other', ['creme', 'riz']]]);
  assert.deepEqual(s.overrides, {});
  apply(s, 'shopping.set_aisle', { name: 'Riz', aisle_id: 'produce' });
  apply(s, 'shopping.set_aisle', { name: 'Riz', aisle_id: '' });
  assert.deepEqual(s.overrides, {});
  assert.throws(() => apply(s, 'shopping.set_aisle', { name: 'Riz', aisle_id: 'nope' }), (e) => e.status === 404);
});

test('shopping.done empties the list and dates the recipes', () => {
  const s = makeState();
  assert.equal(listRecipes(s).find((r) => r.id === R1).last_planned, null);
  apply(s, 'plan.validate', { today });
  apply(s, 'shopping.check', { name: 'riz', checked: true });
  apply(s, 'shopping.add_extra', { id: uid(10), name: 'Piles' });
  apply(s, 'shopping.done', {});
  assert.deepEqual(shopping(s), { groups: [], total: 0, remaining: 0 });
  assert.equal(s.slots['2026-10-07/lunch'].status, 'done');
  assert.deepEqual(s.checked, {});
  assert.deepEqual(s.extras, {});
  const recipes = listRecipes(s);
  assert.equal(recipes.find((r) => r.id === R1).last_planned, '2026-10-07');
  assert.equal(recipes.find((r) => r.id === R2).last_planned, '2026-10-08');
});

test('listRecipes: never planned first, then longest ago, then by title', () => {
  const s = makeState();
  assert.deepEqual(listRecipes(s).map((r) => r.id), [R1, R2]); // R1 never planned (pending only)
  apply(s, 'recipe.create', { id: uid(3), title: 'apple pie', ingredients: ['Pommes', 'pomme', ' '], notes: 'n' });
  apply(s, 'recipe.create', { id: uid(4), title: 'Zucchini', ingredients: [] });
  assert.deepEqual(listRecipes(s).map((r) => r.title), ['apple pie', 'Gratin', 'Zucchini', 'risotto']);
  assert.deepEqual(listRecipes(s)[0], { id: uid(3), title: 'apple pie', ingredients: ['Pommes'], notes: 'n', last_planned: null });
});

test('recipes: create is idempotent, update replaces, delete removes the slots', () => {
  const s = makeState();
  const created = apply(s, 'recipe.create', { id: uid(3), title: ' Soup ', ingredients: ['Leek', 'leeks'] });
  assert.deepEqual(created, { id: uid(3), title: 'Soup', ingredients: ['Leek'], notes: '', last_planned: null });
  apply(s, 'recipe.create', { id: uid(3), title: 'Other title' });
  assert.equal(s.recipes[uid(3)].title, 'Soup');
  const updated = apply(s, 'recipe.update', { id: uid(3), title: 'Soup 2', ingredients: ['Carrots'], notes: 'x' });
  assert.deepEqual(updated.ingredients, ['Carrots']);
  assert.throws(() => apply(s, 'recipe.update', { id: uid(77), title: 'x' }), (e) => e.status === 404);
  apply(s, 'recipe.delete', { id: R1 });
  assert.equal(s.recipes[R1], undefined);
  assert.equal(plan(s, today).days[0].lunch, null);
  assert.equal(plan(s, today).pending, 1);
});

test('slots: text, clear, copy_day', () => {
  const s = makeState();
  apply(s, 'slot.set_text', { date: '2026-10-09', meal: 'lunch', text: '  leftovers   again ' });
  assert.deepEqual(s.slots['2026-10-09/lunch'], { date: '2026-10-09', meal: 'lunch', recipe_id: null, text: 'leftovers again', status: 'done' });
  apply(s, 'plan.validate', { today });
  apply(s, 'plan.copy_day', { from: '2026-10-07', to: '2026-10-10' });
  assert.equal(s.slots['2026-10-10/lunch'].status, 'pending'); // copied recipes start pending
  assert.equal(s.slots['2026-10-10/dinner'].text, 'Restaurant');
  apply(s, 'plan.copy_day', { from: '2026-10-09', to: '2026-10-10' }); // replaces the day
  assert.equal(s.slots['2026-10-10/lunch'].text, 'leftovers again');
  assert.equal(s.slots['2026-10-10/dinner'], undefined);
  apply(s, 'slot.clear', { date: '2026-10-10', meal: 'lunch' });
  apply(s, 'slot.clear', { date: '2026-10-10', meal: 'lunch' }); // clearing an empty slot is fine
  assert.equal(s.slots['2026-10-10/lunch'], undefined);
  assert.throws(() => apply(s, 'slot.set_text', { date: '2026-10-09', meal: 'lunch', text: ' ' }), /cannot be empty/);
});

test('aisles: create before Other, rename, reorder, delete', () => {
  const s = makeState();
  assert.deepEqual(apply(s, 'aisle.create', { id: uid(5), name: ' Butcher  shop ' }), { id: uid(5), name: 'Butcher shop' });
  assert.deepEqual(listAisles(s).map((a) => a.id), ['produce', 'pantry', uid(5), 'other']);
  assert.deepEqual(listAisles(s)[0], { id: 'produce', key: 'produce', name: 'Produce' });
  assert.deepEqual(listAisles(s)[2], { id: uid(5), name: 'Butcher shop' }); // no key
  assert.deepEqual(s.aisles.map((a) => a.position), [0, 1, 2, 3]);
  apply(s, 'aisle.rename', { id: 'produce', name: 'Veg' });
  apply(s, 'aisle.reorder', { ids: ['other', 'produce', 'pantry', uid(5)] });
  assert.deepEqual(listAisles(s).map((a) => a.id), ['other', 'produce', 'pantry', uid(5)]);
  assert.throws(() => apply(s, 'aisle.reorder', { ids: ['other'] }), /list every aisle/);
  assert.throws(() => apply(s, 'aisle.reorder', { ids: ['other', 'other', 'pantry', uid(5)] }), /exactly once/);
  assert.throws(() => apply(s, 'aisle.delete', { id: 'other' }), /Other aisle cannot be deleted/);
  apply(s, 'aisle.delete', { id: uid(5) });
  assert.equal(s.aisles.length, 3);
});

test('an invalid change throws and leaves the state untouched', () => {
  const s = makeState();
  const before = structuredClone(s);
  for (const [type, args] of [
    ['recipe.create', { id: uid(3), title: '  ' }],
    ['recipe.create', { id: 'x', title: 'T' }],
    ['recipe.create', { title: 'T' }],
    ['recipe.create', { id: uid(3), title: 'T', ingredients: 'nope' }],
    ['recipe.update', { id: R1, title: 'x'.repeat(201) }],
    ['slot.set_text', { date: '2026-13-01', meal: 'lunch', text: 'x' }],
    ['slot.set_recipe', { date: today, meal: 'lunch', recipe_id: R1, status: 'bogus' }],
    ['slot.set_recipe', { date: today, meal: 'lunch', recipe_id: uid(50) }],
    ['plan.validate', { today: 'tomorrow' }],
    ['shopping.add_extra', { id: uid(8), name: '' }],
    ['aisle.create', { id: uid(8), name: 'x'.repeat(41) }],
    ['meal.fly', {}],
  ]) {
    assert.throws(() => apply(s, type, args), (e) => e instanceof ApiError && [400, 404].includes(e.status), type);
    assert.deepEqual(s, before, type);
  }
});
