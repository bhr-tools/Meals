// Shopping: the ingredients of the validated recipes, by aisle. Each line
// says which recipes need it, and how many planned meals.

import { h, icon } from '../dom.js';
import { api, engine } from '../api.js';
import { openModal, openMenu, confirmDialog, promptDialog } from '/kit/js/overlay.js';

export function shoppingView(app) {
  let addInput = null;
  const items = () => app.state.shopping.groups.flatMap((g) => g.items.map((item) => ({ item, group: g })));
  const find = (key) => items().find((x) => x.item.key === key);

  /** Ticks an item at once, then tells the server; a failure reloads the truth. */
  async function toggle(key) {
    const found = find(key);
    if (!found) return;
    const { item, group } = found;
    item.checked = !item.checked;
    const s = app.state.shopping;
    s.remaining += item.checked ? -1 : 1;
    group.items.sort((a, b) => (a.checked - b.checked) || (a.key < b.key ? -1 : 1));
    app.render();
    app.select(key);
    if (!(await app.run(() => api.shopping.check(item.name, item.checked)))) await app.refresh();
  }

  async function addItem(name) {
    if (!name.trim()) return;
    if (await app.run(() => api.shopping.addExtra(name))) await app.refresh();
  }

  function removeExtra(key) {
    const item = find(key)?.item;
    if (!item?.extra_id) return;
    app.undoable(`Removed ${item.name}`, () => api.shopping.removeExtra(item.extra_id), async () => {
      await api.shopping.addExtra(item.name);
      if (item.checked) await api.shopping.check(item.name, true);
    });
  }

  async function changeAisle(key, anchor) {
    const found = find(key);
    if (!found) return;
    await app.run(() => app.loadAisles());
    const choices = app.state.aisles.map((a) => ({
      label: a.name,
      selected: a.id === found.group.aisle_id,
      run: async () => {
        if (await app.run(() => api.shopping.setAisle(found.item.name, a.id))) await app.refresh();
      },
    }));
    openMenu(anchor, [
      ...choices,
      'separator',
      { label: 'Automatic', hint: 'forget my choice', run: async () => {
        if (await app.run(() => api.shopping.setAisle(found.item.name, ''))) await app.refresh();
      } },
    ], { title: `Aisle for ${found.item.name}` });
  }

  async function finish() {
    const s = app.state.shopping;
    if (!s.total) return;
    const message = s.remaining
      ? `${s.remaining} ${s.remaining === 1 ? 'item is' : 'items are'} not ticked yet. This empties the list; your plan stays as it is.`
      : 'This empties the list; your plan stays as it is.';
    if (!(await confirmDialog('Finish shopping?', message, { okText: 'Finish', danger: false }))) return;
    // What Done clears, to put it back on u: the listed meals, the items
    // added by hand and the ticks.
    const { slots, extras } = engine.state;
    const listed = Object.values(slots).filter((x) => x.status === 'listed' && x.recipe_id);
    const added = Object.values(extras).map((x) => x.name);
    const ticked = items().filter((x) => x.item.checked).map((x) => x.item.name);
    app.undoable('Shopping finished', () => api.shopping.done(), async () => {
      for (const x of listed) await api.plan.setRecipe(x.date, x.meal, x.recipe_id, 'listed');
      for (const name of added) await api.shopping.addExtra(name);
      for (const name of ticked) await api.shopping.check(name, true);
    });
  }

  async function manageAisles() {
    await app.run(() => app.loadAisles());
    const list = h('div', { class: 'aisle-list' });
    const reload = async () => {
      await app.run(() => app.loadAisles());
      await app.refresh();
      draw();
    };
    const move = async (i, delta) => {
      const ids = app.state.aisles.map((a) => a.id);
      [ids[i], ids[i + delta]] = [ids[i + delta], ids[i]];
      if (await app.run(() => api.aisles.reorder(ids))) await reload();
    };
    function draw() {
      const aisles = app.state.aisles;
      list.replaceChildren(...aisles.map((a, i) => h('div', { class: 'aisle-row' },
        h('span', { class: 'aisle-name' }, a.name),
        h('button', { type: 'button', class: 'icon-button', title: 'Move up', disabled: i === 0, onclick: () => move(i, -1) }, icon('arrowUp')),
        h('button', { type: 'button', class: 'icon-button', title: 'Move down', disabled: i === aisles.length - 1, onclick: () => move(i, 1) }, icon('arrowDown')),
        h('button', { type: 'button', class: 'icon-button', title: 'Rename', onclick: async () => {
          const name = await promptDialog('Rename aisle', { value: a.name });
          if (name && await app.run(() => api.aisles.rename(a.id, name))) await reload();
        } }, icon('edit')),
        a.key === 'other' ? h('span', { class: 'icon-button' }) : h('button', { type: 'button', class: 'icon-button', title: 'Delete', onclick: async () => {
          if (!(await confirmDialog('Delete aisle', `Items in “${a.name}” go back to their automatic aisle.`, { okText: 'Delete' }))) return;
          if (await app.run(() => api.aisles.remove(a.id))) await reload();
        } }, icon('trash')))));
    }
    draw();
    const modal = openModal({
      title: 'Aisles',
      className: 'aisles',
      body: [h('p', { class: 'dialog-message' }, 'Order them like your store. Unknown ingredients go to Other; use “Change aisle” on an item to correct it once.'), list],
      footer: [
        h('button', { type: 'button', class: 'button', onclick: () => modal.close() }, 'Close'),
        h('button', { type: 'button', class: 'button primary', onclick: async () => {
          const name = await promptDialog('New aisle', { okText: 'Add' });
          if (name && await app.run(() => api.aisles.create(name))) await reload();
        } }, icon('plus'), 'Add aisle'),
      ],
    });
  }

  function row({ item }) {
    const refs = item.recipes.map((r) => h('span', { class: 'ref' }, r.title, ' ', h('b', {}, `×${r.count}`)));
    if (item.extra_id) refs.push(h('span', { class: 'ref hand' }, 'added by hand'));
    const more = h('button', { type: 'button', class: 'icon-button row-more', title: 'More', onclick: (e) => {
      e.stopPropagation();
      app.select(item.key);
      const anchor = e.currentTarget;
      openMenu(anchor, [
        { label: 'Change aisle', icon: 'list', run: () => changeAisle(item.key, anchor) },
        item.extra_id ? { label: 'Remove', icon: 'trash', danger: true, run: () => removeExtra(item.key) } : null,
      ].filter(Boolean), { title: item.name });
    } }, icon('more'));
    return {
      key: item.key,
      el: h('div', { class: ['row', 'item', item.checked && 'checked'], onclick: () => { app.select(item.key); toggle(item.key); } },
        h('span', { class: 'check' }, icon('check')),
        h('div', { class: 'row-main' },
          h('div', { class: 'row-title item-name' }, item.name),
          refs.length ? h('div', { class: 'refs' }, refs) : null),
        more),
    };
  }

  return {
    title: 'Shopping',
    subtitle: () => (app.state.shopping.total ? `${app.state.shopping.remaining} left` : ''),
    hints: [[['Space'], 'tick'], [['m'], 'aisle'], [['a'], 'add'], [['A'], 'aisles'], [['D'], 'finish']],

    actions() {
      return [
        h('button', { type: 'button', class: 'button', onclick: manageAisles }, icon('list'), 'Aisles'),
        app.state.shopping.total ? h('button', { type: 'button', class: 'button primary', onclick: finish }, 'Done') : null,
      ].filter(Boolean);
    },

    render() {
      const rows = [];
      addInput = h('input', { type: 'text', class: 'input', maxLength: 100, placeholder: 'Add an item…', autocomplete: 'off', enterKeyHint: 'done' });
      addInput.addEventListener('keydown', (e) => {
        if (e.key === 'Enter') {
          e.preventDefault();
          const name = addInput.value;
          addInput.value = '';
          addItem(name);
        }
      });
      const el = h('div', {}, h('div', { class: 'add-item' }, addInput));
      const { groups, total } = app.state.shopping;
      if (!total) {
        el.append(h('div', { class: 'empty-list' }, 'Nothing to buy. Validate planned recipes in Plan, or add an item above.'));
      }
      for (const g of groups) {
        const left = g.items.filter((i) => !i.checked).length;
        el.append(h('section', { class: 'group' },
          h('div', { class: 'group-header' }, h('span', { class: 'group-title' }, g.aisle), h('span', { class: 'group-count' }, `${left} / ${g.items.length}`)),
          g.items.map((item) => {
            const r = row({ item });
            rows.push(r);
            return r.el;
          })));
      }
      return { el, rows };
    },

    keys(row) {
      const on = (fn) => () => row && fn(row);
      return [
        { keys: [' ', 'Enter', 'x'], label: 'Tick / untick', run: on((r) => toggle(r.key)) },
        { keys: ['m'], label: 'Change aisle', run: on((r) => changeAisle(r.key, r.el)) },
        { keys: ['d'], label: 'Remove a hand-written item', run: on((r) => removeExtra(r.key)) },
        { keys: ['a'], label: 'Add an item', run: () => addInput?.focus() },
        { keys: ['A'], label: 'Aisles', run: manageAisles },
        { keys: ['D'], label: 'Finish shopping', run: finish },
      ];
    },
  };
}
