// Recipes: a title, ingredients (names only) and free notes.

import { h, icon } from '../dom.js';
import { sinceLabel } from '../dates.js';
import { showRecipe, editRecipe } from '../recipe.js';

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
      el: h('div', { class: 'row', onclick: () => { app.select(r.id); showRecipe(app, r); } },
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

  return {
    title: 'Recipes',
    hints: [[['j', 'k'], 'move'], [['a'], 'new'], [['Enter'], 'open'], [['e'], 'edit'], [['/'], 'search']],

    actions() {
      return [h('button', { type: 'button', class: 'button primary', onclick: () => editRecipe(app, null) }, icon('plus'), 'New recipe')];
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

    keys(row) {
      const recipe = () => app.state.recipes.find((r) => r.id === row?.key);
      return [
        { keys: ['a'], label: 'New recipe', run: () => editRecipe(app, null) },
        { keys: ['/'], label: 'Search', run: () => searchInput?.focus() },
        { keys: ['Enter', 'o'], label: 'Open', run: () => recipe() && showRecipe(app, recipe()) },
        { keys: ['e'], label: 'Edit', run: () => recipe() && editRecipe(app, recipe()) },
      ];
    },
  };
}
