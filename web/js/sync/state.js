// Local state of the app, rebuilt from the server snapshot, and the read
// functions (the views) that replace the GET calls when working offline.
//
// State:
//   recipes:    { [id]: { id, title, notes, created_at, ingredients: [{ name, norm }] } }
//   slots:      { ['YYYY-MM-DD/lunch']: { date, meal, recipe_id, text, status } }
//   aisles:     [{ id, key, name, position }]   kept sorted by position
//   overrides:  { [norm]: aisleId }
//   checked:    { [norm]: true }
//   extras:     { [id]: { id, name, norm } }
//   dictionary: [{ words, key }]                most specific first
// Every read function returns new objects: callers may modify them freely.

import { ApiError } from '../errors.js';
import { classify } from '../aisle.js';

export const EMPTY = { recipes: [], slots: [], aisles: [], overrides: {}, checked: [], extras: [], dictionary: [] };

export const WINDOW_DAYS = 7;
export const MEALS = ['lunch', 'dinner'];

/** Turns a server snapshot into the local state. */
export function fromSnapshot(snap) {
  const state = {
    recipes: {}, slots: {}, aisles: [...snap.aisles].sort((a, b) => a.position - b.position),
    overrides: snap.overrides, checked: {}, extras: {}, dictionary: snap.dictionary,
  };
  for (const r of snap.recipes) state.recipes[r.id] = r;
  for (const s of snap.slots) state.slots[slotKey(s.date, s.meal)] = s;
  for (const norm of snap.checked) state.checked[norm] = true;
  for (const e of snap.extras) state.extras[e.id] = e;
  return state;
}

export const slotKey = (date, meal) => `${date}/${meal}`;
export const asciiLower = (s) => s.replace(/[A-Z]/g, (c) => c.toLowerCase());
const cmp = (a, b) => (a < b ? -1 : a > b ? 1 : 0);

/** Throws unless s is a real "YYYY-MM-DD" day. */
export function validDate(s) {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(typeof s === 'string' ? s : '');
  if (m) {
    const d = new Date(0);
    d.setUTCFullYear(Number(m[1]), Number(m[2]) - 1, Number(m[3]));
    if (d.getUTCFullYear() === Number(m[1]) && d.getUTCMonth() === Number(m[2]) - 1 && d.getUTCDate() === Number(m[3])) return;
  }
  throw new ApiError(`Invalid date ${JSON.stringify(s)}, expected YYYY-MM-DD`, 400);
}

/** The day n days after iso (both "YYYY-MM-DD"), computed in UTC. */
export function addDays(iso, n) {
  const [y, m, d] = iso.split('-').map(Number);
  const date = new Date(0);
  date.setUTCFullYear(y, m - 1, d + n);
  const pad = (v, w = 2) => String(v).padStart(w, '0');
  return `${pad(date.getUTCFullYear(), 4)}-${pad(date.getUTCMonth() + 1)}-${pad(date.getUTCDate())}`;
}

/** The rolling window of days starting at today, with the pending count. */
export function plan(state, today) {
  validDate(today);
  const days = [];
  for (let i = 0; i < WINDOW_DAYS; i++) days.push({ date: addDays(today, i), lunch: null, dinner: null });
  let pending = 0;
  for (const day of days) {
    for (const meal of MEALS) {
      const s = state.slots[slotKey(day.date, meal)];
      if (!s) continue;
      const out = { recipe_id: s.recipe_id ?? null, status: s.status };
      const title = s.recipe_id ? state.recipes[s.recipe_id]?.title ?? '' : '';
      if (title) out.title = title;
      if (s.text) out.text = s.text;
      day[meal] = out;
      if (s.recipe_id && s.status === 'pending') pending += 1;
    }
  }
  return { today, days, pending };
}

/** A recipe as the API returns it: ingredient names and the last planned day. */
export function recipeOut(state, r) {
  let last = null;
  for (const s of Object.values(state.slots)) {
    if (s.recipe_id === r.id && s.status !== 'pending' && (last === null || s.date > last)) last = s.date;
  }
  return { id: r.id, title: r.title, ingredients: r.ingredients.map((i) => i.name), notes: r.notes, last_planned: last };
}

/** Every recipe, never planned and longest ago first. */
export function listRecipes(state) {
  return Object.values(state.recipes).map((r) => recipeOut(state, r)).sort((a, b) =>
    (a.last_planned !== null) - (b.last_planned !== null)
    || cmp(a.last_planned ?? '', b.last_planned ?? '')
    || cmp(asciiLower(a.title), asciiLower(b.title))
    || cmp(a.id, b.id));
}

/** The shopping list: listed recipes and hand-written items, grouped by aisle. */
export function shopping(state) {
  const items = new Map();
  const get = (norm, name) => {
    if (!items.has(norm)) items.set(norm, { name, key: norm, checked: false, recipes: [] });
    return items.get(norm);
  };

  const planned = new Map(); // recipe id -> number of listed slots
  for (const s of Object.values(state.slots)) {
    if (s.status === 'listed' && s.recipe_id && state.recipes[s.recipe_id]) {
      planned.set(s.recipe_id, (planned.get(s.recipe_id) ?? 0) + 1);
    }
  }
  const recipes = [...planned.keys()].map((id) => state.recipes[id])
    .sort((a, b) => cmp(asciiLower(a.title), asciiLower(b.title)) || cmp(a.id, b.id));
  for (const r of recipes) {
    const names = new Map(); // norm -> smallest name
    for (const { name, norm } of r.ingredients) {
      if (!names.has(norm) || name < names.get(norm)) names.set(norm, name);
    }
    for (const [norm, name] of names) get(norm, name).recipes.push({ id: r.id, title: r.title, count: planned.get(r.id) });
  }
  for (const e of Object.values(state.extras).sort((a, b) => cmp(a.norm, b.norm))) get(e.norm, e.name).extra_id = e.id;

  const byId = new Map(state.aisles.map((a) => [a.id, a]));
  const byKey = new Map(state.aisles.filter((a) => a.key).map((a) => [a.key, a]));
  const resolve = (norm) => {
    const chosen = byId.get(state.overrides[norm]);
    if (chosen) return chosen;
    const key = classify(norm, state.dictionary);
    return (key !== null && byKey.get(key)) || byKey.get('other');
  };

  const groups = new Map();
  const out = { groups: [], total: 0, remaining: 0 };
  for (const [norm, item] of items) {
    item.checked = Boolean(state.checked[norm]);
    const aisle = resolve(norm);
    if (!aisle) continue; // no aisles yet: nothing has been downloaded
    if (!groups.has(aisle.id)) groups.set(aisle.id, { aisle_id: aisle.id, aisle: aisle.name, items: [] });
    groups.get(aisle.id).items.push(item);
    out.total += 1;
    if (!item.checked) out.remaining += 1;
  }
  for (const aisle of state.aisles) {
    const g = groups.get(aisle.id);
    if (!g) continue;
    g.items.sort((a, b) => (a.checked - b.checked) || cmp(a.key, b.key));
    out.groups.push(g);
  }
  return out;
}

/** The aisles in store order. */
export function listAisles(state) {
  return state.aisles.map((a) => (a.key ? { id: a.id, key: a.key, name: a.name } : { id: a.id, name: a.name }));
}
