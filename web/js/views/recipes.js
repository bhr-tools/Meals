// Recipes: a title, ingredients (names only) and free notes.

import { h, icon, autoGrow } from '../dom.js';
import { api } from '../api.js';
import { openModal, confirmDialog, toast } from '../ui/overlay.js';
import { sinceLabel } from '../dates.js';

const fold = (s) => s.toLowerCase().normalize('NFD').replace(/[̀-ͯ]/g, '');

export function recipesView(app) {
  let query = '';
  let searchInput = null;
  let listEl = null;

  function matches() {
    const words = fold(query).split(/\s+/).filter(Boolean);
    return app.state.recipes.filter((r) => {
      const haystack = fold(`${r.title} ${r.ingredients.join(' ')}`);
      return words.every((w) => haystack.includes(w));
    });
  }

  function drawList() {
    const shown = matches();
    const rows = shown.map((r) => ({
      key: r.id,
      el: h('div', { class: 'row', onclick: () => { app.select(r.id); edit(r); } },
        h('div', { class: 'row-main' },
          h('div', { class: 'row-title' }, r.title),
          h('div', { class: 'row-sub' }, r.ingredients.length ? r.ingredients.join(', ') : 'No ingredients')),
        h('span', { class: 'row-hint' }, sinceLabel(r.last_planned, app.state.today))),
    }));
    listEl.replaceChildren(...(rows.length
      ? rows.map((r) => r.el)
      : [h('div', { class: 'empty-list' }, app.state.recipes.length ? 'No recipe matches.' : 'No recipes yet. Press a to add one.')]));
    return rows;
  }

  /** Create (recipe = null) or edit a recipe. */
  function edit(recipe) {
    const title = h('input', { type: 'text', class: 'input', maxLength: 200, placeholder: 'Recipe name', value: recipe?.title || '' });
    const ingredients = h('textarea', {
      class: 'input field-area', rows: 8, spellcheck: false,
      placeholder: 'One ingredient per line\nzucchini\ncream\nonion',
      value: recipe ? recipe.ingredients.join('\n') : '',
    });
    const notes = h('textarea', {
      class: 'input field-area', rows: 5,
      placeholder: 'Steps, a link, a cooking tip… never added to the shopping list.',
      value: recipe?.notes || '',
    });
    const grow = (t) => t.addEventListener('input', () => autoGrow(t, 150));

    let modal;
    async function save() {
      if (!title.value.trim()) { title.focus(); return; }
      const payload = { title: title.value, ingredients: ingredients.value.split('\n'), notes: notes.value };
      const saved = await app.run(() => (recipe ? api.recipes.update(recipe.id, payload) : api.recipes.create(payload)));
      if (!saved) return;
      modal.close();
      await app.refresh();
      app.select(saved.id);
      toast(recipe ? 'Recipe saved' : 'Recipe added', { type: 'success' });
    }
    async function remove() {
      const ok = await confirmDialog('Delete recipe', `“${recipe.title}” will also be removed from the plan.`, { okText: 'Delete' });
      if (!ok) return;
      if (await app.run(() => api.recipes.remove(recipe.id))) {
        modal.close();
        await app.refresh();
      }
    }

    const field = (label, control) => h('label', { class: 'field' }, h('span', { class: 'field-label' }, label), control);
    const submitOnCtrlEnter = (e) => {
      if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) { e.preventDefault(); save(); }
    };
    title.addEventListener('keydown', (e) => {
      if (e.key === 'Enter') { e.preventDefault(); ingredients.focus(); }
    });
    for (const t of [ingredients, notes]) t.addEventListener('keydown', submitOnCtrlEnter);
    grow(ingredients);
    grow(notes);

    modal = openModal({
      title: recipe ? 'Edit recipe' : 'New recipe',
      className: 'recipe',
      body: [
        field('Name', title),
        field('Ingredients', ingredients),
        h('details', { class: 'notes', open: !!recipe?.notes }, h('summary', {}, 'Notes'), notes),
      ],
      footer: [
        recipe ? h('button', { type: 'button', class: 'button danger push-left', onclick: remove }, 'Delete') : null,
        h('button', { type: 'button', class: 'button', onclick: () => modal.close() }, 'Cancel'),
        h('button', { type: 'button', class: 'button primary', onclick: save }, 'Save'),
      ],
    });
    requestAnimationFrame(() => { autoGrow(ingredients, 150); autoGrow(notes, 150); });
  }

  return {
    title: 'Recipes',
    hints: [[['j', 'k'], 'move'], [['a'], 'new'], [['Enter'], 'edit'], [['/'], 'search']],

    actions() {
      return [h('button', { type: 'button', class: 'button primary', onclick: () => edit(null) }, icon('plus'), 'New recipe')];
    },

    render() {
      searchInput = h('input', { type: 'search', class: 'input', placeholder: 'Search by name or ingredient', value: query, autocomplete: 'off' });
      listEl = h('div', { class: 'list' });
      searchInput.addEventListener('input', () => {
        query = searchInput.value;
        app.setRows(drawList());
      });
      searchInput.addEventListener('keydown', (e) => {
        if (e.key === 'Enter' || e.key === 'ArrowDown') {
          e.preventDefault();
          searchInput.blur();
        }
      });
      const rows = drawList();
      return { el: h('div', {}, h('div', { class: 'search-box' }, searchInput), listEl), rows };
    },

    key(k, row) {
      switch (k) {
        case 'a': edit(null); return true;
        case '/': searchInput?.focus(); return true;
        case 'Enter': case 'e': {
          const recipe = app.state.recipes.find((r) => r.id === row?.key);
          if (recipe) edit(recipe);
          return true;
        }
      }
      return false;
    },
  };
}
