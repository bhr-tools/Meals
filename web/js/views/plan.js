// Plan: the next seven days, two meals each.

import { h, icon } from '../dom.js';
import { api } from '../api.js';
import { openModal, openMenu, toast } from '/kit/js/overlay.js';
import { dayName, shortDate, sinceLabel } from '../dates.js';
import { showRecipe } from '../recipe.js';

const MEALS = [['lunch', 'Lunch'], ['dinner', 'Dinner']];

export function planView(app) {
  const slotAt = (key) => {
    const [date, meal] = key.split('/');
    return { date, meal, slot: app.state.plan?.days.find((d) => d.date === date)?.[meal] };
  };

  async function validate() {
    const { plan, today } = app.state;
    if (!plan?.pending) return toast('Nothing new to validate.');
    const res = await app.run(() => api.plan.validate(today));
    if (!res) return;
    await app.refresh();
    toast(`${res.validated} ${res.validated === 1 ? 'recipe' : 'recipes'} added to the shopping list`,
      { type: 'success', action: { label: 'View', run: () => app.go('shopping') } });
  }

  /** Puts a previous slot back (or empties it), exactly as it was. */
  function restore(date, meal, slot) {
    if (!slot) return api.plan.clear(date, meal);
    return slot.recipe_id
      ? api.plan.setRecipe(date, meal, slot.recipe_id, slot.status)
      : api.plan.setText(date, meal, slot.text);
  }

  /** Keeps a pending recipe out of the shopping list: it counts as already handled. */
  function skipList(key) {
    const { date, meal, slot } = slotAt(key);
    if (slot?.status !== 'pending' || !slot.recipe_id) return;
    app.undoable('Not added to the shopping list',
      () => api.plan.setRecipe(date, meal, slot.recipe_id, 'done'),
      () => restore(date, meal, slot));
  }

  function clearSlot(key) {
    const { date, meal, slot } = slotAt(key);
    if (!slot) return;
    app.undoable('Meal removed', () => api.plan.clear(date, meal), () => restore(date, meal, slot));
  }

  function copyDay(date, anchor) {
    const days = app.state.plan.days.filter((d) => d.date !== date);
    openMenu(anchor, days.map((d) => ({
      label: `${dayName(d.date, app.state.today)}`,
      hint: shortDate(d.date),
      run: async () => {
        if (await app.run(() => api.plan.copyDay(date, d.date))) {
          await app.refresh();
          toast(`Copied to ${dayName(d.date, app.state.today)}`);
        }
      },
    })), { title: `Copy ${dayName(date, app.state.today)} to…` });
  }

  /** A planned recipe opens to be read; an empty slot or a note opens the picker. */
  function open(key) {
    const { slot } = slotAt(key);
    const recipe = slot?.recipe_id && app.state.recipes.find((r) => r.id === slot.recipe_id);
    if (!recipe) return choose(key);
    showRecipe(app, recipe, {
      actions: [{ label: 'Change', run: (close) => { close(); choose(key); } }],
    });
  }

  /** Search dialog: pick a recipe, or turn what was typed into a note. */
  function choose(key) {
    const { date, meal } = slotAt(key);
    const words = (s) => s.toLowerCase().normalize('NFD').replace(/[̀-ͯ]/g, '').split(/\s+/).filter(Boolean);
    let items = [];
    let active = 0;

    const input = h('input', {
      type: 'text', class: 'picker-input', autocomplete: 'off', maxLength: 200,
      placeholder: 'Search a recipe or type a note',
    });
    const list = h('div', { class: 'picker-list' });

    const compute = () => {
      const q = words(input.value);
      const matches = app.state.recipes.filter((r) => {
        const title = words(r.title).join(' ');
        return q.every((w) => title.includes(w));
      });
      if (q.length) matches.sort((a, b) => Number(!words(a.title).join(' ').startsWith(q.join(' '))) - Number(!words(b.title).join(' ').startsWith(q.join(' '))));
      items = matches.map((recipe) => ({ recipe }));
      const text = input.value.trim();
      if (text) items.push({ text });
      active = Math.min(active, Math.max(0, items.length - 1));
    };

    const draw = () => {
      list.replaceChildren();
      if (!items.length) list.append(h('div', { class: 'picker-empty' }, 'No recipes yet. Type a note, or add recipes in the Recipes tab.'));
      items.forEach((item, i) => {
        const row = h('button', { type: 'button', class: ['picker-item', i === active && 'active'], onclick: () => pick(item) },
          h('span', { class: 'picker-label' }, item.recipe ? item.recipe.title : `Note: “${item.text}”`),
          item.recipe ? h('span', { class: 'picker-badge' }, sinceLabel(item.recipe.last_planned, app.state.today)) : null);
        list.append(row);
        if (i === active) requestAnimationFrame(() => row.scrollIntoView({ block: 'nearest' }));
      });
    };

    async function pick(item) {
      modal.close();
      const before = slotAt(key).slot;
      app.undoable(`Added ${item.recipe ? item.recipe.title : 'note'}`,
        () => (item.recipe ? api.plan.setRecipe(date, meal, item.recipe.id) : api.plan.setText(date, meal, item.text)),
        () => restore(date, meal, before));
    }

    input.addEventListener('input', () => { active = 0; compute(); draw(); });
    input.addEventListener('keydown', (e) => {
      const down = e.key === 'ArrowDown' || (e.ctrlKey && e.key === 'n');
      const up = e.key === 'ArrowUp' || (e.ctrlKey && e.key === 'p');
      if (down || up) {
        e.preventDefault();
        if (items.length) active = (active + (down ? 1 : -1) + items.length) % items.length;
        draw();
      } else if (e.key === 'Enter') {
        e.preventDefault();
        if (items[active]) pick(items[active]);
      }
    });

    const modal = openModal({
      className: 'picker',
      body: [h('div', { class: 'picker-search' },
        h('label', { class: 'picker-field' }, icon('search'), input),
        h('button', { type: 'button', class: 'picker-cancel', onclick: () => modal.close() }, 'Cancel')), list],
    });
    compute();
    draw();
  }

  return {
    title: 'Plan',
    hints: [[['j', 'k'], 'move'], [['Enter'], 'open'], [['e'], 'change'], [['x'], 'clear'], [['s'], 'no list'], [['c'], 'copy day'], [['v'], 'validate']],

    actions() {
      const pending = app.state.plan?.pending || 0;
      return pending
        ? [h('button', { type: 'button', class: 'button primary', onclick: validate }, `Validate ${pending} new`)]
        : [];
    },

    render() {
      const { plan, today } = app.state;
      const rows = [];
      const el = h('div', { class: 'days' });
      for (const day of plan?.days || []) {
        const head = h('div', { class: 'day-head' },
          h('span', { class: 'day-name' }, dayName(day.date, today)),
          h('span', { class: 'day-date' }, shortDate(day.date)));
        const copy = h('button', { type: 'button', class: 'icon-button', title: 'Copy this day to…', onclick: (e) => copyDay(day.date, e.currentTarget) }, icon('copy'));
        head.append(copy);
        const box = h('div', { class: ['day', day.date === today && 'today'] }, head);
        for (const [meal, label] of MEALS) {
          const slot = day[meal];
          const key = `${day.date}/${meal}`;
          const text = slot ? (slot.recipe_id ? slot.title : slot.text) : 'Nothing planned';
          const row = h('div', {
            class: ['row', 'slot', !slot && 'empty', slot && !slot.recipe_id && 'note'],
            dataset: { key },
            onclick: () => { app.select(key); open(key); },
          },
          h('span', { class: 'slot-meal' }, label),
          h('span', { class: 'slot-title' }, text),
          slot?.recipe_id && slot.status === 'pending' ? h('button', {
            type: 'button', class: 'slot-skip', title: 'Keep its ingredients out of the shopping list',
            onclick: (e) => { e.stopPropagation(); skipList(key); },
          }, 'No list') : null,
          slot ? h('button', {
            type: 'button', class: 'icon-button slot-remove', title: 'Remove this meal', attrs: { 'aria-label': 'Remove this meal' },
            onclick: (e) => { e.stopPropagation(); clearSlot(key); },
          }, icon('trash')) : null);
          rows.push({ key, el: row });
          box.append(row);
        }
        el.append(box);
      }
      return { el, rows };
    },

    keys(row) {
      const on = (fn) => () => row && fn(row);
      return [
        { keys: ['Enter', 'o'], label: 'Open the recipe', run: on((r) => open(r.key)) },
        { keys: ['e', 'a'], label: 'Choose a recipe or note', run: on((r) => choose(r.key)) },
        { keys: ['x', 'd'], label: 'Clear the slot', run: on((r) => clearSlot(r.key)) },
        { keys: ['s'], label: 'Keep out of the shopping list', run: on((r) => skipList(r.key)) },
        { keys: ['c'], label: 'Copy the day to…', run: on((r) => copyDay(slotAt(r.key).date, r.el)) },
        { keys: ['v'], label: 'Validate new recipes', run: validate },
      ];
    },
  };
}
