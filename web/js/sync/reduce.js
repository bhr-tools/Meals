// Applies changes to the local state as the server will (a port of the store
// methods in internal/db). The server stays the authority: when this code
// and the server disagree on a detail, the next snapshot corrects it.

import { ApiError } from '../errors.js';
import { normalize } from '../aisle.js';
import { MEALS, slotKey, validDate, recipeOut } from './state.js';

const MAX_TITLE = 200;
const MAX_INGREDIENT = 100;
const MAX_INGREDIENTS = 100;
const MAX_NOTES = 20000;
const MAX_SLOT_TEXT = 200;
const MAX_AISLE_NAME = 40;
const STATUSES = ['pending', 'listed', 'done'];
const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

/**
 * Applies one change to the local state, as the server will, and returns
 * what the matching API call returns. Validates everything before changing
 * anything; throws an ApiError (400 or 404) when the change cannot apply.
 */
export function reduce(state, op) {
  const fn = Object.hasOwn(handlers, op.type) ? handlers[op.type] : null;
  if (!fn) throw new ApiError(`Unknown operation ${op.type}`, 400);
  return fn(state, op.args ?? {});
}

// ---- Errors and validation -------------------------------------------------

const invalid = (msg) => new ApiError(msg, 400);
const notFound = () => new ApiError('Not found', 404);
const nowISO = () => new Date().toISOString();
const runes = (s) => [...s].length;
const OK = () => ({ ok: true });

/** A string argument; anything else is malformed. */
function text(v) {
  if (v == null) return '';
  if (typeof v !== 'string') throw invalid('Invalid arguments');
  return v;
}

/** Collapses runs of white space and trims, like strings.Join(strings.Fields(s), " "). */
const fields = (s) => text(s).split(/\s+/).filter(Boolean).join(' ');

/** The id of a created record: required here, since replaying must give the same id. */
function createdId(v) {
  const id = text(v).trim();
  if (id === '') throw invalid('Missing id');
  if (!UUID_RE.test(id)) throw invalid('invalid id');
  return id.toLowerCase();
}

function validMeal(meal) {
  if (!MEALS.includes(meal)) throw invalid(`Unknown meal ${JSON.stringify(meal)}`);
}

/** Validates a recipe submission; returns its title, notes and ingredients without blanks or duplicates. */
function cleanRecipe(a) {
  const title = text(a.title).trim();
  if (title === '') throw invalid('The title cannot be empty');
  if (runes(title) > MAX_TITLE) throw invalid(`The title is too long (max ${MAX_TITLE} characters)`);
  const notes = text(a.notes);
  if (runes(notes) > MAX_NOTES) throw invalid(`The notes are too long (max ${MAX_NOTES} characters)`);
  if (a.ingredients != null && !Array.isArray(a.ingredients)) throw invalid('Invalid arguments');
  const ingredients = [];
  const seen = new Set();
  for (const line of a.ingredients ?? []) {
    const name = fields(line);
    const norm = normalize(name);
    if (norm === '' || seen.has(norm)) continue;
    if (runes(name) > MAX_INGREDIENT) throw invalid(`An ingredient is too long (max ${MAX_INGREDIENT} characters)`);
    seen.add(norm);
    ingredients.push({ name, norm });
  }
  if (ingredients.length > MAX_INGREDIENTS) throw invalid(`Too many ingredients (max ${MAX_INGREDIENTS})`);
  return { title, notes, ingredients };
}

function cleanAisleName(name) {
  name = fields(name);
  if (name === '') throw invalid('The aisle name cannot be empty');
  if (runes(name) > MAX_AISLE_NAME) throw invalid(`The aisle name is too long (max ${MAX_AISLE_NAME} characters)`);
  return name;
}

function normOf(name) {
  const norm = normalize(text(name));
  if (norm === '') throw invalid('The name cannot be empty');
  return norm;
}

const aisleOut = (a) => ({ id: a.id, name: a.name });

