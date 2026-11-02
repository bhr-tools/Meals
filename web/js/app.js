// Meals: application shell. It owns the data, the three views (Plan, Recipes,
// Shopping), the cursor shared by their rows and the Vim-style keys.

import { api, ApiError, onUnauthorized } from './api.js';
import { h, icon, clear, isTextInput } from './dom.js';
import { openModal, openMenu, closeAllModals, topModal, toast, escapeTopLayer, isPopoverOpen } from './ui/overlay.js';
import { todayISO } from './dates.js';
import { planView } from './views/plan.js';
import { recipesView } from './views/recipes.js';
import { shoppingView } from './views/shopping.js';

const $ = (id) => document.getElementById(id);

const TABS = [
  { id: 'plan', label: 'Plan', icon: 'plan', keys: ['1', '&'] },
  { id: 'recipes', label: 'Recipes', icon: 'recipes', keys: ['2', 'é'] },
  { id: 'shopping', label: 'Shopping', icon: 'shopping', keys: ['3', '"'] },
];

class App {
  constructor() {
    const fromHash = location.hash.slice(1);
    this.state = {
      view: TABS.some((t) => t.id === fromHash) ? fromHash : 'plan',
      today: todayISO(),
      plan: null,
      recipes: [],
      shopping: { groups: [], total: 0, remaining: 0 },
      aisles: [],
      auth: { no_auth: false, has_password: true, authenticated: false },
    };
    this.rows = []; // { key, el } of the current view, in cursor order
    this.cursor = 0;
    this.pendingG = 0;
    this.views = { plan: planView(this), recipes: recipesView(this), shopping: shoppingView(this) };
  }

  // -------------------------------------------------------------------------
  // Boot and data
  // -------------------------------------------------------------------------

  async boot() {
    $('brand').append(icon('brand'), 'Meals');
    this.buildNav();
    onUnauthorized(() => this.showLogin());
    window.addEventListener('keydown', (e) => this.onKey(e));
    window.addEventListener('hashchange', () => {
      const id = location.hash.slice(1);
      if (TABS.some((t) => t.id === id) && id !== this.state.view) this.go(id);
    });
    document.addEventListener('visibilitychange', () => document.visibilityState === 'visible' && this.tick());
    window.addEventListener('focus', () => this.tick());
    setInterval(() => this.tick(), 60_000);
    document.addEventListener('focusin', () => this.renderStatus());
    document.addEventListener('focusout', () => setTimeout(() => this.renderStatus(), 0));

    let status;
    try {
      status = await api.auth.status();
    } catch (err) {
      return this.showFatal(err);
    }
    this.state.auth = status;
    $('auth-menu').hidden = status.no_auth;
    if (!status.no_auth && !status.has_password) return this.showSetup();
    if (!status.authenticated) return this.showLogin();
    await this.afterLogin();
  }

  /** Reloads when the day changes (the window slides) or the tab regains focus. */
  async tick() {
    if (!this.state.auth.authenticated || topModal()) return;
    this.state.today = todayISO();
    await this.refresh();
  }

  /** Loads everything the views and the badges need, then redraws. */
  async refresh() {
    try {
      const [plan, recipes, shopping] = await Promise.all([
        api.plan.get(this.state.today), api.recipes.list(), api.shopping.get(),
      ]);
      Object.assign(this.state, { plan, recipes, shopping });
    } catch (err) {
      if (!(err instanceof ApiError && err.status === 401)) toast(err.message, { type: 'error' });
      return;
    }
    this.render();
  }

  async loadAisles() {
    this.state.aisles = await api.aisles.list();
  }

