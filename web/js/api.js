// HTTP client for the Meals API. Every call returns the decoded JSON or
// throws an ApiError whose message can be shown to the user.

import { ApiError } from './errors.js';
import { SyncEngine, newId } from './sync/engine.js';
import { EMPTY, fromSnapshot, plan, listRecipes, shopping, listAisles } from './sync/state.js';
import { reduce } from './sync/reduce.js';

export { ApiError };

/** Local data, kept in sync with the server (see sync/engine.js). */
export const engine = new SyncEngine({ dbName: 'meals', empty: EMPTY, fromSnapshot, reduce });

// Reads answer from the local state; errors become rejected promises, as
// with the network.
const local = (fn) => Promise.resolve().then(() => fn(engine.state));
const change = (type, args) => engine.mutate(type, args);

let unauthorizedHandler = () => {};

/** Called whenever the server says the session is no longer valid. */
export function onUnauthorized(fn) {
  unauthorizedHandler = fn;
}

async function request(method, path, body) {
  const options = { method, credentials: 'same-origin', headers: {} };
  if (body !== undefined) {
    options.headers['Content-Type'] = 'application/json';
    options.body = JSON.stringify(body);
  }

  let res;
  try {
    res = await fetch(path, options);
  } catch {
    throw new ApiError('Cannot reach the server. Check your connection.', 0);
  }

  let data = null;
  if ((res.headers.get('Content-Type') || '').includes('application/json')) {
    data = await res.json().catch(() => null);
  }
  if (!res.ok) {
    if (res.status === 401 && !path.startsWith('/api/auth/')) unauthorizedHandler();
    throw new ApiError(data?.error || `Request failed (${res.status})`, res.status);
  }
  return data;
}

const get = (path) => request('GET', path);
const post = (path, body = {}) => request('POST', path, body);

export const api = {
  auth: {
    status: () => get('/api/auth/status'),
    setup: (password) => post('/api/auth/setup', { password }),
    login: (password) => post('/api/auth/login', { password }),
    logout: () => post('/api/auth/logout'),
    changePassword: (oldPassword, newPassword) =>
      post('/api/auth/change-password', { old_password: oldPassword, new_password: newPassword }),
  },
  plan: {
    get: (today) => local((s) => plan(s, today)),
    setRecipe: (date, meal, recipeId, status) => change('slot.set_recipe', { date, meal, recipe_id: recipeId, status: status || '' }),
    setText: (date, meal, text) => change('slot.set_text', { date, meal, text }),
    clear: (date, meal) => change('slot.clear', { date, meal }),
    copyDay: (from, to) => change('plan.copy_day', { from, to }),
    validate: (today) => change('plan.validate', { today }),
  },
  recipes: {
    list: () => local((s) => listRecipes(s)),
    create: (recipe) => change('recipe.create', { ...recipe, id: newId() }),
    update: (recipeId, recipe) => change('recipe.update', { ...recipe, id: recipeId }),
    remove: (recipeId) => change('recipe.delete', { id: recipeId }),
  },
  shopping: {
    get: () => local((s) => shopping(s)),
    check: (name, checked) => change('shopping.check', { name, checked }),
    addExtra: (name) => change('shopping.add_extra', { id: newId(), name }),
    removeExtra: (extraId) => change('shopping.remove_extra', { id: extraId }),
    setAisle: (name, aisleId) => change('shopping.set_aisle', { name, aisle_id: aisleId }),
    done: () => change('shopping.done', {}),
  },
  aisles: {
    list: () => local((s) => listAisles(s)),
    create: (name) => change('aisle.create', { id: newId(), name }),
    rename: (aisleId, name) => change('aisle.rename', { id: aisleId, name }),
    remove: (aisleId) => change('aisle.delete', { id: aisleId }),
    reorder: (ids) => change('aisle.reorder', { ids }),
  },
};
