// Meals: application shell. It owns the data, the three views (Plan, Recipes,
// Shopping), the cursor shared by their rows and the Vim-style keys.

import { api, ApiError, engine, onUnauthorized } from './api.js';
import { h, icon, clear, isTextInput } from './dom.js';
import { openModal, topModal, toast, confirmDialog } from '/kit/js/overlay.js';
import { initKeys } from '/kit/js/keys.js';
import { createUndo } from '/kit/js/undo.js';
import { tabBar } from '/kit/js/tabbar.js';
import { createLock } from '/kit/js/lock.js';
import { todayISO } from './dates.js';
import { planView } from './views/plan.js';
import { recipesView } from './views/recipes.js';
import { shoppingView } from './views/shopping.js';

const $ = (id) => document.getElementById(id);

const TABS = [
  { id: 'plan', label: 'Plan', icon: 'plan' },
  { id: 'recipes', label: 'Recipes', icon: 'recipes' },
  { id: 'shopping', label: 'Shopping', icon: 'shopping' },
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
    this.undos = createUndo({ toast });
    this.views = { plan: planView(this), recipes: recipesView(this), shopping: shoppingView(this) };
    this.lockScreen = createLock({
      appName: 'Meals',
      beforeLock: () => this.confirmLock(),
      afterLock: () => engine.reset(),
      onUnlocked: () => this.afterLogin(),
    });
  }

  // -------------------------------------------------------------------------
  // Boot and data
  // -------------------------------------------------------------------------

  async boot() {
    $('brand').append(icon('brand'), 'Meals');
    this.buildNav();
    onUnauthorized(() => this.showLogin());
    if (!this.keys) this.keys = initKeys({ table: () => this.keyTable(), helpTitle: 'Keyboard shortcuts' });
    window.addEventListener('hashchange', () => {
      const id = location.hash.slice(1);
      if (TABS.some((t) => t.id === id) && id !== this.state.view) this.go(id);
    });
    document.addEventListener('visibilitychange', () => document.visibilityState === 'visible' && this.tick());
    window.addEventListener('focus', () => this.tick());
    setInterval(() => this.tick(), 60_000);
    document.addEventListener('focusin', () => this.renderStatus());
    document.addEventListener('focusout', () => setTimeout(() => { this.renderStatus(); this.refreshIfStale(); }, 0));
    await this.startEngine();

    let status;
    try {
      status = await api.auth.status();
    } catch (err) {
      // No network: start from the data kept on this device, if any.
      if (!engine.ready) return this.showFatal(err);
      status = { no_auth: false, has_password: true, authenticated: true, offline: true };
    }
    this.state.auth = status;
    $('auth-menu').hidden = status.no_auth;
    if (!status.no_auth && !status.has_password) return this.showSetup();
    if (!status.authenticated) return this.showLogin();
    await this.afterLogin({ sync: !status.offline });
  }

  /** Loads the data kept on this device and starts the background sync (once). */
  async startEngine() {
    if (this.engineStarted) return;
    this.engineStarted = true;
    await engine.init();
    engine.onStatus = (s) => this.renderSync(s);
    engine.onRejected = (messages) => toast(`Not saved: ${messages[0]}${messages.length > 1 ? ` (+${messages.length - 1})` : ''}`, { type: 'error' });
    engine.onAuthRequired = () => this.showLogin();
    engine.onChange = () => this.onRemoteChange();
    engine.start();
    this.registerServiceWorker();
  }

  /** The local data changed under us (server answer, other tab). */
  async onRemoteChange() {
    if (!this.state.auth.authenticated) return;
    // Redrawing would wipe a field being typed in: wait until the user is done.
    if (topModal() || isTextInput(document.activeElement)) {
      this.stale = true;
      return;
    }
    this.stale = false;
    await this.refresh();
  }

  refreshIfStale() {
    if (this.stale && this.state.auth.authenticated && !topModal() && !isTextInput(document.activeElement)) this.onRemoteChange();
  }

  /** Status bar: offline state and changes waiting for the server. */
  renderSync({ status, pending }) {
    const el = $('status-sync');
    const text = {
      offline: pending ? `offline · ${pending} pending` : 'offline',
      auth: 'sign in to sync',
      error: `sync error · ${pending} pending`,
    }[status] ?? (pending ? `${pending} pending` : '');
    el.textContent = text;
    el.hidden = !text;
    el.className = `status-sync ${status}`;
  }

  registerServiceWorker() {
    if (!('serviceWorker' in navigator)) return;
    const hadController = Boolean(navigator.serviceWorker.controller);
    navigator.serviceWorker.addEventListener('controllerchange', () => {
      if (hadController) toast('Meals was updated', { action: { label: 'Reload', run: () => location.reload() } });
    });
    navigator.serviceWorker.register('/sw.js').catch(() => {});
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

  /**
   * Runs an action that u can undo: fn does it, undoFn puts things back.
   * The entry is pushed first (kit/undo.js), and dropped if fn fails.
   * Resolves to true when fn succeeded.
   */
  async undoable(label, fn, undoFn) {
    const entry = this.undos.push({
      label,
      undo: async () => {
        if ((await this.run(async () => { await undoFn(); return true; })) === undefined) return false;
        await this.refresh();
        return true;
      },
    });
    if ((await this.run(async () => { await fn(); return true; })) === undefined) {
      entry.cancel();
      return false;
    }
    await this.refresh();
    return true;
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
    for (const tab of TABS) {
      nav.append(h('button', { type: 'button', class: 'nav-item', dataset: { tab: tab.id }, onclick: () => this.go(tab.id) },
        icon(tab.icon), h('span', { class: 'nav-label' }, tab.label), h('span', { class: 'nav-count' })));
    }
    this.tabs = tabBar($('tabbar'), TABS, { onSelect: (id) => this.go(id) });
  }

  badges() {
    const { plan, shopping } = this.state;
    return { plan: plan?.pending || 0, recipes: 0, shopping: shopping.remaining };
  }

  renderNav() {
    const counts = this.badges();
    this.tabs.setActive(this.state.view);
    for (const tab of TABS) {
      const n = counts[tab.id];
      const alert = tab.id === 'plan' && n > 0;
      this.tabs.setBadge(tab.id, n, { alert });
      const item = $('nav').querySelector(`[data-tab="${tab.id}"]`);
      item.classList.toggle('active', tab.id === this.state.view);
      const count = item.querySelector('.nav-count');
      count.textContent = n ? String(n) : '';
      count.classList.toggle('alert', alert);
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

  /** The keys in force (kit/keys.js): moving and tabs, then the view's own. */
  keyTable() {
    return [
      { keys: ['j', 'ArrowDown'], label: 'Next row', group: 'Everywhere', run: () => this.moveCursor(1) },
      { keys: ['k', 'ArrowUp'], label: 'Previous row', group: 'Everywhere', run: () => this.moveCursor(-1) },
      { keys: ['gg'], label: 'First row', group: 'Everywhere', run: () => this.moveCursorTo(0) },
      { keys: ['G'], label: 'Last row', group: 'Everywhere', run: () => this.moveCursorTo(-1) },
      { digit: true, helpKeys: ['1', '2', '3'], label: 'Plan / Recipes / Shopping', group: 'Everywhere', run: (n) => TABS[n - 1] && this.go(TABS[n - 1].id) },
      { keys: ['u'], label: 'Undo', group: 'Everywhere', run: () => this.undos.run() },
      { keys: ['Escape'], label: 'Close / leave a field', group: 'Everywhere' },
      ...this.view.keys(this.cursorRow()).map((b) => ({ group: this.view.title, ...b })),
    ];
  }

  // -------------------------------------------------------------------------
  // Authentication
  // -------------------------------------------------------------------------

  showSetup() {
    this.state.auth.authenticated = false;
    this.lockScreen.showSetup();
  }

  showLogin() {
    this.state.auth.authenticated = false;
    this.lockScreen.showLogin();
  }

  async afterLogin({ sync = true } = {}) {
    this.state.auth = { ...this.state.auth, has_password: true, authenticated: true };
    if (sync) await engine.sync();
    if (!engine.ready) {
      this.showFatal(new ApiError('Cannot load your data. Check your connection.', 0));
      return;
    }
    await this.refresh();
  }

  /** Before locking: sends what is pending, or asks before losing it. */
  async confirmLock() {
    if (engine.pending) await engine.sync();
    if (!engine.pending) return true;
    return confirmDialog('Unsynced changes',
      `${engine.pending} change(s) have not reached the server yet and will be lost. Lock anyway?`, { okText: 'Lock' });
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
lockButton.addEventListener('click', () => app.lockScreen.menu(lockButton));
app.boot();
