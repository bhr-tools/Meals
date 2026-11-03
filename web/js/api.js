// HTTP client for the Meals API. Every call returns the decoded JSON or
// throws an ApiError whose message can be shown to the user.

export class ApiError extends Error {
  constructor(message, status) {
    super(message);
    this.status = status;
  }
}

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
const put = (path, body) => request('PUT', path, body);
const patch = (path, body) => request('PATCH', path, body);
const del = (path) => request('DELETE', path);
const id = encodeURIComponent;

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
    get: (today) => get(`/api/plan?today=${id(today)}`),
    setRecipe: (date, meal, recipeId, status) => put(`/api/slots/${id(date)}/${id(meal)}`, { recipe_id: recipeId, status }),
    setText: (date, meal, text) => put(`/api/slots/${id(date)}/${id(meal)}`, { text }),
    clear: (date, meal) => del(`/api/slots/${id(date)}/${id(meal)}`),
    copyDay: (from, to) => post('/api/plan/copy-day', { from, to }),
    validate: (today) => post('/api/plan/validate', { today }),
  },
  recipes: {
    list: () => get('/api/recipes'),
    create: (recipe) => post('/api/recipes', recipe),
    update: (recipeId, recipe) => put(`/api/recipes/${id(recipeId)}`, recipe),
    remove: (recipeId) => del(`/api/recipes/${id(recipeId)}`),
  },
  shopping: {
    get: () => get('/api/shopping'),
    check: (name, checked) => put('/api/shopping/checked', { name, checked }),
    addExtra: (name) => post('/api/shopping/extra', { name }),
    removeExtra: (extraId) => del(`/api/shopping/extra/${id(extraId)}`),
    setAisle: (name, aisleId) => put('/api/shopping/aisle', { name, aisle_id: aisleId }),
    done: () => post('/api/shopping/done'),
  },
  aisles: {
    list: () => get('/api/aisles'),
    create: (name) => post('/api/aisles', { name }),
    rename: (aisleId, name) => patch(`/api/aisles/${id(aisleId)}`, { name }),
    remove: (aisleId) => del(`/api/aisles/${id(aisleId)}`),
    reorder: (ids) => put('/api/aisles/order', { ids }),
  },
};