const handlers = {
  // ---- Plan ----

  'slot.set_recipe'(state, a) {
    validDate(a.date);
    validMeal(a.meal);
    const status = text(a.status);
    if (status !== '' && !STATUSES.includes(status)) throw invalid(`Unknown status ${JSON.stringify(status)}`);
    const recipeId = text(a.recipe_id);
    if (!state.recipes[recipeId]) throw notFound();
    const key = slotKey(a.date, a.meal);
    // Choosing the recipe already there changes nothing; a different one starts pending.
    if (state.slots[key]?.recipe_id !== recipeId) {
      state.slots[key] = { date: a.date, meal: a.meal, recipe_id: recipeId, text: '', status: 'pending' };
    }
    if (status !== '') state.slots[key].status = status;
    return OK();
  },

  'slot.set_text'(state, a) {
    validDate(a.date);
    validMeal(a.meal);
    const t = fields(a.text);
    if (t === '') throw invalid('The text cannot be empty');
    if (runes(t) > MAX_SLOT_TEXT) throw invalid(`The text is too long (max ${MAX_SLOT_TEXT} characters)`);
    state.slots[slotKey(a.date, a.meal)] = { date: a.date, meal: a.meal, recipe_id: null, text: t, status: 'done' };
    return OK();
  },

  'slot.clear'(state, a) {
    validDate(a.date);
    validMeal(a.meal);
    delete state.slots[slotKey(a.date, a.meal)];
    return OK();
  },

  'plan.copy_day'(state, a) {
    validDate(a.from);
    validDate(a.to);
    if (a.from === a.to) return OK();
    const copies = MEALS.map((m) => state.slots[slotKey(a.from, m)]).filter(Boolean)
      .map((s) => ({ ...s, date: a.to, status: s.recipe_id ? 'pending' : 'done' }));
    for (const m of MEALS) delete state.slots[slotKey(a.to, m)];
    for (const s of copies) state.slots[slotKey(s.date, s.meal)] = s;
    return OK();
  },

  'plan.validate'(state, a) {
    validDate(a.today);
    let validated = 0;
    for (const s of Object.values(state.slots)) {
      if (s.status === 'pending' && s.recipe_id && s.date >= a.today) {
        s.status = 'listed';
        validated += 1;
      }
    }
    return { validated };
  },

  // ---- Recipes ----

  'recipe.create'(state, a) {
    const { title, notes, ingredients } = cleanRecipe(a);
    const id = createdId(a.id);
    if (!state.recipes[id]) state.recipes[id] = { id, title, notes, created_at: nowISO(), ingredients };
    return recipeOut(state, state.recipes[id]);
  },

  'recipe.update'(state, a) {
    const { title, notes, ingredients } = cleanRecipe(a);
    const r = state.recipes[text(a.id)];
    if (!r) throw notFound();
    Object.assign(r, { title, notes, ingredients });
    return recipeOut(state, r);
  },

  'recipe.delete'(state, a) {
    if (!state.recipes[a.id]) throw notFound();
    delete state.recipes[a.id];
    for (const s of Object.values(state.slots)) {
      if (s.recipe_id === a.id) delete state.slots[slotKey(s.date, s.meal)];
    }
    return OK();
  },

  // ---- Shopping ----

  'shopping.check'(state, a) {
    const norm = normOf(a.name);
    if (a.checked) state.checked[norm] = true;
    else delete state.checked[norm];
    return OK();
  },

  'shopping.add_extra'(state, a) {
    const name = fields(a.name);
    const norm = normalize(name);
    if (norm === '') throw invalid('The name cannot be empty');
    if (runes(name) > MAX_INGREDIENT) throw invalid(`The name is too long (max ${MAX_INGREDIENT} characters)`);
    const id = createdId(a.id);
    // The same normalized name is the same item.
    if (!state.extras[id] && !Object.values(state.extras).some((e) => e.norm === norm)) {
      state.extras[id] = { id, name, norm };
    }
    return OK();
  },

  'shopping.remove_extra'(state, a) {
    delete state.extras[text(a.id)];
    return OK();
  },

  'shopping.set_aisle'(state, a) {
    const norm = normOf(a.name);
    const aisleId = text(a.aisle_id);
    if (aisleId === '') {
      delete state.overrides[norm];
    } else {
      if (!state.aisles.some((x) => x.id === aisleId)) throw notFound();
      state.overrides[norm] = aisleId;
    }
    return OK();
  },

  'shopping.done'(state) {
    for (const s of Object.values(state.slots)) if (s.status === 'listed') s.status = 'done';
    state.checked = {};
    state.extras = {};
    return OK();
  },

  // ---- Aisles ----

  'aisle.create'(state, a) {
    const name = cleanAisleName(a.name);
    const id = createdId(a.id);
    const existing = state.aisles.find((x) => x.id === id);
    if (existing) return aisleOut(existing);
    // Just before Other: Other and everything after it move down.
    const other = state.aisles.find((x) => x.key === 'other');
    const position = other ? other.position : Math.max(-1, ...state.aisles.map((x) => x.position)) + 1;
    for (const x of state.aisles) if (x.position >= position) x.position += 1;
    const aisle = { id, key: '', name, position };
    state.aisles.push(aisle);
    state.aisles.sort((x, y) => x.position - y.position);
    return aisleOut(aisle);
  },

  'aisle.rename'(state, a) {
    const name = cleanAisleName(a.name);
    const aisle = state.aisles.find((x) => x.id === a.id);
    if (!aisle) throw notFound();
    aisle.name = name;
    return OK();
  },

  'aisle.delete'(state, a) {
    const aisle = state.aisles.find((x) => x.id === a.id);
    if (!aisle) throw notFound();
    if (aisle.key === 'other') throw invalid('The Other aisle cannot be deleted');
    state.aisles = state.aisles.filter((x) => x !== aisle);
    for (const [norm, id] of Object.entries(state.overrides)) if (id === aisle.id) delete state.overrides[norm];
    return OK();
  },

  'aisle.reorder'(state, a) {
    const ids = a.ids ?? [];
    if (!Array.isArray(ids) || ids.length !== state.aisles.length) throw invalid('The new order must list every aisle');
    const known = new Set(state.aisles.map((x) => x.id));
    for (const id of ids) {
      if (!known.delete(id)) throw invalid('The new order must list every aisle exactly once');
    }
    const byId = new Map(state.aisles.map((x) => [x.id, x]));
    ids.forEach((id, i) => { byId.get(id).position = i; });
    state.aisles.sort((x, y) => x.position - y.position);
    return OK();
  },
};