  /** Runs an API call; shows its error as a toast. Returns undefined on failure. */
  async run(fn) {
    try {
      return await fn();
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) return undefined; // the login dialog is shown
      toast(err.message, { type: 'error' });
      return undefined;
    }
  }

  // -------------------------------------------------------------------------
  // Rendering
  // -------------------------------------------------------------------------

  get view() {
    return this.views[this.state.view];
  }

  go(id) {
    if (id === this.state.view) return;
    this.state.view = id;
    this.cursor = 0;
    history.replaceState(null, '', `#${id}`);
    this.render();
    $('main').scrollTop = 0;
  }

  buildNav() {
    const nav = clear($('nav'));
    const tabbar = clear($('tabbar'));
    for (const tab of TABS) {
      nav.append(h('button', { type: 'button', class: 'nav-item', dataset: { tab: tab.id }, onclick: () => this.go(tab.id) },
        icon(tab.icon), h('span', { class: 'nav-label' }, tab.label), h('span', { class: 'nav-count' })));
      tabbar.append(h('button', { type: 'button', class: 'tab', dataset: { tab: tab.id }, onclick: () => this.go(tab.id) },
        tab.label, h('span', { class: 'badge' })));
    }
  }

  badges() {
    const { plan, shopping } = this.state;
    return { plan: plan?.pending || 0, recipes: 0, shopping: shopping.remaining };
  }

  renderNav() {
    const counts = this.badges();
    for (const tab of TABS) {
      const n = counts[tab.id];
      for (const el of document.querySelectorAll(`[data-tab="${tab.id}"]`)) {
        el.classList.toggle('active', tab.id === this.state.view);
        const badge = el.querySelector('.nav-count, .badge');
        badge.textContent = n ? String(n) : '';
        badge.classList.toggle('alert', tab.id === 'plan' && n > 0);
      }
    }
  }

  render() {
    const view = this.view;
    this.renderNav();
    $('view-title').textContent = view.title;
    $('view-title').append(view.subtitle ? h('span', { class: 'view-sub' }, view.subtitle()) : '');
    clear($('header-actions')).append(...view.actions());
    const { el, rows } = view.render();
    clear($('content')).append(el);
    this.setRows(rows);
    this.renderHints();
  }

  /** Registers the rows of the view; the cursor stays on the same row when it still exists. */
  setRows(rows) {
    const previous = this.rows[this.cursor]?.key;
    this.rows = rows;
    const same = rows.findIndex((r) => r.key === previous);
    this.cursor = same >= 0 ? same : Math.min(this.cursor, Math.max(0, rows.length - 1));
    this.paintCursor(false);
  }

  paintCursor(scroll = true) {
    this.rows.forEach((r, i) => r.el.classList.toggle('cursor', i === this.cursor));
    if (scroll) this.rows[this.cursor]?.el.scrollIntoView({ block: 'nearest' });
    this.renderStatus();
  }

  moveCursor(delta) {
    if (!this.rows.length) return;
    this.cursor = Math.max(0, Math.min(this.rows.length - 1, this.cursor + delta));
    this.paintCursor();
  }

  moveCursorTo(i) {
    if (!this.rows.length) return;
    this.cursor = i < 0 ? this.rows.length + i : Math.min(i, this.rows.length - 1);
    this.paintCursor();
  }

  /** Puts the cursor on the row with this key (after a click). */
  select(key) {
    const i = this.rows.findIndex((r) => r.key === key);
    if (i >= 0) {
      this.cursor = i;
      this.paintCursor(false);
    }
  }

  cursorRow() {
    return this.rows[this.cursor] || null;
  }

  renderStatus() {
    const typing = isTextInput(document.activeElement);
    const mode = $('status-mode');
    mode.textContent = typing ? '-- INSERT --' : '-- NORMAL --';
    mode.className = `status-mode ${typing ? 'insert' : 'normal'}`;
    $('status-position').textContent = this.rows.length ? `${Math.min(this.cursor + 1, this.rows.length)} / ${this.rows.length}` : '';
  }

  renderHints() {
    const hints = [...this.view.hints, [['?'], 'help']];
    clear($('status-hints')).append(...hints.map(([keys, label]) =>
      h('span', {}, keys.map((k) => h('kbd', {}, k)), ' ', label)));
  }

  // -------------------------------------------------------------------------
  // Keys
  // -------------------------------------------------------------------------

  onKey(e) {
    if (e.defaultPrevented || e.isComposing) return;
    if (e.key === 'Escape') {
      if (escapeTopLayer()) {
        e.preventDefault();
        return;
      }
      if (isTextInput(document.activeElement)) {
        e.preventDefault();
        document.activeElement.blur();
        return;
      }
    }
    if (topModal() || isPopoverOpen() || isTextInput(e.target)) return;
    if (e.ctrlKey || e.metaKey || e.altKey) return;
    if (this.normalKey(e.key)) e.preventDefault();
  }

  normalKey(key) {
    if (key === 'g') {
      if (Date.now() - this.pendingG < 1000) {
        this.pendingG = 0;
        this.moveCursorTo(0);
      } else {
        this.pendingG = Date.now();
      }
      return true;
    }
    this.pendingG = 0;

    const tab = TABS.find((t) => t.keys.includes(key));
    if (tab) {
      this.go(tab.id);
      return true;
    }
    const at = TABS.findIndex((t) => t.id === this.state.view);
    switch (key) {
      case 'j': case 'ArrowDown': this.moveCursor(1); return true;
      case 'k': case 'ArrowUp': this.moveCursor(-1); return true;
      case 'G': this.moveCursorTo(-1); return true;
      case 'h': case 'ArrowLeft': this.go(TABS[(at + TABS.length - 1) % TABS.length].id); return true;
      case 'l': case 'ArrowRight': this.go(TABS[(at + 1) % TABS.length].id); return true;
      case '?': this.openHelp(); return true;
    }
    return this.view.key(key, this.cursorRow());
  }

  openHelp() {
    const row = (keys, label) => h('div', { class: 'help-row' }, h('span', {}, label),
      h('span', { class: 'help-keys' }, keys.map((k) => h('kbd', {}, k))));
    const section = (title, rows) => [h('h3', {}, title), ...rows.map(([k, l]) => row(k, l))];
    openModal({
      title: 'Keyboard shortcuts',
      className: 'help',
      body: h('div', { class: 'help-grid' },
        h('div', {},
          ...section('Everywhere', [
            [['j', 'k'], 'Move'], [['gg', 'G'], 'First / last'], [['h', 'l'], 'Previous / next tab'],
            [['1', '2', '3'], 'Plan / Recipes / Shopping'], [['Esc'], 'Close / leave a field'], [['?'], 'This help'],
          ])),
        h('div', {},
          ...section('Plan', [
            [['Enter'], 'Choose a recipe or note'], [['x'], 'Clear the slot'], [['c'], 'Copy the day to…'], [['v'], 'Validate new recipes'],
          ]),
          ...section('Recipes', [[['a'], 'New recipe'], [['Enter', 'e'], 'Edit'], [['/'], 'Search']]),
          ...section('Shopping', [
            [['Space'], 'Tick'], [['m'], 'Change aisle'], [['d'], 'Remove a hand-written item'],
            [['a'], 'Add an item'], [['A'], 'Edit aisles'], [['D'], 'Finish shopping'],
          ]))),
    });
  }

  // -------------------------------------------------------------------------
  // Authentication
  // -------------------------------------------------------------------------

  passwordForm({ title, intro, fields, submitLabel, dismissible, onSubmit }) {
    const error = h('div', { class: 'form-error', hidden: true });
    const inputs = fields.map((f) => h('input', {
      type: 'password', class: 'input', placeholder: f.placeholder, autocomplete: f.autocomplete, attrs: { 'aria-label': f.label },
    }));
    const submit = h('button', { type: 'submit', class: 'button primary wide' }, submitLabel);
    const form = h('form', { class: 'auth-form' },
      intro ? h('p', { class: 'dialog-message' }, intro) : null,
      error,
      fields.map((f, i) => h('label', { class: 'field' }, h('span', { class: 'field-label' }, f.label), inputs[i])),
      submit);
    let modal;
    form.addEventListener('submit', async (e) => {
      e.preventDefault();
      error.hidden = true;
      submit.disabled = true;
      try {
        const message = await onSubmit(inputs.map((i) => i.value));
        if (message) {
          error.textContent = message;
          error.hidden = false;
        } else {
          modal.close();
        }
      } catch (err) {
        error.textContent = err.message;
        error.hidden = false;
      } finally {
        submit.disabled = false;
      }
    });
    modal = openModal({ title, body: form, className: 'dialog auth', dismissible });
    return modal;
  }

  showSetup() {
    closeAllModals({ keepUndismissible: false });
    this.state.auth.authenticated = false;
    this.passwordForm({
      title: 'Welcome to Meals',
      intro: 'Choose a master password to protect this instance. You can change it later.',
      fields: [
        { label: 'Master password', placeholder: 'At least 4 characters', autocomplete: 'new-password' },
        { label: 'Confirm password', placeholder: 'Repeat the password', autocomplete: 'new-password' },
      ],
      submitLabel: 'Set password and continue',
      dismissible: false,
      onSubmit: async ([password, confirm]) => {
        if (password.length < 4) return 'The password must be at least 4 characters long.';
        if (password !== confirm) return 'The passwords do not match.';
        await api.auth.setup(password);
        this.afterLogin();
        return null;
      },
    });
  }

  showLogin() {
    if (topModal()?.el.classList.contains('auth')) return;
    closeAllModals({ keepUndismissible: false });
    this.state.auth.authenticated = false;
    this.passwordForm({
      title: 'Meals is locked',
      intro: 'Enter your master password to unlock.',
      fields: [{ label: 'Password', placeholder: 'Master password', autocomplete: 'current-password' }],
      submitLabel: 'Unlock',
      dismissible: false,
      onSubmit: async ([password]) => {
        if (!password) return 'Enter your password.';
        await api.auth.login(password);
        this.afterLogin();
        return null;
      },
    });
  }

  async afterLogin() {
    this.state.auth = { ...this.state.auth, has_password: true, authenticated: true };
    await this.refresh();
  }

  changePassword() {
    this.passwordForm({
      title: 'Change password',
      fields: [
        { label: 'Current password', placeholder: 'Current master password', autocomplete: 'current-password' },
        { label: 'New password', placeholder: 'At least 4 characters', autocomplete: 'new-password' },
        { label: 'Confirm new password', placeholder: 'Repeat the new password', autocomplete: 'new-password' },
      ],
      submitLabel: 'Update password',
      dismissible: true,
      onSubmit: async ([current, next, confirm]) => {
        if (next.length < 4) return 'The new password must be at least 4 characters long.';
        if (next !== confirm) return 'The new passwords do not match.';
        await api.auth.changePassword(current, next);
        toast('Password updated. Other devices were signed out.', { type: 'success' });
        return null;
      },
    });
  }

  async lock() {
    await this.run(() => api.auth.logout());
    this.showLogin();
  }

  showFatal(err) {
    const modal = openModal({
      title: 'Meals is unavailable',
      className: 'dialog',
      dismissible: false,
      body: h('p', { class: 'dialog-message' }, err.message),
      footer: h('button', { type: 'button', class: 'button primary', onclick: () => { modal.close(); this.boot(); } }, 'Retry'),
    });
  }
}

const app = new App();
const lockButton = $('auth-menu');
lockButton.append(icon('lock'));
lockButton.addEventListener('click', () => openMenu(lockButton, [
  { label: 'Change password', icon: 'lock', run: () => app.changePassword() },
  { label: 'Lock', icon: 'lock', run: () => app.lock() },
], { title: 'Security' }));
app.boot();
